package tool

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"forgeflow/internal/apperror"
	"forgeflow/internal/domain"
)

func TestPatchToolAppliesAuthorizedTextPatch(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, "hello.txt"), "old\n")
	initializePatchRepository(t, workspace)
	candidate := &patchTool{limits: DefaultPatchLimits()}
	patch := textPatch("hello.txt", "old", "new")
	input := patchInput(t, patch, []string{"hello.txt"})
	output, err := candidate.Execute(context.Background(), CallContext{
		Workspace: domain.WorkspaceRef{ID: "workspace-1", Path: workspace}, AllowedPaths: []string{"hello.txt"},
	}, input)
	if err != nil {
		t.Fatal(err)
	}
	var result ApplyPatchOutput
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(workspace, "hello.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.ReplaceAll(string(content), "\r\n", "\n") != "new\n" || result.ChangedLines != 2 || len(result.AppliedFiles) != 1 {
		t.Fatalf("content=%q result=%+v", content, result)
	}
}

func TestPatchToolCreatesAuthorizedNestedFile(t *testing.T) {
	workspace := t.TempDir()
	initializePatchRepository(t, workspace)
	patch := "diff --git a/newdir/new.txt b/newdir/new.txt\n" +
		"new file mode 100644\n--- /dev/null\n+++ b/newdir/new.txt\n@@ -0,0 +1 @@\n+new\n"
	candidate := &patchTool{limits: DefaultPatchLimits()}
	_, err := candidate.Execute(context.Background(), CallContext{
		Workspace: domain.WorkspaceRef{ID: "workspace-1", Path: workspace}, AllowedPaths: []string{"newdir/new.txt"},
	}, patchInput(t, patch, []string{"newdir/new.txt"}))
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(workspace, "newdir", "new.txt"))
	if err != nil || strings.ReplaceAll(string(content), "\r\n", "\n") != "new\n" {
		t.Fatalf("nested content=%q err=%v", content, err)
	}
}

func TestPatchToolRejectsUnauthorizedProtectedAndMisdeclaredChanges(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, "hello.txt"), "old\n")
	writeTestFile(t, filepath.Join(workspace, ".env"), "SECRET=old\n")
	candidate := &patchTool{limits: DefaultPatchLimits()}
	tests := []struct {
		name     string
		patch    string
		expected []string
		allowed  []string
	}{
		{name: "outside approved plan", patch: textPatch("hello.txt", "old", "new"), expected: []string{"hello.txt"}, allowed: []string{"other.txt"}},
		{name: "protected secret", patch: textPatch(".env", "SECRET=old", "SECRET=new"), expected: []string{".env"}, allowed: []string{".env"}},
		{name: "misdeclared files", patch: textPatch("hello.txt", "old", "new"), expected: []string{"other.txt"}, allowed: []string{"hello.txt", "other.txt"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := candidate.Execute(context.Background(), CallContext{
				Workspace: domain.WorkspaceRef{ID: "workspace-1", Path: workspace}, AllowedPaths: test.allowed,
			}, patchInput(t, test.patch, test.expected))
			if !apperror.IsCode(err, apperror.CodePolicyDenied) {
				t.Fatalf("error = %v, want policy denied", err)
			}
		})
	}
	content, err := os.ReadFile(filepath.Join(workspace, "hello.txt"))
	if err != nil || string(content) != "old\n" {
		t.Fatalf("unauthorized patch changed file: content=%q err=%v", content, err)
	}
}

func TestDefaultPatchLimitsProtectAgentGovernanceSources(t *testing.T) {
	limits := DefaultPatchLimits()
	protected := []string{
		"internal/planner/prompts/planner_v1.md",
		"internal/developer/prompts/developer_v1.md",
		"internal/reviewer/prompts/reviewer_v1.md",
		"internal/security/prompts/security_v1.md",
		"internal/policy/runtime.go",
		"internal/judge/judge.go",
		"internal/governance/store.go",
		"internal/eval/grader.go",
		"internal/eval/datasets/software_v1.json",
	}
	for _, path := range protected {
		if err := validatePatchAuthorization([]string{path}, []string{path}, limits.ProtectedPaths); !apperror.IsCode(err, apperror.CodePolicyDenied) {
			t.Fatalf("path %q error = %v, want policy denied", path, err)
		}
	}
}

func TestPatchToolRejectsTraversalSymlinkAndBudgets(t *testing.T) {
	workspace := t.TempDir()
	candidate := &patchTool{limits: PatchLimits{MaxPatchBytes: 1024, MaxFiles: 1, MaxChangedLines: 1, ProtectedPaths: []string{".env"}}}
	traversal := strings.ReplaceAll(textPatch("safe.txt", "old", "new"), "a/safe.txt", "a/../safe.txt")
	traversal = strings.ReplaceAll(traversal, "b/safe.txt", "b/../safe.txt")
	if _, _, _, err := candidate.decode(patchInput(t, traversal, []string{"../safe.txt"})); !apperror.IsCode(err, apperror.CodePolicyDenied) && !apperror.IsCode(err, apperror.CodeValidation) {
		t.Fatalf("traversal error = %v", err)
	}
	if _, _, _, err := candidate.decode(patchInput(t, textPatch("safe.txt", "old", "new"), []string{"safe.txt"})); !apperror.IsCode(err, apperror.CodeBudget) {
		t.Fatalf("changed-line budget error = %v", err)
	}

	outside := t.TempDir()
	writeTestFile(t, filepath.Join(outside, "secret.txt"), "old\n")
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(workspace, "link.txt")); err != nil {
		if errors.Is(err, os.ErrPermission) || runtime.GOOS == "windows" {
			t.Skip("symbolic links require additional privileges on this platform")
		}
		t.Fatal(err)
	}
	permissive := &patchTool{limits: DefaultPatchLimits()}
	_, err := permissive.Execute(context.Background(), CallContext{
		Workspace: domain.WorkspaceRef{ID: "workspace-1", Path: workspace}, AllowedPaths: []string{"link.txt"},
	}, patchInput(t, textPatch("link.txt", "old", "new"), []string{"link.txt"}))
	if !apperror.IsCode(err, apperror.CodePolicyDenied) {
		t.Fatalf("symlink escape error = %v", err)
	}
}

func initializePatchRepository(t *testing.T, workspace string) {
	t.Helper()
	command := exec.Command("git", "-C", workspace, "init")
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, output)
	}
}

func textPatch(path, before, after string) string {
	return "diff --git a/" + path + " b/" + path + "\n" +
		"--- a/" + path + "\n" +
		"+++ b/" + path + "\n" +
		"@@ -1 +1 @@\n" +
		"-" + before + "\n" +
		"+" + after + "\n"
}

func patchInput(t *testing.T, patch string, expected []string) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(ApplyPatchInput{Patch: patch, ExpectedFiles: expected})
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
