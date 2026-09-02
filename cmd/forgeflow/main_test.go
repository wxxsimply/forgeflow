package main

import (
	"testing"

	fulleval "forgeflow/internal/eval"
	"forgeflow/internal/evalexec"
)

func TestEvalConfigurationBindsProductionDeveloperPrompt(t *testing.T) {
	pricing := evalexec.UsagePricing{}
	for _, mode := range []fulleval.Mode{fulleval.ModePlannerDeveloper, fulleval.ModeForgeFlow} {
		configuration := evalConfiguration(mode, "deepseek", "model-v1", "none", "developer/v2", "0000000000000000000000000000000000000001", "0000000000000000000000000000000000000002", "0000000000000000000000000000000000000003", pricing, 1, 0)
		if configuration.PromptVersions["developer"] != "developer/v2" {
			t.Fatalf("mode %s recorded developer prompt %q", mode, configuration.PromptVersions["developer"])
		}
	}
}

func TestRecordedEvidenceCostSumsAllModes(t *testing.T) {
	first, second := 0.25, 0.75
	file := fulleval.EvidenceFile{Runs: []fulleval.Evidence{
		{Configuration: fulleval.Configuration{Mode: fulleval.ModeSingleAgent}, Observations: []fulleval.Observation{{CaseID: "one", CostUSD: &first}}},
		{Configuration: fulleval.Configuration{Mode: fulleval.ModeForgeFlow}, Observations: []fulleval.Observation{{CaseID: "two", CostUSD: &second}}},
	}}
	got, err := recordedEvidenceCost(file)
	if err != nil {
		t.Fatal(err)
	}
	if got != 1 {
		t.Fatalf("cost=%f want=1", got)
	}
}

func TestRecordedEvidenceCostRejectsMissingMeasurement(t *testing.T) {
	file := fulleval.EvidenceFile{Runs: []fulleval.Evidence{{
		Configuration: fulleval.Configuration{Mode: fulleval.ModeSingleAgent},
		Observations:  []fulleval.Observation{{CaseID: "one"}},
	}}}
	if _, err := recordedEvidenceCost(file); err == nil {
		t.Fatal("expected missing cost measurement to be rejected")
	}
}
