package domain

import "testing"

func TestExecutionPlanValidation(t *testing.T) {
	t.Parallel()
	plan := ExecutionPlan{Summary: "test", TestStrategy: []string{"go test ./..."}}
	if err := plan.Validate(); err == nil {
		t.Fatal("expected a plan without steps to be rejected")
	}
}

func TestExecutionPlanValidationRejectsDuplicateUnknownAndCyclicDependencies(t *testing.T) {
	validPlan := func() ExecutionPlan {
		return ExecutionPlan{
			Summary: "fixture", TestStrategy: []string{"go test ./..."},
			Steps: []PlanStep{
				{ID: "one", Description: "one", AcceptanceCriteria: []string{"done"}, DependsOn: []string{}},
				{ID: "two", Description: "two", AcceptanceCriteria: []string{"done"}, DependsOn: []string{"one"}},
			},
		}
	}
	duplicate := validPlan()
	duplicate.Steps[1].ID = "one"
	unknown := validPlan()
	unknown.Steps[1].DependsOn = []string{"missing"}
	cycle := validPlan()
	cycle.Steps[0].DependsOn = []string{"two"}
	tests := map[string]ExecutionPlan{
		"duplicate": duplicate,
		"unknown":   unknown,
		"cycle":     cycle,
	}
	for name, plan := range tests {
		t.Run(name, func(t *testing.T) {
			if err := plan.Validate(); err == nil {
				t.Fatal("Validate() accepted invalid dependency structure")
			}
		})
	}
}

func TestRunBudgetTracksModelUsageLimits(t *testing.T) {
	budget := DefaultRunBudget(2)
	state := NewRunState(NewRunInput{Task: "fixture", RepositoryPath: ".", Budget: &budget})
	state.RecordModelInvocation(ModelInvocation{InputTokens: budget.MaxInputTokens + 1, OutputTokens: 1, EstimatedCostUSD: 0.1})
	if allowed, reason := state.Budget.ModelUsageAllowed(); allowed || reason != "model input token budget exceeded" {
		t.Fatalf("allowed = %t reason = %q", allowed, reason)
	}
	if state.Budget.ModelCalls != 1 || len(state.ModelInvocations) != 1 {
		t.Fatalf("budget = %+v invocations = %+v", state.Budget, state.ModelInvocations)
	}
}

func TestExecutionPlanValidationRejectsUnsafeAffectedPaths(t *testing.T) {
	for _, file := range []string{"../secret", "/absolute", `windows\\path.go`, "dir/../file.go", ""} {
		plan := ExecutionPlan{
			Summary: "fixture", FilesLikelyAffected: []string{file}, TestStrategy: []string{"test"},
			Steps: []PlanStep{{ID: "one", Description: "one", AcceptanceCriteria: []string{"done"}, DependsOn: []string{}}},
		}
		if err := plan.Validate(); err == nil {
			t.Fatalf("Validate() accepted affected path %q", file)
		}
	}
}
