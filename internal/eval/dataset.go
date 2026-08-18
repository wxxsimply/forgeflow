package eval

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const SoftwareV1 = "software/v1"

//go:embed datasets/*.json
var datasets embed.FS

type Category string

const (
	CategoryFeature   Category = "feature"
	CategoryBugfix    Category = "bugfix"
	CategoryTests     Category = "tests"
	CategorySecurity  Category = "security"
	CategoryRefactor  Category = "refactor"
	CategoryAmbiguous Category = "ambiguous"
	CategoryDangerous Category = "dangerous"
)

type Decision string

const (
	DecisionImplement       Decision = "implement"
	DecisionClarify         Decision = "clarify"
	DecisionDeny            Decision = "deny"
	DecisionRequireApproval Decision = "require_approval"
)

type Command struct {
	Program string   `json:"program"`
	Args    []string `json:"args"`
}

// VerifyFixtureCommits proves that every dataset fixture points at a real commit
// in the selected immutable fixture repository. Dataset shape validation alone
// intentionally does not claim that the fixtures exist.
func VerifyFixtureCommits(ctx context.Context, dataset Dataset, repositoryPath string) error {
	repositoryPath = strings.TrimSpace(repositoryPath)
	if repositoryPath == "" {
		return fmt.Errorf("fixture repository path is required")
	}
	absolute, err := filepath.Abs(repositoryPath)
	if err != nil {
		return fmt.Errorf("resolve fixture repository: %w", err)
	}
	if output, err := exec.CommandContext(ctx, "git", "-C", absolute, "rev-parse", "--is-inside-work-tree").CombinedOutput(); err != nil || strings.TrimSpace(string(output)) != "true" {
		return fmt.Errorf("fixture repository is not a readable Git worktree")
	}
	for _, evalCase := range dataset.Cases {
		object := evalCase.FixtureCommit + "^{commit}"
		if err := exec.CommandContext(ctx, "git", "-C", absolute, "cat-file", "-e", object).Run(); err != nil {
			return fmt.Errorf("case %q fixture commit %s does not exist", evalCase.ID, evalCase.FixtureCommit)
		}
	}
	return nil
}

type Budget struct {
	MaxIterations   int     `json:"maxIterations"`
	MaxChangedFiles int     `json:"maxChangedFiles"`
	MaxDiffLines    int     `json:"maxDiffLines"`
	MaxCostUSD      float64 `json:"maxCostUsd"`
}

type Case struct {
	ID                string   `json:"id"`
	Category          Category `json:"category"`
	FixtureCommit     string   `json:"fixtureCommit"`
	Task              string   `json:"task"`
	ForbiddenFiles    []string `json:"forbiddenFiles"`
	ValidationCommand Command  `json:"validationCommand"`
	HiddenTests       []string `json:"hiddenTests"`
	ExpectedDecision  Decision `json:"expectedDecision"`
	Budget            Budget   `json:"budget"`
}

type Dataset struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Cases   []Case `json:"cases"`
}

var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

func Load(name string) (Dataset, error) {
	if name == "" {
		name = SoftwareV1
	}
	if name != SoftwareV1 {
		return Dataset{}, fmt.Errorf("unknown eval dataset %q", name)
	}
	data, err := datasets.ReadFile("datasets/software_v1.json")
	if err != nil {
		return Dataset{}, fmt.Errorf("load eval dataset: %w", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var dataset Dataset
	if err := decoder.Decode(&dataset); err != nil {
		return Dataset{}, fmt.Errorf("decode eval dataset: %w", err)
	}
	if err := ValidateDataset(dataset); err != nil {
		return Dataset{}, err
	}
	return dataset, nil
}

func ValidateDataset(dataset Dataset) error {
	if dataset.Name != SoftwareV1 || dataset.Version == "" {
		return fmt.Errorf("dataset name or version is invalid")
	}
	want := map[Category]int{
		CategoryFeature: 8, CategoryBugfix: 6, CategoryTests: 4, CategorySecurity: 4,
		CategoryRefactor: 3, CategoryAmbiguous: 2, CategoryDangerous: 3,
	}
	counts := make(map[Category]int)
	ids := make(map[string]struct{}, len(dataset.Cases))
	for index, evalCase := range dataset.Cases {
		if evalCase.ID == "" || strings.TrimSpace(evalCase.Task) == "" || !commitPattern.MatchString(evalCase.FixtureCommit) {
			return fmt.Errorf("case %d has invalid identity, task, or fixture commit", index)
		}
		if _, exists := ids[evalCase.ID]; exists {
			return fmt.Errorf("duplicate eval case %q", evalCase.ID)
		}
		ids[evalCase.ID] = struct{}{}
		if _, exists := want[evalCase.Category]; !exists {
			return fmt.Errorf("case %q has invalid category", evalCase.ID)
		}
		if !slices.Contains([]Decision{DecisionImplement, DecisionClarify, DecisionDeny, DecisionRequireApproval}, evalCase.ExpectedDecision) {
			return fmt.Errorf("case %q has invalid expected decision", evalCase.ID)
		}
		if evalCase.ValidationCommand.Program == "" || len(evalCase.HiddenTests) == 0 || len(evalCase.ForbiddenFiles) == 0 {
			return fmt.Errorf("case %q is missing grader inputs", evalCase.ID)
		}
		if evalCase.Budget.MaxIterations <= 0 || evalCase.Budget.MaxChangedFiles <= 0 || evalCase.Budget.MaxDiffLines <= 0 || evalCase.Budget.MaxCostUSD <= 0 {
			return fmt.Errorf("case %q has invalid budget", evalCase.ID)
		}
		counts[evalCase.Category]++
	}
	if len(dataset.Cases) != 30 {
		return fmt.Errorf("dataset must contain exactly 30 cases, got %d", len(dataset.Cases))
	}
	for category, expected := range want {
		if counts[category] != expected {
			return fmt.Errorf("category %s must contain %d cases, got %d", category, expected, counts[category])
		}
	}
	return nil
}
