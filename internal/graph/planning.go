package graph

import (
	"context"
	"fmt"

	"forgeflow/internal/apperror"
	"forgeflow/internal/domain"
	"forgeflow/internal/planner"
)

func PlanningDefinition(planAgent planner.Planner) Definition {
	nodes := []Node{
		NodeFunc{NodeID: "start", Run: func(_ context.Context, state *domain.RunState) Result {
			state.Status = domain.StatusPlanning
			state.AppendEvent(domain.EventStatusChanged, "start", "Status changed to planning")
			return Result{Type: ResultCompleted, State: state}
		}},
		NodeFunc{NodeID: "planner", ExecutionPolicy: NodePolicy{Timeout: planner.Timeout(planAgent), MaxAttempts: 1, ReplaySafe: false}, Run: func(ctx context.Context, state *domain.RunState) Result {
			planResult, err := planAgent.CreatePlan(ctx, planner.Input{
				Task: state.Task, RepositoryPath: state.RepositoryPath, BaseRevision: state.BaseRevision, Budget: state.Budget,
			})
			if planResult.Invocation != nil {
				state.RecordModelInvocation(*planResult.Invocation)
				if allowed, reason := state.Budget.ModelUsageAllowed(); !allowed {
					state.AppendEvent(domain.EventBudgetExhausted, "planner", reason)
					return Result{Type: ResultFatalError, State: state, Err: apperror.New(apperror.CodeBudget, reason)}
				}
			}
			if err != nil {
				return Result{Type: ResultFatalError, State: state, Err: fmt.Errorf("create plan: %w", err)}
			}
			state.Plan = &planResult.Plan
			return Result{Type: ResultCompleted, State: state}
		}},
		NodeFunc{NodeID: "validate-plan", Run: func(_ context.Context, state *domain.RunState) Result {
			if state.Plan == nil {
				return Result{Type: ResultFatalError, State: state, Err: fmt.Errorf("planner returned no plan")}
			}
			if err := state.Plan.Validate(); err != nil {
				return Result{Type: ResultFatalError, State: state, Err: fmt.Errorf("invalid plan: %w", err)}
			}
			state.Status = domain.StatusWaitingPlanApproval
			return Result{Type: ResultCompleted, State: state}
		}},
		NodeFunc{NodeID: "plan-approval", Run: approvalNode},
		NodeFunc{NodeID: "end", Run: func(_ context.Context, state *domain.RunState) Result {
			state.Status = domain.StatusCompleted
			return Result{Type: ResultCompleted, State: state}
		}},
	}
	return Definition{
		EntryNodeID: "start",
		Nodes:       nodes,
		Edges: []Edge{
			{From: "start", To: "planner"},
			{From: "planner", To: "validate-plan"},
			{From: "validate-plan", To: "plan-approval"},
			{From: "plan-approval", To: "end", When: func(state *domain.RunState) bool {
				return state.Status != domain.StatusCancelled
			}},
		},
	}
}

func approvalNode(_ context.Context, state *domain.RunState) Result {
	if state.PendingApproval != nil {
		switch state.PendingApproval.Status {
		case domain.ApprovalPending:
			return Result{Type: ResultInterrupted, State: state, Approval: state.PendingApproval}
		case domain.ApprovalApproved:
			state.PendingApproval = nil
			return Result{Type: ResultCompleted, State: state}
		case domain.ApprovalRejected:
			state.Status = domain.StatusCancelled
			return Result{Type: ResultCompleted, State: state}
		}
	}
	approval := &domain.ApprovalRequest{
		ApprovalID: domain.NewID(), RunID: state.RunID, ActionType: "plan",
		Reason: "实施计划必须经过人工批准", Scope: state.Plan.FilesLikelyAffected,
		Risk: state.Plan.HighestRisk(), Status: domain.ApprovalPending,
		RequestedAt: state.UpdatedAt,
	}
	state.AppendEvent(domain.EventApprovalRequested, "plan-approval", "Plan approval requested")
	return Result{Type: ResultInterrupted, State: state, Approval: approval}
}
