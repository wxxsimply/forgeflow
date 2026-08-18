package checkpoint

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"forgeflow/internal/domain"
)

func TestFileStoreRoundTrip(t *testing.T) {
	t.Parallel()
	store := NewFileStore(t.TempDir())
	state := domain.NewRunState(domain.NewRunInput{Task: "test", RepositoryPath: "."})
	if err := store.Save(context.Background(), state, state.Version); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := store.Load(context.Background(), state.RunID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !reflect.DeepEqual(loaded, state) {
		t.Fatalf("loaded state differs from saved state")
	}
}

func TestFileStoreRejectsStaleVersion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	directory := t.TempDir()
	store := NewFileStore(directory)
	secondStore := NewFileStore(directory)
	state := domain.NewRunState(domain.NewRunInput{Task: "test", RepositoryPath: "."})
	if err := store.Save(ctx, state, state.Version); err != nil {
		t.Fatalf("initial save: %v", err)
	}

	first, err := store.Load(ctx, state.RunID)
	if err != nil {
		t.Fatalf("load first snapshot: %v", err)
	}
	stale, err := secondStore.Load(ctx, state.RunID)
	if err != nil {
		t.Fatalf("load stale snapshot: %v", err)
	}
	first.Task = "first writer"
	if err := store.Save(ctx, first, first.Version); err != nil {
		t.Fatalf("save first writer: %v", err)
	}
	stale.Task = "stale writer"
	if err := secondStore.Save(ctx, stale, stale.Version); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale save error = %v, want ErrConflict", err)
	}

	loaded, err := store.Load(ctx, state.RunID)
	if err != nil {
		t.Fatalf("load winner: %v", err)
	}
	if loaded.Task != "first writer" {
		t.Fatalf("stored task = %q, stale writer overwrote the checkpoint", loaded.Task)
	}
}
