package artifact

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
)

func TestFileStorePersistsBodyOutsideMetadata(t *testing.T) {
	metadata := NewMemoryMetadata()
	store, err := NewFileStore(t.TempDir(), metadata, 1024)
	if err != nil {
		t.Fatal(err)
	}
	created, err := store.Put(context.Background(), PutRequest{
		RunID: "11111111-1111-4111-8111-111111111111", Kind: KindPatch,
		ContentType: "text/x-diff", Attributes: map[string]string{"source": "judge"},
	}, strings.NewReader("diff body"))
	if err != nil {
		t.Fatal(err)
	}
	if created.SHA256 == "" || created.Size != int64(len("diff body")) || strings.Contains(created.StorageKey, "..") {
		t.Fatalf("created=%+v", created)
	}
	body, loaded, err := store.Open(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	content, _ := io.ReadAll(body)
	if string(content) != "diff body" || loaded.ID != created.ID {
		t.Fatalf("content=%q loaded=%+v", content, loaded)
	}
}

func TestFileStoreDetectsSameSizeBodyTampering(t *testing.T) {
	metadata := NewMemoryMetadata()
	root := t.TempDir()
	store, err := NewFileStore(root, metadata, 1024)
	if err != nil {
		t.Fatal(err)
	}
	created, err := store.Put(context.Background(), PutRequest{RunID: "run", Kind: KindLog, ContentType: "text/plain"}, strings.NewReader("good"))
	if err != nil {
		t.Fatal(err)
	}
	path, err := store.resolveStorageKey(created.StorageKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("evil"), 0o600); err != nil {
		t.Fatal(err)
	}
	body, _, err := store.Open(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	if _, err := io.ReadAll(body); err == nil {
		t.Fatal("tampered artifact checksum was accepted")
	}
}

func TestFileStoreRejectsOversizedBody(t *testing.T) {
	store, err := NewFileStore(t.TempDir(), NewMemoryMetadata(), 4)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Put(context.Background(), PutRequest{RunID: "run", Kind: KindLog, ContentType: "text/plain"}, strings.NewReader("12345"))
	if err == nil {
		t.Fatal("oversized artifact was accepted")
	}
}
