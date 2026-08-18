package repository

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"forgeflow/internal/apperror"
)

func TestSafeFileReaderListsReadsAndSearches(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "README.md", "Hello ForgeFlow\nsecond line\n")
	writeTestFile(t, root, filepath.Join("internal", "main.go"), "package internal\n\nconst Name = \"ForgeFlow\"\n")
	writeTestFile(t, root, filepath.Join(".git", "secret"), "must not be listed")
	reader, err := NewSafeFileReader(root, DefaultLimits())
	if err != nil {
		t.Fatalf("NewSafeFileReader() error = %v", err)
	}

	listed, err := reader.ListFiles(context.Background(), ".", true)
	if err != nil {
		t.Fatalf("ListFiles() error = %v", err)
	}
	for _, entry := range listed.Entries {
		if entry.Path == ".git/secret" {
			t.Fatal("ignored .git content was listed")
		}
	}
	content, err := reader.ReadFile(context.Background(), "README.md")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if content.Text != "Hello ForgeFlow\nsecond line\n" || content.SHA256 == "" {
		t.Fatalf("content = %+v", content)
	}
	matches, err := reader.SearchCode(context.Background(), "forgeflow", SearchOptions{Extensions: []string{".go"}})
	if err != nil {
		t.Fatalf("SearchCode() error = %v", err)
	}
	if len(matches) != 1 || matches[0].Path != "internal/main.go" || matches[0].Line != 3 {
		t.Fatalf("matches = %+v", matches)
	}
}

func TestSafeFileReaderRejectsTraversalAbsoluteAndLargeFiles(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, parent, "outside.txt", "outside")
	writeTestFile(t, root, "large.txt", "this content is larger than sixteen bytes")
	limits := DefaultLimits()
	limits.MaxReadBytes = 16
	reader, err := NewSafeFileReader(root, limits)
	if err != nil {
		t.Fatalf("NewSafeFileReader() error = %v", err)
	}

	for _, path := range []string{"../outside.txt", filepath.Join(parent, "outside.txt")} {
		if _, err := reader.ReadFile(context.Background(), path); !apperror.IsCode(err, apperror.CodePolicyDenied) {
			t.Fatalf("ReadFile(%q) error = %v", path, err)
		}
	}
	if _, err := reader.ReadFile(context.Background(), "large.txt"); !apperror.IsCode(err, apperror.CodePolicyDenied) {
		t.Fatalf("large file error = %v", err)
	}
}

func TestSafeFileReaderRejectsSymlinkEscape(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(parent, "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "escape.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symbolic links are unavailable in this environment: %v", err)
	}
	reader, err := NewSafeFileReader(root, DefaultLimits())
	if err != nil {
		t.Fatalf("NewSafeFileReader() error = %v", err)
	}
	if _, err := reader.ReadFile(context.Background(), "escape.txt"); !apperror.IsCode(err, apperror.CodePolicyDenied) {
		t.Fatalf("symlink escape error = %v", err)
	}
}

func TestSafeFileReaderMarksTruncatedLists(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "one.txt", "1")
	writeTestFile(t, root, "two.txt", "2")
	limits := DefaultLimits()
	limits.MaxListEntries = 1
	reader, err := NewSafeFileReader(root, limits)
	if err != nil {
		t.Fatal(err)
	}
	listed, err := reader.ListFiles(context.Background(), ".", true)
	if err != nil {
		t.Fatalf("ListFiles() error = %v", err)
	}
	if !listed.Truncated || len(listed.Entries) != 1 {
		t.Fatalf("list = %+v", listed)
	}
}
