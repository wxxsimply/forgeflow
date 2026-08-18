package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"forgeflow/internal/apperror"
	"forgeflow/internal/domain"
	"forgeflow/internal/policy"
	"forgeflow/internal/repository"
	"forgeflow/internal/sandbox"
)

type fakeInspector struct{}

func (fakeInspector) Inspect(_ context.Context, ref domain.RepositoryRef) (domain.RepositorySummary, error) {
	return domain.RepositorySummary{
		Root: ref.Path, RequestedRevision: ref.BaseRevision, BaseCommit: "base", HeadCommit: "head", Clean: true,
	}, nil
}

func TestRepositoryToolsUseSafeReaderAndStrictContracts(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, "AGENTS.md"), "root rules")
	writeTestFile(t, filepath.Join(workspace, "main.go"), "package main\n// forge needle\n")
	registry := NewRegistry()
	if err := RegisterRepositoryTools(registry, repository.DefaultLimits(), fakeInspector{}); err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(registry, policy.DefaultEngine())
	if err != nil {
		t.Fatal(err)
	}
	call := testCall()
	call.Workspace = domain.WorkspaceRef{ID: "workspace-1", Path: workspace}

	read, err := runtime.Execute(context.Background(), call, "read_file", json.RawMessage(`{"path":"main.go"}`))
	if err != nil || !json.Valid(read.Output) {
		t.Fatalf("read output = %s, err = %v", read.Output, err)
	}
	search, err := runtime.Execute(context.Background(), call, "search_code", json.RawMessage(`{"query":"needle","extensions":[".go"]}`))
	if err != nil {
		t.Fatal(err)
	}
	var matches []repository.SearchMatch
	if err := json.Unmarshal(search.Output, &matches); err != nil || len(matches) != 1 || matches[0].Path != "main.go" {
		t.Fatalf("matches = %+v, err = %v", matches, err)
	}
	rules, err := runtime.Execute(context.Background(), call, "read_project_rules", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	var documents projectRulesOutput
	if err := json.Unmarshal(rules.Output, &documents); err != nil || len(documents.Documents) != 1 {
		t.Fatalf("rules = %+v, err = %v", documents, err)
	}
	status, err := runtime.Execute(context.Background(), call, "inspect_git_status", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	var gitStatus gitStatusOutput
	if err := json.Unmarshal(status.Output, &gitStatus); err != nil || gitStatus.BaseCommit != "base" {
		t.Fatalf("status = %+v, err = %v", gitStatus, err)
	}

	if _, err := runtime.Execute(context.Background(), call, "read_file", json.RawMessage(`{"path":"../secret"}`)); !apperror.IsCode(err, apperror.CodePolicyDenied) {
		t.Fatalf("path escape error = %v", err)
	}
	if _, err := runtime.Execute(context.Background(), call, "read_file", json.RawMessage(`{"path":"main.go","unknown":true}`)); !apperror.IsCode(err, apperror.CodeValidation) {
		t.Fatalf("unknown field error = %v", err)
	}
}

func TestCommandToolsPassOnlyStructuredAllowlistedRequests(t *testing.T) {
	runner := &sandbox.FakeRunner{Results: []sandbox.Result{{ExitCode: 0, Stdout: "ok"}}}
	registry := NewRegistry()
	if err := RegisterCommandTools(registry, runner, testImageForTool); err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(registry, policy.DefaultEngine())
	if err != nil {
		t.Fatal(err)
	}
	call := testCall()
	call.Agent = "tester"
	input := json.RawMessage(`{"program":"go","args":["test","./..."],"workingDir":".","envAllow":["CI"],"timeoutMilliseconds":1000}`)
	invocation, err := runtime.Execute(context.Background(), call, "run_test", input)
	if err != nil || invocation.Audit.Status != domain.ToolCallSucceeded || runner.CallCount() != 1 {
		t.Fatalf("invocation = %+v, calls=%d, err=%v", invocation, runner.CallCount(), err)
	}
	request := runner.Requests[0]
	if request.Program != "go" || len(request.Args) != 2 || len(request.Environment) != 1 || request.Environment["CI"] != "true" {
		t.Fatalf("sandbox request = %+v", request)
	}

	unsafeInputs := []json.RawMessage{
		json.RawMessage(`{"program":"powershell","args":["-Command","whoami"]}`),
		json.RawMessage(`{"program":"go","args":["test","./...;whoami"]}`),
		json.RawMessage(`{"program":"go","args":["run","."]}`),
		json.RawMessage(`{"program":"go","args":["test"],"envAllow":["OPENAI_API_KEY"]}`),
	}
	for _, candidate := range unsafeInputs {
		if _, err := runtime.Execute(context.Background(), call, "run_test", candidate); !apperror.IsCode(err, apperror.CodePolicyDenied) {
			t.Fatalf("unsafe command %s error = %v", candidate, err)
		}
	}
	if runner.CallCount() != 1 {
		t.Fatalf("unsafe commands reached sandbox; calls=%d", runner.CallCount())
	}
}

const testImageForTool = "example/forgeflow@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

var _ repository.RepositoryInspector = fakeInspector{}
