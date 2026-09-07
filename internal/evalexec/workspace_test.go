package evalexec

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"forgeflow/internal/apperror"
	fulleval "forgeflow/internal/eval"
)

func TestPatchAndValidationUseRealExitCodes(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is unavailable")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/eval\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "value.go"), []byte("package value\n\nfunc Value() int { return 1 }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init"}, {"add", "."}, {"-c", "user.name=Eval", "-c", "user.email=eval@example.invalid", "commit", "-m", "fixture"}} {
		command := exec.Command("git", args...)
		command.Dir = root
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	patch := "diff --git a/value.go b/value.go\nindex d8c5951..8da35ef 100644\n--- a/value.go\n+++ b/value.go\n@@ -1,3 +1,3 @@\n package value\n \n-func Value() int { return 1 }\n+func Value() int { return 2 }\n"
	if err := applyPatch(context.Background(), root, patch); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "value.go"))
	if err != nil || !strings.Contains(string(data), "return 2") {
		t.Fatalf("patched file=%q err=%v", data, err)
	}
	output, err := runCommand(context.Background(), root, fulleval.Command{Program: "go", Args: []string{"test", "./..."}}, time.Minute)
	if err != nil {
		t.Fatalf("validation failed: %v output=%s", err, output)
	}
}

func TestPatchPrecheckRejectsMalformedAndMismatchedPatchesWithoutWrites(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is unavailable")
	}
	for _, test := range []struct{ name, hunk string }{
		{"malformed", "@@ -1,2 +1,2 @@\n-old\n+new\n"},
		{"mismatched", "@@ -1 +1 @@\n-missing\n+new\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			command := exec.Command("git", "init", root)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("git init: %v: %s", err, output)
			}
			path := filepath.Join(root, "value.txt")
			if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			patch := "diff --git a/value.txt b/value.txt\n--- a/value.txt\n+++ b/value.txt\n" + test.hunk
			err := applyPatch(context.Background(), root, patch)
			if !apperror.IsCode(err, apperror.CodeModelOutput) {
				t.Fatalf("expected rejected patch: %v", err)
			}
			var observation fulleval.Observation
			terminalFailure(&observation, err, root, Options{})
			if observation.FailureStage != "patch_check" || observation.PatchApplicable {
				t.Fatalf("observation=%+v", observation)
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil || string(data) != "old\n" {
				t.Fatalf("rejected patch changed file: %q error=%v", data, readErr)
			}
		})
	}
}

func TestPatchExecutionPreservesExpiredContext(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	err := applyPatch(ctx, t.TempDir(), "unused")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline was lost: %v", err)
	}
	var observation fulleval.Observation
	terminalFailure(&observation, err, "", Options{})
	if observation.Outcome != "timed_out" || observation.FailureStage != "patch_check" {
		t.Fatalf("observation=%+v", observation)
	}
}

func TestPatchExecutionStartFailureIsNotModelOutput(t *testing.T) {
	err := applyPatch(context.Background(), filepath.Join(t.TempDir(), "absent"), "unused")
	if err == nil || !apperror.IsCode(err, apperror.CodeInternal) {
		t.Fatalf("expected process start error: %v", err)
	}
	var observation fulleval.Observation
	terminalFailure(&observation, err, "", Options{})
	if observation.FailureStage != "patch_check" || observation.FailureCode != "internal_error" {
		t.Fatalf("observation=%+v", observation)
	}
}
