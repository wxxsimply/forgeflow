package buildinfo

import (
	"regexp"
	"strings"
)

const (
	DevelopmentVersion = "development"
	UnknownCommit      = "unknown"
)

var (
	// Commit is populated with -ldflags at build time. Invalid values fail closed
	// to UnknownCommit instead of being exposed as trusted release metadata.
	Commit        = UnknownCommit
	commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

type Info struct {
	ServiceVersion string `json:"serviceVersion"`
	GitCommit      string `json:"gitCommit"`
}

func New(serviceVersion, gitCommit string) Info {
	version := strings.TrimSpace(serviceVersion)
	if version == "" {
		version = DevelopmentVersion
	}
	commit := strings.ToLower(strings.TrimSpace(gitCommit))
	if !commitPattern.MatchString(commit) {
		commit = UnknownCommit
	}
	return Info{ServiceVersion: version, GitCommit: commit}
}
