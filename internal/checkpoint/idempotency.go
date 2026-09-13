package checkpoint

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"forgeflow/internal/domain"
)

var (
	ErrIdempotencyMismatch      = errors.New("idempotency key was already used with a different request")
	ErrIdempotencyLegacyPending = errors.New("legacy pending request requires reconciliation before retry")
)

// IdempotentCreator atomically creates a checkpoint and its request mapping.
// Stores that cannot guarantee this must not emulate it with separate writes.
type IdempotentCreator interface {
	CreateIdempotent(context.Context, *domain.RunState, string, []byte) (*domain.RunState, error)
}

func (s *PostgresStore) CreateIdempotent(ctx context.Context, state *domain.RunState, key string, request []byte) (*domain.RunState, error) {
	if s == nil || s.db == nil || state == nil || state.OwnerID == "" || state.Version != 0 || key == "" || len(key) > 128 {
		return nil, fmt.Errorf("invalid idempotent checkpoint creation")
	}
	// READ COMMITTED sees the winner after waiting for its unique-key insert.
	// The row lock also serializes reuse of expired keys.
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	sum := sha256.Sum256(request)
	result, err := tx.ExecContext(ctx, `INSERT INTO idempotency_keys(owner_id,key,request_hash,run_id,status,expires_at)
		VALUES($1,$2,$3,NULL,'pending',now()+interval '24 hours') ON CONFLICT(owner_id,key) DO NOTHING`, state.OwnerID, key, sum[:])
	if err != nil {
		return nil, err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	var runID sql.NullString
	var stored []byte
	var status string
	var expired bool
	if err := tx.QueryRowContext(ctx, `SELECT run_id,request_hash,status,expires_at<=now()
		FROM idempotency_keys WHERE owner_id=$1 AND key=$2 FOR UPDATE`, state.OwnerID, key).Scan(&runID, &stored, &status, &expired); err != nil {
		return nil, err
	}
	if inserted == 0 {
		// Old releases committed pending claims separately. An unmapped run
		// may exist: never automatically delete/reuse these, even after expiry.
		if status == "pending" {
			return nil, ErrIdempotencyLegacyPending
		}
		if !expired {
			if !bytes.Equal(stored, sum[:]) {
				return nil, ErrIdempotencyMismatch
			}
			var encoded []byte
			if err := tx.QueryRowContext(ctx, `SELECT state_json FROM runs WHERE id=$1 AND owner_id=$2`, runID, state.OwnerID).Scan(&encoded); err != nil {
				return nil, err
			}
			var replay domain.RunState
			if err := json.Unmarshal(encoded, &replay); err != nil {
				return nil, err
			}
			normalizeRunState(&replay)
			if err := tx.Commit(); err != nil {
				return nil, err
			}
			return &replay, nil
		}
	}
	if err := s.saveTx(ctx, tx, state, 0); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE idempotency_keys SET request_hash=$3,run_id=$4,status='completed',
		created_at=now(),expires_at=now()+interval '24 hours' WHERE owner_id=$1 AND key=$2`, state.OwnerID, key, sum[:], state.RunID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	state.Version = 1
	return state, nil
}
