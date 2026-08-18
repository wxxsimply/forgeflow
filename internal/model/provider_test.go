package model

import (
	"context"
	"testing"
)

func TestFakeProviderRecordsRequestsAndReturnsConfiguredResponse(t *testing.T) {
	fake := &FakeProvider{Responses: []Response{{ID: "response-1", OutputText: `{}`}}}
	response, err := fake.Generate(context.Background(), Request{Model: "fixture"})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if response.ID != "response-1" || fake.CallCount() != 1 || len(fake.Requests) != 1 {
		t.Fatalf("response = %+v calls = %d requests = %d", response, fake.CallCount(), len(fake.Requests))
	}
}

func TestPricingEstimate(t *testing.T) {
	pricing := Pricing{InputUSDPerMillionTokens: 5, OutputUSDPerMillionTokens: 30}
	cost := pricing.Estimate(Usage{InputTokens: 1_000, OutputTokens: 500})
	if cost != 0.02 {
		t.Fatalf("cost = %f, want 0.02", cost)
	}
}
