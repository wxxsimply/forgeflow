package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"forgeflow/internal/apperror"
	"forgeflow/internal/checkpoint"
	"forgeflow/internal/domain"
	"forgeflow/internal/graph"
	"forgeflow/internal/lifecycle"
	"forgeflow/internal/planner"
)

type Service struct {
	store           checkpoint.Store
	definition      graph.Definition
	resumeValidator lifecycle.Validator
}

func NewService(store checkpoint.Store, planAgent planner.Planner) *Service {
	return NewServiceWithDefinition(store, graph.PlanningDefinition(planAgent))
}

func NewServiceWithDefinition(store checkpoint.Store, definition graph.Definition) *Service {
	validator, _ := lifecycle.NewValidator(lifecycle.Options{})
	return &Service{store: store, definition: definition, resumeValidator: validator}
}

func NewServiceWithResumeValidator(store checkpoint.Store, definition graph.Definition, validator lifecycle.Validator) *Service {
	if validator == nil {
		validator, _ = lifecycle.NewValidator(lifecycle.Options{})
	}
	return &Service{store: store, definition: definition, resumeValidator: validator}
}

type CreateInput struct {
	OwnerID        string
	RepositoryID   string
	Task           string
	RepositoryPath string
	BaseRevision   string
	MaxIterations  int
	Budget         *domain.RunBudget
}

func (s *Service) Create(ctx context.Context, input CreateInput) (*domain.RunState, error) {
	state, err := s.CreateQueued(ctx, input)
	if err != nil {
		return nil, err
	}
	return s.execute(ctx, state)
}

// CreateQueued persists the initial checkpoint and lets the durable worker run
// the graph. API processes use this path so they never need model credentials.
func (s *Service) CreateQueued(ctx context.Context, input CreateInput) (*domain.RunState, error) {
	state := domain.NewRunState(domain.NewRunInput{
		OwnerID: input.OwnerID, RepositoryID: input.RepositoryID,
		Task: input.Task, RepositoryPath: input.RepositoryPath, BaseRevision: input.BaseRevision,
		MaxIterations: input.MaxIterations, Budget: input.Budget,
	})
	if err := s.store.Save(ctx, state, state.Version); err != nil {
		return nil, wrapStoreError(err, "run.create.checkpoint", "could not save the initial run checkpoint")
	}
	return state, nil
}

func (s *Service) Get(ctx context.Context, runID string) (*domain.RunState, error) {
	state, err := s.store.Load(ctx, runID)
	if errors.Is(err, checkpoint.ErrNotFound) {
		return nil, apperror.New(apperror.CodeNotFound, fmt.Sprintf("run %q not found", runID))
	}
	if err != nil {
		return nil, apperror.Wrap(err, apperror.CodeInternal, "run.get", "could not load the run")
	}
	return state, nil
}

func (s *Service) ResolveApproval(ctx context.Context, runID string, approve bool, comment string) (*domain.RunState, error) {
	return s.resolveApproval(ctx, runID, approve, comment, -1)
}

func (s *Service) ResolveApprovalVersion(ctx context.Context, runID string, approve bool, comment string, expectedVersion int64) (*domain.RunState, error) {
	return s.resolveApproval(ctx, runID, approve, comment, expectedVersion)
}

// ResolveApprovalQueued persists the decision and wakes the worker without
// executing Agent code in the API process.
func (s *Service) ResolveApprovalQueued(ctx context.Context, runID string, approve bool, comment string, expectedVersion int64) (*domain.RunState, error) {
	return s.resolveApprovalOnly(ctx, runID, approve, comment, expectedVersion)
}

func (s *Service) resolveApproval(ctx context.Context, runID string, approve bool, comment string, expectedVersion int64) (*domain.RunState, error) {
	state, err := s.resolveApprovalOnly(ctx, runID, approve, comment, expectedVersion)
	if err != nil {
		return nil, err
	}
	return s.execute(ctx, state)
}

func (s *Service) resolveApprovalOnly(ctx context.Context, runID string, approve bool, comment string, expectedVersion int64) (*domain.RunState, error) {
	state, err := s.Get(ctx, runID)
	if err != nil {
		return nil, err
	}
	if expectedVersion >= 0 && state.Version != expectedVersion {
		return nil, apperror.New(apperror.CodeConflict, "approval changed; reload it before deciding")
	}
	if state.PendingApproval == nil || state.PendingApproval.Status != domain.ApprovalPending {
		return nil, apperror.New(apperror.CodeConflict, "run has no pending approval")
	}
	now := time.Now().UTC()
	if approve {
		state.PendingApproval.Status = domain.ApprovalApproved
	} else {
		state.PendingApproval.Status = domain.ApprovalRejected
	}
	state.PendingApproval.Comment = comment
	state.PendingApproval.ResolvedAt = &now
	state.AppendEvent(domain.EventApprovalResolved, state.CurrentNodeID, "Approval resolved")
	if err := s.store.Save(ctx, state, state.Version); err != nil {
		return nil, wrapStoreError(err, "run.approval.save", "could not save the approval")
	}
	return state, nil
}

func (s *Service) Cancel(ctx context.Context, runID, requestedBy, reason string) (*domain.RunState, error) {
	state, err := s.CancelQueued(ctx, runID, requestedBy, reason)
	if err != nil {
		return nil, err
	}
	return s.execute(ctx, state)
}

func (s *Service) CancelQueued(ctx context.Context, runID, requestedBy, reason string) (*domain.RunState, error) {
	state, err := s.Get(ctx, runID)
	if err != nil {
		return nil, err
	}
	if state.Status.Terminal() {
		return nil, apperror.New(apperror.CodeConflict, "terminal run cannot be cancelled")
	}
	state.RequestCancellation(requestedBy, reason)
	if err := s.store.Save(ctx, state, state.Version); err != nil {
		return nil, wrapStoreError(err, "run.cancel.save", "could not save the cancellation request")
	}
	return state, nil
}

func (s *Service) Pause(ctx context.Context, runID, requestedBy, reason string) (*domain.RunState, error) {
	state, err := s.PauseQueued(ctx, runID, requestedBy, reason)
	if err != nil {
		return nil, err
	}
	return s.execute(ctx, state)
}

func (s *Service) PauseQueued(ctx context.Context, runID, requestedBy, reason string) (*domain.RunState, error) {
	state, err := s.Get(ctx, runID)
	if err != nil {
		return nil, err
	}
	if state.Status.Terminal() || state.Status == domain.StatusPaused {
		return nil, apperror.New(apperror.CodeConflict, "run cannot be paused in its current status")
	}
	state.RequestPause(requestedBy, reason)
	if err := s.store.Save(ctx, state, state.Version); err != nil {
		return nil, wrapStoreError(err, "run.pause.save", "could not save the pause request")
	}
	return state, nil
}

func (s *Service) Resume(ctx context.Context, runID string) (*domain.RunState, error) {
	state, err := s.ResumeQueued(ctx, runID)
	if err != nil {
		return nil, err
	}
	return s.execute(ctx, state)
}

func (s *Service) ResumeQueued(ctx context.Context, runID string) (*domain.RunState, error) {
	state, err := s.Get(ctx, runID)
	if err != nil {
		return nil, err
	}
	if state.Status != domain.StatusPaused || !state.Pause.Requested() {
		return nil, apperror.New(apperror.CodeConflict, "run is not paused")
	}
	if err := s.resumeValidator.Validate(ctx, state); err != nil {
		return nil, apperror.Wrap(err, apperror.CodeConflict, "run.resume.compatibility", "paused run is no longer compatible")
	}
	previous := state.Pause.PreviousStatus
	if previous == "" || previous == domain.StatusPaused || previous.Terminal() {
		previous = domain.StatusCreated
	}
	state.Status = previous
	state.Pause = domain.PauseState{}
	state.ResumeGuard = nil
	state.AppendEvent(domain.EventRunResumed, state.CurrentNodeID, "Run resumed")
	if err := s.store.Save(ctx, state, state.Version); err != nil {
		return nil, wrapStoreError(err, "run.resume.save", "could not save resumed state")
	}
	return state, nil
}

func (s *Service) Continue(ctx context.Context, runID string) (*domain.RunState, error) {
	state, err := s.Get(ctx, runID)
	if err != nil {
		return nil, err
	}
	return s.execute(ctx, state)
}

func (s *Service) execute(ctx context.Context, state *domain.RunState) (*domain.RunState, error) {
	runtime, err := graph.NewRuntime(s.definition, s.store)
	if err != nil {
		return nil, err
	}
	return runtime.Execute(ctx, state)
}

func wrapStoreError(err error, operation, message string) error {
	if errors.Is(err, checkpoint.ErrConflict) {
		return apperror.Wrap(err, apperror.CodeConflict, operation, "run state changed concurrently")
	}
	return apperror.Wrap(err, apperror.CodeTransient, operation, message)
}
