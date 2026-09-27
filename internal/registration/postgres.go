package registration

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"time"
)

type PostgresStore struct{ db *sql.DB }

func NewPostgresStore(db *sql.DB) *PostgresStore { return &PostgresStore{db: db} }

func (s *PostgresStore) Issue(ctx context.Context, email string, hash []byte, now, expires, resendAfter time.Time) (bool, error) {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM registration_codes WHERE expires_at < $1`, now.Add(-24*time.Hour)); err != nil {
		return false, err
	}
	var issued string
	err := s.db.QueryRowContext(ctx, `INSERT INTO registration_codes(normalized_email,code_hash,expires_at,resend_after,attempts,created_at)
VALUES($1,$2,$3,$4,0,$5)
ON CONFLICT(normalized_email) DO UPDATE SET code_hash=EXCLUDED.code_hash,expires_at=EXCLUDED.expires_at,
resend_after=EXCLUDED.resend_after,attempts=0,created_at=EXCLUDED.created_at
WHERE registration_codes.resend_after <= $5
RETURNING normalized_email`, email, hash, expires, resendAfter, now).Scan(&issued)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (s *PostgresStore) Consume(ctx context.Context, email string, expected []byte, now time.Time) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var actual []byte
	var expires time.Time
	var attempts int
	err = tx.QueryRowContext(ctx, `SELECT code_hash,expires_at,attempts FROM registration_codes WHERE normalized_email=$1 FOR UPDATE`, email).Scan(&actual, &expires, &attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !expires.After(now) || attempts >= 5 {
		return false, nil
	}
	if subtle.ConstantTimeCompare(actual, expected) != 1 {
		if _, err := tx.ExecContext(ctx, `UPDATE registration_codes SET attempts=attempts+1 WHERE normalized_email=$1`, email); err != nil {
			return false, err
		}
		return false, tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM registration_codes WHERE normalized_email=$1`, email); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (s *PostgresStore) Revoke(ctx context.Context, email string, hash []byte) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM registration_codes WHERE normalized_email=$1 AND code_hash=$2`, email, hash)
	return err
}
