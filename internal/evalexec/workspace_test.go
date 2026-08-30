package evalexec

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
