package governance

import (
	"strings"
	"testing"

	fulleval "forgeflow/internal/eval"
)

func TestInitialPromotionAllowedRequiresCompleteThirtyCaseEvidence(t *testing.T) {
	cost, latency := 0.42, 12_000.0
	report := fulleval.Report{
		Total: 30, Passed: 30,
		Metrics: fulleval.Metrics{AverageCostUSD: &cost, P95LatencyMS: &latency},
	}
	if err := InitialPromotionAllowed(report); err != nil {
		t.Fatalf("complete evidence rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*fulleval.Report)
	}{
		{name: "too few cases", mutate: func(value *fulleval.Report) { value.Total, value.Passed = 29, 29 }},
		{name: "failed case", mutate: func(value *fulleval.Report) { value.Passed = 29 }},
		{name: "unavailable metric", mutate: func(value *fulleval.Report) { value.Unavailable = []string{"averageCostUsd"} }},
		{name: "missing cost", mutate: func(value *fulleval.Report) { value.Metrics.AverageCostUSD = nil }},
		{name: "missing latency", mutate: func(value *fulleval.Report) { value.Metrics.P95LatencyMS = nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := report
			test.mutate(&candidate)
			if err := InitialPromotionAllowed(candidate); err == nil || !strings.Contains(err.Error(), "30 passing cases") {
				t.Fatalf("error = %v, want promotion gate rejection", err)
			}
		})
	}
}
