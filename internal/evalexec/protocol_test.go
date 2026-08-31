package evalexec

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	fulleval "forgeflow/internal/eval"
	"forgeflow/internal/model"
)

func TestValidateChangeSetRejectsForbiddenAndBudgetOverflow(t *testing.T) {
	evalCase := fulleval.Case{ForbiddenFiles: []string{".env", "migrations"}, Budget: fulleval.Budget{MaxChangedFiles: 1, MaxDiffLines: 2}}
	if err := validateChangeSet(solution{Decision: fulleval.DecisionImplement, ChangedFiles: []string{".env"}, Patch: "--- a/.env\n+++ b/.env\n+x=1\n"}, evalCase); err == nil {
		t.Fatal("expected forbidden path rejection")
	}
	if err := validateChangeSet(solution{Decision: fulleval.DecisionImplement, ChangedFiles: []string{"a.go"}, Patch: "--- a/a.go\n+++ b/a.go\n+a\n+b\n+c\n"}, evalCase); err == nil {
		t.Fatal("expected diff budget rejection")
	}
}

func TestMeterUsesCacheReadWritePrices(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	provider := &model.FakeProvider{Responses: []model.Response{{Status: "completed", Usage: model.Usage{InputTokens: 100, CachedInputTokens: 30, CacheWriteInputTokens: 20, OutputTokens: 20}}}}
	meter := &meter{provider: provider, pricing: UsagePricing{Mode: PricingModeCacheReadWrite, InputUSDPerMillionTokens: 10, CachedUSDPerMillionTokens: 2, CacheWriteUSDPerMillion: 12.5, OutputUSDPerMillionTokens: 30, Source: "https://example.com/pricing", ValidUntil: now.Add(time.Hour)}, now: func() time.Time { return now }}
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
	meter := &meter{provider: provider, pricing: UsagePricing{Mode: PricingModeCacheHitMiss, InputUSDPerMillionTokens: 10, CachedUSDPerMillionTokens: 2, OutputUSDPerMillionTokens: 30, Source: "https://example.com/pricing", ValidUntil: now.Add(time.Hour)}, now: func() time.Time { return now }}
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
	meter := &meter{provider: provider, pricing: UsagePricing{Mode: PricingModeCacheHitMiss, InputUSDPerMillionTokens: 10, CachedUSDPerMillionTokens: 2, OutputUSDPerMillionTokens: 30, Source: "https://example.com/pricing", ValidUntil: now.Add(time.Second)}, now: func() time.Time { return now }}
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
