package planner

import (
	"strings"
	"testing"

	"forgeflow/internal/apperror"
)

func TestDecodeExecutionPlanAcceptsStrictValidPlan(t *testing.T) {
	plan, err := DecodeExecutionPlan(validPlanJSON())
	if err != nil {
		t.Fatalf("DecodeExecutionPlan() error = %v", err)
	}
	if len(plan.Steps) != 2 || plan.Steps[1].DependsOn[0] != "inspect" {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestDecodeExecutionPlanRejectsUnknownMissingAndTrailingFields(t *testing.T) {
	tests := map[string]string{
		"unknown":  strings.Replace(validPlanJSON(), `"summary":"fixture"`, `"summary":"fixture","surprise":true`, 1),
		"missing":  strings.Replace(validPlanJSON(), `,"dependsOn":[]`, "", 1),
		"trailing": validPlanJSON() + `{}`,
	}
	for name, output := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeExecutionPlan(output); !apperror.IsCode(err, apperror.CodeModelOutput) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestDecodeExecutionPlanRejectsInvalidRiskDependencyAndCycle(t *testing.T) {
	tests := map[string]string{
		"risk":       strings.Replace(validPlanJSON(), `"level":"medium"`, `"level":"critical"`, 1),
		"dependency": strings.Replace(validPlanJSON(), `"dependsOn":["inspect"]`, `"dependsOn":["missing"]`, 1),
		"cycle":      strings.Replace(validPlanJSON(), `"dependsOn":[]`, `"dependsOn":["implement"]`, 1),
	}
	for name, output := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeExecutionPlan(output); !apperror.IsCode(err, apperror.CodeModelOutput) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestExecutionPlanSchemaIsDefensiveCopy(t *testing.T) {
	first := ExecutionPlanSchema()
	first[0] = 'x'
	second := ExecutionPlanSchema()
	if second[0] != '{' {
		t.Fatal("schema was mutated through a returned slice")
	}
}

func validPlanJSON() string {
	return `{
      "summary":"fixture",
      "assumptions":["repository tests are available"],
      "filesLikelyAffected":["internal/example.go"],
      "steps":[
        {"id":"inspect","description":"Inspect repository","acceptanceCriteria":["Rules identified"],"dependsOn":[]},
        {"id":"implement","description":"Implement change","acceptanceCriteria":["Tests pass"],"dependsOn":["inspect"]}
      ],
      "risks":[{"level":"medium","description":"Scope may change"}],
      "testStrategy":["Run go test ./..."]
    }`
}
