package auth

import (
	"context"
	"database/sql"
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
	return s.findUser(ctx, `SELECT id,email,password_hash,role,status,created_at FROM users WHERE normalized_email=$1`, NormalizeEmail(email))
}
func (s *PostgresStore) FindUserByID(ctx context.Context, id string) (UserCredential, error) {
	return s.findUser(ctx, `SELECT id,email,password_hash,role,status,created_at FROM users WHERE id=$1`, id)
}
func (s *PostgresStore) UpdatePasswordHash(ctx context.Context, id, hash string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET password_hash=$2,updated_at=now() WHERE id=$1`, id, hash)
	return err
}
func (s *PostgresStore) findUser(ctx context.Context, q, arg string) (UserCredential, error) {
	var u UserCredential
	err := s.db.QueryRowContext(ctx, q, arg).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.Status, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return u, err
}
func (s *PostgresStore) CreateSession(ctx context.Context, v Session) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions(id,user_id,token_hash,csrf_hash,source_ip,user_agent,created_at,last_seen_at,expires_at,idle_expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, v.ID, v.UserID, v.TokenHash, v.CSRFHash, v.SourceIP, v.UserAgent, v.CreatedAt, v.LastSeenAt, v.ExpiresAt, v.IdleExpiresAt)
	return err
}
func (s *PostgresStore) FindSessionByTokenHash(ctx context.Context, h []byte) (Session, error) {
	var v Session
	err := s.db.QueryRowContext(ctx, `SELECT id,user_id,token_hash,csrf_hash,source_ip,user_agent,created_at,last_seen_at,expires_at,idle_expires_at,revoked_at FROM sessions WHERE token_hash=$1`, h).Scan(&v.ID, &v.UserID, &v.TokenHash, &v.CSRFHash, &v.SourceIP, &v.UserAgent, &v.CreatedAt, &v.LastSeenAt, &v.ExpiresAt, &v.IdleExpiresAt, &v.RevokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return v, err
}
func (s *PostgresStore) ListSessions(ctx context.Context, userID string) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,user_id,source_ip,user_agent,created_at,last_seen_at,expires_at,idle_expires_at,revoked_at FROM sessions WHERE user_id=$1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Session{}
	for rows.Next() {
		var v Session
		if err := rows.Scan(&v.ID, &v.UserID, &v.SourceIP, &v.UserAgent, &v.CreatedAt, &v.LastSeenAt, &v.ExpiresAt, &v.IdleExpiresAt, &v.RevokedAt); err != nil {
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
