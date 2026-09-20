package artifact

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

var ErrNotFound = errors.New("artifact not found")
var ErrStorageKeyChanged = errors.New("artifact storage key changed")

type PostgresMetadata struct{ db *sql.DB }

func NewPostgresMetadata(db *sql.DB) *PostgresMetadata { return &PostgresMetadata{db: db} }

func (r *PostgresMetadata) Insert(ctx context.Context, meta Meta) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("PostgreSQL artifact metadata is not configured")
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO artifacts(
        id,run_id,kind,storage_key,sha256,size_bytes,content_type,metadata,created_at
    ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		meta.ID, meta.RunID, meta.Kind, meta.StorageKey, meta.SHA256, meta.Size,
		meta.ContentType, encodeAttributes(meta.Attributes), meta.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert artifact metadata: %w", err)
	}
	return nil
}

func (r *PostgresMetadata) Get(ctx context.Context, id string) (Meta, error) {
	var meta Meta
	var attributes []byte
	err := r.db.QueryRowContext(ctx, `SELECT id,run_id,kind,storage_key,sha256,
        size_bytes,content_type,metadata,created_at FROM artifacts WHERE id=$1`, id).Scan(
		&meta.ID, &meta.RunID, &meta.Kind, &meta.StorageKey, &meta.SHA256,
		&meta.Size, &meta.ContentType, &attributes, &meta.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Meta{}, ErrNotFound
	}
	if err != nil {
		return Meta{}, fmt.Errorf("get artifact metadata: %w", err)
	}
	if err := json.Unmarshal(attributes, &meta.Attributes); err != nil {
		return Meta{}, fmt.Errorf("decode artifact metadata: %w", err)
	}
	return meta, nil
}

func (r *PostgresMetadata) List(ctx context.Context, runID string) ([]Meta, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,run_id,kind,storage_key,sha256,
        size_bytes,content_type,metadata,created_at FROM artifacts
        WHERE run_id=$1 ORDER BY created_at,id`, runID)
	if err != nil {
		return nil, fmt.Errorf("list artifact metadata: %w", err)
	}
	defer rows.Close()
	result := make([]Meta, 0)
	for rows.Next() {
		var meta Meta
		var attributes []byte
		if err := rows.Scan(&meta.ID, &meta.RunID, &meta.Kind, &meta.StorageKey, &meta.SHA256,
			&meta.Size, &meta.ContentType, &attributes, &meta.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(attributes, &meta.Attributes); err != nil {
			return nil, err
		}
		result = append(result, meta)
	}
	return result, rows.Err()
}

func (r *PostgresMetadata) UpdateStorageKey(ctx context.Context, id, previous, next string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE artifacts SET storage_key=$1 WHERE id=$2 AND storage_key=$3`, next, id, previous)
	if err != nil {
		return fmt.Errorf("update artifact storage key: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read artifact storage key update result: %w", err)
	}
	if affected != 1 {
		return ErrStorageKeyChanged
	}
	return nil
}

func (r *PostgresMetadata) Delete(ctx context.Context, id string) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM artifacts WHERE id=$1`, id); err != nil {
		return fmt.Errorf("delete artifact metadata: %w", err)
	}
	return nil
}

var _ MetadataRepository = (*PostgresMetadata)(nil)
