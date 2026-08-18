package httpapi

import (
	"net/http"
	"strings"
	"time"

	"forgeflow/internal/apperror"
	"forgeflow/internal/auth"
	"forgeflow/internal/checkpoint"
	"forgeflow/internal/domain"
	fulleval "forgeflow/internal/eval"
	"forgeflow/internal/governance"
)

func (s *Server) createEvalRun(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if p.User.Role != auth.RoleAdmin {
		s.fail(w, r, forbidden())
		return
	}
	var evidence fulleval.EvidenceFile
	if err := decode(r, &evidence); err != nil {
		s.fail(w, r, err)
		return
	}
	dataset, err := fulleval.Load(fulleval.SoftwareV1)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	report, err := fulleval.BuildComparison(dataset, evidence, time.Now().UTC())
	if err != nil {
		s.fail(w, r, validation(err.Error()))
		return
	}
	run := governance.EvalRun{ID: domain.NewID(), CreatedBy: p.User.ID, Dataset: dataset.Name, DatasetVersion: dataset.Version, Status: "completed", Report: report, CreatedAt: time.Now().UTC()}
	if err := s.options.Governance.CreateEvalRun(r.Context(), run); err != nil {
		s.fail(w, r, err)
		return
	}
	s.audit(r, p.User.ID, "eval.create", "eval_run", run.ID, map[string]any{"dataset": run.Dataset})
	w.Header().Set("Location", "/api/v1/evals/runs/"+run.ID)
	writeJSON(w, http.StatusCreated, run)
}

func (s *Server) listEvalRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := s.options.Governance.ListEvalRuns(r.Context(), parseLimit(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": runs})
}

func (s *Server) getEvalRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.options.Governance.GetEvalRun(r.Context(), r.PathValue("evalRunId"))
	if err != nil {
		s.fail(w, r, notFound(err))
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) listAgents(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": s.options.Catalog.Agents()})
}

func (s *Server) listPrompts(w http.ResponseWriter, r *http.Request) {
	releases, err := s.options.Governance.ListReleases(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": s.options.Catalog.Prompts(), "releases": releases})
}

func (s *Server) promotePrompt(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if p.User.Role != auth.RoleAdmin {
		s.fail(w, r, forbidden())
		return
	}
	agent, version := r.PathValue("agent"), r.PathValue("version")
	promptVersion := agent + "/" + version
	prompt, err := s.options.Catalog.Prompt(agent, promptVersion)
	if err != nil {
		s.fail(w, r, notFound(checkpoint.ErrNotFound))
		return
	}
	var input struct {
		EvalRunID string `json:"evalRunId"`
		Comment   string `json:"comment"`
	}
	if err := decode(r, &input); err != nil {
		s.fail(w, r, err)
		return
	}
	if !uuidPattern.MatchString(input.EvalRunID) || len(input.Comment) > 2000 {
		s.fail(w, r, validation("evalRunId or comment is invalid"))
		return
	}
	candidateRun, err := s.options.Governance.GetEvalRun(r.Context(), input.EvalRunID)
	if err != nil {
		s.fail(w, r, notFound(err))
		return
	}
	candidate, err := governance.ForgeFlowReport(candidateRun)
	if err != nil {
		s.fail(w, r, validation(err.Error()))
		return
	}
	if candidate.Configuration.PromptVersions[agent] != promptVersion {
		s.fail(w, r, validation("eval report does not bind the requested prompt version"))
		return
	}
	currentRelease, currentErr := s.options.Governance.ActiveRelease(r.Context(), agent)
	if currentErr == nil {
		currentRun, err := s.options.Governance.GetEvalRun(r.Context(), currentRelease.EvalRunID)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		current, err := governance.ForgeFlowReport(currentRun)
		if err != nil {
			s.fail(w, r, validation(err.Error()))
			return
		}
		decision := fulleval.CheckPromotion(current, candidate, fulleval.DefaultPromotionThresholds())
		if !decision.Allowed {
			s.fail(w, r, apperror.New(apperror.CodeConflict, "prompt promotion blocked: "+strings.Join(decision.Reasons, ", ")))
			return
		}
	} else if currentErr != checkpoint.ErrNotFound {
		s.fail(w, r, currentErr)
		return
	} else if err := governance.InitialPromotionAllowed(candidate); err != nil {
		s.fail(w, r, apperror.New(apperror.CodeConflict, err.Error()))
		return
	}
	release := governance.PromptRelease{ID: domain.NewID(), Agent: agent, Version: promptVersion, PromptSHA256: prompt.SHA256, EvalRunID: input.EvalRunID, PromotedBy: p.User.ID, Comment: input.Comment, Active: true, CreatedAt: time.Now().UTC()}
	if err := s.options.Governance.Promote(r.Context(), release); err != nil {
		s.fail(w, r, err)
		return
	}
	s.audit(r, p.User.ID, "prompt.promote", "prompt_release", release.ID, map[string]any{"agent": agent, "version": version, "evalRunId": input.EvalRunID})
	writeJSON(w, http.StatusCreated, release)
}

func (s *Server) rollbackPrompt(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if p.User.Role != auth.RoleAdmin {
		s.fail(w, r, forbidden())
		return
	}
	var input struct {
		ReleaseID string `json:"releaseId"`
		Comment   string `json:"comment"`
	}
	if err := decode(r, &input); err != nil {
		s.fail(w, r, err)
		return
	}
	if !uuidPattern.MatchString(input.ReleaseID) || len(input.Comment) > 2000 {
		s.fail(w, r, validation("releaseId or comment is invalid"))
		return
	}
	target, err := s.options.Governance.GetRelease(r.Context(), input.ReleaseID)
	if err != nil || target.Agent != r.PathValue("agent") {
		s.fail(w, r, notFound(checkpoint.ErrNotFound))
		return
	}
	if _, err := s.options.Catalog.Prompt(target.Agent, target.Version); err != nil {
		s.fail(w, r, apperror.New(apperror.CodeConflict, "rollback target is not embedded in this release"))
		return
	}
	active, err := s.options.Governance.ActiveRelease(r.Context(), target.Agent)
	if err != nil {
		s.fail(w, r, apperror.New(apperror.CodeConflict, "prompt has no active release"))
		return
	}
	if active.ID == target.ID {
		s.fail(w, r, apperror.New(apperror.CodeConflict, "target release is already active"))
		return
	}
	release := governance.PromptRelease{ID: domain.NewID(), Agent: target.Agent, Version: target.Version, PromptSHA256: target.PromptSHA256, EvalRunID: target.EvalRunID, PromotedBy: p.User.ID, RollbackOf: active.ID, Comment: input.Comment, Active: true, CreatedAt: time.Now().UTC()}
	if err := s.options.Governance.Promote(r.Context(), release); err != nil {
		s.fail(w, r, err)
		return
	}
	s.audit(r, p.User.ID, "prompt.rollback", "prompt_release", release.ID, map[string]any{"agent": release.Agent, "targetReleaseId": target.ID, "rollbackOf": active.ID})
	writeJSON(w, http.StatusCreated, release)
}
