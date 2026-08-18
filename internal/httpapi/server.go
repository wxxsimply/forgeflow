package httpapi

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"forgeflow/internal/apperror"
	"forgeflow/internal/application"
	"forgeflow/internal/auth"
	"forgeflow/internal/checkpoint"
	"forgeflow/internal/controlplane"
	"forgeflow/internal/domain"
	"forgeflow/internal/governance"
	"forgeflow/internal/observability"
	repoharness "forgeflow/internal/repository"
)

const SessionCookie = "forgeflow_session"
const CSRFCookie = "forgeflow_csrf"

//go:embed openapi.yaml
var specFS embed.FS

type Options struct {
	Auth            *auth.Service
	Control         *controlplane.Store
	Runs            *application.Service
	Inspector       *repoharness.GitInspector
	CookieSecure    bool
	CookieDomain    string
	CookieMaxAge    time.Duration
	AllowedOrigins  []string
	RepositoryRoots []string
	MutationLimiter auth.Limiter
	MetricsEnabled  bool
	Governance      *governance.Store
	Catalog         *governance.Catalog
}

type Server struct {
	options         Options
	handler         http.Handler
	origins         map[string]bool
	repositoryRoots []string
}
type contextKey string

const principalKey contextKey = "principal"
const requestIDKey contextKey = "request-id"

func New(options Options) (*Server, error) {
	if options.Auth == nil || options.Control == nil || options.Runs == nil || options.Inspector == nil || options.Governance == nil || options.Catalog == nil {
		return nil, fmt.Errorf("HTTP API dependencies are required")
	}
	if options.CookieMaxAge <= 0 {
		options.CookieMaxAge = 24 * time.Hour
	}
	if options.MutationLimiter == nil {
		options.MutationLimiter = auth.NewMemoryLimiter(30, time.Minute)
	}
	s := &Server{options: options, origins: map[string]bool{}}
	for _, origin := range options.AllowedOrigins {
		s.origins[strings.TrimRight(origin, "/")] = true
	}
	for _, root := range options.RepositoryRoots {
		resolved, err := filepath.Abs(root)
		if err != nil {
			return nil, fmt.Errorf("resolve repository root: %w", err)
		}
		if evaluated, err := filepath.EvalSymlinks(resolved); err == nil {
			resolved = evaluated
		}
		s.repositoryRoots = append(s.repositoryRoots, filepath.Clean(resolved))
	}
	if len(s.repositoryRoots) == 0 {
		return nil, fmt.Errorf("at least one repository root is required")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	if options.MetricsEnabled {
		mux.Handle("GET /metrics", observability.DefaultMetrics().Handler())
	}
	mux.HandleFunc("GET /api/openapi.yaml", s.openAPI)
	mux.HandleFunc("POST /api/v1/auth/login", s.login)
	mux.Handle("POST /api/v1/auth/logout", s.protected(true, http.HandlerFunc(s.logout)))
	mux.Handle("GET /api/v1/auth/me", s.protected(false, http.HandlerFunc(s.me)))
	mux.Handle("GET /api/v1/auth/sessions", s.protected(false, http.HandlerFunc(s.sessions)))
	mux.Handle("DELETE /api/v1/auth/sessions/{sessionId}", s.protected(true, s.validatedID("sessionId", http.HandlerFunc(s.revokeSession))))
	mux.Handle("POST /api/v1/repositories", s.protected(true, http.HandlerFunc(s.createRepository)))
	mux.Handle("GET /api/v1/repositories", s.protected(false, http.HandlerFunc(s.listRepositories)))
	mux.Handle("GET /api/v1/repositories/{repositoryId}", s.protected(false, s.validatedID("repositoryId", http.HandlerFunc(s.getRepository))))
	mux.Handle("POST /api/v1/repositories/{repositoryId}/validate", s.protected(true, s.validatedID("repositoryId", http.HandlerFunc(s.validateRepository))))
	mux.Handle("DELETE /api/v1/repositories/{repositoryId}", s.protected(true, s.validatedID("repositoryId", http.HandlerFunc(s.deleteRepository))))
	mux.Handle("POST /api/v1/runs", s.protected(true, http.HandlerFunc(s.createRun)))
	mux.Handle("GET /api/v1/runs", s.protected(false, http.HandlerFunc(s.listRuns)))
	mux.Handle("GET /api/v1/runs/{runId}", s.protected(false, s.validatedID("runId", http.HandlerFunc(s.getRun))))
	mux.Handle("GET /api/v1/runs/{runId}/events", s.protected(false, s.validatedID("runId", http.HandlerFunc(s.runEvents))))
	mux.Handle("GET /api/v1/runs/{runId}/stream", s.protected(false, s.validatedID("runId", http.HandlerFunc(s.runStream))))
	mux.Handle("POST /api/v1/runs/{runId}/pause", s.protected(true, s.validatedID("runId", http.HandlerFunc(s.pauseRun))))
	mux.Handle("POST /api/v1/runs/{runId}/resume", s.protected(true, s.validatedID("runId", http.HandlerFunc(s.resumeRun))))
	mux.Handle("POST /api/v1/runs/{runId}/cancel", s.protected(true, s.validatedID("runId", http.HandlerFunc(s.cancelRun))))
	mux.Handle("GET /api/v1/runs/{runId}/artifacts", s.protected(false, s.validatedID("runId", http.HandlerFunc(s.runArtifacts))))
	mux.Handle("GET /api/v1/runs/{runId}/report", s.protected(false, s.validatedID("runId", http.HandlerFunc(s.runReport))))
	mux.Handle("GET /api/v1/approvals", s.protected(false, http.HandlerFunc(s.listApprovals)))
	mux.Handle("GET /api/v1/approvals/{approvalId}", s.protected(false, s.validatedID("approvalId", http.HandlerFunc(s.getApproval))))
	mux.Handle("POST /api/v1/approvals/{approvalId}/decision", s.protected(true, s.validatedID("approvalId", http.HandlerFunc(s.decideApproval))))
	mux.Handle("POST /api/v1/evals/runs", s.protected(true, http.HandlerFunc(s.createEvalRun)))
	mux.Handle("GET /api/v1/evals/runs", s.protected(false, http.HandlerFunc(s.listEvalRuns)))
	mux.Handle("GET /api/v1/evals/runs/{evalRunId}", s.protected(false, s.validatedID("evalRunId", http.HandlerFunc(s.getEvalRun))))
	mux.Handle("GET /api/v1/agents", s.protected(false, http.HandlerFunc(s.listAgents)))
	mux.Handle("GET /api/v1/prompts", s.protected(false, http.HandlerFunc(s.listPrompts)))
	mux.Handle("POST /api/v1/prompts/{agent}/{version}/promote", s.protected(true, http.HandlerFunc(s.promotePrompt)))
	mux.Handle("POST /api/v1/prompts/{agent}/rollback", s.protected(true, http.HandlerFunc(s.rollbackPrompt)))
	s.handler = s.requestContext(s.observeHTTP(mux))
	return s, nil
}

type responseObserver struct {
	http.ResponseWriter
	status int
}

func (w *responseObserver) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *responseObserver) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(body)
}
func (w *responseObserver) Flush()                      { _ = http.NewResponseController(w.ResponseWriter).Flush() }
func (w *responseObserver) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (s *Server) observeHTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		route := "unmatched"
		if matcher, ok := next.(interface {
			Handler(*http.Request) (http.Handler, string)
		}); ok {
			_, route = matcher.Handler(r)
		}
		parentContext := observability.ExtractHTTPContext(r.Context(), r.Header)
		ctx, span := observability.StartHTTPSpan(parentContext, r.Method, route, requestID(r))
		observer := &responseObserver{ResponseWriter: w}
		request := r.WithContext(ctx)
		next.ServeHTTP(observer, request)
		status := observer.status
		if status == 0 {
			status = http.StatusOK
		}
		observability.SetHTTPRoute(span, route)
		observability.DefaultMetrics().HTTP(r.Method, route, status, time.Since(started))
		if status == http.StatusTooManyRequests {
			observability.DefaultMetrics().RateLimited("api")
		}
		observability.EndSpan(span, nil, strconv.Itoa(status))
	})
}
func (s *Server) Handler() http.Handler { return s.handler }

func (s *Server) requestContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if !requestIDPattern.MatchString(id) {
			id = domain.NewID()
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, id)))
	})
}
func (s *Server) protected(csrf bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(SessionCookie)
		if err != nil {
			s.fail(w, r, apperror.New(apperror.CodeUnauthorized, "authentication required"))
			return
		}
		principal, err := s.options.Auth.Authenticate(r.Context(), cookie.Value)
		if err != nil {
			s.clearCookies(w)
			s.fail(w, r, err)
			return
		}
		if csrf {
			csrfCookie, err := r.Cookie(CSRFCookie)
			if err != nil || s.options.Auth.ValidateCSRF(principal, r.Header.Get("X-CSRF-Token"), cookieValue(csrfCookie)) != nil {
				s.fail(w, r, apperror.New(apperror.CodeForbidden, "CSRF validation failed"))
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey, principal)))
	})
}

func (s *Server) validatedID(name string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !uuidPattern.MatchString(r.PathValue(name)) {
			s.fail(w, r, validation(name+" is invalid"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !s.originAllowed(r) {
		s.fail(w, r, apperror.New(apperror.CodeForbidden, "request origin is not allowed"))
		return
	}
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Remember bool   `json:"remember"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	old := ""
	if c, _ := r.Cookie(SessionCookie); c != nil {
		old = c.Value
	}
	result, retry, err := s.options.Auth.Login(r.Context(), in.Email, in.Password, sourceIP(r), r.UserAgent(), old)
	if err != nil {
		outcome := "failure"
		if retry > 0 {
			outcome = "rate_limited"
		}
		observability.DefaultMetrics().Auth(outcome)
		if retry > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(max(1, int(retry.Seconds()))))
		}
		s.fail(w, r, err)
		return
	}
	observability.DefaultMetrics().Auth("success")
	maxAge := int(s.options.CookieMaxAge.Seconds())
	if !in.Remember {
		maxAge = 0
	}
	s.setCookies(w, result.Token, result.CSRFToken, maxAge)
	s.audit(r, result.Principal.User.ID, "auth.login", "session", result.Principal.Session.ID, nil)
	writeJSON(w, http.StatusOK, map[string]any{"user": result.Principal.User, "session": result.Principal.Session, "csrfToken": result.CSRFToken})
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	c, _ := r.Cookie(SessionCookie)
	if c != nil {
		_ = s.options.Auth.Logout(r.Context(), c.Value)
	}
	p := principal(r)
	s.audit(r, p.User.ID, "auth.logout", "session", p.Session.ID, nil)
	s.clearCookies(w)
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, principal(r).User)
}
func (s *Server) sessions(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	items, err := s.options.Auth.Sessions(r.Context(), p.User.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "currentSessionId": p.Session.ID})
}
func (s *Server) revokeSession(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	id := r.PathValue("sessionId")
	if err := s.options.Auth.RevokeSession(r.Context(), p.User.ID, id); err != nil {
		s.fail(w, r, notFound(err))
		return
	}
	s.audit(r, p.User.ID, "session.revoke", "session", id, nil)
	if id == p.Session.ID {
		s.clearCookies(w)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) createRepository(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if !p.User.Role.CanWriteRuns() {
		s.fail(w, r, forbidden())
		return
	}
	var in struct {
		Name          string `json:"name"`
		LocalPath     string `json:"localPath"`
		DefaultBranch string `json:"defaultBranch"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 128 {
		s.fail(w, r, validation("repository name is required"))
		return
	}
	absolute, err := filepath.Abs(in.LocalPath)
	if err != nil || strings.TrimSpace(in.LocalPath) == "" {
		s.fail(w, r, validation("repository path is invalid"))
		return
	}
	if evaluated, evalErr := filepath.EvalSymlinks(absolute); evalErr == nil {
		absolute = evaluated
	}
	if !s.repositoryPathAllowed(absolute) {
		s.fail(w, r, forbidden())
		return
	}
	if in.DefaultBranch == "" {
		in.DefaultBranch = "HEAD"
	}
	now := time.Now().UTC()
	v := controlplane.Repository{ID: domain.NewID(), OwnerID: p.User.ID, Name: in.Name, LocalPath: absolute, DefaultBranch: in.DefaultBranch, CreatedAt: now, UpdatedAt: now}
	if err := s.options.Control.CreateRepository(r.Context(), v); err != nil {
		s.fail(w, r, apperror.Wrap(err, apperror.CodeConflict, "repository.create", "repository could not be created"))
		return
	}
	s.audit(r, p.User.ID, "repository.create", "repository", v.ID, nil)
	writeJSON(w, http.StatusCreated, v)
}
func (s *Server) listRepositories(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	page, err := s.options.Control.ListRepositories(r.Context(), p.User.ID, isAdmin(p), r.URL.Query().Get("cursor"), parseLimit(r))
	if err != nil {
		s.fail(w, r, validation(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, page)
}
func (s *Server) getRepository(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	v, err := s.options.Control.GetRepository(r.Context(), r.PathValue("repositoryId"), p.User.ID, isAdmin(p))
	if err != nil {
		s.fail(w, r, notFound(err))
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (s *Server) validateRepository(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if !p.User.Role.CanWriteRuns() {
		s.fail(w, r, forbidden())
		return
	}
	v, err := s.options.Control.GetRepository(r.Context(), r.PathValue("repositoryId"), p.User.ID, isAdmin(p))
	if err != nil {
		s.fail(w, r, notFound(err))
		return
	}
	summary, err := s.options.Inspector.Inspect(r.Context(), domain.RepositoryRef{Path: v.LocalPath, BaseRevision: v.DefaultBranch})
	if err != nil {
		s.fail(w, r, apperror.Wrap(err, apperror.CodeValidation, "repository.validate", "repository validation failed"))
		return
	}
	writeJSON(w, http.StatusOK, summary)
}
func (s *Server) deleteRepository(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if !p.User.Role.CanWriteRuns() {
		s.fail(w, r, forbidden())
		return
	}
	if err := s.options.Control.DeleteRepository(r.Context(), r.PathValue("repositoryId"), p.User.ID, isAdmin(p)); err != nil {
		s.fail(w, r, notFound(err))
		return
	}
	s.audit(r, p.User.ID, "repository.delete", "repository", r.PathValue("repositoryId"), nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) createRun(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if !p.User.Role.CanWriteRuns() {
		s.fail(w, r, forbidden())
		return
	}
	if !s.allowMutation(w, r, "run:"+p.User.ID) {
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		s.fail(w, r, validation("request body is too large"))
		return
	}
	var in struct {
		RepositoryID  string `json:"repositoryId"`
		Task          string `json:"task"`
		BaseRevision  string `json:"baseRevision"`
		MaxIterations int    `json:"maxIterations"`
	}
	if err := decodeBytes(body, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if strings.TrimSpace(in.Task) == "" || len(in.Task) > 20000 {
		s.fail(w, r, validation("task is required and must not exceed 20000 bytes"))
		return
	}
	if in.MaxIterations < 0 || in.MaxIterations > 10 {
		s.fail(w, r, validation("maxIterations must be between 1 and 10 when provided"))
		return
	}
	if !uuidPattern.MatchString(in.RepositoryID) {
		s.fail(w, r, validation("repositoryId is invalid"))
		return
	}
	repo, err := s.options.Control.GetRepository(r.Context(), in.RepositoryID, p.User.ID, isAdmin(p))
	if err != nil {
		s.fail(w, r, notFound(err))
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key != "" {
		if len(key) > 128 {
			s.fail(w, r, validation("Idempotency-Key is too long"))
			return
		}
		existing, err := s.options.Control.ClaimIdempotency(r.Context(), p.User.ID, key, body)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		if existing.Found {
			if !existing.Match {
				s.fail(w, r, apperror.New(apperror.CodeConflict, "Idempotency-Key was already used with a different request"))
				return
			}
			if existing.Pending {
				s.fail(w, r, apperror.New(apperror.CodeConflict, "an identical request is already being created"))
				return
			}
			state, err := s.options.Control.LoadRun(r.Context(), existing.RunID, p.User.ID, isAdmin(p))
			if err != nil {
				s.fail(w, r, notFound(err))
				return
			}
			w.Header().Set("Location", "/api/v1/runs/"+state.RunID)
			writeJSON(w, http.StatusAccepted, state)
			return
		}
	}
	base := in.BaseRevision
	if base == "" {
		base = repo.DefaultBranch
	}
	state, err := s.options.Runs.CreateQueued(r.Context(), application.CreateInput{OwnerID: p.User.ID, RepositoryID: repo.ID, Task: in.Task, RepositoryPath: repo.LocalPath, BaseRevision: base, MaxIterations: in.MaxIterations})
	if err != nil {
		if key != "" {
			s.options.Control.ReleaseIdempotency(r.Context(), p.User.ID, key, body)
		}
		s.fail(w, r, err)
		return
	}
	if key != "" {
		if err := s.options.Control.SaveIdempotency(r.Context(), p.User.ID, key, body, state.RunID); err != nil {
			s.fail(w, r, apperror.Wrap(err, apperror.CodeConflict, "run.idempotency", "could not finalize idempotent request"))
			return
		}
	}
	s.audit(r, p.User.ID, "run.create", "run", state.RunID, nil)
	w.Header().Set("Location", "/api/v1/runs/"+state.RunID)
	w.Header().Set("ETag", etag(state.Version))
	writeJSON(w, http.StatusAccepted, state)
}
func (s *Server) listRuns(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	page, err := s.options.Control.ListRuns(r.Context(), p.User.ID, isAdmin(p), r.URL.Query().Get("cursor"), parseLimit(r))
	if err != nil {
		s.fail(w, r, validation(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, page)
}
func (s *Server) getRun(w http.ResponseWriter, r *http.Request) {
	state, ok := s.authorizedRun(w, r)
	if !ok {
		return
	}
	w.Header().Set("ETag", etag(state.Version))
	writeJSON(w, http.StatusOK, state)
}
func (s *Server) runEvents(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	after, err := parseAfter(r)
	if err != nil {
		s.fail(w, r, validation("event cursor is invalid"))
		return
	}
	items, err := s.options.Control.ListEvents(r.Context(), r.PathValue("runId"), p.User.ID, isAdmin(p), after, parseLimit(r))
	if err != nil {
		s.fail(w, r, notFound(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "nextCursor": lastSequence(items, after)})
}
func (s *Server) runStream(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	after, err := parseAfter(r)
	if err != nil {
		s.fail(w, r, validation("event cursor is invalid"))
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.fail(w, r, apperror.New(apperror.CodeInternal, "streaming is unavailable"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		items, err := s.options.Control.ListEvents(r.Context(), r.PathValue("runId"), p.User.ID, isAdmin(p), after, 200)
		if err != nil {
			if after == 0 {
				s.fail(w, r, notFound(err))
			}
			return
		}
		for _, item := range items {
			data, _ := json.Marshal(item.Event)
			fmt.Fprintf(w, "id: %d\ndata: %s\n\n", item.Sequence, data)
			after = item.Sequence
		}
		if len(items) > 0 {
			flusher.Flush()
		}
		if r.URL.Query().Get("once") == "1" {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}
func (s *Server) pauseRun(w http.ResponseWriter, r *http.Request)  { s.mutateRun(w, r, "pause") }
func (s *Server) resumeRun(w http.ResponseWriter, r *http.Request) { s.mutateRun(w, r, "resume") }
func (s *Server) cancelRun(w http.ResponseWriter, r *http.Request) { s.mutateRun(w, r, "cancel") }
func (s *Server) mutateRun(w http.ResponseWriter, r *http.Request, action string) {
	p := principal(r)
	if !p.User.Role.CanWriteRuns() {
		s.fail(w, r, forbidden())
		return
	}
	if _, ok := s.authorizedRun(w, r); !ok {
		return
	}
	var in struct {
		Reason string `json:"reason"`
	}
	if action != "resume" {
		if err := decode(r, &in); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	var state *domain.RunState
	var err error
	switch action {
	case "pause":
		state, err = s.options.Runs.PauseQueued(r.Context(), r.PathValue("runId"), p.User.ID, in.Reason)
	case "resume":
		state, err = s.options.Runs.ResumeQueued(r.Context(), r.PathValue("runId"))
	case "cancel":
		state, err = s.options.Runs.CancelQueued(r.Context(), r.PathValue("runId"), p.User.ID, in.Reason)
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.audit(r, p.User.ID, "run."+action, "run", state.RunID, nil)
	w.Header().Set("ETag", etag(state.Version))
	writeJSON(w, http.StatusOK, state)
}
func (s *Server) runArtifacts(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	items, err := s.options.Control.ListArtifacts(r.Context(), r.PathValue("runId"), p.User.ID, isAdmin(p))
	if err != nil {
		s.fail(w, r, notFound(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (s *Server) runReport(w http.ResponseWriter, r *http.Request) {
	state, ok := s.authorizedRun(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runId": state.RunID, "status": state.Status, "plan": state.Plan, "implementation": state.Implementation, "tests": state.TestAssessment, "review": state.ReviewResult, "security": state.SecurityResult, "decision": state.JudgeDecision, "error": state.Error})
}

func (s *Server) listApprovals(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	status := r.URL.Query().Get("status")
	if status != "" && status != "pending" && status != "approved" && status != "rejected" {
		s.fail(w, r, validation("approval status is invalid"))
		return
	}
	items, err := s.options.Control.ListApprovals(r.Context(), p.User.ID, isAdmin(p), status)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (s *Server) getApproval(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	v, err := s.options.Control.GetApproval(r.Context(), r.PathValue("approvalId"), p.User.ID, isAdmin(p))
	if err != nil {
		s.fail(w, r, notFound(err))
		return
	}
	w.Header().Set("ETag", etag(v.RunVersion))
	writeJSON(w, http.StatusOK, v)
}
func (s *Server) decideApproval(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if !p.User.Role.CanApprove() {
		s.fail(w, r, forbidden())
		return
	}
	if !s.allowMutation(w, r, "approval:"+p.User.ID) {
		return
	}
	v, err := s.options.Control.GetApproval(r.Context(), r.PathValue("approvalId"), p.User.ID, isAdmin(p))
	if err != nil {
		s.fail(w, r, notFound(err))
		return
	}
	expected, err := parseETag(r.Header.Get("If-Match"))
	if err != nil {
		s.fail(w, r, apperror.New(apperror.CodeConflict, "a current If-Match run version is required"))
		return
	}
	if expected != v.RunVersion {
		s.fail(w, r, apperror.New(apperror.CodeConflict, "approval changed; reload it before deciding"))
		return
	}
	var in struct {
		Decision string `json:"decision"`
		Comment  string `json:"comment"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if in.Decision != "approve" && in.Decision != "reject" {
		s.fail(w, r, validation("decision must be approve or reject"))
		return
	}
	state, err := s.options.Runs.ResolveApprovalQueued(r.Context(), v.Request.RunID, in.Decision == "approve", in.Comment, expected)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	decisionMetric := "rejected"
	if in.Decision == "approve" {
		decisionMetric = "approved"
	}
	observability.DefaultMetrics().Approval(decisionMetric, time.Since(v.Request.RequestedAt))
	s.audit(r, p.User.ID, "approval."+in.Decision, "approval", v.Request.ApprovalID, map[string]any{"runId": state.RunID, "version": expected})
	w.Header().Set("ETag", etag(state.Version))
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) authorizedRun(w http.ResponseWriter, r *http.Request) (*domain.RunState, bool) {
	p := principal(r)
	state, err := s.options.Control.LoadRun(r.Context(), r.PathValue("runId"), p.User.ID, isAdmin(p))
	if err != nil {
		s.fail(w, r, notFound(err))
		return nil, false
	}
	return state, true
}
func (s *Server) openAPI(w http.ResponseWriter, r *http.Request) {
	data, _ := specFS.ReadFile("openapi.yaml")
	w.Header().Set("Content-Type", "application/yaml")
	w.Write(data)
}
func (s *Server) allowMutation(w http.ResponseWriter, r *http.Request, key string) bool {
	result := s.options.MutationLimiter.Allow(key, time.Now().UTC())
	if !result.Allowed {
		w.Header().Set("Retry-After", strconv.Itoa(max(1, int(result.RetryAfter.Seconds()))))
		s.fail(w, r, apperror.New(apperror.CodeRateLimited, "too many requests; try again later"))
		return false
	}
	return true
}
func (s *Server) originAllowed(r *http.Request) bool {
	origin := strings.TrimRight(r.Header.Get("Origin"), "/")
	return origin == "" || len(s.origins) == 0 || s.origins[origin]
}
func (s *Server) repositoryPathAllowed(candidate string) bool {
	for _, root := range s.repositoryRoots {
		relative, err := filepath.Rel(root, candidate)
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative) {
			return true
		}
	}
	return false
}
func (s *Server) setCookies(w http.ResponseWriter, token, csrf string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Value: token, Path: "/", Domain: s.options.CookieDomain, MaxAge: maxAge, HttpOnly: true, Secure: s.options.CookieSecure, SameSite: http.SameSiteLaxMode})
	http.SetCookie(w, &http.Cookie{Name: CSRFCookie, Value: csrf, Path: "/", Domain: s.options.CookieDomain, MaxAge: maxAge, HttpOnly: false, Secure: s.options.CookieSecure, SameSite: http.SameSiteLaxMode})
}
func (s *Server) clearCookies(w http.ResponseWriter) { s.setCookies(w, "", "", -1) }
func (s *Server) audit(r *http.Request, actor, action, kind, id string, details map[string]any) {
	if err := s.options.Control.Audit(r.Context(), controlplane.AuditEntry{ActorID: actor, Action: action, ResourceType: kind, ResourceID: id, RequestID: requestID(r), SourceIP: sourceIP(r), Details: details}); err != nil {
		slog.Error("write audit log", "error", err, "request_id", requestID(r))
	}
}
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	code := apperror.CodeOf(err)
	status := http.StatusInternalServerError
	switch code {
	case apperror.CodeValidation:
		status = http.StatusBadRequest
	case apperror.CodeUnauthorized:
		status = http.StatusUnauthorized
	case apperror.CodeForbidden, apperror.CodePolicyDenied:
		status = http.StatusForbidden
	case apperror.CodeNotFound:
		status = http.StatusNotFound
	case apperror.CodeConflict:
		status = http.StatusConflict
	case apperror.CodeRateLimited:
		status = http.StatusTooManyRequests
	case apperror.CodeNotImplemented:
		status = http.StatusNotImplemented
	case apperror.CodeTransient:
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, map[string]any{"code": code, "message": apperror.MessageOf(err), "requestId": requestID(r), "details": map[string]any{}})
}
func decode(r *http.Request, target any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
	if err != nil || len(body) > 1<<20 {
		return validation("request body is too large")
	}
	return decodeBytes(body, target)
}
func decodeBytes(body []byte, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return validation("request body is invalid")
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return validation("request body must contain one JSON value")
	}
	return nil
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func principal(r *http.Request) auth.Principal {
	return r.Context().Value(principalKey).(auth.Principal)
}
func isAdmin(p auth.Principal) bool    { return p.User.Role == auth.RoleAdmin }
func requestID(r *http.Request) string { v, _ := r.Context().Value(requestIDKey).(string); return v }
func sourceIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}
func cookieValue(c *http.Cookie) string {
	if c == nil {
		return ""
	}
	return c.Value
}
func parseLimit(r *http.Request) int { v, _ := strconv.Atoi(r.URL.Query().Get("limit")); return v }
func parseAfter(r *http.Request) (int64, error) {
	v := r.URL.Query().Get("after")
	if v == "" {
		v = r.Header.Get("Last-Event-ID")
	}
	if v == "" {
		return 0, nil
	}
	return strconv.ParseInt(v, 10, 64)
}
func lastSequence(items []controlplane.SequencedEvent, fallback int64) int64 {
	if len(items) == 0 {
		return fallback
	}
	return items[len(items)-1].Sequence
}
func etag(version int64) string { return fmt.Sprintf("\"%d\"", version) }
func parseETag(value string) (int64, error) {
	return strconv.ParseInt(strings.Trim(strings.TrimSpace(value), "\""), 10, 64)
}
func validation(message string) error { return apperror.New(apperror.CodeValidation, message) }
func forbidden() error                { return apperror.New(apperror.CodeForbidden, "permission denied") }
func notFound(err error) error {
	if errors.Is(err, checkpoint.ErrNotFound) || errors.Is(err, auth.ErrNotFound) {
		return apperror.New(apperror.CodeNotFound, "resource not found")
	}
	return err
}

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)
var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
