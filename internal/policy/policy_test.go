package policy

import (
	"context"
	"testing"

	"forgeflow/internal/domain"
)

func TestDefaultEngineDeniesUnknownUnsafeAndOverBudgetRequests(t *testing.T) {
	engine := DefaultEngine()
	tests := []struct {
		name   string
		mutate func(*Request)
		code   string
	}{
		{name: "unknown tool", mutate: func(request *Request) { request.ToolName = "delete_everything" }, code: "unknown_tool"},
		{name: "path traversal", mutate: func(request *Request) { request.Metadata.Paths = []string{"../secret"} }, code: "path_denied"},
		{name: "absolute path", mutate: func(request *Request) { request.Metadata.Paths = []string{"C:/Users/secret"} }, code: "path_denied"},
		{name: "network", mutate: func(request *Request) { request.Metadata.NetworkTargets = []string{"example.com"} }, code: "network_denied"},
		{name: "wrong agent", mutate: func(request *Request) { request.Agent = "reporter" }, code: "agent_not_allowed"},
		{name: "budget", mutate: func(request *Request) { request.Budget.MaxToolCalls, request.Budget.ToolCalls = 1, 1 }, code: "budget_exhausted"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := baseRequest("read_file")
			test.mutate(&request)
			decision, err := engine.Evaluate(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			if decision.Action != ActionDeny || decision.Code != test.code {
				t.Fatalf("decision = %+v, want deny/%s", decision, test.code)
			}
		})
	}
}

func TestDefaultEngineAllowsReadAndStrictCommands(t *testing.T) {
	engine := DefaultEngine()
	readDecision, err := engine.Evaluate(context.Background(), baseRequest("read_file"))
	if err != nil || readDecision.Action != ActionAllow {
		t.Fatalf("read decision = %+v, err = %v", readDecision, err)
	}

	command := baseRequest("run_test")
	command.Agent = "tester"
	command.Risk = domain.RiskMedium
	command.Metadata.Command = &Command{Program: "go", Args: []string{"test", "./..."}, WorkingDir: ".", EnvAllow: []string{"CI"}}
	decision, err := engine.Evaluate(context.Background(), command)
	if err != nil || decision.Action != ActionAllow {
		t.Fatalf("command decision = %+v, err = %v", decision, err)
	}

	for _, test := range []struct {
		mutate func(*Request)
		code   string
	}{
		{mutate: func(request *Request) { request.Metadata.Command.Program = "powershell" }, code: "command_denied"},
		{mutate: func(request *Request) { request.Metadata.Command.Args = []string{"test", "./...;curl", "example.com"} }, code: "command_denied"},
		{mutate: func(request *Request) {
			request.Metadata.Command.Program = "go"
			request.Metadata.Command.Args = []string{"run", "."}
		}, code: "command_denied"},
		{mutate: func(request *Request) { request.Metadata.Command.EnvAllow = []string{"OPENAI_API_KEY"} }, code: "environment_denied"},
	} {
		candidate := command
		commandCopy := *command.Metadata.Command
		candidate.Metadata.Command = &commandCopy
		test.mutate(&candidate)
		decision, err = engine.Evaluate(context.Background(), candidate)
		if err != nil {
			t.Fatal(err)
		}
		if decision.Action != ActionDeny || decision.Code != test.code {
			t.Fatalf("unsafe command decision = %+v", decision)
		}
	}
}

func TestApprovalIsBoundToExactAction(t *testing.T) {
	engine := DefaultEngine()
	request := baseRequest("apply_patch")
	request.Agent = "developer"
	request.Risk = domain.RiskHigh
	request.Metadata.Paths = []string{"internal/app.go"}
	decision, err := engine.Evaluate(context.Background(), request)
	if err != nil || decision.Action != ActionRequireApproval {
		t.Fatalf("decision = %+v, err = %v", decision, err)
	}

	request.Approval = &ApprovalEvidence{
		Approved: true, ToolName: request.ToolName, ToolVersion: request.ToolVersion,
		InputSHA256: request.InputSHA256, WorkspaceID: request.WorkspaceID,
		PolicyVersion: engine.Version(),
	}
	decision, err = engine.Evaluate(context.Background(), request)
	if err != nil || decision.Action != ActionAllow {
		t.Fatalf("approved decision = %+v, err = %v", decision, err)
	}

	for _, mutate := range []func(*ApprovalEvidence){
		func(evidence *ApprovalEvidence) { evidence.InputSHA256 = "tampered" },
		func(evidence *ApprovalEvidence) { evidence.WorkspaceID = "other" },
		func(evidence *ApprovalEvidence) { evidence.ToolVersion = "v2" },
		func(evidence *ApprovalEvidence) { evidence.PolicyVersion = "policy/old" },
	} {
		copyEvidence := *request.Approval
		mutate(&copyEvidence)
		candidate := request
		candidate.Approval = &copyEvidence
		decision, err = engine.Evaluate(context.Background(), candidate)
		if err != nil {
			t.Fatal(err)
		}
		if decision.Action != ActionDeny || decision.Code != "approval_mismatch" {
			t.Fatalf("tampered approval decision = %+v", decision)
		}
	}
}

func baseRequest(toolName string) Request {
	return Request{
		Phase: PhaseBefore, RunID: "run-1", Agent: "planner", ToolName: toolName,
		ToolVersion: "v1", Risk: domain.RiskLow, InputSHA256: "sha256",
		WorkspaceID: "workspace-1", WorkspacePath: "D:/managed/workspace-1",
		Metadata: Metadata{Paths: []string{"README.md"}}, Budget: domain.DefaultRunBudget(2),
	}
}
