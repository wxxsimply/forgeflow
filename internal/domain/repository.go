package domain

import "time"

type RepositoryRef struct {
	Path         string `json:"path"`
	BaseRevision string `json:"baseRevision"`
}

type DocumentKind string

const (
	DocumentProjectRule DocumentKind = "project_rule"
	DocumentReadme      DocumentKind = "readme"
	DocumentBuildConfig DocumentKind = "build_config"
	DocumentTestConfig  DocumentKind = "test_config"
)

type DiscoveredDocument struct {
	Path      string       `json:"path"`
	Kind      DocumentKind `json:"kind"`
	Size      int64        `json:"size"`
	SHA256    string       `json:"sha256"`
	Content   string       `json:"content"`
	Truncated bool         `json:"truncated"`
}

type RepositorySummary struct {
	Root              string               `json:"root"`
	RequestedRevision string               `json:"requestedRevision"`
	BaseCommit        string               `json:"baseCommit"`
	HeadCommit        string               `json:"headCommit"`
	Clean             bool                 `json:"clean"`
	Documents         []DiscoveredDocument `json:"documents"`
}

type WorkspaceRef struct {
	ID             string    `json:"id"`
	Path           string    `json:"path"`
	RepositoryRoot string    `json:"repositoryRoot"`
	BaseCommit     string    `json:"baseCommit"`
	CreatedAt      time.Time `json:"createdAt"`
}

type DiffArtifact struct {
	Patch        string   `json:"patch"`
	SHA256       string   `json:"sha256"`
	Size         int64    `json:"size"`
	ChangedFiles []string `json:"changedFiles"`
}
