package eval

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestVerifyFixtureCommits(t *testing.T) {
	repository := t.TempDir()
	for _, args := range [][]string{{"init"}, {"config", "user.email", "eval@example.test"}, {"config", "user.name", "Eval Test"}, {"commit", "--allow-empty", "-m", "fixture"}} {
		command := exec.Command("git", append([]string{"-C", repository}, args...)...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, output)
		}
	}
	output, err := exec.Command("git", "-C", repository, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	commit := strings.TrimSpace(string(output))
	dataset := Dataset{Cases: []Case{{ID: "fixture-1", FixtureCommit: commit}}}
	if err := VerifyFixtureCommits(context.Background(), dataset, repository); err != nil {
		t.Fatalf("real fixture rejected: %v", err)
	}
	dataset.Cases[0].FixtureCommit = strings.Repeat("0", 40)
	if err := VerifyFixtureCommits(context.Background(), dataset, repository); err == nil {
		t.Fatal("missing fixture commit was accepted")
	}
}
