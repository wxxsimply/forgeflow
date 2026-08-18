package graph

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"forgeflow/internal/apperror"
	"forgeflow/internal/checkpoint"
	"forgeflow/internal/domain"
	"forgeflow/internal/policy"
	toolruntime "forgeflow/internal/tool"
)

type approvalTestTool struct{ executions atomic.Int32 }

func (*approvalTestTool) Spec() toolruntime.Spec {
	return toolruntime.Spec{
		Name: "apply_patch", Version: "v1", Description: "approval test tool",
		InputSchema: json.RawMessage(`{"type":"object"}`), OutputSchema: json.RawMessage(`{"type":"object"}`),
		Risk: domain.RiskHigh, Timeout: time.Second, MaxOutputBytes: 1024,
	}
}

func (*approvalTestTool) Analyze(json.RawMessage) (policy.Metadata, error) {
	return policy.Metadata{Paths: []string{"internal/app.go"}}, nil
}

func (t *approvalTestTool) Execute(context.Context, toolruntime.CallContext, json.RawMessage) (json.RawMessage, error) {
	t.executions.Add(1)
	return json.RawMessage(`{"changed":true}`), nil
}

func (*approvalTestTool) ValidateOutput(output json.RawMessage) error {
	if !json.Valid(output) {
		return apperror.New(apperror.CodeValidation, "invalid output")
	}
	return nil
}

func TestToolNodeApprovalSurvivesCheckpointRestart(t *testing.T) {
	candidate := &approvalTestTool{}
	toolRuntime := approvalRuntime(t, candidate)
	directory := t.TempDir()
	store := checkpoint.NewFileStore(directory)
	definition := approvalDefinition(toolRuntime)
	state := approvalState(t.TempDir())

	firstRuntime := mustRuntime(t, definition, store)
	waiting, err := firstRuntime.Execute(context.Background(), state)
	if err != nil {
		t.Fatal(err)
	}
	if waiting.Status != domain.StatusWaitingActionApproval || waiting.PendingApproval == nil || candidate.executions.Load() != 0 {
		t.Fatalf("waiting state = %+v, executions=%d", waiting.PendingApproval, candidate.executions.Load())
	}
	if len(waiting.ToolCallAudits) != 1 || waiting.ToolCallAudits[0].Status != domain.ToolCallApprovalRequired {
		t.Fatalf("audits = %+v", waiting.ToolCallAudits)
	}

	reloaded, err := checkpoint.NewFileStore(directory).Load(context.Background(), waiting.RunID)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	reloaded.PendingApproval.Status = domain.ApprovalApproved
	reloaded.PendingApproval.ResolvedAt = &now
	if err := checkpoint.NewFileStore(directory).Save(context.Background(), reloaded, reloaded.Version); err != nil {
		t.Fatal(err)
	}

	resumed, err := mustRuntime(t, definition, checkpoint.NewFileStore(directory)).Execute(context.Background(), reloaded)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Status != domain.StatusCompleted || resumed.PendingApproval != nil || candidate.executions.Load() != 1 {
		t.Fatalf("resumed status=%s pending=%+v executions=%d", resumed.Status, resumed.PendingApproval, candidate.executions.Load())
	}
	if len(resumed.ToolCallAudits) != 1 || resumed.ToolCallAudits[0].Status != domain.ToolCallSucceeded {
		t.Fatalf("resumed audits = %+v", resumed.ToolCallAudits)
	}
}

func TestToolNodeRejectsTamperedApprovalWithoutExecution(t *testing.T) {
	candidate := &approvalTestTool{}
	toolRuntime := approvalRuntime(t, candidate)
	store := checkpoint.NewFileStore(t.TempDir())
	definition := approvalDefinition(toolRuntime)
	waiting, err := mustRuntime(t, definition, store).Execute(context.Background(), approvalState(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	waiting.PendingApproval.Status = domain.ApprovalApproved
	waiting.PendingApproval.InputSHA256 = "tampered"
	now := time.Now().UTC()
	waiting.PendingApproval.ResolvedAt = &now
	if err := store.Save(context.Background(), waiting, waiting.Version); err != nil {
		t.Fatal(err)
	}

	failed, err := mustRuntime(t, definition, store).Execute(context.Background(), waiting)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != domain.StatusFailed || candidate.executions.Load() != 0 {
		t.Fatalf("status=%s executions=%d", failed.Status, candidate.executions.Load())
	}
	if len(failed.ToolCallAudits) != 1 || failed.ToolCallAudits[0].ErrorCode != "approval_mismatch" {
		t.Fatalf("audits = %+v", failed.ToolCallAudits)
	}
}

func approvalRuntime(t *testing.T, candidate toolruntime.Tool) *toolruntime.Runtime {
	t.Helper()
	registry := toolruntime.NewRegistry()
	if err := registry.Register(candidate); err != nil {
		t.Fatal(err)
	}
	runtime, err := toolruntime.NewRuntime(registry, policy.DefaultEngine())
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

func approvalDefinition(runtime *toolruntime.Runtime) Definition {
	return Definition{
		EntryNodeID: "patch",
		Nodes: []Node{ToolNode{
			NodeID: "patch", Runtime: runtime, ToolName: "apply_patch", Agent: "developer",
			Input: func(*domain.RunState) json.RawMessage { return json.RawMessage(`{"patch":"safe"}`) },
		}},
	}
}

func approvalState(workspace string) *domain.RunState {
	state := domain.NewRunState(domain.NewRunInput{Task: "patch", RepositoryPath: workspace})
	state.CurrentNodeID = "patch"
	state.Workspace = &domain.WorkspaceRef{ID: "workspace-1", Path: workspace, RepositoryRoot: workspace}
	return state
}
