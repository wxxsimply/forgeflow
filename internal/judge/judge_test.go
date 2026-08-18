package judge

import (
	"strings"
	"testing"

	"forgeflow/internal/domain"
)

func TestEvaluatePassesOnlyCompleteCleanEvidence(t *testing.T) {
	input := validInput()
	decision := Evaluate(input)
	if decision.Action != domain.JudgePass || decision.InputSHA256 == "" {
		t.Fatalf("decision=%+v", decision)
	}
	second := Evaluate(input)
	if second.InputSHA256 != decision.InputSHA256 || strings.Join(second.Reasons, "|") != strings.Join(decision.Reasons, "|") {
		t.Fatalf("judge is not deterministic: first=%+v second=%+v", decision, second)
	}
}

func TestEvaluateUsesRepairBudgetForConfirmedFailures(t *testing.T) {
	for name, mutate := range map[string]func(*Input){
		"test failure": func(input *Input) { input.Test.ExitCode = 1; input.Test.Passed = false },
		"blocking review": func(input *Input) {
			input.Review.Findings = []domain.ReviewFinding{{ID: "REV-1", Severity: domain.SeverityBlocking, File: "main.go", Title: "wrong result", Evidence: "return value is wrong", FailureMode: "request fails", Recommendation: "return the expected value", Confirmed: true}}
		},
		"confirmed security": func(input *Input) {
			input.Security.Findings = []domain.SecurityFinding{{ID: "SEC-1", Severity: domain.SeverityHigh, File: "main.go", Title: "command injection", Evidence: "input reaches shell", Impact: "arbitrary commands", Recommendation: "remove shell", Confirmed: true}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			input := validInput()
			mutate(&input)
			if decision := Evaluate(input); decision.Action != domain.JudgeRepair {
				t.Fatalf("decision=%+v", decision)
			}
			input.RepairCount = input.Budget.MaxRepairs
			if decision := Evaluate(input); decision.Action != domain.JudgeFail {
				t.Fatalf("exhausted decision=%+v", decision)
			}
		})
	}
}

func TestEvaluateEscalatesUncertainHighFinding(t *testing.T) {
	input := validInput()
	input.Security.Findings = []domain.SecurityFinding{{
		ID: "SEC-UNCERTAIN", Severity: domain.SeverityHigh, File: "main.go", Title: "possible authorization bypass",
		Evidence: "caller identity is not visible in this diff", Impact: "cross-tenant access",
		Recommendation: "verify the upstream identity contract", Confirmed: false, HumanReview: true,
	}}
	if decision := Evaluate(input); decision.Action != domain.JudgeHumanReview {
		t.Fatalf("decision=%+v", decision)
	}
}

func TestEvaluateCannotBeOverriddenByPassingSecurityModel(t *testing.T) {
	input := validInput()
	input.Diff.Patch = "diff --git a/main.go b/main.go\n--- a/main.go\n+++ b/main.go\n@@ -1 +1,2 @@\n package main\n+var api_key = \"supersecretvalue\"\n"
	input.Diff.Size = int64(len(input.Diff.Patch))
	input.Security = &domain.SecurityResult{Summary: "model says pass", Findings: []domain.SecurityFinding{}}
	decision := Evaluate(input)
	if decision.Action != domain.JudgeRepair || len(decision.FindingIDs) == 0 || !strings.HasPrefix(decision.FindingIDs[0], "SEC-DET-") {
		t.Fatalf("decision=%+v", decision)
	}
}

func TestEvaluateHardFailsAssessmentAndPolicyViolations(t *testing.T) {
	input := validInput()
	input.AssessmentErrors = map[string]string{"security": "deadline exceeded"}
	if decision := Evaluate(input); decision.Action != domain.JudgeFail {
		t.Fatalf("assessment decision=%+v", decision)
	}
	input = validInput()
	input.Diff.ChangedFiles = []string{".env"}
	input.Plan.FilesLikelyAffected = []string{".env"}
	if decision := Evaluate(input); decision.Action != domain.JudgeFail {
		t.Fatalf("policy decision=%+v", decision)
	}
	input = validInput()
	input.Budget.ModelCalls = input.Budget.MaxModelCalls + 1
	if decision := Evaluate(input); decision.Action != domain.JudgeFail || !strings.Contains(strings.Join(decision.Reasons, " "), "model call budget") {
		t.Fatalf("budget decision=%+v", decision)
	}
}

func validInput() Input {
	diff := &domain.DiffArtifact{Patch: "diff --git a/main.go b/main.go\n--- a/main.go\n+++ b/main.go\n@@ -1 +1 @@\n-old\n+new\n", ChangedFiles: []string{"main.go"}}
	diff.Size = int64(len(diff.Patch))
	return Input{
		Test:             &domain.TestAssessment{ToolCallID: "tool-1", ExitCode: 0, Passed: true},
		Review:           &domain.ReviewResult{Summary: "pass", Findings: []domain.ReviewFinding{}},
		Security:         &domain.SecurityResult{Summary: "pass", Findings: []domain.SecurityFinding{}},
		AssessmentErrors: map[string]string{}, Diff: diff,
		Plan:   &domain.ExecutionPlan{Summary: "change main", Assumptions: []string{}, FilesLikelyAffected: []string{"main.go"}, Steps: []domain.PlanStep{{ID: "one", Description: "change", AcceptanceCriteria: []string{"works"}, DependsOn: []string{}}}, Risks: []domain.PlanRisk{}, TestStrategy: []string{"go test ./..."}},
		Budget: domain.DefaultRunBudget(2), WorkspaceID: "workspace-1",
	}
}
