package graph

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"forgeflow/internal/apperror"
	"forgeflow/internal/domain"
)

func TestDetectTestCommandUsesAllowlistedBuildConfiguration(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	command, err := DetectTestCommand(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if command.Program != "go" || len(command.Args) != 2 || command.Args[0] != "test" || command.WorkingDir != "." {
		t.Fatalf("command=%+v", command)
	}
	if _, err := DetectTestCommand(t.TempDir()); !apperror.IsCode(err, apperror.CodeValidation) {
		t.Fatalf("missing configuration error=%v", err)
	}
}

func TestValidateDiffBudgetRejectsScopeAndResourceExcess(t *testing.T) {
	plan := domain.ExecutionPlan{FilesLikelyAffected: []string{"main.go"}}
	budget := domain.DefaultRunBudget(2)
	valid := domain.DiffArtifact{Patch: "--- a/main.go\n+++ b/main.go\n-old\n+new\n", Size: 40, ChangedFiles: []string{"main.go"}}
	if err := validateDiffBudget(valid, plan, budget); err != nil {
		t.Fatal(err)
	}
	offScope := valid
	offScope.ChangedFiles = []string{"other.go"}
	if err := validateDiffBudget(offScope, plan, budget); !apperror.IsCode(err, apperror.CodePolicyDenied) {
		t.Fatalf("scope error=%v", err)
	}
	overBudget := valid
	overBudget.Size = budget.MaxDiffBytes + 1
	if err := validateDiffBudget(overBudget, plan, budget); !apperror.IsCode(err, apperror.CodeBudget) {
		t.Fatalf("budget error=%v", err)
	}
}

func TestJudgeNodeRejectsTamperedHighRiskApproval(t *testing.T) {
	state := domain.NewRunState(domain.NewRunInput{Task: "test", RepositoryPath: "."})
	state.Workspace = &domain.WorkspaceRef{ID: "workspace-1", Path: "."}
	state.Plan = &domain.ExecutionPlan{Summary: "change", Assumptions: []string{}, FilesLikelyAffected: []string{"main.go"}, Steps: []domain.PlanStep{{ID: "one", Description: "change", AcceptanceCriteria: []string{"works"}, DependsOn: []string{}}}, Risks: []domain.PlanRisk{}, TestStrategy: []string{"go test ./..."}}
	state.Diff = &domain.DiffArtifact{Patch: "diff --git a/main.go b/main.go\n--- a/main.go\n+++ b/main.go\n@@ -1 +1 @@\n-old\n+new\n", ChangedFiles: []string{"main.go"}}
	state.TestAssessment = &domain.TestAssessment{ToolCallID: "tool-1", ExitCode: 0, Passed: true}
	state.ReviewResult = &domain.ReviewResult{Summary: "pass", Findings: []domain.ReviewFinding{}}
	state.SecurityResult = &domain.SecurityResult{Summary: "review", Findings: []domain.SecurityFinding{{
		ID: "SEC-HUMAN", Severity: domain.SeverityHigh, File: "main.go", Title: "uncertain boundary",
		Evidence: "upstream contract unavailable", Impact: "authorization bypass",
		Recommendation: "verify upstream contract", Confirmed: false, HumanReview: true,
	}}}
	result := judgeNode(context.Background(), state)
	if result.Type != ResultInterrupted || result.Approval == nil || result.Approval.InputSHA256 == "" {
		t.Fatalf("result=%+v", result)
	}
	state.PendingApproval = result.Approval
	state.PendingApproval.Status = domain.ApprovalApproved
	state.PendingApproval.InputSHA256 = "tampered"
	result = judgeNode(context.Background(), state)
	if result.Type != ResultFatalError || !apperror.IsCode(result.Err, apperror.CodePolicyDenied) {
		t.Fatalf("tampered approval result=%s err=%v", result.Type, result.Err)
	}
}
