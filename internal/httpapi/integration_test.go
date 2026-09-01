package httpapi_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"forgeflow/internal/application"
	"forgeflow/internal/auth"
	"forgeflow/internal/checkpoint"
	"forgeflow/internal/config"
	"forgeflow/internal/controlplane"
	"forgeflow/internal/domain"
	fulleval "forgeflow/internal/eval"
	"forgeflow/internal/governance"
	"forgeflow/internal/httpapi"
	"forgeflow/internal/planner"
	pg "forgeflow/internal/postgres"
	"forgeflow/internal/repository"
	"forgeflow/migrations"
)

type apiFixture struct {
	server    *httptest.Server
	db        *sql.DB
	auth      *auth.Service
	authStore *auth.PostgresStore
	runs      *application.Service
}

func TestAuthenticationCSRFHorizontalAuthorizationAndApprovalVersion(t *testing.T) {
	f := newFixture(t, auth.NewMemoryLimiter(20, time.Minute))
	admin := f.login(t, "admin@example.com", "correct horse battery staple", "attacker-fixed-token")
	if admin.session == "attacker-fixed-token" || admin.csrf == "" {
		t.Fatal("login did not rotate session and CSRF secrets")
	}
	var stored []byte
	if err := f.db.QueryRow(`SELECT token_hash FROM sessions WHERE id=$1`, admin.sessionID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(stored, []byte(admin.session)) {
		t.Fatal("database contains the raw session token")
	}

	response := f.request(t, admin, http.MethodPost, "/api/v1/repositories", `{"name":"forgeflow","localPath":"."}`, false, nil)
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("missing CSRF status=%d body=%s", response.StatusCode, read(response))
	}
	response = f.request(t, admin, http.MethodPost, "/api/v1/repositories", `{"name":"forgeflow","localPath":"."}`, true, nil)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("repository status=%d body=%s", response.StatusCode, read(response))
	}
	var repo controlplane.Repository
	decodeResponse(t, response, &repo)

	runBody := `{"repositoryId":"` + repo.ID + `","task":"test secure API"}`
	response = f.request(t, admin, http.MethodPost, "/api/v1/runs", `{"repositoryId":"`+repo.ID+`","task":"invalid budget","maxIterations":11}`, true, map[string]string{"Idempotency-Key": "run-invalid-budget"})
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid maxIterations status=%d body=%s", response.StatusCode, read(response))
	}
	response = f.request(t, admin, http.MethodPost, "/api/v1/runs", runBody, true, map[string]string{"Idempotency-Key": "run-1"})
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("create run status=%d body=%s", response.StatusCode, read(response))
	}
	var queued domain.RunState
	decodeResponse(t, response, &queued)
	response = f.request(t, admin, http.MethodPost, "/api/v1/runs", runBody, true, map[string]string{"Idempotency-Key": "run-1"})
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("idempotent replay status=%d", response.StatusCode)
	}
	var replay domain.RunState
	decodeResponse(t, response, &replay)
	if replay.RunID != queued.RunID {
		t.Fatal("idempotent replay created another run")
	}
	concurrentBody := `{"repositoryId":"` + repo.ID + `","task":"concurrent idempotency"}`
	statuses := make([]int, 2)
	var wait sync.WaitGroup
	for index := range statuses {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result := f.request(t, admin, http.MethodPost, "/api/v1/runs", concurrentBody, true, map[string]string{"Idempotency-Key": "run-concurrent"})
			statuses[index] = result.StatusCode
			_ = read(result)
		}()
	}
	wait.Wait()
	for _, status := range statuses {
		if status != http.StatusAccepted && status != http.StatusConflict {
			t.Fatalf("concurrent idempotency statuses=%v", statuses)
		}
	}
	var duplicateCount int
	if err := f.db.QueryRow(`SELECT count(*) FROM runs WHERE task='concurrent idempotency'`).Scan(&duplicateCount); err != nil || duplicateCount != 1 {
		t.Fatalf("concurrent runs=%d err=%v", duplicateCount, err)
	}

	viewerHash, _ := auth.HashPassword("viewer secure password", testPasswordParams())
	viewerID := domain.NewID()
	if err := f.authStore.CreateUser(context.Background(), auth.UserCredential{User: auth.User{ID: viewerID, Email: "viewer@example.com", Role: auth.RoleViewer, Status: "active", CreatedAt: time.Now().UTC()}, PasswordHash: viewerHash}); err != nil {
		t.Fatal(err)
	}
	viewer := f.login(t, "viewer@example.com", "viewer secure password", "")
	response = f.request(t, viewer, http.MethodGet, "/api/v1/evals/runs", "", false, nil)
	if response.StatusCode != http.StatusOK || !strings.Contains(read(response), `"items":[]`) {
		t.Fatal("authenticated viewer did not receive the honest empty eval history")
	}
	response = f.request(t, viewer, http.MethodGet, "/api/v1/agents", "", false, nil)
	if response.StatusCode != http.StatusOK || !strings.Contains(read(response), `"planner"`) {
		t.Fatal("authenticated viewer could not read the agent catalog")
	}
	response = f.request(t, viewer, http.MethodPost, "/api/v1/evals/runs", `{}`, true, nil)
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer eval import status=%d body=%s", response.StatusCode, read(response))
	}
	response = f.request(t, admin, http.MethodPost, "/api/v1/evals/runs", `{}`, true, nil)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid admin eval import status=%d body=%s", response.StatusCode, read(response))
	}
	response = f.request(t, viewer, http.MethodGet, "/api/v1/runs/"+queued.RunID, "", false, nil)
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("horizontal read status=%d body=%s", response.StatusCode, read(response))
	}
	response = f.request(t, viewer, http.MethodPost, "/api/v1/runs/"+queued.RunID+"/cancel", `{"reason":"no"}`, true, nil)
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer mutation status=%d", response.StatusCode)
	}

	waiting, err := f.runs.Create(context.Background(), application.CreateInput{OwnerID: admin.userID, RepositoryID: repo.ID, Task: "approval version", RepositoryPath: "."})
	if err != nil {
		t.Fatal(err)
	}
	response = f.request(t, admin, http.MethodGet, "/api/v1/approvals/"+waiting.PendingApproval.ApprovalID, "", false, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("get approval status=%d body=%s", response.StatusCode, read(response))
	}
	currentETag := response.Header.Get("ETag")
	_ = read(response)
	response = f.request(t, admin, http.MethodPost, "/api/v1/approvals/"+waiting.PendingApproval.ApprovalID+"/decision", `{"decision":"approve"}`, true, map[string]string{"If-Match": "\"1\""})
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("stale approval status=%d body=%s", response.StatusCode, read(response))
	}
	response = f.request(t, admin, http.MethodPost, "/api/v1/approvals/"+waiting.PendingApproval.ApprovalID+"/decision", `{"decision":"approve"}`, true, map[string]string{"If-Match": currentETag})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("current approval status=%d body=%s", response.StatusCode, read(response))
	}
	_ = read(response)

	response = f.request(t, admin, http.MethodGet, "/api/v1/runs/"+waiting.RunID+"/stream?once=1", "", false, nil)
	if response.StatusCode != http.StatusOK || !strings.Contains(read(response), "id: 1") {
		t.Fatalf("SSE did not expose sequenced events")
	}
}

func TestLoginRateLimitAndUniformFailure(t *testing.T) {
	f := newFixture(t, auth.NewMemoryLimiter(1, time.Minute))
	first := f.rawLogin(t, "missing@example.com", "not-the-password", "")
	if first.StatusCode != http.StatusUnauthorized {
		t.Fatalf("first=%d", first.StatusCode)
	}
	var firstError map[string]any
	decodeResponse(t, first, &firstError)
	second := f.rawLogin(t, "missing@example.com", "not-the-password", "")
	if second.StatusCode != http.StatusTooManyRequests || second.Header.Get("Retry-After") == "" {
		t.Fatalf("second=%d retry=%q", second.StatusCode, second.Header.Get("Retry-After"))
	}
}

func TestPromptPromotionRollbackIsImmutableAuditedAndDoesNotRewriteRuns(t *testing.T) {
	f := newFixture(t, auth.NewMemoryLimiter(20, time.Minute))
	admin := f.login(t, "admin@example.com", "correct horse battery staple", "")
	ctx := context.Background()
	governanceStore := governance.NewStore(f.db)
	cost, latency := 0.01, 1000.0
	createEval := func() string {
		t.Helper()
		id := domain.NewID()
		report := fulleval.Report{
			Configuration: fulleval.Configuration{
				Mode:           fulleval.ModeForgeFlow,
				ModelVersions:  map[string]string{"planner": "test-model"},
				PromptVersions: map[string]string{"planner": "planner/v1"},
			},
			Total: 30, Passed: 30,
			Metrics: fulleval.Metrics{AverageCostUSD: &cost, P95LatencyMS: &latency},
		}
		err := governanceStore.CreateEvalRun(ctx, governance.EvalRun{
			ID: id, CreatedBy: admin.userID, Dataset: "software", DatasetVersion: "v1",
			Status: "completed", Report: fulleval.ComparisonReport{Reports: []fulleval.Report{report}}, CreatedAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}

	run, err := f.runs.CreateQueued(ctx, application.CreateInput{OwnerID: admin.userID, Task: "preserve bound release", RepositoryPath: "."})
	if err != nil {
		t.Fatal(err)
	}
	var before []byte
	if err := f.db.QueryRow("SELECT state_json FROM runs WHERE id=$1", run.RunID).Scan(&before); err != nil {
		t.Fatal(err)
	}

	firstEval := createEval()
	response := f.request(t, admin, http.MethodPost, "/api/v1/prompts/planner/v1/promote", "{\"evalRunId\":\""+firstEval+"\",\"comment\":\"initial approved release\"}", true, nil)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("initial promotion status=%d body=%s", response.StatusCode, read(response))
	}
	var first governance.PromptRelease
	decodeResponse(t, response, &first)
	if first.Model != "test-model" || !first.Active {
		t.Fatalf("initial release=%+v", first)
	}

	secondEval := createEval()
	response = f.request(t, admin, http.MethodPost, "/api/v1/prompts/planner/v1/promote", "{\"evalRunId\":\""+secondEval+"\",\"comment\":\"approved replacement release\"}", true, nil)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("replacement promotion status=%d body=%s", response.StatusCode, read(response))
	}
	var second governance.PromptRelease
	decodeResponse(t, response, &second)

	var after []byte
	if err := f.db.QueryRow("SELECT state_json FROM runs WHERE id=$1", run.RunID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("promotion rewrote an existing run checkpoint")
	}

	response = f.request(t, admin, http.MethodPost, "/api/v1/prompts/planner/rollback", "{\"releaseId\":\""+first.ID+"\",\"comment\":\"approved rollback drill\"}", true, nil)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("rollback status=%d body=%s", response.StatusCode, read(response))
	}
	var rollback governance.PromptRelease
	decodeResponse(t, response, &rollback)
	if rollback.ID == first.ID || rollback.ID == second.ID || rollback.RollbackOf != second.ID || rollback.EvalRunID != firstEval || !rollback.Active {
		t.Fatalf("rollback did not create an immutable release: %+v", rollback)
	}

	var releaseCount, activeCount int
	if err := f.db.QueryRow("SELECT count(*),count(*) FILTER (WHERE active) FROM prompt_releases WHERE agent='planner'").Scan(&releaseCount, &activeCount); err != nil {
		t.Fatal(err)
	}
	if releaseCount != 3 || activeCount != 1 {
		t.Fatalf("release history count=%d active=%d", releaseCount, activeCount)
	}
	for _, check := range []struct {
		id, action, reason string
	}{
		{first.ID, "prompt.promote", "initial approved release"},
		{second.ID, "prompt.promote", "approved replacement release"},
		{rollback.ID, "prompt.rollback", "approved rollback drill"},
	} {
		var actor, action string
		var details []byte
		if err := f.db.QueryRow("SELECT actor_id::text,action,details FROM audit_log WHERE resource_id=$1", check.id).Scan(&actor, &action, &details); err != nil {
			t.Fatal(err)
		}
		if actor != admin.userID || action != check.action || !bytes.Contains(details, []byte(check.reason)) || !bytes.Contains(details, []byte("\"evalRunId\"")) {
			t.Fatalf("audit actor=%q action=%q details=%s", actor, action, details)
		}
	}
}

type loginState struct{ session, csrf, sessionID, userID string }

func (f *apiFixture) login(t *testing.T, email, password, old string) loginState {
	t.Helper()
	response := f.rawLogin(t, email, password, old)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("login status=%d body=%s", response.StatusCode, read(response))
	}
	var body struct {
		User    auth.User    `json:"user"`
		Session auth.Session `json:"session"`
		CSRF    string       `json:"csrfToken"`
	}
	decodeResponse(t, response, &body)
	state := loginState{csrf: body.CSRF, sessionID: body.Session.ID, userID: body.User.ID}
	for _, c := range response.Cookies() {
		if c.Name == httpapi.SessionCookie {
			state.session = c.Value
		}
	}
	return state
}
func (f *apiFixture) rawLogin(t *testing.T, email, password, old string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, f.server.URL+"/api/v1/auth/login", strings.NewReader(`{"email":"`+email+`","password":"`+password+`"}`))
	req.Header.Set("Content-Type", "application/json")
	if old != "" {
		req.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: old})
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return response
}
func (f *apiFixture) request(t *testing.T, state loginState, method, path, body string, csrf bool, headers map[string]string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, f.server.URL+path, strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: state.session})
	req.AddCookie(&http.Cookie{Name: httpapi.CSRFCookie, Value: state.csrf})
	if csrf {
		req.Header.Set("X-CSRF-Token", state.csrf)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return response
}
func newFixture(t *testing.T, accountLimiter auth.Limiter) *apiFixture {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("FORGEFLOW_TEST_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("FORGEFLOW_TEST_POSTGRES_DSN is not configured")
	}
	db, err := pg.Open(context.Background(), pg.Config{DSN: dsn, MaxOpenConns: 10, MaxIdleConns: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	guard, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := guard.ExecContext(context.Background(), `SELECT pg_advisory_lock(7308441907124661110)`); err != nil {
		guard.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = guard.ExecContext(context.Background(), `SELECT pg_advisory_unlock(7308441907124661110)`)
		_ = guard.Close()
	})
	var name string
	if err := db.QueryRow(`SELECT current_database()`).Scan(&name); err != nil || !strings.Contains(strings.ToLower(name), "test") {
		t.Fatalf("unsafe test database %q err=%v", name, err)
	}
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`TRUNCATE TABLE prompt_releases,eval_runs,audit_log,idempotency_keys,sessions,tool_calls,model_calls,artifacts,jobs,outbox,node_executions,approvals,run_events,checkpoints,runs,repositories,users CASCADE`); err != nil {
		t.Fatal(err)
	}
	store := auth.NewPostgresStore(db)
	authService, err := auth.NewService(store, auth.Options{PasswordParams: testPasswordParams(), SessionTTL: time.Hour, IdleTTL: time.Hour, AccountLimiter: accountLimiter, SourceLimiter: auth.NewMemoryLimiter(100, time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authService.BootstrapAdmin(context.Background(), "admin@example.com", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	runStore := checkpoint.NewPostgresStore(db)
	runs := application.NewService(runStore, planner.Mock{})
	catalog, err := governance.NewCatalog(config.Config{
		PlannerModel: "test-model", PlannerPromptVersion: "planner/v1",
		DeveloperModel: "test-model", DeveloperPromptVersion: "developer/v1",
		ReviewerModel: "test-model", ReviewerPromptVersion: "reviewer/v1",
		SecurityModel: "test-model", SecurityPromptVersion: "security/v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	server, err := httpapi.New(httpapi.Options{Auth: authService, Control: controlplane.NewStore(db), Runs: runs, Inspector: repository.NewGitInspector(repository.DefaultLimits()), CookieSecure: false, RepositoryRoots: []string{"."}, Governance: governance.NewStore(db), Catalog: catalog})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	return &apiFixture{server: httpServer, db: db, auth: authService, authStore: store, runs: runs}
}
func testPasswordParams() auth.PasswordParams {
	return auth.PasswordParams{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}
}
func decodeResponse(t *testing.T, response *http.Response, target any) {
	t.Helper()
	defer response.Body.Close()
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		t.Fatal(err)
	}
}
func read(response *http.Response) string {
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	return string(data)
}
