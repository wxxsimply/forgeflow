package repository

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"forgeflow/internal/apperror"
	"forgeflow/internal/domain"
)

func TestWorkspaceDiffDoesNotModifyOriginalRepository(t *testing.T) {
	ctx := context.Background()
	repositoryPath := createTestRepository(t)
	originalReadme, err := os.ReadFile(filepath.Join(repositoryPath, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	workspaceRoot := filepath.Join(t.TempDir(), "workspaces")
	manager, err := NewGitWorkspaceManager(workspaceRoot, DefaultLimits())
	if err != nil {
		t.Fatalf("NewGitWorkspaceManager() error = %v", err)
	}
	workspace, err := manager.Prepare(ctx, domain.RepositoryRef{Path: repositoryPath, BaseRevision: "main"})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}

	writeTestFile(t, workspace.Path, "README.md", "# Changed only in worktree\n")
	writeTestFile(t, workspace.Path, "new.txt", "new file\n")
	artifact, err := manager.Diff(ctx, workspace)
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}
	if artifact.SHA256 == "" || artifact.Size == 0 || !strings.Contains(artifact.Patch, "Changed only in worktree") {
		t.Fatalf("artifact = %+v", artifact)
	}
	if !slices.Contains(artifact.ChangedFiles, "README.md") || !slices.Contains(artifact.ChangedFiles, "new.txt") {
		t.Fatalf("changed files = %+v", artifact.ChangedFiles)
	}

	afterReadme, err := os.ReadFile(filepath.Join(repositoryPath, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(afterReadme) != string(originalReadme) {
		t.Fatal("original repository README was modified")
	}
	if _, err := os.Stat(filepath.Join(repositoryPath, "new.txt")); !os.IsNotExist(err) {
		t.Fatal("new worktree file appeared in the original repository")
	}
	if status := runTestGit(t, repositoryPath, "status", "--porcelain=v1"); status != "" {
		t.Fatalf("original repository became dirty: %s", status)
	}

	if err := manager.Cleanup(ctx, workspace); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if _, err := os.Stat(workspace.Path); !os.IsNotExist(err) {
		t.Fatal("workspace still exists after cleanup")
	}
}

func TestWorkspaceRejectsInvalidRevisionAndOutsideCleanup(t *testing.T) {
	repositoryPath := createTestRepository(t)
	manager, err := NewGitWorkspaceManager(filepath.Join(t.TempDir(), "workspaces"), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Prepare(context.Background(), domain.RepositoryRef{Path: repositoryPath, BaseRevision: "missing"}); !apperror.IsCode(err, apperror.CodeNotFound) {
		t.Fatalf("missing revision error = %v", err)
	}
	outside := domain.WorkspaceRef{ID: "outside", Path: repositoryPath, RepositoryRoot: repositoryPath, BaseCommit: runTestGit(t, repositoryPath, "rev-parse", "HEAD")}
	if err := manager.Cleanup(context.Background(), outside); !apperror.IsCode(err, apperror.CodePolicyDenied) {
		t.Fatalf("outside cleanup error = %v", err)
	}
}
