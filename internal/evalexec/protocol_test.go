package evalexec

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"forgeflow/internal/domain"
	fulleval "forgeflow/internal/eval"
	"forgeflow/internal/model"
)

func TestDevelopUsesConfiguredProductionPromptAndSchema(t *testing.T) {
	root := t.TempDir()
	workspacePath := filepath.Join(root, "workspace")
	for _, path := range []string{workspacePath, filepath.Join(root, "fixture"), filepath.Join(root, "grader")} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "main.go"), []byte("package fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	implementation := domain.ImplementationResult{
		Summary: "update main", ChangedFiles: []string{"main.go"},
		Patch:    "diff --git a/main.go b/main.go\n--- a/main.go\n+++ b/main.go\n@@ -1 +1 @@\n-package fixture\n+package updated\n",
		Evidence: []string{}, UnresolvedIssues: []string{}, RequestedApprovals: []string{},
	}
	output, err := json.Marshal(implementation)
	if err != nil {
		t.Fatal(err)
	}
	provider := &model.FakeProvider{Responses: []model.Response{{Status: "completed", OutputText: string(output)}}}
	pricing := UsagePricing{
		Mode: PricingModeCacheHitMiss, InputUSDPerMillionTokens: 1, CachedUSDPerMillionTokens: 1,
		OutputUSDPerMillionTokens: 1, Source: "https://example.com/pricing", ValidFrom: time.Now().UTC().Add(-time.Minute), ValidUntil: time.Now().UTC().Add(time.Hour),
	}
	budget, err := NewCostBudget(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	core, err := newCore(Options{
		Provider: provider, Pricing: pricing, CostBudget: budget,
		FixtureRepository: filepath.Join(root, "fixture"), GraderRepository: filepath.Join(root, "grader"),
		WorkspaceRoot: filepath.Join(root, "worktrees"), Model: "model-v1", MaxOutputTokens: 1000,
		DeveloperPromptVersion: "developer/v2",
	})
	if err != nil {
		t.Fatal(err)
	}
	meter := &meter{provider: provider, pricing: pricing, budget: budget}
	evalCase := fulleval.Case{
		ID: "feature-01", Task: "update main", ForbiddenFiles: []string{".env"},
		ValidationCommand: fulleval.Command{Program: "go", Args: []string{"test", "./..."}},
		Budget:            fulleval.Budget{MaxIterations: 1, MaxChangedFiles: 1, MaxDiffLines: 10, MaxCostUSD: 1},
	}
	result, err := core.develop(context.Background(), meter, evalCase, plan{
		Decision: fulleval.DecisionImplement, Rationale: "bounded update",
		FilesLikelyAffected: []string{"main.go"}, Steps: []string{"update main"},
	}, domain.WorkspaceRef{ID: "workspace-1", Path: workspacePath, BaseCommit: strings.Repeat("a", 40)}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Patch != implementation.Patch {
		t.Fatalf("patch=%q", result.Patch)
	}
	request := provider.Requests[0]
	if !strings.Contains(request.Instructions, "exactly these six top-level keys") ||
		!strings.Contains(request.Input, `"approvedPlan"`) ||
		!strings.Contains(string(request.ResponseFormat.Schema), "requestedApprovals") {
		t.Fatalf("request is not bound to production developer/v2: %+v", request)
	}
	if err := core.validateDeveloperPromptBinding(fulleval.Configuration{PromptVersions: map[string]string{"developer": "developer/v1"}}); err == nil {
		t.Fatal("expected mismatched evidence prompt version to be rejected")
	}
	before := provider.CallCount()
	executor := &PlannerDeveloperExecutor{core: core}
	if _, err := executor.Execute(context.Background(), evalCase, fulleval.Configuration{
		Mode: fulleval.ModePlannerDeveloper, PromptVersions: map[string]string{"developer": "developer/v1"},
	}); err == nil {
		t.Fatal("expected executor to reject mismatched evidence prompt version")
	}
	if provider.CallCount() != before {
		t.Fatal("provider was called before prompt binding mismatch was rejected")
	}
}

func TestEvalDeveloperPlanRejectsForbiddenPathBeforeContextBuild(t *testing.T) {
	_, err := evalDeveloperPlan(plan{
		Decision: fulleval.DecisionImplement, Rationale: "change protected file",
		FilesLikelyAffected: []string{"migrations/000001.sql"}, Steps: []string{"edit migration"},
	}, fulleval.Case{
		ForbiddenFiles: []string{"migrations"}, ValidationCommand: fulleval.Command{Program: "go", Args: []string{"test", "./..."}},
		Budget: fulleval.Budget{MaxIterations: 1, MaxChangedFiles: 1, MaxDiffLines: 10, MaxCostUSD: 1},
	})
	if err == nil {
		t.Fatal("expected forbidden planner path to be rejected")
	}
}

func TestFileSetChecksAreOrderIndependentAndBounded(t *testing.T) {
	if !sameFileSet([]string{"b.go", "a.go"}, []string{"a.go", "b.go"}) {
		t.Fatal("same file set was rejected")
	}
	if sameFileSet([]string{"a.go"}, []string{"a.go", "b.go"}) {
		t.Fatal("different file sets were accepted")
	}
	if !filesWithinApprovedSet([]string{"a.go"}, []string{"a.go", "b.go"}) ||
		filesWithinApprovedSet([]string{"a.go", "other.go"}, []string{"a.go", "b.go"}) {
		t.Fatal("approved file subset check is incorrect")
	}
}

func TestValidateChangeSetRejectsForbiddenAndBudgetOverflow(t *testing.T) {
	evalCase := fulleval.Case{ForbiddenFiles: []string{".env", "migrations"}, Budget: fulleval.Budget{MaxChangedFiles: 1, MaxDiffLines: 2}}
	if err := validateChangeSet(solution{Decision: fulleval.DecisionImplement, ChangedFiles: []string{".env"}, Patch: "--- a/.env\n+++ b/.env\n+x=1\n"}, evalCase); err == nil {
		t.Fatal("expected forbidden path rejection")
	}
	if err := validateChangeSet(solution{Decision: fulleval.DecisionImplement, ChangedFiles: []string{"a.go"}, Patch: "--- a/a.go\n+++ b/a.go\n+a\n+b\n+c\n"}, evalCase); err == nil {
		t.Fatal("expected diff budget rejection")
	}
}

func TestMeterRefusesCallBeforeProviderWhenCostBudgetCannotReserveMaximum(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	provider := &model.FakeProvider{Responses: []model.Response{{Status: "completed"}}}
	budget, err := NewCostBudget(0.001, 0)
	if err != nil {
		t.Fatal(err)
	}
	meter := &meter{
		provider: provider,
		pricing: UsagePricing{
			Mode: PricingModeCacheHitMiss, InputUSDPerMillionTokens: 10,
			CachedUSDPerMillionTokens: 2, OutputUSDPerMillionTokens: 30,
			Source: "https://example.com/pricing", ValidFrom: now.Add(-time.Minute), ValidUntil: now.Add(time.Hour),
		},
		budget: budget,
		now:    func() time.Time { return now },
	}
	_, err = meter.call(context.Background(), model.Request{Model: "test", Input: "small", MaxOutputTokens: 100}, "test", time.Second)
	if !errors.Is(err, ErrCostBudgetExceeded) {
		t.Fatalf("error=%v want cost budget exceeded", err)
	}
	if provider.CallCount() != 0 {
		t.Fatal("provider was called without a conservative cost reservation")
	}
}

func TestMeterCommitsMeasuredCostToSharedBudget(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	provider := &model.FakeProvider{Responses: []model.Response{{Status: "completed", Usage: model.Usage{InputTokens: 100, OutputTokens: 20}}}}
	budget, err := NewCostBudget(1, 0.25)
	if err != nil {
		t.Fatal(err)
	}
	pricing := UsagePricing{
		Mode: PricingModeCacheHitMiss, InputUSDPerMillionTokens: 10,
		CachedUSDPerMillionTokens: 2, OutputUSDPerMillionTokens: 30,
		Source: "https://example.com/pricing", ValidFrom: now.Add(-time.Minute), ValidUntil: now.Add(time.Hour),
	}
	meter := &meter{provider: provider, pricing: pricing, budget: budget, now: func() time.Time { return now }}
	if _, err := meter.call(context.Background(), model.Request{Model: "test", MaxOutputTokens: 100}, "test", time.Second); err != nil {
		t.Fatal(err)
	}
	measured := (100.0*10 + 20.0*30) / 1_000_000
	spent, remaining := budget.Snapshot()
	if math.Abs(spent-(0.25+measured)) > 1e-12 {
		t.Fatalf("spent=%f want=%f", spent, 0.25+measured)
	}
	if math.Abs(remaining-(0.75-measured)) > 1e-12 {
		t.Fatalf("remaining=%f want=%f", remaining, 0.75-measured)
	}
}

func TestNewCostBudgetRejectsAlreadyExceededCampaign(t *testing.T) {
	_, err := NewCostBudget(1, 1.01)
	if !errors.Is(err, ErrCostBudgetExceeded) {
		t.Fatalf("error=%v want cost budget exceeded", err)
	}
}

func TestExecutorRequiresCostBudget(t *testing.T) {
	_, err := NewMux(Options{Provider: &model.FakeProvider{}})
	if err == nil || !strings.Contains(err.Error(), "cost budget is required") {
		t.Fatalf("error=%v want required cost budget", err)
	}
}

func TestMeterUsesCacheReadWritePrices(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	provider := &model.FakeProvider{Responses: []model.Response{{Status: "completed", Usage: model.Usage{InputTokens: 100, CachedInputTokens: 30, CacheWriteInputTokens: 20, OutputTokens: 20}}}}
	meter := &meter{provider: provider, pricing: UsagePricing{Mode: PricingModeCacheReadWrite, InputUSDPerMillionTokens: 10, CachedUSDPerMillionTokens: 2, CacheWriteUSDPerMillion: 12.5, OutputUSDPerMillionTokens: 30, Source: "https://example.com/pricing", ValidFrom: now.Add(-time.Minute), ValidUntil: now.Add(time.Hour)}, now: func() time.Time { return now }}
	if _, err := meter.call(context.Background(), model.Request{Model: "test"}, "test", time.Second); err != nil {
		t.Fatal(err)
	}
	want := (50.0*10 + 30.0*2 + 20.0*12.5 + 20.0*30) / 1_000_000
	if math.Abs(meter.cost-want) > 1e-12 {
		t.Fatalf("cost=%f want=%f", meter.cost, want)
	}
	if meter.usage.CacheWriteInputTokens != 20 {
		t.Fatalf("cache write tokens=%d want=20", meter.usage.CacheWriteInputTokens)
	}
}

func TestMeterUsesCacheHitMissPrices(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	provider := &model.FakeProvider{Responses: []model.Response{{Status: "completed", Usage: model.Usage{InputTokens: 100, CachedInputTokens: 40, OutputTokens: 20}}}}
	meter := &meter{provider: provider, pricing: UsagePricing{Mode: PricingModeCacheHitMiss, InputUSDPerMillionTokens: 10, CachedUSDPerMillionTokens: 2, OutputUSDPerMillionTokens: 30, Source: "https://example.com/pricing", ValidFrom: now.Add(-time.Minute), ValidUntil: now.Add(time.Hour)}, now: func() time.Time { return now }}
	if _, err := meter.call(context.Background(), model.Request{Model: "test"}, "test", time.Second); err != nil {
		t.Fatal(err)
	}
	want := (60.0*10 + 40.0*2 + 20.0*30) / 1_000_000
	if math.Abs(meter.cost-want) > 1e-12 {
		t.Fatalf("cost=%f want=%f", meter.cost, want)
	}
}

func TestMeterRefusesCallOutsidePricingWindow(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	provider := &model.FakeProvider{Responses: []model.Response{{Status: "completed"}}}
	meter := &meter{provider: provider, pricing: UsagePricing{Mode: PricingModeCacheHitMiss, InputUSDPerMillionTokens: 10, CachedUSDPerMillionTokens: 2, OutputUSDPerMillionTokens: 30, Source: "https://example.com/pricing", ValidFrom: now.Add(-time.Minute), ValidUntil: now.Add(time.Second)}, now: func() time.Time { return now }}
	if _, err := meter.call(context.Background(), model.Request{Model: "test"}, "test", time.Minute); err == nil {
		t.Fatal("expected pricing-window rejection")
	}
	if provider.CallCount() != 0 {
		t.Fatal("provider was called after the pricing window became unsafe")
	}
}

func TestRedactRemovesPathsKeysAndSecrets(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test-secret-value-123456789")
	options := Options{WorkspaceRoot: `D:\private\work`, FixtureRepository: `D:\private\fixture`, GraderRepository: `D:\private\grader`}
	value := redact(`failure D:\private\work\case sk-test-secret-value-123456789`, `D:\private\work\case`, options)
	if strings.Contains(value, `D:\private`) || strings.Contains(value, "sk-test") {
		t.Fatalf("redaction leaked sensitive value: %s", value)
	}
}

func TestDetectSecretAndDiffLines(t *testing.T) {
	if !detectSecret(`api_key = "abcdefghijklmnop"`) {
		t.Fatal("expected secret detection")
	}
	if got := countDiffLines("--- a/x\n+++ b/x\n-old\n+new\n context\n"); got != 2 {
		t.Fatalf("diff lines=%d", got)
	}
}
