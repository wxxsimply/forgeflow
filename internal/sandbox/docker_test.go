package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"forgeflow/internal/apperror"
)

const testImage = "example/forgeflow@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestDockerRunnerBuildsIsolatedInvocation(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "run-1")
	workingDirectory := filepath.Join(workspace, "src")
	makeDirectory(t, workingDirectory)
	runner, err := NewDockerRunner(DockerConfig{
		WorkspaceRoot: root, AllowedImages: []string{testImage}, CPUs: "0.5", Memory: "256m",
		PIDsLimit: 64, TmpfsSizeBytes: 1024, MaxTimeout: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	args, name, err := runner.BuildArgs(Request{
		Image: testImage, WorkspacePath: workspace, WorkingDir: "src",
		Program: "go", Args: []string{"test", "./..."}, Environment: map[string]string{"CI": "true"},
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, required := range []string{
		"--network none", "--read-only", "--cap-drop ALL", "no-new-privileges",
		"--user 10001:10001", "--cpus 0.5", "--memory 256m", "--pids-limit 64",
		"/tmp:rw,noexec,nosuid,size=1024", "dst=/workspace,rw", "--workdir /workspace/src",
	} {
		if !strings.Contains(joined, required) {
			t.Fatalf("docker args missing %q: %s", required, joined)
		}
	}
	if name == "" || !slices.Contains(args, testImage) || !slices.Contains(args, "go") {
		t.Fatalf("invalid docker invocation: name=%q args=%v", name, args)
	}
	if strings.Contains(joined, "docker.sock") || strings.Contains(joined, "OPENAI_API_KEY") {
		t.Fatalf("sensitive mount or environment leaked: %s", joined)
	}
}

func TestDockerRunnerRejectsEscapesSecretsShellsAndUnpinnedImages(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "run-1")
	makeDirectory(t, workspace)
	runner, err := NewDockerRunner(DockerConfig{WorkspaceRoot: root, AllowedImages: []string{testImage}})
	if err != nil {
		t.Fatal(err)
	}
	tests := []Request{
		{Image: testImage, WorkspacePath: root, Program: "go"},
		{Image: testImage, WorkspacePath: t.TempDir(), Program: "go"},
		{Image: testImage, WorkspacePath: workspace, Program: "powershell", Args: []string{"-Command", "whoami"}},
		{Image: testImage, WorkspacePath: workspace, Program: "go", Environment: map[string]string{"OPENAI_API_KEY": "secret"}},
		{Image: "example/forgeflow:latest", WorkspacePath: workspace, Program: "go"},
		{Image: testImage, WorkspacePath: workspace, Program: "go", WorkingDir: "../outside"},
	}
	for _, request := range tests {
		if _, _, err := runner.BuildArgs(request); !apperror.IsCode(err, apperror.CodePolicyDenied) {
			t.Fatalf("BuildArgs(%+v) error = %v, want policy denied", request, err)
		}
	}
	if _, err := NewDockerRunner(DockerConfig{WorkspaceRoot: root, AllowedImages: []string{"example/forgeflow:latest"}}); err == nil {
		t.Fatal("NewDockerRunner accepted an unpinned image")
	}
}

func TestDockerRunnerIsDisabledByDefault(t *testing.T) {
	root := t.TempDir()
	runner, err := NewDockerRunner(DockerConfig{WorkspaceRoot: root, AllowedImages: []string{testImage}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = runner.Run(context.Background(), Request{})
	if !apperror.IsCode(err, apperror.CodePolicyDenied) {
		t.Fatalf("Run() error = %v, want policy denied", err)
	}
}

func makeDirectory(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}
