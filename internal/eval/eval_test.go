package eval

import (
	"context"
	"strings"
	"testing"
	"time"
)

type executorFunc func(context.Context, Case, Configuration) (Observation, error)

func (f executorFunc) Execute(ctx context.Context, evalCase Case, configuration Configuration) (Observation, error) {
	return f(ctx, evalCase, configuration)
}

func TestSoftwareDatasetHasRequiredDistribution(t *testing.T) {
	dataset, err := Load(SoftwareV1)
	if err != nil {
		t.Fatal(err)
	}
	if len(dataset.Cases) != 30 {
		t.Fatalf("cases=%d", len(dataset.Cases))
	}
}

func TestDeterministicFailuresCannotBeOverriddenByModelScore(t *testing.T) {
	dataset, err := Load(SoftwareV1)
	if err != nil {
		t.Fatal(err)
	}
	score := 1.0
	observation := passingObservation(dataset.Cases[0], true)
	observation.ModelScore = &score
	observation.ChangedFiles = []string{".env"}
	grade := GradeCase(dataset.Cases[0], observation)
	if grade.Passed || !contains(grade.DeterministicFailures, "forbidden_file_changed") {
		t.Fatalf("grade=%+v", grade)
	}
}

func TestReportDoesNotFabricateMissingCostOrLatency(t *testing.T) {
	dataset, err := Load(SoftwareV1)
	if err != nil {
		t.Fatal(err)
	}
	report, err := BuildReport(dataset, evidenceFor(dataset, ModeForgeFlow, false), time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if report.Metrics.AverageCostUSD != nil || report.Metrics.P95LatencyMS != nil || len(report.Unavailable) != 2 {
		t.Fatalf("metrics=%+v unavailable=%v", report.Metrics, report.Unavailable)
	}
}

func TestBuildSmokeReportIsPartialAndNonPromotable(t *testing.T) {
	dataset, err := Load(SoftwareV1)
	if err != nil {
		t.Fatal(err)
	}
	evidence := evidenceFor(dataset, ModePlannerDeveloper, true)
	evidence.Observations = evidence.Observations[:2]
	report, err := BuildSmokeReport(dataset, evidence, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if report.SchemaVersion != "forgeflow.eval.smoke-report/v1" || report.Total != 2 || len(report.Grades) != 2 {
		t.Fatalf("report=%+v", report)
	}
	if _, err := BuildSmokeReport(dataset, Evidence{Dataset: dataset.Name}, time.Unix(1, 0)); err == nil {
		t.Fatal("empty smoke evidence was accepted")
	}
}

func TestComparisonRequiresAllThreeModes(t *testing.T) {
	dataset, err := Load(SoftwareV1)
	if err != nil {
		t.Fatal(err)
	}
	file := EvidenceFile{Runs: []Evidence{
		evidenceFor(dataset, ModeSingleAgent, true), evidenceFor(dataset, ModePlannerDeveloper, true), evidenceFor(dataset, ModeForgeFlow, true),
	}}
	report, err := BuildComparison(dataset, file, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Reports) != 3 || report.Markdown() == "" {
		t.Fatalf("report=%+v", report)
	}
}

func TestCandidateComparisonRequiresOnlyDeveloperPromptAndPriorCostToDiffer(t *testing.T) {
	dataset, err := Load(SoftwareV1)
	if err != nil {
		t.Fatal(err)
	}
	current, err := BuildComparison(dataset, candidateComparisonEvidence(dataset, "developer/v1", 0), time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := BuildComparison(dataset, candidateComparisonEvidence(dataset, "developer/v2", 9), time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}

	report, err := BuildCandidateComparison(current, candidate, time.Unix(3, 0))
	if err != nil {
		t.Fatal(err)
	}
	if report.CurrentPromptVersion != "developer/v1" || report.CandidatePromptVersion != "developer/v2" || len(report.Modes) != 3 {
		t.Fatalf("report=%+v", report)
	}
	if !report.PromotionGate.Allowed || !strings.Contains(report.Markdown(), "developer/v1` -> `developer/v2") {
		t.Fatalf("gate=%+v markdown=%q", report.PromotionGate, report.Markdown())
	}
}

func TestCandidateComparisonRejectsConfigurationDriftAndIdenticalPrompt(t *testing.T) {
	dataset, err := Load(SoftwareV1)
	if err != nil {
		t.Fatal(err)
	}
	current, err := BuildComparison(dataset, candidateComparisonEvidence(dataset, "developer/v1", 0), time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	samePrompt, err := BuildComparison(dataset, candidateComparisonEvidence(dataset, "developer/v1", 9), time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := BuildCandidateComparison(current, samePrompt, time.Unix(3, 0)); err == nil {
		t.Fatal("expected identical developer prompts to be rejected")
	}

	driftedEvidence := candidateComparisonEvidence(dataset, "developer/v2", 9)
	driftedEvidence.Runs[2].Configuration.ModelVersions["developer"] = "different-model"
	drifted, err := BuildComparison(dataset, driftedEvidence, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := BuildCandidateComparison(current, drifted, time.Unix(3, 0)); err == nil {
		t.Fatal("expected model drift to be rejected")
	}
}

func TestCandidateComparisonRejectsUnlinkedCampaignCost(t *testing.T) {
	dataset, err := Load(SoftwareV1)
	if err != nil {
		t.Fatal(err)
	}
	current, err := BuildComparison(dataset, candidateComparisonEvidence(dataset, "developer/v1", 0), time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := BuildComparison(dataset, candidateComparisonEvidence(dataset, "developer/v2", 8), time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := BuildCandidateComparison(current, candidate, time.Unix(3, 0)); err == nil {
		t.Fatal("expected unlinked candidate prior campaign cost to be rejected")
	}
}

func TestPromotionBlocksDeterministicRegressionAndIncompleteEvidence(t *testing.T) {
	dataset, err := Load(SoftwareV1)
	if err != nil {
		t.Fatal(err)
	}
	current, err := BuildReport(dataset, evidenceFor(dataset, ModeForgeFlow, true), time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	candidateEvidence := evidenceFor(dataset, ModeForgeFlow, true)
	candidateEvidence.Observations[0].ExplicitTestsPassed = false
	candidate, err := BuildReport(dataset, candidateEvidence, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	decision := CheckPromotion(current, candidate, DefaultPromotionThresholds())
	if decision.Allowed || len(decision.Reasons) == 0 {
		t.Fatalf("decision=%+v", decision)
	}

	incomplete, err := BuildReport(dataset, evidenceFor(dataset, ModeForgeFlow, false), time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	decision = CheckPromotion(current, incomplete, DefaultPromotionThresholds())
	if decision.Allowed || !contains(decision.Reasons, "incomplete_measurements") {
		t.Fatalf("decision=%+v", decision)
	}
}

func TestPromotionAllowsNonRegressingCompleteCandidate(t *testing.T) {
	dataset, err := Load(SoftwareV1)
	if err != nil {
		t.Fatal(err)
	}
	current, err := BuildReport(dataset, evidenceFor(dataset, ModeForgeFlow, true), time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := BuildReport(dataset, evidenceFor(dataset, ModeForgeFlow, true), time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	decision := CheckPromotion(current, candidate, DefaultPromotionThresholds())
	if !decision.Allowed {
		t.Fatalf("decision=%+v", decision)
	}
}

func TestRunnerExecutesEveryFixedCaseAndPreservesMeasurements(t *testing.T) {
	dataset, err := Load(SoftwareV1)
	if err != nil {
		t.Fatal(err)
	}
	runner := Runner{Dataset: dataset, Now: func() time.Time { return time.Unix(5, 0) }, Executor: executorFunc(func(_ context.Context, evalCase Case, _ Configuration) (Observation, error) {
		return passingObservation(evalCase, true), nil
	})}
	evidence, err := runner.Run(context.Background(), evidenceFor(dataset, ModeForgeFlow, true).Configuration)
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence.Observations) != 30 || !evidence.ObservedAt.Equal(time.Unix(5, 0)) {
		t.Fatalf("evidence=%+v", evidence)
	}
}

func evidenceFor(dataset Dataset, mode Mode, measured bool) Evidence {
	observations := make([]Observation, 0, len(dataset.Cases))
	for _, evalCase := range dataset.Cases {
		observations = append(observations, passingObservation(evalCase, measured))
	}
	return Evidence{
		Dataset: dataset.Name, ObservedAt: time.Unix(1, 0), Observations: observations,
		Configuration: Configuration{Mode: mode, ModelVersions: map[string]string{"planner": "test-model"}, PromptVersions: map[string]string{"planner": "planner/v1"}, PolicyVersion: "policy/v1", ToolVersions: map[string]string{"run_test": "v1"}, GitCommit: "0000000000000000000000000000000000000001"},
	}
}

func candidateComparisonEvidence(dataset Dataset, developerPrompt string, priorCost float64) EvidenceFile {
	runs := make([]Evidence, 0, 3)
	for _, mode := range []Mode{ModeSingleAgent, ModePlannerDeveloper, ModeForgeFlow} {
		evidence := evidenceFor(dataset, mode, true)
		evidence.Configuration = Configuration{
			Mode: mode, ReasoningEffort: "low", ModelVersions: map[string]string{"developer": "model-v1"},
			PromptVersions: map[string]string{"single_agent": "eval/single-agent/v1"},
			PolicyVersion:  "policy/v1", ToolVersions: map[string]string{"run_test": "v1"},
			GitCommit:               "0000000000000000000000000000000000000001",
			FixtureRepositoryCommit: "0000000000000000000000000000000000000002",
			GraderCommit:            "0000000000000000000000000000000000000003",
			ExecutionEnvironment:    "test", ModelProvider: "deepseek", PricingMode: "cache_hit_miss",
			PricingSource: "https://example.com/pricing", PricingValidFrom: "2029-12-31T00:00:00Z", PricingValidUntil: "2030-01-01T00:00:00Z",
			PricingUSDPerMTok: map[string]float64{"input": 1, "output": 2}, MaxTotalCostUSD: 20, PriorCostUSD: priorCost,
		}
		if mode != ModeSingleAgent {
			evidence.Configuration.PromptVersions = map[string]string{"planner": "eval/planner/v1", "developer": developerPrompt}
		}
		runs = append(runs, evidence)
	}
	return EvidenceFile{Runs: runs}
}

func passingObservation(evalCase Case, measured bool) Observation {
	observation := Observation{CaseID: evalCase.ID, Decision: evalCase.ExpectedDecision, PatchApplicable: true, ChangedFiles: []string{"internal/example.go"}, BuildPassed: true, ExplicitTestsPassed: true, HiddenTestResults: map[string]bool{}, Iterations: 1, DiffLines: 10}
	for _, hidden := range evalCase.HiddenTests {
		observation.HiddenTestResults[hidden] = true
	}
	if measured {
		cost := 0.1
		duration := int64(100)
		observation.CostUSD = &cost
		observation.DurationMS = &duration
	}
	if evalCase.ExpectedDecision != DecisionImplement {
		observation.ChangedFiles = []string{}
		observation.DiffLines = 0
	}
	return observation
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
