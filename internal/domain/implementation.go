package domain

import (
	"fmt"
	"path"
	"slices"
	"strings"
	"time"
)

type ImplementationResult struct {
	Summary            string   `json:"summary"`
	Patch              string   `json:"patch"`
	ChangedFiles       []string `json:"changedFiles"`
	Evidence           []string `json:"evidence"`
	UnresolvedIssues   []string `json:"unresolvedIssues"`
	RequestedApprovals []string `json:"requestedApprovals"`
}

func (r ImplementationResult) Validate() error {
	if strings.TrimSpace(r.Summary) == "" || len(r.Summary) > 4_000 {
		return fmt.Errorf("implementation summary must contain 1 to 4000 characters")
	}
	if strings.TrimSpace(r.Patch) == "" {
		return fmt.Errorf("implementation patch is required")
	}
	if len(r.ChangedFiles) == 0 || len(r.ChangedFiles) > 200 {
		return fmt.Errorf("implementation must declare between 1 and 200 changed files")
	}
	seen := make(map[string]struct{}, len(r.ChangedFiles))
	for _, file := range r.ChangedFiles {
		if !normalizedRelativePath(file) {
			return fmt.Errorf("implementation file %q is not a normalized relative path", file)
		}
		if _, duplicate := seen[file]; duplicate {
			return fmt.Errorf("implementation file %q is repeated", file)
		}
		seen[file] = struct{}{}
	}
	for name, values := range map[string][]string{
		"evidence": r.Evidence, "unresolved issues": r.UnresolvedIssues, "requested approvals": r.RequestedApprovals,
	} {
		if len(values) > 100 {
			return fmt.Errorf("implementation %s exceeds 100 entries", name)
		}
		for _, value := range values {
			if strings.TrimSpace(value) == "" || len(value) > 4_000 {
				return fmt.Errorf("implementation %s contains an invalid entry", name)
			}
		}
	}
	return nil
}

func (r ImplementationResult) FilesMatch(actual []string) bool {
	declared := append([]string(nil), r.ChangedFiles...)
	observed := append([]string(nil), actual...)
	slices.Sort(declared)
	slices.Sort(observed)
	return slices.Equal(declared, observed)
}

type TestCommand struct {
	Program    string        `json:"program"`
	Args       []string      `json:"args"`
	WorkingDir string        `json:"workingDir"`
	EnvAllow   []string      `json:"envAllow"`
	Timeout    time.Duration `json:"timeout"`
}

type TestAssessment struct {
	ToolCallID  string        `json:"toolCallId"`
	Program     string        `json:"program"`
	Args        []string      `json:"args"`
	ExitCode    int           `json:"exitCode"`
	Stdout      string        `json:"stdout"`
	Stderr      string        `json:"stderr"`
	Duration    time.Duration `json:"duration"`
	Truncated   bool          `json:"truncated"`
	Passed      bool          `json:"passed"`
	CompletedAt time.Time     `json:"completedAt"`
}

func normalizedRelativePath(value string) bool {
	if value == "" || strings.ContainsAny(value, "\x00\\\r\n") || strings.HasPrefix(value, "/") {
		return false
	}
	cleaned := path.Clean(value)
	return cleaned == value && cleaned != "." && cleaned != ".." && !strings.HasPrefix(cleaned, "../")
}
