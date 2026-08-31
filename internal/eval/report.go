package eval

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
)

type Mode string

const (
	ModeSingleAgent      Mode = "single_agent"
	ModePlannerDeveloper Mode = "planner_developer"
	ModeForgeFlow        Mode = "forgeflow"
)

type Configuration struct {
	Mode                    Mode               `json:"mode"`
	ModelVersions           map[string]string  `json:"modelVersions"`
	PromptVersions          map[string]string  `json:"promptVersions"`
	PolicyVersion           string             `json:"policyVersion"`
	ToolVersions            map[string]string  `json:"toolVersions"`
	GitCommit               string             `json:"gitCommit"`
	FixtureRepositoryCommit string             `json:"fixtureRepositoryCommit,omitempty"`
	GraderCommit            string             `json:"graderCommit,omitempty"`
	ExecutionEnvironment    string             `json:"executionEnvironment,omitempty"`
	PricingUSDPerMTok       map[string]float64 `json:"pricingUsdPerMTok,omitempty"`
}

type Evidence struct {
	Dataset       string        `json:"dataset"`
	Configuration Configuration `json:"configuration"`
	ObservedAt    time.Time     `json:"observedAt"`
	Observations  []Observation `json:"observations"`
}

type Metrics struct {
	CompletionRate        float64  `json:"completionRate"`
	HiddenTestPassRate    float64  `json:"hiddenTestPassRate"`
	RegressionRate        float64  `json:"regressionRate"`
	HumanInterventionRate float64  `json:"humanInterventionRate"`
	AverageCostUSD        *float64 `json:"averageCostUsd"`
	P95LatencyMS          *float64 `json:"p95LatencyMs"`
}

type Report struct {
	SchemaVersion  string        `json:"schemaVersion"`
	Dataset        string        `json:"dataset"`
	DatasetVersion string        `json:"datasetVersion"`
	GeneratedAt    time.Time     `json:"generatedAt"`
	Configuration  Configuration `json:"configuration"`
	Total          int           `json:"total"`
	Passed         int           `json:"passed"`
	Grades         []Grade       `json:"grades"`
	Metrics        Metrics       `json:"metrics"`
	Unavailable    []string      `json:"unavailableMetrics"`
}

type EvidenceFile struct {
	Runs []Evidence `json:"runs"`
}

type ComparisonReport struct {
	SchemaVersion string    `json:"schemaVersion"`
	Dataset       string    `json:"dataset"`
	GeneratedAt   time.Time `json:"generatedAt"`
	Reports       []Report  `json:"reports"`
}

func BuildReport(dataset Dataset, evidence Evidence, now time.Time) (Report, error) {
	if evidence.Dataset != dataset.Name {
		return Report{}, fmt.Errorf("evidence dataset %q does not match %q", evidence.Dataset, dataset.Name)
	}
	if !slices.Contains([]Mode{ModeSingleAgent, ModePlannerDeveloper, ModeForgeFlow}, evidence.Configuration.Mode) {
		return Report{}, fmt.Errorf("invalid baseline mode %q", evidence.Configuration.Mode)
	}
	if evidence.ObservedAt.IsZero() {
		return Report{}, fmt.Errorf("evidence observedAt is required")
	}
	if evidence.Configuration.GitCommit == "" || evidence.Configuration.PolicyVersion == "" || len(evidence.Configuration.ModelVersions) == 0 || len(evidence.Configuration.PromptVersions) == 0 || len(evidence.Configuration.ToolVersions) == 0 {
		return Report{}, fmt.Errorf("evidence configuration must record git, model, prompt, policy, and tool versions")
	}
	if !commitPattern.MatchString(evidence.Configuration.GitCommit) {
		return Report{}, fmt.Errorf("evidence git commit must be a 40-character lowercase SHA")
	}
	if err := ValidateObservations(dataset, evidence.Observations); err != nil {
		return Report{}, err
	}
	cases := make(map[string]Case, len(dataset.Cases))
	for _, evalCase := range dataset.Cases {
		cases[evalCase.ID] = evalCase
	}
	report := Report{SchemaVersion: "forgeflow.eval.report/v1", Dataset: dataset.Name, DatasetVersion: dataset.Version, GeneratedAt: now.UTC(), Configuration: evidence.Configuration, Total: len(dataset.Cases), Grades: make([]Grade, 0, len(dataset.Cases)), Unavailable: []string{}}
	hiddenPassed, hiddenTotal, regressions, interventions := 0, 0, 0, 0
	costs := make([]float64, 0, report.Total)
	durations := make([]float64, 0, report.Total)
	for _, observation := range evidence.Observations {
		evalCase, exists := cases[observation.CaseID]
		if !exists {
			return Report{}, fmt.Errorf("unknown case %q", observation.CaseID)
		}
		grade := GradeCase(evalCase, observation)
		report.Grades = append(report.Grades, grade)
		if grade.Passed {
			report.Passed++
		}
		hiddenPassed += grade.HiddenPassed
		hiddenTotal += grade.HiddenTotal
		if observation.Regression {
			regressions++
		}
		if observation.HumanIntervention {
			interventions++
		}
		if observation.CostUSD != nil {
			costs = append(costs, *observation.CostUSD)
		}
		if observation.DurationMS != nil {
			durations = append(durations, float64(*observation.DurationMS))
		}
	}
	report.Metrics.CompletionRate = ratio(report.Passed, report.Total)
	report.Metrics.HiddenTestPassRate = ratio(hiddenPassed, hiddenTotal)
	report.Metrics.RegressionRate = ratio(regressions, report.Total)
	report.Metrics.HumanInterventionRate = ratio(interventions, report.Total)
	if len(costs) == report.Total {
		value := average(costs)
		report.Metrics.AverageCostUSD = &value
	} else {
		report.Unavailable = append(report.Unavailable, "averageCostUsd")
	}
	if len(durations) == report.Total {
		value := percentile95(durations)
		report.Metrics.P95LatencyMS = &value
	} else {
		report.Unavailable = append(report.Unavailable, "p95LatencyMs")
	}
	slices.SortFunc(report.Grades, func(a, b Grade) int { return strings.Compare(a.CaseID, b.CaseID) })
	return report, nil
}

func BuildComparison(dataset Dataset, evidenceFile EvidenceFile, now time.Time) (ComparisonReport, error) {
	if len(evidenceFile.Runs) != 3 {
		return ComparisonReport{}, fmt.Errorf("comparison requires exactly three baseline runs")
	}
	report := ComparisonReport{SchemaVersion: "forgeflow.eval.comparison/v1", Dataset: dataset.Name, GeneratedAt: now.UTC(), Reports: make([]Report, 0, 3)}
	modes := map[Mode]bool{}
	for _, evidence := range evidenceFile.Runs {
		if modes[evidence.Configuration.Mode] {
			return ComparisonReport{}, fmt.Errorf("duplicate baseline mode %q", evidence.Configuration.Mode)
		}
		modes[evidence.Configuration.Mode] = true
		item, err := BuildReport(dataset, evidence, now)
		if err != nil {
			return ComparisonReport{}, err
		}
		report.Reports = append(report.Reports, item)
	}
	for _, mode := range []Mode{ModeSingleAgent, ModePlannerDeveloper, ModeForgeFlow} {
		if !modes[mode] {
			return ComparisonReport{}, fmt.Errorf("comparison is missing mode %q", mode)
		}
	}
	slices.SortFunc(report.Reports, func(a, b Report) int {
		return strings.Compare(string(a.Configuration.Mode), string(b.Configuration.Mode))
	})
	return report, nil
}

func DecodeEvidence(data []byte) (EvidenceFile, error) {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var file EvidenceFile
	if err := decoder.Decode(&file); err != nil {
		return EvidenceFile{}, fmt.Errorf("decode eval evidence: %w", err)
	}
	return file, nil
}

func ratio(numerator, denominator int) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}
func average(values []float64) float64 {
	total := 0.0
	for _, value := range values {
		total += value
	}
	return total / float64(len(values))
}
func percentile95(values []float64) float64 {
	values = slices.Clone(values)
	slices.Sort(values)
	index := int(math.Ceil(float64(len(values))*.95)) - 1
	if index < 0 {
		index = 0
	}
	return values[index]
}

func (report ComparisonReport) Markdown() string {
	var builder strings.Builder
	builder.WriteString("# ForgeFlow Eval Comparison\n\n")
	builder.WriteString(fmt.Sprintf("- Dataset: `%s`\n- Generated: `%s`\n\n", report.Dataset, report.GeneratedAt.Format(time.RFC3339)))
	builder.WriteString("| Mode | Passed | Completion | Hidden tests | Regression | Human intervention | Avg cost (USD) | P95 latency (ms) |\n|---|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, item := range report.Reports {
		builder.WriteString(fmt.Sprintf("| %s | %d/%d | %.2f%% | %.2f%% | %.2f%% | %.2f%% | %s | %s |\n", item.Configuration.Mode, item.Passed, item.Total, item.Metrics.CompletionRate*100, item.Metrics.HiddenTestPassRate*100, item.Metrics.RegressionRate*100, item.Metrics.HumanInterventionRate*100, optionalFloat(item.Metrics.AverageCostUSD), optionalFloat(item.Metrics.P95LatencyMS)))
	}
	builder.WriteString("\n`N/A` means the evidence did not contain that measurement; ForgeFlow does not synthesize missing metrics.\n")
	return builder.String()
}

func optionalFloat(value *float64) string {
	if value == nil {
		return "N/A"
	}
	return fmt.Sprintf("%.4f", *value)
}
