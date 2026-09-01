package lifecycle

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"forgeflow/internal/domain"
)

func TestValidatorChecksWorkspaceBaseCommit(t *testing.T) {
	workspace := t.TempDir()
	git(t, workspace, "init")
	if err := os.WriteFile(filepath.Join(workspace, "file.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, workspace, "config", "user.email", "test@example.invalid")
	git(t, workspace, "config", "user.name", "Test")
	git(t, workspace, "add", ".")
	git(t, workspace, "commit", "-m", "initial")
	commit := git(t, workspace, "rev-parse", "HEAD")
	state := domain.NewRunState(domain.NewRunInput{Task: "x", RepositoryPath: workspace})
	state.Workspace = &domain.WorkspaceRef{ID: "workspace-1", Path: workspace, BaseCommit: commit}
	validator, err := NewValidator(Options{})
	if err != nil {
		t.Fatal(err)
	}
	guard := validator.Capture(state)
	state.ResumeGuard = &guard
	if err := validator.Validate(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	state.Workspace.BaseCommit = strings.Repeat("0", 40)
	if err := validator.Validate(context.Background(), state); err == nil {
		t.Fatal("validator accepted changed base commit")
	}
}

func TestValidatorChecksPromptModelPolicyAndToolBindings(t *testing.T) {
	state := domain.NewRunState(domain.NewRunInput{Task: "x", RepositoryPath: "."})
	state.ModelInvocations = []domain.ModelInvocation{{
		Agent: "planner", Model: "model-v1", PromptVersion: "planner/v1", PromptSHA256: strings.Repeat("a", 64),
	}}
	state.ToolCallAudits = []domain.ToolCallAudit{{
		ToolName: "run_test", ToolVersion: "v1", PolicyVersion: "policy/v1",
	}}
	validator, err := NewValidator(Options{
		CurrentPolicyVersion:   "policy/v1",
		ExpectedPromptVersions: map[string]string{"planner": "planner/v1"},
		ExpectedPromptSHA256:   map[string]string{"planner": strings.Repeat("a", 64)},
		ExpectedModelVersions:  map[string]string{"planner": "model-v1"},
		ExpectedToolVersions:   map[string]string{"run_test": "v1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	guard := validator.Capture(state)
	state.ResumeGuard = &guard
	if err := validator.Validate(context.Background(), state); err != nil {
		t.Fatalf("matching bindings rejected: %v", err)
	}

	incompatible, err := NewValidator(Options{
		CurrentPolicyVersion:   "policy/v2",
		ExpectedPromptVersions: map[string]string{"planner": "planner/v1"},
		ExpectedPromptSHA256:   map[string]string{"planner": strings.Repeat("a", 64)},
		ExpectedModelVersions:  map[string]string{"planner": "model-v2"},
		ExpectedToolVersions:   map[string]string{"run_test": "v2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := incompatible.Validate(context.Background(), state); err == nil {
		t.Fatal("validator accepted incompatible runtime bindings")
	}
}

func git(t *testing.T, directory string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", directory}, args...)...)
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}
