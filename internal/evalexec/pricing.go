package evalexec

import (
	"fmt"
	"math"
	"strings"
	"time"

	"forgeflow/internal/model"
)

type PricingMode string

const (
	PricingModeCacheHitMiss   PricingMode = "cache_hit_miss"
	PricingModeCacheReadWrite PricingMode = "cache_read_write"
)

type UsagePricing struct {
	Mode                      PricingMode
	InputUSDPerMillionTokens  float64
	CachedUSDPerMillionTokens float64
	CacheWriteUSDPerMillion   float64
	OutputUSDPerMillionTokens float64
	Source                    string
	ValidFrom                 time.Time
	ValidUntil                time.Time
}

// MaxRequestCost returns a conservative authorization amount. A UTF-8 byte is
// treated as at most one input token, with additional room for provider message
// framing. Cached input is intentionally priced at the highest applicable input
// rate because cache behavior is not known before the response arrives.
func (p UsagePricing) MaxRequestCost(request model.Request) (float64, error) {
	if request.MaxOutputTokens <= 0 {
		return 0, fmt.Errorf("model request must set a positive output token limit")
	}
	inputTokens := len([]byte(request.Instructions)) + len([]byte(request.Input)) +
		len([]byte(request.ResponseFormat.Name)) + len([]byte(request.ResponseFormat.Description)) +
		len(request.ResponseFormat.Schema) + 2048
	inputRate := math.Max(p.InputUSDPerMillionTokens, p.CachedUSDPerMillionTokens)
	if p.Mode == PricingModeCacheReadWrite {
		inputRate = math.Max(inputRate, p.CacheWriteUSDPerMillion)
	}
	return float64(inputTokens)/1_000_000*inputRate +
		float64(request.MaxOutputTokens)/1_000_000*p.OutputUSDPerMillionTokens, nil
}

func (p UsagePricing) Validate(now time.Time) error {
	if p.Mode != PricingModeCacheHitMiss && p.Mode != PricingModeCacheReadWrite {
		return fmt.Errorf("pricing mode must be %q or %q", PricingModeCacheHitMiss, PricingModeCacheReadWrite)
	}
	if p.InputUSDPerMillionTokens <= 0 || p.CachedUSDPerMillionTokens <= 0 || p.OutputUSDPerMillionTokens <= 0 {
		return fmt.Errorf("real positive input, cached-input, and output token prices are required")
	}
	if p.Mode == PricingModeCacheReadWrite && p.CacheWriteUSDPerMillion <= 0 {
		return fmt.Errorf("cache-read-write pricing requires a real positive cache-write token price")
	}
	if p.Mode == PricingModeCacheHitMiss && p.CacheWriteUSDPerMillion != 0 {
		return fmt.Errorf("cache-hit-miss pricing must not configure a cache-write token price")
	}
	if !strings.HasPrefix(strings.TrimSpace(p.Source), "https://") {
		return fmt.Errorf("pricing source must be an HTTPS URL")
	}
	if p.ValidFrom.IsZero() || now.Before(p.ValidFrom) {
		return fmt.Errorf("pricing validity start must be set and not be in the future")
	}
	if p.ValidUntil.IsZero() || !p.ValidUntil.After(p.ValidFrom) || !p.ValidUntil.After(now) {
		return fmt.Errorf("pricing validity deadline must be in the future")
	}
	return nil
}

func (p UsagePricing) CanStart(now time.Time, timeout time.Duration) error {
	if err := p.Validate(now); err != nil {
		return err
	}
	if timeout <= 0 || !now.Add(timeout).Before(p.ValidUntil) {
		return fmt.Errorf("pricing validity window is too short for another model call")
	}
	return nil
}

func (p UsagePricing) Estimate(usage model.Usage) (float64, error) {
	if usage.InputTokens < 0 || usage.OutputTokens < 0 || usage.CachedInputTokens < 0 || usage.CacheWriteInputTokens < 0 {
		return 0, fmt.Errorf("provider returned a negative token measurement")
	}
	uncachedInput := usage.InputTokens - usage.CachedInputTokens
	if p.Mode == PricingModeCacheReadWrite {
		uncachedInput -= usage.CacheWriteInputTokens
	} else if usage.CacheWriteInputTokens != 0 {
		return 0, fmt.Errorf("provider returned cache-write tokens for cache-hit-miss pricing")
	}
	if uncachedInput < 0 {
		return 0, fmt.Errorf("provider token breakdown exceeds total input tokens")
	}
	cost := float64(uncachedInput)/1_000_000*p.InputUSDPerMillionTokens +
		float64(usage.CachedInputTokens)/1_000_000*p.CachedUSDPerMillionTokens +
		float64(usage.OutputTokens)/1_000_000*p.OutputUSDPerMillionTokens
	if p.Mode == PricingModeCacheReadWrite {
		cost += float64(usage.CacheWriteInputTokens) / 1_000_000 * p.CacheWriteUSDPerMillion
	}
	return cost, nil
}
