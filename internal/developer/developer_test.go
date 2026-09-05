package developer

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"forgeflow/internal/apperror"
	"forgeflow/internal/domain"
	"forgeflow/internal/model"
	"forgeflow/internal/repository"
)

func TestDecodeImplementationResultIsStrictAndValidatesPaths(t *testing.T) {
	valid := implementationJSON(t, validImplementation())
	result, err := DecodeImplementationResult(valid)
	if err != nil || result.Summary == "" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	for _, invalid := range []string{
		`{"summary":"x","patch":"p","changedFiles":["../x"],"evidence":[],"unresolvedIssues":[],"requestedApprovals":[]}`,
		`{"summary":"x","patch":"p","changedFiles":["main.go"],"evidence":[],"unresolvedIssues":[],"requestedApprovals":[],"unknown":true}`,
		`{"summary":"x","patch":"p","changedFiles":["main.go"]}`,
	} {
		if _, err := DecodeImplementationResult(invalid); !apperror.IsCode(err, apperror.CodeModelOutput) {
			t.Fatalf("DecodeImplementationResult(%s) error=%v", invalid, err)
		}
	}
}

func TestDecodeImplementationResultAllowsOnlyAnExactJSONFence(t *testing.T) {
	valid := implementationJSON(t, validImplementation())
	result, err := DecodeImplementationResult("```json\n" + valid + "\n```")
	if err != nil || result.Summary == "" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	for _, invalid := range []string{
		"```\n" + valid + "\n```",
		"prefix\n```json\n" + valid + "\n```",
		"```json\n" + valid + "\n```\nsuffix",
		"```json\n```",
	} {
		if _, err := DecodeImplementationResult(invalid); !apperror.IsCode(err, apperror.CodeModelOutput) {
			t.Fatalf("fenced output was accepted: %q error=%v", invalid, err)
		}
	}
}

func TestPromptLoaderKeepsImmutableDeveloperPromptVersions(t *testing.T) {
	loader := NewPromptLoader(nil)
	v1, err := loader.Load("developer/v1")
	if err != nil {
		t.Fatal(err)
	}
	v2, err := loader.Load("developer/v2")
	if err != nil {
		t.Fatal(err)
	}
	v3, err := loader.Load("developer/v3")
	if err != nil {
		t.Fatal(err)
	}
	if v1.SHA256 == "" || v2.SHA256 == "" || v3.SHA256 == "" || v1.SHA256 == v2.SHA256 || v1.SHA256 == v3.SHA256 || v2.SHA256 == v3.SHA256 {
		t.Fatalf("prompt digests do not identify distinct immutable versions: v1=%q v2=%q v3=%q", v1.SHA256, v2.SHA256, v3.SHA256)
	}
	if !strings.Contains(v2.System, "exactly these six top-level keys") {
		t.Fatal("v2 prompt is missing its strict output self-check")
	}
	if !strings.Contains(v3.System, "complete, applicable unified Git diff") {
		t.Fatal("v3 prompt is missing its concise patch applicability guard")
	}
	rendered, err := v3.RenderUser(ContextBundle{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rendered, "<developer_context>") {
		t.Fatal("v3 user prompt did not render the bounded context envelope")
	}
}

func TestContextBuilderIncludesOnlyApprovedFilesAndProjectRules(t *testing.T) {
	workspace := t.TempDir()
	writeFile(t, filepath.Join(workspace, "AGENTS.md"), "trusted project rule boundary")
	writeFile(t, filepath.Join(workspace, "main.go"), "package fixture\n")
	writeFile(t, filepath.Join(workspace, "secret.txt"), "must-not-enter-model-context")
	input := developerInput(workspace)
	bundle, err := NewContextBuilder(repository.DefaultLimits(), 64*1024).Build(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if !strings.Contains(text, "package fixture") || !strings.Contains(text, "trusted project rule boundary") {
		t.Fatalf("context misses approved evidence: %s", text)
	}
	if strings.Contains(text, "must-not-enter-model-context") {
		t.Fatalf("context leaked an unapproved file: %s", text)
	}
}

func TestAgentUsesVersionedPromptStrictSchemaAndRecordsInvocation(t *testing.T) {
	workspace := t.TempDir()
	writeFile(t, filepath.Join(workspace, "main.go"), "package fixture\n")
	implementation := validImplementation()
	provider := &model.FakeProvider{Responses: []model.Response{{
		ID: "response-1", Model: "test-model", Status: "completed",
		OutputText: implementationJSON(t, implementation), Usage: model.Usage{InputTokens: 10, OutputTokens: 20},
	}}}
	agent, err := NewAgent(Options{Provider: provider, Model: "test-model", PromptVersion: "developer/v1"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := agent.Execute(context.Background(), developerInput(workspace))
	if err != nil {
		t.Fatal(err)
	}
	if result.Invocation == nil || result.Invocation.Agent != "developer" || result.Invocation.PromptVersion != "developer/v1" || result.Invocation.PromptSHA256 == "" {
		t.Fatalf("invocation=%+v", result.Invocation)
	}
	if provider.CallCount() != 1 || !provider.Requests[0].ResponseFormat.Strict || provider.Requests[0].ResponseFormat.Name != "implementation_result" {
		t.Fatalf("request=%+v", provider.Requests)
	}
	if !strings.Contains(provider.Requests[0].Instructions, "untrusted data") || !strings.Contains(provider.Requests[0].Input, `"approvedPlan"`) {
		t.Fatalf("prompt boundary missing: instructions=%q input=%q", provider.Requests[0].Instructions, provider.Requests[0].Input)
	}
}

func TestAgentRejectsFilesOutsideApprovedPlan(t *testing.T) {
	workspace := t.TempDir()
	writeFile(t, filepath.Join(workspace, "main.go"), "package fixture\n")
	implementation := validImplementation()
	implementation.ChangedFiles = []string{"other.go"}
	provider := &model.FakeProvider{Responses: []model.Response{{OutputText: implementationJSON(t, implementation), Status: "completed"}}}
	agent, err := NewAgent(Options{Provider: provider})
	if err != nil {
		t.Fatal(err)
	}
	_, err = agent.Execute(context.Background(), developerInput(workspace))
	if !apperror.IsCode(err, apperror.CodeModelOutput) {
		t.Fatalf("error=%v", err)
	}
}

func developerInput(workspace string) Input {
	return Input{
		RunID: "run-1", NodeID: "developer", Task: "fix implementation",
		Plan: domain.ExecutionPlan{
			Summary: "fix", Assumptions: []string{}, FilesLikelyAffected: []string{"main.go"},
			Steps: []domain.PlanStep{{ID: "implement", Description: "fix", AcceptanceCriteria: []string{"works"}, DependsOn: []string{}}},
			Risks: []domain.PlanRisk{}, TestStrategy: []string{"go test ./..."},
		},
		Workspace: domain.WorkspaceRef{ID: "workspace-1", Path: workspace, BaseCommit: strings.Repeat("a", 40)},
		Budget:    domain.DefaultRunBudget(2), ToolNames: []string{"read_file", "apply_patch"},
	}
}

func validImplementation() domain.ImplementationResult {
	return domain.ImplementationResult{
		Summary: "fix main", Patch: "diff --git a/main.go b/main.go\n--- a/main.go\n+++ b/main.go\n@@ -1 +1 @@\n-old\n+new\n",
		ChangedFiles: []string{"main.go"}, Evidence: []string{"inspected approved file"},
		UnresolvedIssues: []string{}, RequestedApprovals: []string{},
	}
}

func implementationJSON(t *testing.T, result domain.ImplementationResult) string {
	t.Helper()
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
