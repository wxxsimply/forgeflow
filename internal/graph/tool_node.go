package graph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"forgeflow/internal/apperror"
	"forgeflow/internal/domain"
	"forgeflow/internal/policy"
	toolruntime "forgeflow/internal/tool"
)

type ToolNode struct {
	NodeID          string
	Runtime         *toolruntime.Runtime
	ToolName        string
	Agent           string
	Input           func(*domain.RunState) json.RawMessage
	AuthorizedPaths func(*domain.RunState) []string
	OnOutput        func(*domain.RunState, json.RawMessage, domain.ToolCallAudit) error
	NodeTimeout     time.Duration
}

func (n ToolNode) ID() string { return n.NodeID }

func (n ToolNode) Policy() NodePolicy {
	timeout := n.NodeTimeout
	if timeout <= 0 {
		timeout = 12 * time.Minute
	}
	return NodePolicy{Timeout: timeout, MaxAttempts: 1, ReplaySafe: false}
}

func (n ToolNode) IdempotencyKey(state *domain.RunState) string {
	input := n.input(state)
	payload := make([]byte, 0, len(n.ToolName)+len(input)+len(workspaceID(state))+2)
	payload = append(payload, n.ToolName...)
	payload = append(payload, 0)
	payload = append(payload, input...)
	payload = append(payload, 0)
	payload = append(payload, workspaceID(state)...)
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func (n ToolNode) Execute(ctx context.Context, state *domain.RunState) Result {
	if n.Runtime == nil || n.NodeID == "" || n.ToolName == "" || n.Agent == "" {
		return Result{Type: ResultFatalError, State: state, Err: apperror.New(apperror.CodeValidation, "tool node is incomplete")}
	}
	if state.Workspace == nil || state.Workspace.ID == "" || state.Workspace.Path == "" {
		return Result{Type: ResultFatalError, State: state, Err: apperror.New(apperror.CodeValidation, "tool node requires a prepared workspace")}
	}

	callID := domain.NewID()
	var evidence *policy.ApprovalEvidence
	if pending := state.PendingApproval; pending != nil {
		if pending.ActionType != "tool" || pending.ToolName != n.ToolName {
			return Result{Type: ResultFatalError, State: state, Err: apperror.New(apperror.CodeConflict, "pending approval belongs to another action")}
		}
		callID = pending.ToolCallID
		switch pending.Status {
		case domain.ApprovalPending:
			return Result{Type: ResultInterrupted, State: state, Approval: pending}
		case domain.ApprovalRejected:
			audit := domain.ToolCallAudit{
				CallID: callID, NodeID: n.NodeID, Agent: n.Agent, ToolName: n.ToolName,
				ToolVersion: pending.ToolVersion, InputSHA256: pending.InputSHA256,
				WorkspaceID: pending.WorkspaceID, PolicyVersion: pending.PolicyVersion,
				PolicyAction: string(policy.ActionDeny), Status: domain.ToolCallDenied,
				ErrorCode: "approval_rejected", CreatedAt: pending.RequestedAt, CompletedAt: pending.ResolvedAt,
			}
			state.RecordToolCall(audit)
			state.AppendEvent(domain.EventToolCallDenied, n.NodeID, "Tool action was rejected")
			state.PendingApproval = nil
			state.Status = domain.StatusCancelled
			return Result{Type: ResultCompleted, State: state}
		case domain.ApprovalApproved:
			evidence = &policy.ApprovalEvidence{
				Approved: true, ToolName: pending.ToolName, ToolVersion: pending.ToolVersion,
				InputSHA256: pending.InputSHA256, WorkspaceID: pending.WorkspaceID,
				PolicyVersion: pending.PolicyVersion,
			}
		default:
			return Result{Type: ResultFatalError, State: state, Err: apperror.New(apperror.CodeConflict, "approval status is invalid")}
		}
	}

	invocation, err := n.Runtime.Execute(ctx, toolruntime.CallContext{
		CallID: callID, NodeID: n.NodeID, RunID: state.RunID, TraceID: state.TraceID,
		Agent: n.Agent, Workspace: *state.Workspace, Budget: state.Budget, Approval: evidence,
		AllowedPaths: n.authorizedPaths(state),
		OnStarted: func() {
			state.AppendEvent(domain.EventToolCallStarted, n.NodeID, "Tool call "+n.ToolName+" started")
		},
	}, n.ToolName, n.input(state))
	state.RecordToolCall(invocation.Audit)
	if invocation.Approval != nil {
		requirement := invocation.Approval
		approval := &domain.ApprovalRequest{
			ApprovalID: domain.NewID(), RunID: state.RunID, ActionType: "tool",
			Reason: requirement.Reason, Scope: requirement.Scope, Risk: requirement.Risk,
			Status: domain.ApprovalPending, RequestedAt: time.Now().UTC(),
			ToolCallID: requirement.CallID, ToolName: requirement.ToolName,
			ToolVersion: requirement.ToolVersion, InputSHA256: requirement.InputSHA256,
			WorkspaceID: requirement.WorkspaceID, PolicyVersion: requirement.PolicyVersion,
		}
		state.Status = domain.StatusWaitingActionApproval
		state.AppendEvent(domain.EventApprovalRequested, n.NodeID, "Tool action approval requested")
		return Result{Type: ResultInterrupted, State: state, Approval: approval}
	}
	if err != nil {
		state.PendingApproval = nil
		eventType := domain.EventToolCallFailed
		if invocation.Audit.Status == domain.ToolCallDenied {
			eventType = domain.EventToolCallDenied
		}
		state.AppendEvent(eventType, n.NodeID, err.Error())
		return Result{Type: ResultFatalError, State: state, Err: err}
	}
	if n.OnOutput != nil {
		if err := n.OnOutput(state, invocation.Output, invocation.Audit); err != nil {
			state.AppendEvent(domain.EventToolCallFailed, n.NodeID, err.Error())
			return Result{Type: ResultFatalError, State: state, Err: err}
		}
	}
	state.PendingApproval = nil
	state.AppendEvent(domain.EventToolCallCompleted, n.NodeID, "Tool call "+n.ToolName+" completed")
	return Result{Type: ResultCompleted, State: state}
}

func (n ToolNode) input(state *domain.RunState) json.RawMessage {
	if n.Input == nil {
		return json.RawMessage(`{}`)
	}
	return n.Input(state)
}

func workspaceID(state *domain.RunState) string {
	if state.Workspace == nil {
		return ""
	}
	return state.Workspace.ID
}

func (n ToolNode) authorizedPaths(state *domain.RunState) []string {
	if n.AuthorizedPaths == nil {
		return nil
	}
	return append([]string(nil), n.AuthorizedPaths(state)...)
}

var _ Node = ToolNode{}
