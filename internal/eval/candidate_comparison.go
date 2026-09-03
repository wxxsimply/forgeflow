package eval

import (
	"fmt"
	"maps"
	"math"
	"reflect"
	"strings"
	"time"
)

const candidateComparisonSchema = "forgeflow.eval.candidate-comparison/v1"

type CandidateModeResult struct {
	Passed  int     `json:"passed"`
	Total   int     `json:"total"`
	Metrics Metrics `json:"metrics"`
}

type CandidateMetricDelta struct {
	Passed                int      `json:"passed"`
	CompletionRate        float64  `json:"completionRate"`
	HiddenTestPassRate    float64  `json:"hiddenTestPassRate"`
	RegressionRate        float64  `json:"regressionRate"`
	HumanInterventionRate float64  `json:"humanInterventionRate"`
	AverageCostUSD        *float64 `json:"averageCostUsd"`
	P95LatencyMS          *float64 `json:"p95LatencyMs"`
}

type CandidateModeComparison struct {
	Mode      Mode                 `json:"mode"`
	Current   CandidateModeResult  `json:"current"`
	Candidate CandidateModeResult  `json:"candidate"`
	Delta     CandidateMetricDelta `json:"delta"`
}

type CandidateComparisonReport struct {
	SchemaVersion           string                    `json:"schemaVersion"`
	Dataset                 string                    `json:"dataset"`
	DatasetVersion          string                    `json:"datasetVersion"`
	GeneratedAt             time.Time                 `json:"generatedAt"`
	GitCommit               string                    `json:"gitCommit"`
	FixtureRepositoryCommit string                    `json:"fixtureRepositoryCommit"`
	GraderCommit            string                    `json:"graderCommit"`
	CurrentPromptVersion    string                    `json:"currentPromptVersion"`
	CandidatePromptVersion  string                    `json:"candidatePromptVersion"`
	Modes                   []CandidateModeComparison `json:"modes"`
	PromotionThresholds     PromotionThresholds       `json:"promotionThresholds"`
	PromotionGate           PromotionDecision         `json:"promotionGate"`
}

func BuildCandidateComparison(current, candidate ComparisonReport, now time.Time) (CandidateComparisonReport, error) {
	currentByMode, datasetVersion, err := indexComparisonReport(current)
	if err != nil {
		return CandidateComparisonReport{}, fmt.Errorf("current comparison: %w", err)
	}
	candidateByMode, candidateDatasetVersion, err := indexComparisonReport(candidate)
	if err != nil {
		return CandidateComparisonReport{}, fmt.Errorf("candidate comparison: %w", err)
	}
	if current.Dataset != candidate.Dataset || datasetVersion != candidateDatasetVersion {
		return CandidateComparisonReport{}, fmt.Errorf("current and candidate datasets do not match")
	}

	currentPrompt, err := comparisonDeveloperPrompt(currentByMode)
	if err != nil {
		return CandidateComparisonReport{}, fmt.Errorf("current comparison: %w", err)
	}
	candidatePrompt, err := comparisonDeveloperPrompt(candidateByMode)
	if err != nil {
		return CandidateComparisonReport{}, fmt.Errorf("candidate comparison: %w", err)
	}
	if currentPrompt == candidatePrompt {
		return CandidateComparisonReport{}, fmt.Errorf("current and candidate developer prompt versions are identical")
	}
	currentCampaignCost, err := comparisonCampaignCost(currentByMode)
	if err != nil {
		return CandidateComparisonReport{}, fmt.Errorf("current comparison: %w", err)
	}
	candidatePriorCost, err := comparisonPriorCost(candidateByMode)
	if err != nil {
		return CandidateComparisonReport{}, fmt.Errorf("candidate comparison: %w", err)
	}
	if math.Abs(candidatePriorCost-currentCampaignCost) > 1e-9 {
		return CandidateComparisonReport{}, fmt.Errorf("candidate prior campaign cost does not equal current campaign cost")
	}

	modes := make([]CandidateModeComparison, 0, 3)
	for _, mode := range []Mode{ModeSingleAgent, ModePlannerDeveloper, ModeForgeFlow} {
		currentReport := currentByMode[mode]
		candidateReport := candidateByMode[mode]
		if !reflect.DeepEqual(comparableCandidateConfiguration(currentReport.Configuration), comparableCandidateConfiguration(candidateReport.Configuration)) {
			return CandidateComparisonReport{}, fmt.Errorf("mode %q configuration differs beyond developer prompt and prior campaign cost", mode)
		}
		if currentReport.Total != candidateReport.Total {
			return CandidateComparisonReport{}, fmt.Errorf("mode %q total case count differs", mode)
		}
		modes = append(modes, CandidateModeComparison{
			Mode:      mode,
			Current:   CandidateModeResult{Passed: currentReport.Passed, Total: currentReport.Total, Metrics: currentReport.Metrics},
			Candidate: CandidateModeResult{Passed: candidateReport.Passed, Total: candidateReport.Total, Metrics: candidateReport.Metrics},
			Delta:     candidateMetricDelta(currentReport, candidateReport),
		})
	}

	thresholds := DefaultPromotionThresholds()
	configuration := currentByMode[ModeForgeFlow].Configuration
	return CandidateComparisonReport{
		SchemaVersion: candidateComparisonSchema, Dataset: current.Dataset, DatasetVersion: datasetVersion,
		GeneratedAt: now.UTC(), GitCommit: configuration.GitCommit,
		FixtureRepositoryCommit: configuration.FixtureRepositoryCommit, GraderCommit: configuration.GraderCommit,
		CurrentPromptVersion: currentPrompt, CandidatePromptVersion: candidatePrompt, Modes: modes,
		PromotionThresholds: thresholds,
		PromotionGate:       CheckPromotion(currentByMode[ModeForgeFlow], candidateByMode[ModeForgeFlow], thresholds),
	}, nil
}

func indexComparisonReport(report ComparisonReport) (map[Mode]Report, string, error) {
	if report.SchemaVersion != "forgeflow.eval.comparison/v1" || strings.TrimSpace(report.Dataset) == "" {
		return nil, "", fmt.Errorf("invalid comparison report schema or dataset")
	}
	if len(report.Reports) != 3 {
		return nil, "", fmt.Errorf("comparison report must contain exactly three modes")
	}
	byMode := make(map[Mode]Report, 3)
	datasetVersion := ""
	for _, item := range report.Reports {
		if item.SchemaVersion != "forgeflow.eval.report/v1" || item.Dataset != report.Dataset || strings.TrimSpace(item.DatasetVersion) == "" {
			return nil, "", fmt.Errorf("mode %q has invalid report metadata", item.Configuration.Mode)
		}
		if item.Total != 30 || len(item.Grades) != item.Total || item.Passed < 0 || item.Passed > item.Total {
			return nil, "", fmt.Errorf("mode %q does not contain a complete 30-case report", item.Configuration.Mode)
		}
		if datasetVersion == "" {
			datasetVersion = item.DatasetVersion
		} else if item.DatasetVersion != datasetVersion {
			return nil, "", fmt.Errorf("dataset versions differ within comparison")
		}
		if _, exists := byMode[item.Configuration.Mode]; exists {
			return nil, "", fmt.Errorf("duplicate mode %q", item.Configuration.Mode)
		}
		byMode[item.Configuration.Mode] = item
	}
	for _, mode := range []Mode{ModeSingleAgent, ModePlannerDeveloper, ModeForgeFlow} {
		if _, exists := byMode[mode]; !exists {
			return nil, "", fmt.Errorf("missing mode %q", mode)
		}
	}
	return byMode, datasetVersion, nil
}

func comparisonDeveloperPrompt(reports map[Mode]Report) (string, error) {
	version := ""
	for _, mode := range []Mode{ModePlannerDeveloper, ModeForgeFlow} {
		value := strings.TrimSpace(reports[mode].Configuration.PromptVersions["developer"])
		if value == "" {
			return "", fmt.Errorf("mode %q has no developer prompt version", mode)
		}
		if version == "" {
			version = value
		} else if value != version {
			return "", fmt.Errorf("developer prompt versions differ between affected modes")
		}
	}
	return version, nil
}

func comparableCandidateConfiguration(configuration Configuration) Configuration {
	configuration.PromptVersions = maps.Clone(configuration.PromptVersions)
	delete(configuration.PromptVersions, "developer")
	configuration.PriorCostUSD = 0
	return configuration
}

func comparisonCampaignCost(reports map[Mode]Report) (float64, error) {
	priorCost, err := comparisonPriorCost(reports)
	if err != nil {
		return 0, err
	}
	total := priorCost
	for _, mode := range []Mode{ModeSingleAgent, ModePlannerDeveloper, ModeForgeFlow} {
		report := reports[mode]
		if report.Metrics.AverageCostUSD == nil || math.IsNaN(*report.Metrics.AverageCostUSD) || math.IsInf(*report.Metrics.AverageCostUSD, 0) || *report.Metrics.AverageCostUSD < 0 {
			return 0, fmt.Errorf("mode %q has no valid measured average cost", mode)
		}
		total += *report.Metrics.AverageCostUSD * float64(report.Total)
	}
	return total, nil
}

func comparisonPriorCost(reports map[Mode]Report) (float64, error) {
	priorCost := reports[ModeSingleAgent].Configuration.PriorCostUSD
	if math.IsNaN(priorCost) || math.IsInf(priorCost, 0) || priorCost < 0 {
		return 0, fmt.Errorf("prior campaign cost is invalid")
	}
	for _, mode := range []Mode{ModePlannerDeveloper, ModeForgeFlow} {
		if math.Abs(reports[mode].Configuration.PriorCostUSD-priorCost) > 1e-9 {
			return 0, fmt.Errorf("prior campaign cost differs between modes")
		}
	}
	return priorCost, nil
}

func candidateMetricDelta(current, candidate Report) CandidateMetricDelta {
	return CandidateMetricDelta{
		Passed:                candidate.Passed - current.Passed,
		CompletionRate:        candidate.Metrics.CompletionRate - current.Metrics.CompletionRate,
		HiddenTestPassRate:    candidate.Metrics.HiddenTestPassRate - current.Metrics.HiddenTestPassRate,
		RegressionRate:        candidate.Metrics.RegressionRate - current.Metrics.RegressionRate,
		HumanInterventionRate: candidate.Metrics.HumanInterventionRate - current.Metrics.HumanInterventionRate,
		AverageCostUSD:        subtractOptional(candidate.Metrics.AverageCostUSD, current.Metrics.AverageCostUSD),
		P95LatencyMS:          subtractOptional(candidate.Metrics.P95LatencyMS, current.Metrics.P95LatencyMS),
	}
}

func subtractOptional(candidate, current *float64) *float64 {
	if candidate == nil || current == nil {
		return nil
	}
	value := *candidate - *current
	return &value
}

func (report CandidateComparisonReport) Markdown() string {
	var builder strings.Builder
	builder.WriteString("# ForgeFlow Developer Prompt Candidate Comparison\n\n")
	builder.WriteString(fmt.Sprintf("- Dataset: `%s` (`%s`)\n", report.Dataset, report.DatasetVersion))
	builder.WriteString(fmt.Sprintf("- Git commit: `%s`\n", report.GitCommit))
	builder.WriteString(fmt.Sprintf("- Fixture commit: `%s`\n", report.FixtureRepositoryCommit))
	builder.WriteString(fmt.Sprintf("- Grader commit: `%s`\n", report.GraderCommit))
	builder.WriteString(fmt.Sprintf("- Developer Prompt: `%s` -> `%s`\n", report.CurrentPromptVersion, report.CandidatePromptVersion))
	builder.WriteString(fmt.Sprintf("- Generated: `%s`\n\n", report.GeneratedAt.Format(time.RFC3339)))
	builder.WriteString("Deltas are candidate minus current.\n\n")
	builder.WriteString("| Mode | Passed current -> candidate (delta) | Completion delta | Hidden tests delta | Regression delta | Human intervention delta | Avg cost delta (USD) | P95 latency delta (ms) |\n")
	builder.WriteString("|---|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, item := range report.Modes {
		builder.WriteString(fmt.Sprintf("| %s | %d/%d -> %d/%d (%+d) | %+.2f%% | %+.2f%% | %+.2f%% | %+.2f%% | %s | %s |\n",
			item.Mode, item.Current.Passed, item.Current.Total, item.Candidate.Passed, item.Candidate.Total, item.Delta.Passed,
			item.Delta.CompletionRate*100, item.Delta.HiddenTestPassRate*100, item.Delta.RegressionRate*100,
			item.Delta.HumanInterventionRate*100, optionalSignedFloat(item.Delta.AverageCostUSD), optionalSignedFloat(item.Delta.P95LatencyMS)))
	}
	builder.WriteString(fmt.Sprintf("\nPromotion Gate allowed: `%t`\n", report.PromotionGate.Allowed))
	if len(report.PromotionGate.Reasons) > 0 {
		builder.WriteString("\nGate reasons:\n")
		for _, reason := range report.PromotionGate.Reasons {
			builder.WriteString(fmt.Sprintf("\n- `%s`", reason))
		}
		builder.WriteString("\n")
	}
	builder.WriteString("\nThe automatic gate does not constitute human approval or Promotion.\n")
	return builder.String()
}

func optionalSignedFloat(value *float64) string {
	if value == nil {
		return "N/A"
	}
	return fmt.Sprintf("%+.4f", *value)
}
