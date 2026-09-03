package evalexec

import (
	"strings"
	"testing"
	"time"
)

func TestUsagePricingRejectsNotYetActiveWindow(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	pricing := validUsagePricing(now)
	pricing.ValidFrom = now.Add(time.Minute)
	if err := pricing.Validate(now); err == nil || !strings.Contains(err.Error(), "start") {
		t.Fatalf("error=%v want pricing start rejection", err)
	}
}

func TestUsagePricingAcceptsCurrentBoundedWindow(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	pricing := validUsagePricing(now)
	if err := pricing.Validate(now); err != nil {
		t.Fatal(err)
	}
}

func TestUsagePricingRejectsReversedWindow(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	pricing := validUsagePricing(now)
	pricing.ValidUntil = pricing.ValidFrom
	if err := pricing.Validate(now); err == nil || !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("error=%v want pricing deadline rejection", err)
	}
}

func validUsagePricing(now time.Time) UsagePricing {
	return UsagePricing{
		Mode: PricingModeCacheHitMiss, InputUSDPerMillionTokens: 1, CachedUSDPerMillionTokens: 0.5,
		OutputUSDPerMillionTokens: 2, Source: "https://example.com/pricing",
		ValidFrom: now.Add(-time.Minute), ValidUntil: now.Add(time.Hour),
	}
}
