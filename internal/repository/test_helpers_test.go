package repository

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func createTestRepository(t *testing.T) string {
	t.Helper()
	repositoryPath := filepath.Join(t.TempDir(), "repository")
	runTestGit(t, "", "init", "-b", "main", repositoryPath)
	writeTestFile(t, repositoryPath, "README.md", "# Fixture\n\nRepository harness fixture.\n")
	writeTestFile(t, repositoryPath, "AGENTS.md", "# Rules\n\nRun go test ./... before completion.\n")
	writeTestFile(t, repositoryPath, "go.mod", "module fixture\n\ngo 1.22\n")
	writeTestFile(t, repositoryPath, filepath.Join("cmd", "main.go"), "package main\n\nfunc main() {}\n")
	runTestGit(t, repositoryPath, "add", ".")
	runTestGit(t, repositoryPath, "-c", "user.name=ForgeFlow Test", "-c", "user.email=forgeflow@example.test", "commit", "-m", "initial fixture")
	return repositoryPath
}

func writeTestFile(t *testing.T, root, relativePath, content string) {
	t.Helper()
	path := filepath.Join(root, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create parent directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", relativePath, err)
	}
}

func runTestGit(t *testing.T, directory string, args ...string) string {
	t.Helper()
	commandArgs := args
	if directory != "" {
		commandArgs = append([]string{"-C", directory}, args...)
	}
	command := exec.Command("git", commandArgs...)
	command.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, stderr.String())
	}
	return strings.TrimSpace(string(output))
}
