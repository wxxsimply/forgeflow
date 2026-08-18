package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"forgeflow/internal/apperror"
	"forgeflow/internal/domain"
)

type GitWorkspaceManager struct {
	workspaceRoot string
	inspector     RepositoryInspector
	git           gitRunner
}

func NewGitWorkspaceManager(workspaceRoot string, limits Limits) (*GitWorkspaceManager, error) {
	if strings.TrimSpace(workspaceRoot) == "" {
		return nil, apperror.New(apperror.CodeValidation, "workspace root cannot be empty")
	}
	absolute, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return nil, apperror.Wrap(err, apperror.CodeValidation, "repository.workspace.root", "workspace root is invalid")
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return nil, apperror.Wrap(err, apperror.CodeInternal, "repository.workspace.root", "workspace root could not be created")
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, mapPathError(err, "workspace root is not accessible")
	}
	return &GitWorkspaceManager{
		workspaceRoot: filepath.Clean(resolved),
		inspector:     NewGitInspector(limits),
		git:           newGitRunner(limits.MaxPatchBytes),
	}, nil
}

func (m *GitWorkspaceManager) Prepare(ctx context.Context, ref domain.RepositoryRef) (domain.WorkspaceRef, error) {
	summary, err := m.inspector.Inspect(ctx, ref)
	if err != nil {
		return domain.WorkspaceRef{}, err
	}
	id := domain.NewID()
	workspacePath := filepath.Join(m.workspaceRoot, id)
	if !withinRoot(m.workspaceRoot, workspacePath) {
		return domain.WorkspaceRef{}, apperror.New(apperror.CodePolicyDenied, "generated workspace path escapes the workspace root")
	}
	if _, err := os.Stat(workspacePath); !errors.Is(err, os.ErrNotExist) {
		return domain.WorkspaceRef{}, apperror.New(apperror.CodeConflict, "generated workspace path already exists")
	}
	if _, err := m.git.run(ctx, summary.Root, "worktree", "add", "--detach", workspacePath, summary.BaseCommit); err != nil {
		return domain.WorkspaceRef{}, apperror.Wrap(err, apperror.CodeInternal, "repository.workspace.prepare", "Git worktree could not be created")
	}
	resolvedWorkspace, err := filepath.EvalSymlinks(workspacePath)
	if err != nil || !withinRoot(m.workspaceRoot, resolvedWorkspace) {
		_ = m.removeWorktree(context.WithoutCancel(ctx), summary.Root, workspacePath)
		return domain.WorkspaceRef{}, apperror.New(apperror.CodePolicyDenied, "created worktree is outside the managed workspace root")
	}
	head, err := m.git.run(ctx, resolvedWorkspace, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil || strings.TrimSpace(string(head)) != summary.BaseCommit {
		_ = m.removeWorktree(context.WithoutCancel(ctx), summary.Root, resolvedWorkspace)
		return domain.WorkspaceRef{}, apperror.New(apperror.CodeInternal, "created worktree does not match the requested base commit")
	}
	return domain.WorkspaceRef{
		ID: id, Path: filepath.Clean(resolvedWorkspace), RepositoryRoot: summary.Root,
		BaseCommit: summary.BaseCommit, CreatedAt: time.Now().UTC(),
	}, nil
}

func (m *GitWorkspaceManager) Diff(ctx context.Context, workspace domain.WorkspaceRef) (domain.DiffArtifact, error) {
	workspacePath, err := m.validateWorkspace(ctx, workspace)
	if err != nil {
		return domain.DiffArtifact{}, err
	}
	if !validCommitHash(workspace.BaseCommit) {
		return domain.DiffArtifact{}, apperror.New(apperror.CodeValidation, "workspace base commit is invalid")
	}
	if _, err := m.git.run(ctx, workspacePath, "add", "--intent-to-add", "--all", "--", "."); err != nil {
		return domain.DiffArtifact{}, apperror.Wrap(err, apperror.CodeInternal, "repository.workspace.diff", "new files could not be prepared for diff")
	}
	patch, err := m.git.run(ctx, workspacePath, "diff", "--binary", "--no-ext-diff", "--no-renames", workspace.BaseCommit, "--")
	if err != nil {
		return domain.DiffArtifact{}, apperror.Wrap(err, apperror.CodeInternal, "repository.workspace.diff", "workspace diff could not be generated")
	}
	namesOutput, err := m.git.run(ctx, workspacePath, "diff", "--name-only", "-z", "--no-renames", workspace.BaseCommit, "--")
	if err != nil {
		return domain.DiffArtifact{}, apperror.Wrap(err, apperror.CodeInternal, "repository.workspace.diff", "changed file list could not be generated")
	}
	changedFiles := parseNULList(namesOutput)
	sort.Strings(changedFiles)
	digest := sha256.Sum256(patch)
	return domain.DiffArtifact{
		Patch: string(patch), SHA256: hex.EncodeToString(digest[:]), Size: int64(len(patch)), ChangedFiles: changedFiles,
	}, nil
}

func (m *GitWorkspaceManager) Cleanup(ctx context.Context, workspace domain.WorkspaceRef) error {
	if strings.TrimSpace(workspace.RepositoryRoot) == "" {
		return apperror.New(apperror.CodeValidation, "workspace repository root is required")
	}
	workspacePath, err := filepath.Abs(workspace.Path)
	if err != nil || !withinRoot(m.workspaceRoot, workspacePath) {
		return apperror.New(apperror.CodePolicyDenied, "workspace cleanup path is outside the managed root")
	}
	if _, err := os.Stat(workspacePath); errors.Is(err, os.ErrNotExist) {
		_, pruneErr := m.git.run(ctx, workspace.RepositoryRoot, "worktree", "prune")
		return pruneErr
	} else if err != nil {
		return mapPathError(err, "workspace cannot be inspected before cleanup")
	}
	if err := m.removeWorktree(ctx, workspace.RepositoryRoot, workspacePath); err != nil {
		return err
	}
	if _, err := os.Stat(workspacePath); !errors.Is(err, os.ErrNotExist) {
		return apperror.New(apperror.CodeInternal, "Git reported cleanup success but the workspace still exists")
	}
	return nil
}

func (m *GitWorkspaceManager) validateWorkspace(ctx context.Context, workspace domain.WorkspaceRef) (string, error) {
	if workspace.ID == "" || workspace.Path == "" || workspace.RepositoryRoot == "" {
		return "", apperror.New(apperror.CodeValidation, "workspace reference is incomplete")
	}
	absolute, err := filepath.Abs(workspace.Path)
	if err != nil || !withinRoot(m.workspaceRoot, absolute) {
		return "", apperror.New(apperror.CodePolicyDenied, "workspace path is outside the managed root")
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", mapPathError(err, "workspace does not exist")
	}
	if !withinRoot(m.workspaceRoot, resolved) {
		return "", apperror.New(apperror.CodePolicyDenied, "workspace symbolic link escapes the managed root")
	}
	rootOutput, err := m.git.run(ctx, resolved, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", apperror.Wrap(err, apperror.CodeValidation, "repository.workspace.validate", "workspace is not a Git worktree")
	}
	gitRoot := filepath.Clean(strings.TrimSpace(string(rootOutput)))
	if !samePath(gitRoot, resolved) {
		return "", apperror.New(apperror.CodePolicyDenied, "workspace Git root does not match the managed path")
	}
	return filepath.Clean(resolved), nil
}

func (m *GitWorkspaceManager) removeWorktree(ctx context.Context, repositoryRoot, workspacePath string) error {
	if !withinRoot(m.workspaceRoot, workspacePath) {
		return apperror.New(apperror.CodePolicyDenied, "refusing to remove a worktree outside the managed root")
	}
	if _, err := m.git.run(ctx, repositoryRoot, "worktree", "remove", "--force", workspacePath); err != nil {
		return apperror.Wrap(err, apperror.CodeInternal, "repository.workspace.cleanup", "Git worktree could not be removed")
	}
	_, err := m.git.run(ctx, repositoryRoot, "worktree", "prune")
	return err
}

func validCommitHash(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func parseNULList(value []byte) []string {
	parts := strings.Split(string(value), "\x00")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			result = append(result, filepath.ToSlash(part))
		}
	}
	return result
}

func samePath(left, right string) bool {
	leftAbsolute, leftErr := filepath.Abs(left)
	rightAbsolute, rightErr := filepath.Abs(right)
	if leftErr != nil || rightErr != nil {
		return false
	}
	relative, err := filepath.Rel(leftAbsolute, rightAbsolute)
	return err == nil && relative == "."
}

var _ WorkspaceManager = (*GitWorkspaceManager)(nil)

func (m *GitWorkspaceManager) String() string {
	return fmt.Sprintf("GitWorkspaceManager(root=%s)", m.workspaceRoot)
}
