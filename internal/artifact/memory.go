package artifact

import (
	"context"
	"sort"
	"sync"
)

type MemoryMetadata struct {
	mu    sync.Mutex
	items map[string]Meta
}

func NewMemoryMetadata() *MemoryMetadata { return &MemoryMetadata{items: map[string]Meta{}} }

func (m *MemoryMetadata) Insert(_ context.Context, meta Meta) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items[meta.ID] = meta
	return nil
}

func (m *MemoryMetadata) Get(_ context.Context, id string) (Meta, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	meta, exists := m.items[id]
	if !exists {
		return Meta{}, ErrNotFound
	}
	return meta, nil
}

func (m *MemoryMetadata) List(_ context.Context, runID string) ([]Meta, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]Meta, 0)
	for _, meta := range m.items {
		if meta.RunID == runID {
			result = append(result, meta)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.Before(result[j].CreatedAt) })
	return result, nil
}

var _ MetadataRepository = (*MemoryMetadata)(nil)
