package domain

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
)

type RiskLevel string

const (
	RiskLow    RiskLevel = "low"
	RiskMedium RiskLevel = "medium"
	RiskHigh   RiskLevel = "high"
)

type PlanStep struct {
	ID                 string   `json:"id"`
	Description        string   `json:"description"`
	AcceptanceCriteria []string `json:"acceptanceCriteria"`
	DependsOn          []string `json:"dependsOn"`
}

type PlanRisk struct {
	Level       RiskLevel `json:"level"`
	Description string    `json:"description"`
}

type ExecutionPlan struct {
	Summary             string     `json:"summary"`
	Assumptions         []string   `json:"assumptions"`
	FilesLikelyAffected []string   `json:"filesLikelyAffected"`
	Steps               []PlanStep `json:"steps"`
	Risks               []PlanRisk `json:"risks"`
	TestStrategy        []string   `json:"testStrategy"`
}

var planStepIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

func (p ExecutionPlan) Validate() error {
	if value := strings.TrimSpace(p.Summary); value == "" {
		return errors.New("plan summary is required")
	} else if len(value) > 2_000 {
		return errors.New("plan summary exceeds 2000 characters")
	}
	if len(p.Steps) == 0 || len(p.Steps) > 50 {
		return errors.New("plan must contain between 1 and 50 steps")
	}
	stepIDs := make(map[string]struct{}, len(p.Steps))
	for index, step := range p.Steps {
		if !planStepIDPattern.MatchString(step.ID) {
			return fmt.Errorf("step %d id must match %s", index, planStepIDPattern.String())
		}
		if _, duplicate := stepIDs[step.ID]; duplicate {
			return fmt.Errorf("duplicate step id %q", step.ID)
		}
		stepIDs[step.ID] = struct{}{}
		if strings.TrimSpace(step.Description) == "" {
			return fmt.Errorf("step %q description is required", step.ID)
		}
		if len(step.AcceptanceCriteria) == 0 || len(step.AcceptanceCriteria) > 20 {
			return fmt.Errorf("step %q needs acceptance criteria", step.ID)
		}
		if len(step.DependsOn) > 20 {
			return fmt.Errorf("step %q has too many dependencies", step.ID)
		}
		for _, criterion := range step.AcceptanceCriteria {
			if strings.TrimSpace(criterion) == "" {
				return fmt.Errorf("step %q contains an empty acceptance criterion", step.ID)
			}
		}
	}
	dependencies := make(map[string][]string, len(p.Steps))
	for _, step := range p.Steps {
		seen := make(map[string]struct{}, len(step.DependsOn))
		for _, dependency := range step.DependsOn {
			if dependency == step.ID {
				return fmt.Errorf("step %q cannot depend on itself", step.ID)
			}
			if _, exists := stepIDs[dependency]; !exists {
				return fmt.Errorf("step %q depends on unknown step %q", step.ID, dependency)
			}
			if _, duplicate := seen[dependency]; duplicate {
				return fmt.Errorf("step %q repeats dependency %q", step.ID, dependency)
			}
			seen[dependency] = struct{}{}
			dependencies[step.ID] = append(dependencies[step.ID], dependency)
		}
	}
	if cycle := findPlanCycle(dependencies); len(cycle) > 0 {
		return fmt.Errorf("plan step dependency cycle detected at %q", cycle[0])
	}
	if len(p.Assumptions) > 50 || len(p.FilesLikelyAffected) > 200 || len(p.Risks) > 50 || len(p.TestStrategy) > 50 {
		return errors.New("plan exceeds collection limits")
	}
	for _, assumption := range p.Assumptions {
		if strings.TrimSpace(assumption) == "" {
			return errors.New("plan contains an empty assumption")
		}
	}
	seenFiles := make(map[string]struct{}, len(p.FilesLikelyAffected))
	for _, file := range p.FilesLikelyAffected {
		if strings.TrimSpace(file) == "" || strings.ContainsAny(file, "\x00\\") || strings.HasPrefix(file, "/") {
			return fmt.Errorf("affected file %q must be a repository-relative slash path", file)
		}
		cleaned := path.Clean(file)
		if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || cleaned != file {
			return fmt.Errorf("affected file %q is not a normalized repository-relative path", file)
		}
		if _, duplicate := seenFiles[file]; duplicate {
			return fmt.Errorf("affected file %q is repeated", file)
		}
		seenFiles[file] = struct{}{}
	}
	for _, risk := range p.Risks {
		if risk.Level != RiskLow && risk.Level != RiskMedium && risk.Level != RiskHigh {
			return fmt.Errorf("invalid risk level %q", risk.Level)
		}
		if strings.TrimSpace(risk.Description) == "" {
			return errors.New("risk description is required")
		}
	}
	if len(p.TestStrategy) == 0 {
		return errors.New("plan must contain a test strategy")
	}
	for _, test := range p.TestStrategy {
		if strings.TrimSpace(test) == "" {
			return errors.New("plan contains an empty test strategy")
		}
	}
	return nil
}

func findPlanCycle(dependencies map[string][]string) []string {
	const (
		unvisited = iota
		visiting
		visited
	)
	states := make(map[string]int, len(dependencies))
	var visit func(string) []string
	visit = func(stepID string) []string {
		if states[stepID] == visiting {
			return []string{stepID}
		}
		if states[stepID] == visited {
			return nil
		}
		states[stepID] = visiting
		for _, dependency := range dependencies[stepID] {
			if cycle := visit(dependency); len(cycle) > 0 {
				return cycle
			}
		}
		states[stepID] = visited
		return nil
	}
	for stepID := range dependencies {
		if cycle := visit(stepID); len(cycle) > 0 {
			return cycle
		}
	}
	return nil
}

func (p ExecutionPlan) HighestRisk() RiskLevel {
	highest := RiskLow
	for _, risk := range p.Risks {
		if risk.Level == RiskHigh {
			return RiskHigh
		}
		if risk.Level == RiskMedium {
			highest = RiskMedium
		}
	}
	return highest
}
