package planner

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"strings"

	"forgeflow/internal/apperror"
	"forgeflow/internal/domain"
	"forgeflow/internal/model"
)

//go:embed evals/*.json
var evalSuites embed.FS

type EvalCase struct {
	Name        string           `json:"name"`
	Category    string           `json:"category"`
	Task        string           `json:"task"`
	Output      json.RawMessage  `json:"output"`
	ExpectValid bool             `json:"expectValid"`
	MinimumRisk domain.RiskLevel `json:"minimumRisk,omitempty"`
}

type EvalFailure struct {
	Case    string `json:"case"`
	Message string `json:"message"`
}

type EvalReport struct {
	Suite    string        `json:"suite"`
	Total    int           `json:"total"`
	Passed   int           `json:"passed"`
	Failures []EvalFailure `json:"failures"`
}

func RunEvalSuite(suite string) (EvalReport, error) {
	if suite == "" {
		suite = "planner/v1"
	}
	if suite != "planner/v1" {
		return EvalReport{}, apperror.New(apperror.CodeValidation, "only planner/v1 eval suite is available")
	}
	data, err := evalSuites.ReadFile("evals/planner_v1.json")
	if err != nil {
		return EvalReport{}, apperror.Wrap(err, apperror.CodeInternal, "planner.eval.load", "planner eval suite could not be loaded")
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var cases []EvalCase
	if err := decoder.Decode(&cases); err != nil {
		return EvalReport{}, apperror.Wrap(err, apperror.CodeInternal, "planner.eval.decode", "planner eval suite is invalid")
	}
	report := EvalReport{Suite: suite, Total: len(cases), Failures: []EvalFailure{}}
	for _, evalCase := range cases {
		fake := &model.FakeProvider{Responses: []model.Response{{
			ID: "eval-" + evalCase.Name, Model: "eval-model", Status: "completed", OutputText: string(evalCase.Output),
		}}}
		agent, agentErr := NewAgent(Options{Provider: fake, Model: "eval-model"})
		if agentErr != nil {
			return EvalReport{}, agentErr
		}
		result, planErr := agent.CreatePlan(context.Background(), Input{
			Task: evalCase.Task, RepositoryPath: ".", RepositoryContext: "Fixed planner evaluation fixture.",
		})
		if evalCase.ExpectValid && planErr != nil {
			report.Failures = append(report.Failures, EvalFailure{Case: evalCase.Name, Message: planErr.Error()})
			continue
		}
		if !evalCase.ExpectValid && planErr == nil {
			report.Failures = append(report.Failures, EvalFailure{Case: evalCase.Name, Message: "invalid output was accepted"})
			continue
		}
		if planErr == nil && !riskAtLeast(result.Plan.HighestRisk(), evalCase.MinimumRisk) {
			report.Failures = append(report.Failures, EvalFailure{Case: evalCase.Name, Message: fmt.Sprintf("risk %s is below expected %s", result.Plan.HighestRisk(), evalCase.MinimumRisk)})
			continue
		}
		report.Passed++
	}
	return report, nil
}

func riskAtLeast(actual, minimum domain.RiskLevel) bool {
	order := map[domain.RiskLevel]int{domain.RiskLow: 1, domain.RiskMedium: 2, domain.RiskHigh: 3}
	return minimum == "" || order[actual] >= order[minimum]
}
