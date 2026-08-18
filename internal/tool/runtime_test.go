package tool

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"forgeflow/internal/apperror"
	"forgeflow/internal/domain"
	"forgeflow/internal/policy"
)

type fakeTool struct {
	spec       Spec
	metadata   policy.Metadata
	output     json.RawMessage
	err        error
	delay      time.Duration
	executions int
}

func (f *fakeTool) Spec() Spec { return f.spec }

func (f *fakeTool) Analyze(input json.RawMessage) (policy.Metadata, error) {
	var value map[string]any
	if err := decodeStrict(input, &value); err != nil {
		return policy.Metadata{}, err
	}
	return f.metadata, nil
}

func (f *fakeTool) Execute(ctx context.Context, _ CallContext, _ json.RawMessage) (json.RawMessage, error) {
	f.executions++
	if f.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(f.delay):
		}
	}
	return f.output, f.err
}

func (f *fakeTool) ValidateOutput(output json.RawMessage) error {
	if !json.Valid(output) {
		return errors.New("invalid output")
	}
	return nil
}

func TestRuntimeRedactsOutputPreservesFormattingAndRecordsAudit(t *testing.T) {
	candidate := newFakeTool("read_file", domain.RiskLow)
	candidate.output = json.RawMessage(`{"text":"line one\n  sk-abcdefghijklmnop  \nline three","apiToken":"secret"}`)
	runtime := newTestRuntime(t, candidate)
	invocation, err := runtime.Execute(context.Background(), testCall(), "read_file", json.RawMessage(`{"path":"README.md"}`))
	if err != nil {
		t.Fatal(err)
	}
	var output map[string]string
	if err := json.Unmarshal(invocation.Output, &output); err != nil {
		t.Fatal(err)
	}
	if output["text"] != "line one\n  [REDACTED]  \nline three" || output["apiToken"] != "[REDACTED]" {
		t.Fatalf("redacted output = %#v", output)
	}
	if invocation.Audit.Status != domain.ToolCallSucceeded || invocation.Audit.PolicyRuleID != "repo-read-file" || invocation.Audit.InputSHA256 == "" {
		t.Fatalf("audit = %+v", invocation.Audit)
	}
}

func TestRuntimeDefaultDenyBudgetAndTimeout(t *testing.T) {
	candidate := newFakeTool("read_file", domain.RiskLow)
	candidate.output = json.RawMessage(`{"ok":true}`)
	runtime := newTestRuntime(t, candidate)

	if _, err := runtime.Execute(context.Background(), testCall(), "missing", json.RawMessage(`{}`)); !apperror.IsCode(err, apperror.CodePolicyDenied) {
		t.Fatalf("unknown tool error = %v", err)
	}
	budgetCall := testCall()
	budgetCall.Budget.MaxToolCalls, budgetCall.Budget.ToolCalls = 1, 1
	if _, err := runtime.Execute(context.Background(), budgetCall, "read_file", json.RawMessage(`{}`)); !apperror.IsCode(err, apperror.CodeBudget) {
		t.Fatalf("budget error = %v", err)
	}

	timeoutTool := newFakeTool("read_file", domain.RiskLow)
	timeoutTool.delay = 50 * time.Millisecond
	timeoutTool.spec.Timeout = time.Millisecond
	runtime = newTestRuntime(t, timeoutTool)
	if _, err := runtime.Execute(context.Background(), testCall(), "read_file", json.RawMessage(`{}`)); !apperror.IsCode(err, apperror.CodeTimeout) {
		t.Fatalf("timeout error = %v", err)
	}
}

func TestRuntimeRequiresExactApprovalBeforeExecution(t *testing.T) {
	candidate := newFakeTool("apply_patch", domain.RiskHigh)
	candidate.metadata.Paths = []string{"internal/app.go"}
	candidate.output = json.RawMessage(`{"changed":true}`)
	runtime := newTestRuntime(t, candidate)
	call := testCall()
	call.Agent = "developer"
	input := json.RawMessage(`{"patch":"safe"}`)

	first, err := runtime.Execute(context.Background(), call, "apply_patch", input)
	if err != nil || first.Approval == nil || candidate.executions != 0 {
		t.Fatalf("first invocation = %+v, executions=%d, err=%v", first, candidate.executions, err)
	}
	requirement := first.Approval
	call.CallID = requirement.CallID
	call.Approval = &policy.ApprovalEvidence{
		Approved: true, ToolName: requirement.ToolName, ToolVersion: requirement.ToolVersion,
		InputSHA256: requirement.InputSHA256, WorkspaceID: requirement.WorkspaceID,
		PolicyVersion: requirement.PolicyVersion,
	}
	second, err := runtime.Execute(context.Background(), call, "apply_patch", input)
	if err != nil || candidate.executions != 1 || second.Audit.Status != domain.ToolCallSucceeded {
		t.Fatalf("approved invocation = %+v, executions=%d, err=%v", second, candidate.executions, err)
	}

	call.Approval.InputSHA256 = "tampered"
	if _, err := runtime.Execute(context.Background(), call, "apply_patch", input); !apperror.IsCode(err, apperror.CodePolicyDenied) {
		t.Fatalf("tampered approval error = %v", err)
	}
	if candidate.executions != 1 {
		t.Fatalf("tampered approval executed tool %d times", candidate.executions)
	}
}

func TestRegistryRejectsDuplicatesAndSeals(t *testing.T) {
	registry := NewRegistry()
	candidate := newFakeTool("read_file", domain.RiskLow)
	if err := registry.Register(candidate); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(candidate); !apperror.IsCode(err, apperror.CodeConflict) {
		t.Fatalf("duplicate error = %v", err)
	}
	registry.Seal()
	if err := registry.Register(newFakeTool("list_files", domain.RiskLow)); !apperror.IsCode(err, apperror.CodeConflict) {
		t.Fatalf("sealed error = %v", err)
	}
}

func newFakeTool(name string, risk domain.RiskLevel) *fakeTool {
	return &fakeTool{
		spec: Spec{
			Name: name, Version: "v1", Description: "test tool",
			InputSchema: json.RawMessage(objectSchema), OutputSchema: json.RawMessage(objectSchema),
			Risk: risk, Timeout: time.Second, MaxOutputBytes: 1024,
		},
		metadata: policy.Metadata{Paths: []string{"."}}, output: json.RawMessage(`{}`),
	}
}

func newTestRuntime(t *testing.T, candidate Tool) *Runtime {
	t.Helper()
	registry := NewRegistry()
	if err := registry.Register(candidate); err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(registry, policy.DefaultEngine())
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

func testCall() CallContext {
	return CallContext{
		RunID: "run-1", TraceID: "trace-1", NodeID: "node-1", Agent: "planner",
		Workspace: domain.WorkspaceRef{ID: "workspace-1", Path: "D:/managed/workspace-1"},
		Budget:    domain.DefaultRunBudget(2),
	}
}
