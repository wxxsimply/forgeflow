package repository

import (
	"context"

	"forgeflow/internal/domain"
)

type RepositoryInspector interface {
	Inspect(context.Context, domain.RepositoryRef) (domain.RepositorySummary, error)
}

type FileReader interface {
	ListFiles(context.Context, string, bool) (FileList, error)
	ReadFile(context.Context, string) (FileContent, error)
	SearchCode(context.Context, string, SearchOptions) ([]SearchMatch, error)
}

type WorkspaceManager interface {
	Prepare(context.Context, domain.RepositoryRef) (domain.WorkspaceRef, error)
	Diff(context.Context, domain.WorkspaceRef) (domain.DiffArtifact, error)
	Cleanup(context.Context, domain.WorkspaceRef) error
}

type Limits struct {
	MaxReadBytes     int64
	MaxListEntries   int
	MaxSearchMatches int
	MaxSearchFiles   int
	MaxPatchBytes    int64
}

func DefaultLimits() Limits {
	return Limits{
		MaxReadBytes:     256 * 1024,
		MaxListEntries:   10_000,
		MaxSearchMatches: 500,
		MaxSearchFiles:   5_000,
		MaxPatchBytes:    4 * 1024 * 1024,
	}
}

type FileEntry struct {
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	IsDir     bool   `json:"isDir"`
	IsSymlink bool   `json:"isSymlink"`
}

type FileList struct {
	Entries   []FileEntry `json:"entries"`
	Truncated bool        `json:"truncated"`
}

type FileContent struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	Text   string `json:"text"`
}

type SearchOptions struct {
	CaseSensitive bool
	Extensions    []string
}

type SearchMatch struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Preview string `json:"preview"`
}
