package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"forgeflow/internal/apperror"
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

func TestEvalCompareAndPromotionEnforceComparableCampaigns(t *testing.T) {
	directory := t.TempDir()
	currentPath := filepath.Join(directory, "current.json")
	candidatePath := filepath.Join(directory, "candidate.json")
	outputPath := filepath.Join(directory, "comparison.json")
	writeReport := func(path string, report fulleval.ComparisonReport) {
		t.Helper()
		data, err := json.Marshal(report)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	current := comparisonForCLI("developer/v1", 0)
	candidate := comparisonForCLI("developer/v2", 0.09)
	writeReport(currentPath, current)
	writeReport(candidatePath, candidate)
	if err := runEvalCompare([]string{"--current", currentPath, "--candidate", candidatePath, "--output", outputPath}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	var comparison fulleval.CandidateComparisonReport
	if err := json.Unmarshal(data, &comparison); err != nil {
		t.Fatal(err)
	}
	if comparison.CurrentPromptVersion != "developer/v1" || comparison.CandidatePromptVersion != "developer/v2" {
		t.Fatalf("comparison=%+v", comparison)
	}
	if err := runPromotion(currentPath, candidatePath, false); !apperror.IsCode(err, apperror.CodeApprovalNeeded) {
		t.Fatalf("promotion error=%v", err)
	}

	for index := range candidate.Reports {
		candidate.Reports[index].Configuration.PriorCostUSD = 0
	}
	writeReport(candidatePath, candidate)
	if err := runPromotion(currentPath, candidatePath, true); !apperror.IsCode(err, apperror.CodeValidation) {
		t.Fatalf("unlinked promotion error=%v", err)
	}
}

func TestPromotionRejectsSmokeReport(t *testing.T) {
	directory := t.TempDir()
	smokePath := filepath.Join(directory, "smoke.json")
	if err := os.WriteFile(smokePath, []byte(`{"schemaVersion":"forgeflow.eval.smoke-report/v1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runPromotion(smokePath, smokePath, true); !apperror.IsCode(err, apperror.CodeValidation) {
		t.Fatalf("promotion error=%v", err)
	}
}

func comparisonForCLI(developerPrompt string, priorCost float64) fulleval.ComparisonReport {
	reports := make([]fulleval.Report, 0, 3)
	for _, mode := range []fulleval.Mode{fulleval.ModeSingleAgent, fulleval.ModePlannerDeveloper, fulleval.ModeForgeFlow} {
		cost, latency := 0.001, 100.0
		prompts := map[string]string{"single_agent": "eval/single-agent/v1"}
		if mode != fulleval.ModeSingleAgent {
			prompts = map[string]string{"planner": "eval/planner/v1", "developer": developerPrompt}
		}
		grades := make([]fulleval.Grade, 0, 30)
		for index := range 30 {
			grades = append(grades, fulleval.Grade{CaseID: fmt.Sprintf("case-%02d", index+1), Passed: true, DeterministicFailures: []string{}})
		}
		reports = append(reports, fulleval.Report{
			SchemaVersion: "forgeflow.eval.report/v1", Dataset: fulleval.SoftwareV1, DatasetVersion: "1", GeneratedAt: time.Unix(1, 0),
			Configuration: fulleval.Configuration{
				Mode: mode, ReasoningEffort: "low", ModelVersions: map[string]string{"developer": "model-v1"}, PromptVersions: prompts,
				PolicyVersion: "policy/v1", ToolVersions: map[string]string{"run_test": "v1"}, GitCommit: "0000000000000000000000000000000000000001",
				FixtureRepositoryCommit: "0000000000000000000000000000000000000002", GraderCommit: "0000000000000000000000000000000000000003",
				ExecutionEnvironment: "test", ModelProvider: "deepseek", PricingMode: "cache_hit_miss", PricingSource: "https://example.com/pricing",
				PricingValidFrom: "2029-12-31T00:00:00Z", PricingValidUntil: "2030-01-01T00:00:00Z", PricingUSDPerMTok: map[string]float64{"input": 1, "output": 2}, MaxTotalCostUSD: 1, PriorCostUSD: priorCost,
			},
			Total: 30, Passed: 30, Grades: grades,
			Metrics: fulleval.Metrics{CompletionRate: 1, HiddenTestPassRate: 1, AverageCostUSD: &cost, P95LatencyMS: &latency}, Unavailable: []string{},
		})
	}
	return fulleval.ComparisonReport{SchemaVersion: "forgeflow.eval.comparison/v1", Dataset: fulleval.SoftwareV1, GeneratedAt: time.Unix(1, 0), Reports: reports}
}
