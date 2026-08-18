package reviewer

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

func TestAgentReceivesOnlyIndependentEvidenceBundle(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "AGENTS.md"), []byte("review-project-rule"), 0o600); err != nil {
		t.Fatal(err)
	}
	provider := &model.FakeProvider{Responses: []model.Response{{Status: "completed", OutputText: `{"summary":"pass","findings":[]}`}}}
	agent, err := NewAgent(Options{Provider: provider})
	if err != nil {
		t.Fatal(err)
	}
	result, err := agent.Execute(context.Background(), reviewerInput(workspace))
	if err != nil || result.Review.Summary != "pass" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	request := provider.Requests[0]
	if !request.ResponseFormat.Strict || request.ResponseFormat.Name != "review_result" {
		t.Fatalf("request=%+v", request)
	}
	if strings.Contains(request.Input, "developer-private-marker") || strings.Contains(request.Input, "implementation") || !strings.Contains(request.Input, "review-project-rule") {
		t.Fatalf("reviewer context boundary failed: %s", request.Input)
	}
}

func TestDecodeReviewResultIsStrict(t *testing.T) {
	if _, err := DecodeReviewResult(`{"summary":"x","findings":[],"unknown":true}`); err == nil {
		t.Fatal("unknown field was accepted")
	}
	valid := domain.ReviewResult{Summary: "pass", Findings: []domain.ReviewFinding{}}
	data, _ := json.Marshal(valid)
	if _, err := DecodeReviewResult(string(data)); err != nil {
		t.Fatal(err)
	}
}

func reviewerInput(workspace string) assessment.Input {
	diff := domain.DiffArtifact{Patch: "diff --git a/main.go b/main.go\n--- a/main.go\n+++ b/main.go\n@@ -1 +1 @@\n-old\n+new\n", ChangedFiles: []string{"main.go"}, SHA256: strings.Repeat("a", 64)}
	diff.Size = int64(len(diff.Patch))
	return assessment.Input{
		RunID: "run-1", NodeID: "reviewer", Task: "change main",
		Plan:      domain.ExecutionPlan{Summary: "change", Assumptions: []string{}, FilesLikelyAffected: []string{"main.go"}, Steps: []domain.PlanStep{{ID: "one", Description: "change", AcceptanceCriteria: []string{"works"}, DependsOn: []string{}}}, Risks: []domain.PlanRisk{}, TestStrategy: []string{"go test ./..."}},
		Workspace: domain.WorkspaceRef{ID: "workspace-1", Path: workspace, BaseCommit: strings.Repeat("a", 40)},
		Diff:      diff, Test: domain.TestAssessment{ToolCallID: "tool-1", ExitCode: 0, Passed: true}, Budget: domain.DefaultRunBudget(2),
	}
}
