package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

type PostgresStore struct{ db *sql.DB }

func NewPostgresStore(db *sql.DB) *PostgresStore { return &PostgresStore{db: db} }

func (s *PostgresStore) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&n)
	return n, err
}
func (s *PostgresStore) CreateUser(ctx context.Context, u UserCredential) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO users(id,email,normalized_email,password_hash,role,status,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$7)`, u.ID, u.Email, NormalizeEmail(u.Email), u.PasswordHash, u.Role, u.Status, u.CreatedAt)
	return err
}
func (s *PostgresStore) FindUserByEmail(ctx context.Context, email string) (UserCredential, error) {
	return s.findUser(ctx, `SELECT id,email,password_hash,role,status,created_at,mfa_secret_ciphertext,mfa_pending_secret_ciphertext,mfa_pending_expires_at,mfa_enabled_at,mfa_recovery_code_hashes,mfa_last_used_step FROM users WHERE normalized_email=$1`, NormalizeEmail(email))
}
func (s *PostgresStore) FindUserByID(ctx context.Context, id string) (UserCredential, error) {
	return s.findUser(ctx, `SELECT id,email,password_hash,role,status,created_at,mfa_secret_ciphertext,mfa_pending_secret_ciphertext,mfa_pending_expires_at,mfa_enabled_at,mfa_recovery_code_hashes,mfa_last_used_step FROM users WHERE id=$1`, id)
}
func (s *PostgresStore) UpdatePasswordHash(ctx context.Context, id, hash string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET password_hash=$2,updated_at=now() WHERE id=$1`, id, hash)
	return err
}
func (s *PostgresStore) findUser(ctx context.Context, q, arg string) (UserCredential, error) {
	var u UserCredential
	var recovery []byte
	err := s.db.QueryRowContext(ctx, q, arg).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.Status, &u.CreatedAt, &u.MFASecretCiphertext, &u.MFAPendingSecretCiphertext, &u.MFAPendingExpiresAt, &u.MFAEnabledAt, &recovery, &u.MFALastUsedStep)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal(recovery, &u.MFARecoveryCodeHashes)
		u.MFAEnabled = u.MFAEnabledAt != nil
	}
	return u, err
}

func (s *PostgresStore) SetMFAPending(ctx context.Context, userID string, ciphertext []byte, expiresAt time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE users SET mfa_pending_secret_ciphertext=$2,mfa_pending_expires_at=$3,updated_at=now() WHERE id=$1 AND role='admin' AND status='active' AND mfa_enabled_at IS NULL`, userID, ciphertext, expiresAt)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) EnableMFA(ctx context.Context, userID, sessionID string, expectedCiphertext []byte, recoveryHashes []string, lastStep int64, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	encoded, err := json.Marshal(recoveryHashes)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE users SET mfa_secret_ciphertext=mfa_pending_secret_ciphertext,mfa_pending_secret_ciphertext=NULL,mfa_pending_expires_at=NULL,mfa_enabled_at=$4,mfa_recovery_code_hashes=$5,mfa_last_used_step=$6,updated_at=$4 WHERE id=$1 AND mfa_pending_secret_ciphertext=$2 AND mfa_pending_expires_at>$3 AND mfa_enabled_at IS NULL`, userID, expectedCiphertext, now, now, encoded, lastStep)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return ErrNotFound
	}
	result, err = tx.ExecContext(ctx, `UPDATE sessions SET mfa_verified_at=$3,revoked_at=CASE WHEN id=$2 THEN revoked_at ELSE COALESCE(revoked_at,$3) END WHERE user_id=$1 AND revoked_at IS NULL`, userID, sessionID, now)
	if err != nil {
		return err
	}
	rows, _ = result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

func (s *PostgresStore) UseMFATimestep(ctx context.Context, userID string, step int64) (bool, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE users SET mfa_last_used_step=$2,updated_at=now() WHERE id=$1 AND mfa_enabled_at IS NOT NULL AND mfa_last_used_step<$2`, userID, step)
	if err != nil {
		return false, err
	}
	rows, _ := result.RowsAffected()
	return rows == 1, nil
}

func (s *PostgresStore) ConsumeMFARecoveryCode(ctx context.Context, userID, hash string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE users SET mfa_recovery_code_hashes=COALESCE((SELECT jsonb_agg(value) FROM jsonb_array_elements_text(mfa_recovery_code_hashes) AS value WHERE value<>$2),'[]'::jsonb),updated_at=now() WHERE id=$1 AND mfa_enabled_at IS NOT NULL AND mfa_recovery_code_hashes ? $2`, userID, hash)
	if err != nil {
		return false, err
	}
	rows, _ := result.RowsAffected()
	return rows == 1, nil
}
func (s *PostgresStore) CreateSession(ctx context.Context, v Session) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions(id,user_id,token_hash,csrf_hash,source_ip,user_agent,created_at,last_seen_at,expires_at,idle_expires_at,mfa_verified_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, v.ID, v.UserID, v.TokenHash, v.CSRFHash, v.SourceIP, v.UserAgent, v.CreatedAt, v.LastSeenAt, v.ExpiresAt, v.IdleExpiresAt, v.MFAVerifiedAt)
	return err
}
func (s *PostgresStore) FindSessionByTokenHash(ctx context.Context, h []byte) (Session, error) {
	var v Session
	err := s.db.QueryRowContext(ctx, `SELECT id,user_id,token_hash,csrf_hash,source_ip,user_agent,created_at,last_seen_at,expires_at,idle_expires_at,mfa_verified_at,revoked_at FROM sessions WHERE token_hash=$1`, h).Scan(&v.ID, &v.UserID, &v.TokenHash, &v.CSRFHash, &v.SourceIP, &v.UserAgent, &v.CreatedAt, &v.LastSeenAt, &v.ExpiresAt, &v.IdleExpiresAt, &v.MFAVerifiedAt, &v.RevokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return v, err
}
func (s *PostgresStore) ListSessions(ctx context.Context, userID string) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,user_id,source_ip,user_agent,created_at,last_seen_at,expires_at,idle_expires_at,mfa_verified_at,revoked_at FROM sessions WHERE user_id=$1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Session{}
	for rows.Next() {
		var v Session
		if err := rows.Scan(&v.ID, &v.UserID, &v.SourceIP, &v.UserAgent, &v.CreatedAt, &v.LastSeenAt, &v.ExpiresAt, &v.IdleExpiresAt, &v.MFAVerifiedAt, &v.RevokedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *PostgresStore) TouchSession(ctx context.Context, id string, last, idle time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET last_seen_at=$2,idle_expires_at=LEAST(expires_at,$3) WHERE id=$1 AND revoked_at IS NULL`, id, last, idle)
	return err
}
func (s *PostgresStore) RevokeSession(ctx context.Context, userID, id string) error {
	r, err := s.db.ExecContext(ctx, `UPDATE sessions SET revoked_at=COALESCE(revoked_at,now()) WHERE id=$1 AND user_id=$2`, id, userID)
	if err != nil {
		return err
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
func (s *PostgresStore) RevokeToken(ctx context.Context, h []byte) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET revoked_at=COALESCE(revoked_at,now()) WHERE token_hash=$1`, h)
	return err
}

var _ Store = (*PostgresStore)(nil)
