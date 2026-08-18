package security

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"forgeflow/internal/assessment"
	"forgeflow/internal/domain"
	"forgeflow/internal/model"
)

func TestDeterministicFindingsDetectAddedSecretsAndShells(t *testing.T) {
	diff := domain.DiffArtifact{Patch: "diff --git a/main.go b/main.go\n--- a/main.go\n+++ b/main.go\n@@ -1 +1,3 @@\n package main\n+var api_key = \"supersecretvalue\"\n+exec.Command(\"sh\", \"-c\", userInput)\n", ChangedFiles: []string{"main.go"}}
	findings := DeterministicFindings(diff)
	if len(findings) != 2 {
		t.Fatalf("findings=%+v", findings)
	}
	for _, finding := range findings {
		if !finding.Confirmed || (finding.Severity != domain.SeverityHigh && finding.Severity != domain.SeverityCritical) {
			t.Fatalf("finding=%+v", finding)
		}
	}
}

func TestAgentUsesIndependentContextAndCannotDowngradeDeterministicFinding(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "AGENTS.md"), []byte("project-rule-marker"), 0o600); err != nil {
		t.Fatal(err)
	}
	provider := &model.FakeProvider{Responses: []model.Response{{
		Status: "completed", OutputText: `{"summary":"model says pass","findings":[]}`,
	}}}
	agent, err := NewAgent(Options{Provider: provider})
	if err != nil {
		t.Fatal(err)
	}
	result, err := agent.Execute(context.Background(), securityInput(workspace))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Security.Findings) != 1 || !strings.HasPrefix(result.Security.Findings[0].ID, "SEC-DET-") {
		t.Fatalf("result=%+v", result.Security)
	}
	if len(provider.Requests) != 1 || !provider.Requests[0].ResponseFormat.Strict {
		t.Fatalf("requests=%+v", provider.Requests)
	}
	if strings.Contains(provider.Requests[0].Input, "developer-private-marker") || !strings.Contains(provider.Requests[0].Input, "project-rule-marker") {
		t.Fatalf("security context boundary failed: %s", provider.Requests[0].Input)
	}
}

func TestDecodeSecurityResultRejectsUnknownFields(t *testing.T) {
	if _, err := DecodeSecurityResult(`{"summary":"x","findings":[],"unknown":true}`); err == nil {
		t.Fatal("unknown field was accepted")
	}
	valid := domain.SecurityResult{Summary: "pass", Findings: []domain.SecurityFinding{}}
	data, _ := json.Marshal(valid)
	if _, err := DecodeSecurityResult(string(data)); err != nil {
		t.Fatal(err)
	}
}

func securityInput(workspace string) assessment.Input {
	diff := domain.DiffArtifact{Patch: "diff --git a/main.go b/main.go\n--- a/main.go\n+++ b/main.go\n@@ -1 +1,2 @@\n package main\n+var api_key = \"supersecretvalue\"\n", ChangedFiles: []string{"main.go"}, SHA256: strings.Repeat("a", 64)}
	diff.Size = int64(len(diff.Patch))
	return assessment.Input{
		RunID: "run-1", NodeID: "security", Task: "secure change",
		Plan:      domain.ExecutionPlan{Summary: "change", Assumptions: []string{}, FilesLikelyAffected: []string{"main.go"}, Steps: []domain.PlanStep{{ID: "one", Description: "change", AcceptanceCriteria: []string{"works"}, DependsOn: []string{}}}, Risks: []domain.PlanRisk{}, TestStrategy: []string{"go test ./..."}},
		Workspace: domain.WorkspaceRef{ID: "workspace-1", Path: workspace, BaseCommit: strings.Repeat("a", 40)},
		Diff:      diff, Test: domain.TestAssessment{ToolCallID: "tool-1", ExitCode: 0, Passed: true}, Budget: domain.DefaultRunBudget(2),
	}
}
