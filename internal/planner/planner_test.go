package planner

import (
	"context"
	"errors"
	"strings"
	"testing"

	"forgeflow/internal/apperror"
	"forgeflow/internal/domain"
	"forgeflow/internal/model"
)

func TestAgentUsesProviderSchemaAndRecordsUsage(t *testing.T) {
	fake := &model.FakeProvider{Responses: []model.Response{{
		ID: "resp-fixture", Model: "gpt-fixture", Status: "completed", OutputText: validPlanJSON(),
		Usage: model.Usage{InputTokens: 1_000, OutputTokens: 500, CachedInputTokens: 100, ReasoningTokens: 25},
	}}}
	agent, err := NewAgent(Options{
		Provider: fake, Model: "gpt-fixture", PromptVersion: "planner/v1",
		Pricing: model.Pricing{InputUSDPerMillionTokens: 5, OutputUSDPerMillionTokens: 30},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := agent.CreatePlan(context.Background(), Input{
		Task: "add a fixture", RepositoryPath: ".", BaseRevision: "HEAD",
		RepositoryContext: "README fixture", Budget: domain.DefaultRunBudget(2),
	})
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}
	if result.Plan.Summary != "fixture" || result.Invocation == nil {
		t.Fatalf("result = %+v", result)
	}
	if result.Invocation.EstimatedCostUSD != 0.02 || result.Invocation.PromptVersion != "planner/v1" || result.Invocation.PromptSHA256 == "" {
		t.Fatalf("invocation = %+v", result.Invocation)
	}
	if len(fake.Requests) != 1 || !fake.Requests[0].ResponseFormat.Strict || string(fake.Requests[0].ResponseFormat.Schema) == "" {
		t.Fatalf("requests = %+v", fake.Requests)
	}
	if strings.Contains(fake.Requests[0].Input, "OPENAI_API_KEY") {
		t.Fatal("prompt unexpectedly contains a secret variable name")
	}
}

func TestAgentRejectsBudgetBeforeProviderCall(t *testing.T) {
	fake := &model.FakeProvider{}
	agent, err := NewAgent(Options{Provider: fake})
	if err != nil {
		t.Fatal(err)
	}
	budget := domain.DefaultRunBudget(2)
	budget.ModelCalls = budget.MaxModelCalls
	_, err = agent.CreatePlan(context.Background(), Input{Task: "fixture", RepositoryContext: "fixture", Budget: budget})
	if !apperror.IsCode(err, apperror.CodeBudget) || fake.CallCount() != 0 {
		t.Fatalf("error = %v calls = %d", err, fake.CallCount())
	}
}

func TestAgentMapsProviderErrorsAndPreservesInvocation(t *testing.T) {
	fake := &model.FakeProvider{Errors: []error{&model.Error{Kind: model.ErrorRateLimit, Message: "rate limited"}}}
	agent, err := NewAgent(Options{Provider: fake})
	if err != nil {
		t.Fatal(err)
	}
	result, err := agent.CreatePlan(context.Background(), Input{Task: "fixture", RepositoryContext: "fixture"})
	if !apperror.IsCode(err, apperror.CodeTransient) || result.Invocation == nil || result.Invocation.Status != "failed" {
		t.Fatalf("result = %+v error = %v", result, err)
	}
}

func TestOpenAIPlannerModeFailsClearlyWithoutKey(t *testing.T) {
	_, err := New("openai", Options{})
	if !apperror.IsCode(err, apperror.CodeUnauthorized) || !strings.Contains(apperror.MessageOf(err), "OPENAI_API_KEY") {
		t.Fatalf("error = %v message = %q", err, apperror.MessageOf(err))
	}
	var providerError *model.Error
	if !errors.As(err, &providerError) {
		t.Fatal("provider error was not retained")
	}
}
