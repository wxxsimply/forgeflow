package repository

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"forgeflow/internal/apperror"
	"forgeflow/internal/domain"
)

const maxDiscoveredDocuments = 32

type GitInspector struct {
	limits Limits
	git    gitRunner
}

func NewGitInspector(limits Limits) *GitInspector {
	return &GitInspector{limits: limits, git: newGitRunner(limits.MaxPatchBytes)}
}

func (i *GitInspector) Inspect(ctx context.Context, ref domain.RepositoryRef) (domain.RepositorySummary, error) {
	path, err := filepath.Abs(ref.Path)
	if err != nil {
		return domain.RepositorySummary{}, apperror.Wrap(err, apperror.CodeValidation, "repository.inspect.path", "repository path is invalid")
	}
	rootOutput, err := i.git.run(ctx, path, "rev-parse", "--show-toplevel")
	if err != nil {
		return domain.RepositorySummary{}, apperror.Wrap(err, apperror.CodeValidation, "repository.inspect.root", "path is not inside a Git repository")
	}
	root := filepath.Clean(strings.TrimSpace(string(rootOutput)))
	root, err = filepath.Abs(root)
	if err != nil {
		return domain.RepositorySummary{}, apperror.Wrap(err, apperror.CodeValidation, "repository.inspect.root", "Git repository root is invalid")
	}

	revision := strings.TrimSpace(ref.BaseRevision)
	if revision == "" {
		revision = "HEAD"
	}
	if err := validateRevision(revision); err != nil {
		return domain.RepositorySummary{}, err
	}
	baseOutput, err := i.git.run(ctx, root, "rev-parse", "--verify", "--end-of-options", revision+"^{commit}")
	if err != nil {
		return domain.RepositorySummary{}, apperror.Wrap(err, apperror.CodeNotFound, "repository.inspect.revision", "base revision does not resolve to a commit")
	}
	baseCommit := strings.TrimSpace(string(baseOutput))
	headOutput, err := i.git.run(ctx, root, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return domain.RepositorySummary{}, apperror.Wrap(err, apperror.CodeNotFound, "repository.inspect.head", "repository has no HEAD commit")
	}
	statusOutput, err := i.git.run(ctx, root, "status", "--porcelain=v1", "--untracked-files=normal")
	if err != nil {
		return domain.RepositorySummary{}, apperror.Wrap(err, apperror.CodeInternal, "repository.inspect.status", "repository status could not be read")
	}

	reader, err := NewSafeFileReader(root, i.limits)
	if err != nil {
		return domain.RepositorySummary{}, err
	}
	documents, err := discoverDocuments(ctx, reader)
	if err != nil {
		return domain.RepositorySummary{}, err
	}
	return domain.RepositorySummary{
		Root: root, RequestedRevision: revision, BaseCommit: baseCommit,
		HeadCommit: strings.TrimSpace(string(headOutput)), Clean: len(bytesTrimSpace(statusOutput)) == 0,
		Documents: documents,
	}, nil
}

func discoverDocuments(ctx context.Context, reader *SafeFileReader) ([]domain.DiscoveredDocument, error) {
	listed, err := reader.ListFiles(ctx, ".", true)
	if err != nil {
		return nil, err
	}
	documents := make([]domain.DiscoveredDocument, 0)
	for _, entry := range listed.Entries {
		if entry.IsDir || entry.IsSymlink {
			continue
		}
		kind, wanted := classifyDocument(entry.Path)
		if !wanted {
			continue
		}
		content, err := reader.ReadFile(ctx, entry.Path)
		if err != nil {
			if apperror.IsCode(err, apperror.CodePolicyDenied) {
				continue
			}
			return nil, err
		}
		documents = append(documents, domain.DiscoveredDocument{
			Path: content.Path, Kind: kind, Size: content.Size, SHA256: content.SHA256, Content: content.Text,
		})
		if len(documents) >= maxDiscoveredDocuments {
			break
		}
	}
	sort.Slice(documents, func(left, right int) bool { return documents[left].Path < documents[right].Path })
	return documents, nil
}

func classifyDocument(path string) (domain.DocumentKind, bool) {
	base := strings.ToLower(filepath.Base(path))
	switch {
	case base == "agents.md":
		return domain.DocumentProjectRule, true
	case strings.HasPrefix(base, "readme"):
		return domain.DocumentReadme, true
	case base == "go.mod" || base == "package.json" || base == "makefile" || base == "pyproject.toml" || base == "cargo.toml" || base == "pom.xml" || strings.HasPrefix(base, "build.gradle"):
		return domain.DocumentBuildConfig, true
	case base == "vitest.config.ts" || base == "jest.config.js" || base == "pytest.ini" || base == ".golangci.yml":
		return domain.DocumentTestConfig, true
	default:
		return "", false
	}
}

func validateRevision(revision string) error {
	if revision == "" || len(revision) > 200 || strings.HasPrefix(revision, "-") || strings.ContainsAny(revision, "\x00\r\n\t ") {
		return apperror.New(apperror.CodeValidation, "base revision contains invalid characters")
	}
	return nil
}

func bytesTrimSpace(value []byte) []byte {
	return []byte(strings.TrimSpace(string(value)))
}

var _ RepositoryInspector = (*GitInspector)(nil)

func (i *GitInspector) String() string {
	return fmt.Sprintf("GitInspector(maxReadBytes=%d)", i.limits.MaxReadBytes)
}
