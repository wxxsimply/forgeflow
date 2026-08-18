package planner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"forgeflow/internal/apperror"
	"forgeflow/internal/domain"
)

const maxPlanOutputBytes = 1024 * 1024

var executionPlanSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["summary", "assumptions", "filesLikelyAffected", "steps", "risks", "testStrategy"],
  "properties": {
    "summary": {"type": "string", "minLength": 1, "maxLength": 2000},
    "assumptions": {"type": "array", "maxItems": 50, "items": {"type": "string", "minLength": 1}},
    "filesLikelyAffected": {"type": "array", "maxItems": 200, "items": {"type": "string", "minLength": 1}},
    "steps": {
      "type": "array",
      "minItems": 1,
      "maxItems": 50,
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["id", "description", "acceptanceCriteria", "dependsOn"],
        "properties": {
          "id": {"type": "string", "pattern": "^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$"},
          "description": {"type": "string", "minLength": 1},
          "acceptanceCriteria": {"type": "array", "minItems": 1, "maxItems": 20, "items": {"type": "string", "minLength": 1}},
          "dependsOn": {"type": "array", "maxItems": 20, "items": {"type": "string", "minLength": 1}}
        }
      }
    },
    "risks": {
      "type": "array",
      "maxItems": 50,
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["level", "description"],
        "properties": {
          "level": {"type": "string", "enum": ["low", "medium", "high"]},
          "description": {"type": "string", "minLength": 1}
        }
      }
    },
    "testStrategy": {"type": "array", "minItems": 1, "maxItems": 50, "items": {"type": "string", "minLength": 1}}
  }
}`)

func ExecutionPlanSchema() json.RawMessage {
	return append(json.RawMessage(nil), executionPlanSchema...)
}

type strictPlan struct {
	Summary             *string       `json:"summary"`
	Assumptions         *[]string     `json:"assumptions"`
	FilesLikelyAffected *[]string     `json:"filesLikelyAffected"`
	Steps               *[]strictStep `json:"steps"`
	Risks               *[]strictRisk `json:"risks"`
	TestStrategy        *[]string     `json:"testStrategy"`
}

type strictStep struct {
	ID                 *string   `json:"id"`
	Description        *string   `json:"description"`
	AcceptanceCriteria *[]string `json:"acceptanceCriteria"`
	DependsOn          *[]string `json:"dependsOn"`
}

type strictRisk struct {
	Level       *domain.RiskLevel `json:"level"`
	Description *string           `json:"description"`
}

func DecodeExecutionPlan(output string) (domain.ExecutionPlan, error) {
	if strings.TrimSpace(output) == "" {
		return domain.ExecutionPlan{}, apperror.New(apperror.CodeModelOutput, "model returned an empty plan")
	}
	if len(output) > maxPlanOutputBytes {
		return domain.ExecutionPlan{}, apperror.New(apperror.CodeModelOutput, "model plan exceeds the output size limit")
	}
	decoder := json.NewDecoder(bytes.NewBufferString(output))
	decoder.DisallowUnknownFields()
	var decoded strictPlan
	if err := decoder.Decode(&decoded); err != nil {
		return domain.ExecutionPlan{}, apperror.Wrap(err, apperror.CodeModelOutput, "planner.decode", "model plan is not valid strict JSON")
	}
	if err := requireJSONEOF(decoder); err != nil {
		return domain.ExecutionPlan{}, apperror.Wrap(err, apperror.CodeModelOutput, "planner.decode", "model plan contains trailing data")
	}
	if decoded.Summary == nil || decoded.Assumptions == nil || decoded.FilesLikelyAffected == nil || decoded.Steps == nil || decoded.Risks == nil || decoded.TestStrategy == nil {
		return domain.ExecutionPlan{}, apperror.New(apperror.CodeModelOutput, "model plan is missing a required top-level field")
	}
	steps := make([]domain.PlanStep, 0, len(*decoded.Steps))
	for index, step := range *decoded.Steps {
		if step.ID == nil || step.Description == nil || step.AcceptanceCriteria == nil || step.DependsOn == nil {
			return domain.ExecutionPlan{}, apperror.New(apperror.CodeModelOutput, fmt.Sprintf("model plan step %d is missing a required field", index))
		}
		steps = append(steps, domain.PlanStep{
			ID: *step.ID, Description: *step.Description,
			AcceptanceCriteria: *step.AcceptanceCriteria, DependsOn: *step.DependsOn,
		})
	}
	risks := make([]domain.PlanRisk, 0, len(*decoded.Risks))
	for index, risk := range *decoded.Risks {
		if risk.Level == nil || risk.Description == nil {
			return domain.ExecutionPlan{}, apperror.New(apperror.CodeModelOutput, fmt.Sprintf("model plan risk %d is missing a required field", index))
		}
		risks = append(risks, domain.PlanRisk{Level: *risk.Level, Description: *risk.Description})
	}
	plan := domain.ExecutionPlan{
		Summary: *decoded.Summary, Assumptions: *decoded.Assumptions,
		FilesLikelyAffected: *decoded.FilesLikelyAffected, Steps: steps,
		Risks: risks, TestStrategy: *decoded.TestStrategy,
	}
	if err := plan.Validate(); err != nil {
		return domain.ExecutionPlan{}, apperror.Wrap(err, apperror.CodeModelOutput, "planner.validate", "model plan failed domain validation")
	}
	return plan, nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if err == io.EOF {
		return nil
	}
	if err == nil {
		return fmt.Errorf("unexpected second JSON value")
	}
	return err
}
