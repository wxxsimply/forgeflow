package repository

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"forgeflow/internal/apperror"
	"forgeflow/internal/domain"
)

func TestGitInspectorResolvesCommitAndDiscoversRules(t *testing.T) {
	repositoryPath := createTestRepository(t)
	expectedCommit := runTestGit(t, repositoryPath, "rev-parse", "HEAD")
	inspector := NewGitInspector(DefaultLimits())

	summary, err := inspector.Inspect(context.Background(), domain.RepositoryRef{Path: repositoryPath, BaseRevision: "main"})
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if summary.BaseCommit != expectedCommit || summary.HeadCommit != expectedCommit {
		t.Fatalf("commits = base %q head %q, want %q", summary.BaseCommit, summary.HeadCommit, expectedCommit)
	}
	if !summary.Clean {
		t.Fatal("new fixture repository should be clean")
	}
	documents := make(map[string]domain.DocumentKind)
	for _, document := range summary.Documents {
		documents[document.Path] = document.Kind
	}
	if documents["AGENTS.md"] != domain.DocumentProjectRule || documents["README.md"] != domain.DocumentReadme || documents["go.mod"] != domain.DocumentBuildConfig {
		t.Fatalf("discovered documents = %+v", documents)
	}
}

func TestGitInspectorRejectsInvalidRepositoryAndRevision(t *testing.T) {
	inspector := NewGitInspector(DefaultLimits())
	if _, err := inspector.Inspect(context.Background(), domain.RepositoryRef{Path: filepath.Join(t.TempDir(), "missing"), BaseRevision: "HEAD"}); !apperror.IsCode(err, apperror.CodeValidation) {
		t.Fatalf("invalid repository error = %v", err)
	}

	repositoryPath := createTestRepository(t)
	if _, err := inspector.Inspect(context.Background(), domain.RepositoryRef{Path: repositoryPath, BaseRevision: "missing-branch"}); !apperror.IsCode(err, apperror.CodeNotFound) {
		t.Fatalf("missing revision error = %v", err)
	}
	if _, err := inspector.Inspect(context.Background(), domain.RepositoryRef{Path: repositoryPath, BaseRevision: "--upload-pack=evil"}); !apperror.IsCode(err, apperror.CodeValidation) {
		t.Fatalf("option-like revision error = %v", err)
	}
}

func TestGitInspectorReportsDirtyRepository(t *testing.T) {
	repositoryPath := createTestRepository(t)
	if err := os.WriteFile(filepath.Join(repositoryPath, "untracked.txt"), []byte("dirty"), 0o644); err != nil {
		t.Fatal(err)
	}
	summary, err := NewGitInspector(DefaultLimits()).Inspect(context.Background(), domain.RepositoryRef{Path: repositoryPath})
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if summary.Clean {
		t.Fatal("repository with an untracked file was reported clean")
	}
}
