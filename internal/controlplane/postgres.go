package controlplane

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"forgeflow/internal/artifact"
	"forgeflow/internal/checkpoint"
	"forgeflow/internal/domain"
)

type Repository struct {
	ID            string    `json:"id"`
	OwnerID       string    `json:"ownerId"`
	Name          string    `json:"name"`
	LocalPath     string    `json:"localPath"`
	DefaultBranch string    `json:"defaultBranch"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type SequencedEvent struct {
	Sequence int64           `json:"sequence"`
	Event    domain.RunEvent `json:"event"`
}
type Approval struct {
	Request    domain.ApprovalRequest `json:"request"`
	RunVersion int64                  `json:"runVersion"`
	OwnerID    string                 `json:"-"`
}
type Page[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"nextCursor,omitempty"`
}
type IdempotencyResult struct {
	RunID   string
	Match   bool
	Found   bool
	Pending bool
}
type AuditEntry struct {
	ActorID, Action, ResourceType, ResourceID, RequestID, SourceIP string
	Details                                                        map[string]any
}

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) CreateRepository(ctx context.Context, v Repository) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO repositories(id,owner_id,name,local_path,default_branch,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$6)`, v.ID, v.OwnerID, v.Name, v.LocalPath, v.DefaultBranch, v.CreatedAt)
	return err
}
func (s *Store) GetRepository(ctx context.Context, id, owner string, admin bool) (Repository, error) {
	var v Repository
	err := s.db.QueryRowContext(ctx, `SELECT id,owner_id,name,local_path,default_branch,created_at,updated_at FROM repositories WHERE id=$1 AND (owner_id=$2 OR $3)`, id, owner, admin).Scan(&v.ID, &v.OwnerID, &v.Name, &v.LocalPath, &v.DefaultBranch, &v.CreatedAt, &v.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return v, checkpoint.ErrNotFound
	}
	return v, err
}
func (s *Store) ListRepositories(ctx context.Context, owner string, admin bool, cursor string, limit int) (Page[Repository], error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	at, id, err := decodeCursor(cursor)
	if err != nil {
		return Page[Repository]{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,owner_id,name,local_path,default_branch,created_at,updated_at FROM repositories WHERE (owner_id=$1 OR $2) AND ($3::timestamptz IS NULL OR (created_at,id)<($3,$4::uuid)) ORDER BY created_at DESC,id DESC LIMIT $5`, owner, admin, nullableTime(at), nullableID(id), limit+1)
	if err != nil {
		return Page[Repository]{}, err
	}
	defer rows.Close()
	items := []Repository{}
	for rows.Next() {
		var v Repository
		if err := rows.Scan(&v.ID, &v.OwnerID, &v.Name, &v.LocalPath, &v.DefaultBranch, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return Page[Repository]{}, err
		}
		items = append(items, v)
	}
	page := Page[Repository]{Items: items}
	if len(items) > limit {
		last := items[limit-1]
		page.Items = items[:limit]
		page.NextCursor = encodeCursor(last.CreatedAt, last.ID)
	}
	return page, rows.Err()
}
func (s *Store) DeleteRepository(ctx context.Context, id, owner string, admin bool) error {
	r, err := s.db.ExecContext(ctx, `DELETE FROM repositories WHERE id=$1 AND (owner_id=$2 OR $3)`, id, owner, admin)
	if err != nil {
		return err
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return checkpoint.ErrNotFound
	}
	return nil
}

func (s *Store) LoadRun(ctx context.Context, id, owner string, admin bool) (*domain.RunState, error) {
	var data []byte
	err := s.db.QueryRowContext(ctx, `SELECT state_json FROM runs WHERE id=$1 AND (owner_id=$2 OR $3)`, id, owner, admin).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, checkpoint.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var state domain.RunState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}
func (s *Store) ListRuns(ctx context.Context, owner string, admin bool, cursor string, limit int) (Page[domain.RunState], error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	at, id, err := decodeCursor(cursor)
	if err != nil {
		return Page[domain.RunState]{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT state_json,created_at,id FROM runs WHERE (owner_id=$1 OR $2) AND ($3::timestamptz IS NULL OR (created_at,id)<($3,$4::uuid)) ORDER BY created_at DESC,id DESC LIMIT $5`, owner, admin, nullableTime(at), nullableID(id), limit+1)
	if err != nil {
		return Page[domain.RunState]{}, err
	}
	defer rows.Close()
	items := []domain.RunState{}
	times := []time.Time{}
	for rows.Next() {
		var data []byte
		var at time.Time
		var runID string
		if err := rows.Scan(&data, &at, &runID); err != nil {
			return Page[domain.RunState]{}, err
		}
		var v domain.RunState
		if err := json.Unmarshal(data, &v); err != nil {
			return Page[domain.RunState]{}, err
		}
		items = append(items, v)
		times = append(times, at)
	}
	page := Page[domain.RunState]{Items: items}
	if len(items) > limit {
		page.Items = items[:limit]
		page.NextCursor = encodeCursor(times[limit-1], items[limit-1].RunID)
	}
	return page, rows.Err()
}
func (s *Store) ListEvents(ctx context.Context, runID, owner string, admin bool, after int64, limit int) ([]SequencedEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `SELECT e.sequence,e.payload FROM run_events e JOIN runs r ON r.id=e.run_id WHERE e.run_id=$1 AND e.sequence>$2 AND (r.owner_id=$3 OR $4) ORDER BY e.sequence LIMIT $5`, runID, after, owner, admin, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SequencedEvent{}
	for rows.Next() {
		var v SequencedEvent
		var data []byte
		if err := rows.Scan(&v.Sequence, &data); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(data, &v.Event); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		var exists bool
		if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM runs WHERE id=$1 AND (owner_id=$2 OR $3))`, runID, owner, admin).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, checkpoint.ErrNotFound
		}
	}
	return out, rows.Err()
}
func (s *Store) ListApprovals(ctx context.Context, owner string, admin bool, status string) ([]Approval, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT a.request_json,r.version,r.owner_id FROM approvals a JOIN runs r ON r.id=a.run_id WHERE (r.owner_id=$1 OR $2) AND ($3='' OR a.status=$3) ORDER BY a.requested_at DESC LIMIT 200`, owner, admin, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Approval{}
	for rows.Next() {
		var v Approval
		var data []byte
		if err := rows.Scan(&data, &v.RunVersion, &v.OwnerID); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(data, &v.Request); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) GetApproval(ctx context.Context, id, owner string, admin bool) (Approval, error) {
	var v Approval
	var data []byte
	err := s.db.QueryRowContext(ctx, `SELECT a.request_json,r.version,r.owner_id FROM approvals a JOIN runs r ON r.id=a.run_id WHERE a.id=$1 AND (r.owner_id=$2 OR $3)`, id, owner, admin).Scan(&data, &v.RunVersion, &v.OwnerID)
	if errors.Is(err, sql.ErrNoRows) {
		return v, checkpoint.ErrNotFound
	}
	if err != nil {
		return v, err
	}
	err = json.Unmarshal(data, &v.Request)
	return v, err
}
func (s *Store) ListArtifacts(ctx context.Context, runID, owner string, admin bool) ([]artifact.Meta, error) {
	allowed, err := s.LoadRun(ctx, runID, owner, admin)
	if err != nil {
		return nil, err
	}
	_ = allowed
	return artifact.NewPostgresMetadata(s.db).List(ctx, runID)
}

func (s *Store) ClaimIdempotency(ctx context.Context, owner, key string, request []byte) (IdempotencyResult, error) {
	sum := sha256.Sum256(request)
	result, err := s.db.ExecContext(ctx, `INSERT INTO idempotency_keys(owner_id,key,request_hash,run_id,status,expires_at)
		VALUES($1,$2,$3,NULL,'pending',now()+interval '24 hours') ON CONFLICT(owner_id,key) DO NOTHING`, owner, key, sum[:])
	if err != nil {
		return IdempotencyResult{}, err
	}
	if rows, _ := result.RowsAffected(); rows == 1 {
		return IdempotencyResult{}, nil
	}
	var runID sql.NullString
	var stored []byte
	var status string
	err = s.db.QueryRowContext(ctx, `SELECT run_id,request_hash,status FROM idempotency_keys WHERE owner_id=$1 AND key=$2 AND expires_at>now()`, owner, key).Scan(&runID, &stored, &status)
	if errors.Is(err, sql.ErrNoRows) {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM idempotency_keys WHERE owner_id=$1 AND key=$2 AND expires_at<=now()`, owner, key)
		return s.ClaimIdempotency(ctx, owner, key, request)
	}
	if err != nil {
		return IdempotencyResult{}, err
	}
	return IdempotencyResult{RunID: runID.String, Match: string(stored) == string(sum[:]), Found: true, Pending: status == "pending"}, nil
}
func (s *Store) SaveIdempotency(ctx context.Context, owner, key string, request []byte, runID string) error {
	sum := sha256.Sum256(request)
	result, err := s.db.ExecContext(ctx, `UPDATE idempotency_keys SET run_id=$4,status='completed' WHERE owner_id=$1 AND key=$2 AND request_hash=$3 AND status='pending'`, owner, key, sum[:], runID)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return checkpoint.ErrConflict
	}
	return nil
}
func (s *Store) ReleaseIdempotency(ctx context.Context, owner, key string, request []byte) {
	sum := sha256.Sum256(request)
	_, _ = s.db.ExecContext(ctx, `DELETE FROM idempotency_keys WHERE owner_id=$1 AND key=$2 AND request_hash=$3 AND status='pending'`, owner, key, sum[:])
}
func (s *Store) Audit(ctx context.Context, v AuditEntry) error {
	details, _ := json.Marshal(v.Details)
	_, err := s.db.ExecContext(ctx, `INSERT INTO audit_log(actor_id,action,resource_type,resource_id,request_id,source_ip,details) VALUES(NULLIF($1,'')::uuid,$2,$3,$4,$5,$6,$7)`, v.ActorID, v.Action, v.ResourceType, v.ResourceID, v.RequestID, v.SourceIP, details)
	return err
}

func encodeCursor(at time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(at.UTC().Format(time.RFC3339Nano) + "|" + id))
}
func decodeCursor(value string) (time.Time, string, error) {
	if value == "" {
		return time.Time{}, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("invalid cursor")
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 {
		return time.Time{}, "", fmt.Errorf("invalid cursor")
	}
	if !uuidPattern.MatchString(parts[1]) {
		return time.Time{}, "", fmt.Errorf("invalid cursor")
	}
	at, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, "", fmt.Errorf("invalid cursor")
	}
	return at, parts[1], nil
}

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func nullableTime(v time.Time) any {
	if v.IsZero() {
		return nil
	}
	return v
}
func nullableID(v string) any {
	if v == "" {
		return nil
	}
	return v
}
