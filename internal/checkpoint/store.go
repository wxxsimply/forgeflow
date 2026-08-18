package checkpoint

import (
	"context"

	"forgeflow/internal/domain"
)

type Store interface {
	Save(context.Context, *domain.RunState, int64) error
	Load(context.Context, string) (*domain.RunState, error)
}
