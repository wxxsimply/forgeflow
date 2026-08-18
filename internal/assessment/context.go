package assessment

import (
	"context"
	"path/filepath"
	"sort"
	"strings"

	"forgeflow/internal/apperror"
	"forgeflow/internal/domain"
	"forgeflow/internal/repository"
)

type Input struct {
	RunID     string
	NodeID    string
	Task      string
	Plan      domain.ExecutionPlan
	Workspace domain.WorkspaceRef
	Diff      domain.DiffArtifact
	Test      domain.TestAssessment
	Budget    domain.RunBudget
}

type Bundle struct {
	Task         string                   `json:"task"`
	ApprovedPlan domain.ExecutionPlan     `json:"approvedPlan"`
	ProjectRules []repository.FileContent `json:"projectRules"`
	Diff         domain.DiffArtifact      `json:"diff"`
	Test         domain.TestAssessment    `json:"test"`
}

type ContextBuilder struct {
	limits   repository.Limits
	maxBytes int
}

func NewContextBuilder(limits repository.Limits, maxBytes int) *ContextBuilder {
	if maxBytes <= 0 {
		maxBytes = 512 * 1024
	}
	return &ContextBuilder{limits: limits, maxBytes: maxBytes}
}

func (b *ContextBuilder) Build(ctx context.Context, input Input) (Bundle, error) {
	if strings.TrimSpace(input.Task) == "" || input.Workspace.Path == "" {
		return Bundle{}, apperror.New(apperror.CodeValidation, "assessment task and workspace are required")
	}
	if err := input.Plan.Validate(); err != nil {
		return Bundle{}, apperror.Wrap(err, apperror.CodeValidation, "assessment.context.plan", "approved plan is invalid")
	}
	if input.Diff.SHA256 == "" || len(input.Diff.ChangedFiles) == 0 || input.Test.ToolCallID == "" {
		return Bundle{}, apperror.New(apperror.CodeValidation, "assessment requires final diff and test evidence")
	}
	if len(input.Diff.Patch) > b.maxBytes {
		return Bundle{}, apperror.New(apperror.CodeBudget, "assessment diff exceeds context byte limit")
	}
	reader, err := repository.NewSafeFileReader(input.Workspace.Path, b.limits)
	if err != nil {
		return Bundle{}, err
	}
	listed, err := reader.ListFiles(ctx, ".", true)
	if err != nil {
		return Bundle{}, err
	}
	rulePaths := make([]string, 0)
	for _, entry := range listed.Entries {
		if !entry.IsDir && !entry.IsSymlink && strings.EqualFold(filepath.Base(entry.Path), "AGENTS.md") {
			rulePaths = append(rulePaths, entry.Path)
		}
	}
	sort.Strings(rulePaths)
	remaining := b.maxBytes - len(input.Diff.Patch)
	rules := make([]repository.FileContent, 0, len(rulePaths))
	for _, path := range rulePaths {
		content, err := reader.ReadFile(ctx, path)
		if err != nil {
			return Bundle{}, err
		}
		if len(content.Text) > remaining {
			return Bundle{}, apperror.New(apperror.CodeBudget, "assessment project rules exceed context byte limit")
		}
		remaining -= len(content.Text)
		rules = append(rules, content)
	}
	return Bundle{Task: input.Task, ApprovedPlan: input.Plan, ProjectRules: rules, Diff: input.Diff, Test: input.Test}, nil
}
