package graph

import (
	"context"
	"testing"

	"forgeflow/internal/apperror"
	"forgeflow/internal/checkpoint"
	"forgeflow/internal/domain"
	"forgeflow/internal/model"
	"forgeflow/internal/planner"
)

func TestPlanningGraphStopsAfterModelUsageExceedsBudget(t *testing.T) {
	fake := &model.FakeProvider{Responses: []model.Response{{
		ID: "budget-response", Model: "fixture", Status: "completed",
		OutputText: `{
          "summary":"fixture","assumptions":[],"filesLikelyAffected":[],
          "steps":[{"id":"one","description":"one","acceptanceCriteria":["done"],"dependsOn":[]}],
          "risks":[],"testStrategy":["test"]
        }`,
		Usage: model.Usage{InputTokens: 11, OutputTokens: 1},
	}}}
	agent, err := planner.NewAgent(planner.Options{Provider: fake, Model: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	budget := domain.DefaultRunBudget(2)
	budget.MaxInputTokens = 10
	state := domain.NewRunState(domain.NewRunInput{Task: "fixture", RepositoryPath: ".", Budget: &budget})
	store := checkpoint.NewFileStore(t.TempDir())
	if err := store.Save(context.Background(), state, state.Version); err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(PlanningDefinition(agent), store)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Status != domain.StatusFailed || result.Error == nil || result.Error.Code != string(apperror.CodeBudget) {
		t.Fatalf("result = %+v", result)
	}
	if len(result.ModelInvocations) != 1 || result.Budget.InputTokens != 11 {
		t.Fatalf("invocations = %+v budget = %+v", result.ModelInvocations, result.Budget)
	}
}
