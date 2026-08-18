package developer

import (
	"context"
	"path/filepath"
	"sort"
	"strings"

	"forgeflow/internal/apperror"
	"forgeflow/internal/domain"
	"forgeflow/internal/repository"
)

type ContextBundle struct {
	Task             string                   `json:"task"`
	ApprovedPlan     domain.ExecutionPlan     `json:"approvedPlan"`
	Workspace        WorkspaceSummary         `json:"workspace"`
	ProjectRules     []repository.FileContent `json:"projectRules"`
	ApprovedFiles    []ApprovedFile           `json:"approvedFiles"`
	ToolNames        []string                 `json:"toolNames"`
	PreviousTest     *domain.TestAssessment   `json:"previousTest,omitempty"`
	CurrentDiff      *domain.DiffArtifact     `json:"currentDiff,omitempty"`
	ReviewFindings   []domain.ReviewFinding   `json:"reviewFindings,omitempty"`
	SecurityFindings []domain.SecurityFinding `json:"securityFindings,omitempty"`
}

type WorkspaceSummary struct {
	ID         string `json:"id"`
	BaseCommit string `json:"baseCommit"`
}

type ApprovedFile struct {
	Path    string `json:"path"`
	Exists  bool   `json:"exists"`
	SHA256  string `json:"sha256,omitempty"`
	Content string `json:"content,omitempty"`
}

type ContextBuilder struct {
	limits   repository.Limits
	maxBytes int
}

func NewContextBuilder(limits repository.Limits, maxBytes int) *ContextBuilder {
	if maxBytes <= 0 {
		maxBytes = 128 * 1024
	}
	return &ContextBuilder{limits: limits, maxBytes: maxBytes}
}

func (b *ContextBuilder) Build(ctx context.Context, input Input) (ContextBundle, error) {
	if input.Workspace.ID == "" || input.Workspace.Path == "" {
		return ContextBundle{}, apperror.New(apperror.CodeValidation, "developer workspace is required")
	}
	if err := input.Plan.Validate(); err != nil {
		return ContextBundle{}, apperror.Wrap(err, apperror.CodeValidation, "developer.context.plan", "approved plan is invalid")
	}
	reader, err := repository.NewSafeFileReader(input.Workspace.Path, b.limits)
	if err != nil {
		return ContextBundle{}, err
	}
	bundle := ContextBundle{
		Task: input.Task, ApprovedPlan: input.Plan,
		Workspace:    WorkspaceSummary{ID: input.Workspace.ID, BaseCommit: input.Workspace.BaseCommit},
		ProjectRules: []repository.FileContent{}, ApprovedFiles: []ApprovedFile{},
		ToolNames: append([]string(nil), input.ToolNames...), PreviousTest: input.PreviousTest,
		CurrentDiff:      input.CurrentDiff,
		ReviewFindings:   append([]domain.ReviewFinding(nil), input.ReviewFindings...),
		SecurityFindings: append([]domain.SecurityFinding(nil), input.SecurityFindings...),
	}
	listed, err := reader.ListFiles(ctx, ".", true)
	if err != nil {
		return ContextBundle{}, err
	}
	known := make(map[string]repository.FileEntry, len(listed.Entries))
	for _, entry := range listed.Entries {
		known[entry.Path] = entry
	}
	remaining := b.maxBytes
	if input.CurrentDiff != nil {
		remaining -= len(input.CurrentDiff.Patch)
		if remaining < 0 {
			return ContextBundle{}, apperror.New(apperror.CodeBudget, "developer repair diff exceeds the context byte limit")
		}
	}
	rulePaths := make([]string, 0)
	for path, entry := range known {
		if !entry.IsDir && !entry.IsSymlink && strings.EqualFold(filepath.Base(path), "AGENTS.md") {
			rulePaths = append(rulePaths, path)
		}
	}
	sort.Strings(rulePaths)
	for _, path := range rulePaths {
		content, err := reader.ReadFile(ctx, path)
		if err != nil {
			return ContextBundle{}, err
		}
		if len(content.Text) > remaining {
			return ContextBundle{}, apperror.New(apperror.CodeBudget, "developer project rules exceed the context byte limit")
		}
		remaining -= len(content.Text)
		bundle.ProjectRules = append(bundle.ProjectRules, content)
	}
	for _, path := range input.Plan.FilesLikelyAffected {
		entry, exists := known[path]
		if !exists {
			bundle.ApprovedFiles = append(bundle.ApprovedFiles, ApprovedFile{Path: path, Exists: false})
			continue
		}
		if entry.IsDir || entry.IsSymlink {
			return ContextBundle{}, apperror.New(apperror.CodePolicyDenied, "approved implementation path is not a regular file")
		}
		content, err := reader.ReadFile(ctx, path)
		if err != nil {
			return ContextBundle{}, err
		}
		if len(content.Text) > remaining {
			return ContextBundle{}, apperror.New(apperror.CodeBudget, "developer approved files exceed the context byte limit")
		}
		remaining -= len(content.Text)
		bundle.ApprovedFiles = append(bundle.ApprovedFiles, ApprovedFile{
			Path: path, Exists: true, SHA256: content.SHA256, Content: content.Text,
		})
	}
	sort.Strings(bundle.ToolNames)
	return bundle, nil
}
