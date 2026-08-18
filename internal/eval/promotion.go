package eval

import "fmt"

type PromotionThresholds struct {
	MaxCompletionDrop       float64 `json:"maxCompletionDrop"`
	MaxHiddenTestDrop       float64 `json:"maxHiddenTestDrop"`
	MaxRegressionIncrease   float64 `json:"maxRegressionIncrease"`
	MaxCostIncreaseRatio    float64 `json:"maxCostIncreaseRatio"`
	MaxLatencyIncreaseRatio float64 `json:"maxLatencyIncreaseRatio"`
}

func DefaultPromotionThresholds() PromotionThresholds {
	return PromotionThresholds{MaxCompletionDrop: 0.02, MaxHiddenTestDrop: 0.01, MaxRegressionIncrease: 0, MaxCostIncreaseRatio: 0.10, MaxLatencyIncreaseRatio: 0.15}
}

type PromotionDecision struct {
	Allowed bool     `json:"allowed"`
	Reasons []string `json:"reasons"`
}

func CheckPromotion(current, candidate Report, thresholds PromotionThresholds) PromotionDecision {
	decision := PromotionDecision{Allowed: false, Reasons: []string{}}
	if current.Dataset != candidate.Dataset || current.DatasetVersion != candidate.DatasetVersion {
		decision.Reasons = append(decision.Reasons, "dataset_mismatch")
	}
	if current.Total < 30 || candidate.Total < 30 {
		decision.Reasons = append(decision.Reasons, "insufficient_cases")
	}
	if len(current.Unavailable) > 0 || len(candidate.Unavailable) > 0 || current.Metrics.AverageCostUSD == nil || candidate.Metrics.AverageCostUSD == nil || current.Metrics.P95LatencyMS == nil || candidate.Metrics.P95LatencyMS == nil {
		decision.Reasons = append(decision.Reasons, "incomplete_measurements")
	}
	if candidate.Metrics.CompletionRate < current.Metrics.CompletionRate-thresholds.MaxCompletionDrop {
		decision.Reasons = append(decision.Reasons, "completion_regression")
	}
	if candidate.Metrics.HiddenTestPassRate < current.Metrics.HiddenTestPassRate-thresholds.MaxHiddenTestDrop {
		decision.Reasons = append(decision.Reasons, "hidden_test_regression")
	}
	if candidate.Metrics.RegressionRate > current.Metrics.RegressionRate+thresholds.MaxRegressionIncrease {
		decision.Reasons = append(decision.Reasons, "regression_rate_increase")
	}
	if current.Metrics.AverageCostUSD != nil && candidate.Metrics.AverageCostUSD != nil && *candidate.Metrics.AverageCostUSD > *current.Metrics.AverageCostUSD*(1+thresholds.MaxCostIncreaseRatio) {
		decision.Reasons = append(decision.Reasons, "cost_regression")
	}
	if current.Metrics.P95LatencyMS != nil && candidate.Metrics.P95LatencyMS != nil && *candidate.Metrics.P95LatencyMS > *current.Metrics.P95LatencyMS*(1+thresholds.MaxLatencyIncreaseRatio) {
		decision.Reasons = append(decision.Reasons, "latency_regression")
	}
	for _, grade := range candidate.Grades {
		if !grade.Passed {
			decision.Reasons = append(decision.Reasons, fmt.Sprintf("deterministic_failure:%s", grade.CaseID))
			break
		}
	}
	decision.Allowed = len(decision.Reasons) == 0
	return decision
}
