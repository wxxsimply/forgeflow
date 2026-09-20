package userdata

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"forgeflow/internal/apperror"
	"forgeflow/internal/artifact"
	"forgeflow/internal/domain"
)

const deletionJobType = "user.delete"

type Options struct {
	ExportTTL       time.Duration
	MaxExportBytes  int64
	BackupRetention time.Duration
	Now             func() time.Time
}

type Service struct {
	db        *sql.DB
	artifacts artifact.Store
	options   Options
}

type ExportTicket struct {
	ID        string    `json:"id"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type Deletion struct {
	ID                 string          `json:"id"`
	Status             string          `json:"status"`
	ArtifactManifest   json.RawMessage `json:"artifactManifest"`
	DeletedArtifactIDs json.RawMessage `json:"deletedArtifactIds"`
	Attempts           int             `json:"attempts"`
	LastError          string          `json:"lastError,omitempty"`
	BackupPurgeAfter   time.Time       `json:"backupPurgeAfter"`
	CreatedAt          time.Time       `json:"createdAt"`
	UpdatedAt          time.Time       `json:"updatedAt"`
	CompletedAt        *time.Time      `json:"completedAt,omitempty"`
}

type ExportArchive struct {
	Data     []byte
	Filename string
}

func New(db *sql.DB, artifacts artifact.Store, options Options) (*Service, error) {
	if db == nil || artifacts == nil {
		return nil, fmt.Errorf("user data database and artifact store are required")
	}
	if options.ExportTTL <= 0 {
		options.ExportTTL = 15 * time.Minute
	}
	if options.MaxExportBytes <= 0 {
		options.MaxExportBytes = 512 * 1024 * 1024
	}
	if options.BackupRetention <= 0 {
		options.BackupRetention = 30 * 24 * time.Hour
	}
	if options.Now == nil {
		options.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{db: db, artifacts: artifacts, options: options}, nil
}

func (s *Service) CreateExport(ctx context.Context, ownerID string) (ExportTicket, error) {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM user_data_exports WHERE owner_id=$1 AND expires_at<=clock_timestamp()`, ownerID); err != nil {
		return ExportTicket{}, fmt.Errorf("remove expired user data exports: %w", err)
	}
	ticket := ExportTicket{ID: domain.NewID(), ExpiresAt: s.options.Now().Add(s.options.ExportTTL)}
	result, err := s.db.ExecContext(ctx, `INSERT INTO user_data_exports(id,owner_id,expires_at)
		SELECT $1,id,$3 FROM users WHERE id=$2 AND status='active'`, ticket.ID, ownerID, ticket.ExpiresAt)
	if err != nil {
		return ExportTicket{}, fmt.Errorf("create user data export: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ExportTicket{}, apperror.New(apperror.CodeNotFound, "user account was not found")
	}
	return ticket, nil
}

type snapshotSection struct {
	name  string
	query string
}

var snapshotSections = []snapshotSection{
	{"users", `SELECT COALESCE(jsonb_agg(to_jsonb(t)-ARRAY['password_hash','normalized_email','mfa_secret_ciphertext','mfa_pending_secret_ciphertext','mfa_recovery_code_hashes','mfa_last_used_step']), '[]'::jsonb) FROM users t WHERE id=$1`},
	{"sessions", `SELECT COALESCE(jsonb_agg(to_jsonb(t)-ARRAY['token_hash','csrf_hash']), '[]'::jsonb) FROM sessions t WHERE user_id=$1`},
	{"repositories", `SELECT COALESCE(jsonb_agg(to_jsonb(t)), '[]'::jsonb) FROM repositories t WHERE owner_id=$1`},
	{"runs", `SELECT COALESCE(jsonb_agg(to_jsonb(t)), '[]'::jsonb) FROM runs t WHERE owner_id=$1`},
	{"checkpoints", `SELECT COALESCE(jsonb_agg(to_jsonb(t)), '[]'::jsonb) FROM checkpoints t JOIN runs r ON r.id=t.run_id WHERE r.owner_id=$1`},
	{"run_events", `SELECT COALESCE(jsonb_agg(to_jsonb(t)), '[]'::jsonb) FROM run_events t JOIN runs r ON r.id=t.run_id WHERE r.owner_id=$1`},
	{"approvals", `SELECT COALESCE(jsonb_agg(to_jsonb(t)), '[]'::jsonb) FROM approvals t JOIN runs r ON r.id=t.run_id WHERE r.owner_id=$1`},
	{"node_executions", `SELECT COALESCE(jsonb_agg(to_jsonb(t)), '[]'::jsonb) FROM node_executions t JOIN runs r ON r.id=t.run_id WHERE r.owner_id=$1`},
	{"jobs", `SELECT COALESCE(jsonb_agg(to_jsonb(t)), '[]'::jsonb) FROM jobs t JOIN runs r ON r.id=t.run_id WHERE r.owner_id=$1`},
	{"artifacts", `SELECT COALESCE(jsonb_agg(to_jsonb(t)), '[]'::jsonb) FROM artifacts t JOIN runs r ON r.id=t.run_id WHERE r.owner_id=$1`},
	{"model_calls", `SELECT COALESCE(jsonb_agg(to_jsonb(t)), '[]'::jsonb) FROM model_calls t JOIN runs r ON r.id=t.run_id WHERE r.owner_id=$1`},
	{"tool_calls", `SELECT COALESCE(jsonb_agg(to_jsonb(t)), '[]'::jsonb) FROM tool_calls t JOIN runs r ON r.id=t.run_id WHERE r.owner_id=$1`},
	{"outbox", `SELECT COALESCE(jsonb_agg(to_jsonb(t)), '[]'::jsonb) FROM outbox t WHERE t.payload->>'runId' IN (SELECT id::text FROM runs WHERE owner_id=$1)`},
	{"idempotency_keys", `SELECT COALESCE(jsonb_agg(to_jsonb(t)-'request_hash'), '[]'::jsonb) FROM idempotency_keys t WHERE owner_id=$1`},
	{"audit_log", `SELECT COALESCE(jsonb_agg(to_jsonb(t)), '[]'::jsonb) FROM audit_log t WHERE actor_id=$1 OR resource_id IN (SELECT id::text FROM runs WHERE owner_id=$1) OR resource_id IN (SELECT id::text FROM repositories WHERE owner_id=$1)`},
	{"eval_runs", `SELECT COALESCE(jsonb_agg(to_jsonb(t)), '[]'::jsonb) FROM eval_runs t WHERE created_by=$1`},
	{"prompt_releases", `SELECT COALESCE(jsonb_agg(to_jsonb(t)), '[]'::jsonb) FROM prompt_releases t WHERE promoted_by=$1`},
	{"user_data_exports", `SELECT COALESCE(jsonb_agg(to_jsonb(t)), '[]'::jsonb) FROM user_data_exports t WHERE owner_id=$1`},
	{"user_deletion_requests", `SELECT COALESCE(jsonb_agg(to_jsonb(t)-'owner_hash'), '[]'::jsonb) FROM user_deletion_requests t WHERE owner_id=$1`},
}

func (s *Service) BuildExport(ctx context.Context, ownerID, exportID string) (ExportArchive, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return ExportArchive{}, fmt.Errorf("begin user data export: %w", err)
	}
	defer tx.Rollback()
	var expires time.Time
	if err := tx.QueryRowContext(ctx, `SELECT expires_at FROM user_data_exports
		WHERE id=$1 AND owner_id=$2 AND expires_at>clock_timestamp() AND downloaded_at IS NULL
		FOR UPDATE`, exportID, ownerID).Scan(&expires); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ExportArchive{}, apperror.New(apperror.CodeNotFound, "data export was not found or has expired")
		}
		return ExportArchive{}, fmt.Errorf("load user data export: %w", err)
	}
	snapshot := make(map[string]json.RawMessage, len(snapshotSections))
	for _, section := range snapshotSections {
		var raw []byte
		if err := tx.QueryRowContext(ctx, section.query, ownerID).Scan(&raw); err != nil {
			return ExportArchive{}, fmt.Errorf("export %s: %w", section.name, err)
		}
		snapshot[section.name] = json.RawMessage(raw)
	}
	metas, err := listArtifacts(ctx, tx, ownerID)
	if err != nil {
		return ExportArchive{}, err
	}
	document := struct {
		Format      string                     `json:"format"`
		GeneratedAt time.Time                  `json:"generatedAt"`
		ExpiresAt   time.Time                  `json:"expiresAt"`
		Sections    map[string]json.RawMessage `json:"sections"`
	}{Format: "forgeflow-user-data-v1", GeneratedAt: s.options.Now(), ExpiresAt: expires, Sections: snapshot}
	databaseJSON, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return ExportArchive{}, fmt.Errorf("encode user data export: %w", err)
	}
	var total int64 = int64(len(databaseJSON))
	for _, meta := range metas {
		total += meta.Size
		if total > s.options.MaxExportBytes {
			return ExportArchive{}, apperror.New(apperror.CodeValidation, "data export exceeds the configured size limit")
		}
	}
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	entry, err := archive.Create("database.json")
	if err != nil {
		return ExportArchive{}, fmt.Errorf("create export database entry: %w", err)
	}
	if _, err := entry.Write(databaseJSON); err != nil {
		return ExportArchive{}, fmt.Errorf("write export database entry: %w", err)
	}
	for _, expected := range metas {
		body, actual, err := s.artifacts.Open(ctx, expected.ID)
		if err != nil {
			return ExportArchive{}, fmt.Errorf("open export artifact %s: %w", expected.ID, err)
		}
		if actual.RunID != expected.RunID || actual.SHA256 != expected.SHA256 || actual.Size != expected.Size {
			_ = body.Close()
			return ExportArchive{}, fmt.Errorf("export artifact %s metadata changed", expected.ID)
		}
		entry, err := archive.Create("artifacts/" + expected.ID)
		if err == nil {
			_, err = io.Copy(entry, body)
		}
		closeErr := body.Close()
		if err := errors.Join(err, closeErr); err != nil {
			return ExportArchive{}, fmt.Errorf("copy export artifact %s: %w", expected.ID, err)
		}
	}
	if err := archive.Close(); err != nil {
		return ExportArchive{}, fmt.Errorf("finish user data export: %w", err)
	}
	if int64(buffer.Len()) > s.options.MaxExportBytes {
		return ExportArchive{}, apperror.New(apperror.CodeValidation, "data export exceeds the configured size limit")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE user_data_exports SET downloaded_at=clock_timestamp()
		WHERE id=$1 AND owner_id=$2 AND downloaded_at IS NULL`, exportID, ownerID); err != nil {
		return ExportArchive{}, fmt.Errorf("consume user data export ticket: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return ExportArchive{}, fmt.Errorf("commit user data export snapshot: %w", err)
	}
	return ExportArchive{Data: buffer.Bytes(), Filename: "forgeflow-user-data-" + exportID + ".zip"}, nil
}

func listArtifacts(ctx context.Context, queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, ownerID string) ([]artifact.Meta, error) {
	rows, err := queryer.QueryContext(ctx, `SELECT a.id,a.run_id,a.kind,a.storage_key,a.sha256,a.size_bytes,
		a.content_type,a.metadata,a.created_at FROM artifacts a JOIN runs r ON r.id=a.run_id
		WHERE r.owner_id=$1 ORDER BY a.created_at,a.id`, ownerID)
	if err != nil {
		return nil, fmt.Errorf("list user artifacts: %w", err)
	}
	defer rows.Close()
	var result []artifact.Meta
	for rows.Next() {
		var item artifact.Meta
		var attributes []byte
		if err := rows.Scan(&item.ID, &item.RunID, &item.Kind, &item.StorageKey, &item.SHA256, &item.Size, &item.ContentType, &attributes, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan user artifact: %w", err)
		}
		if err := json.Unmarshal(attributes, &item.Attributes); err != nil {
			return nil, fmt.Errorf("decode user artifact metadata: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Service) RequestDeletion(ctx context.Context, ownerID string) (Deletion, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Deletion{}, fmt.Errorf("begin user deletion request: %w", err)
	}
	defer tx.Rollback()
	var role, status string
	if err := tx.QueryRowContext(ctx, `SELECT role,status FROM users WHERE id=$1 FOR UPDATE`, ownerID).Scan(&role, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Deletion{}, apperror.New(apperror.CodeNotFound, "user account was not found")
		}
		return Deletion{}, fmt.Errorf("lock user for deletion: %w", err)
	}
	if role == "admin" {
		var otherAdmins int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE role='admin' AND status='active' AND id<>$1`, ownerID).Scan(&otherAdmins); err != nil {
			return Deletion{}, fmt.Errorf("count remaining administrators: %w", err)
		}
		if otherAdmins == 0 {
			return Deletion{}, apperror.New(apperror.CodeConflict, "the last active administrator cannot be deleted")
		}
	}
	ownerDigest := sha256.Sum256([]byte(ownerID))
	ownerHash := hex.EncodeToString(ownerDigest[:])
	now := s.options.Now()
	requestID := domain.NewID()
	backupPurgeAfter := now.Add(s.options.BackupRetention)
	var deletion Deletion
	err = tx.QueryRowContext(ctx, `INSERT INTO user_deletion_requests(
			id,owner_id,owner_hash,status,backup_purge_after,created_at,updated_at
		) VALUES($1,$2,$3,'pending',$4,$5,$5)
		ON CONFLICT(owner_hash) DO UPDATE SET updated_at=user_deletion_requests.updated_at
		RETURNING id,status,artifact_manifest,deleted_artifact_ids,attempts,last_error,
			backup_purge_after,created_at,updated_at,completed_at`,
		requestID, ownerID, ownerHash, backupPurgeAfter, now).Scan(
		&deletion.ID, &deletion.Status, &deletion.ArtifactManifest, &deletion.DeletedArtifactIDs,
		&deletion.Attempts, &deletion.LastError, &deletion.BackupPurgeAfter,
		&deletion.CreatedAt, &deletion.UpdatedAt, &deletion.CompletedAt)
	if err != nil {
		return Deletion{}, fmt.Errorf("persist user deletion request: %w", err)
	}
	if deletion.Status != "completed" {
		if _, err := tx.ExecContext(ctx, `UPDATE users SET status='deletion_pending',updated_at=$2 WHERE id=$1`, ownerID, now); err != nil {
			return Deletion{}, fmt.Errorf("freeze user account: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE sessions SET revoked_at=COALESCE(revoked_at,$2) WHERE user_id=$1`, ownerID, now); err != nil {
			return Deletion{}, fmt.Errorf("revoke user sessions: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE jobs SET status='dead',last_error='account deletion requested',updated_at=$2
			WHERE run_id IN (SELECT id FROM runs WHERE owner_id=$1) AND status IN ('queued','retry')`, ownerID, now); err != nil {
			return Deletion{}, fmt.Errorf("quiesce user jobs: %w", err)
		}
		payload, _ := json.Marshal(map[string]string{"deletionId": deletion.ID})
		if _, err := tx.ExecContext(ctx, `INSERT INTO jobs(id,type,run_id,dedupe_key,payload,status,max_attempts,available_at)
			VALUES($1,$2,NULL,$3,$4,'queued',100,$5)
			ON CONFLICT(dedupe_key) DO NOTHING`, domain.NewID(), deletionJobType, "user-delete:"+deletion.ID, payload, now); err != nil {
			return Deletion{}, fmt.Errorf("enqueue user deletion: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return Deletion{}, fmt.Errorf("commit user deletion request: %w", err)
	}
	return deletion, nil
}

func (s *Service) GetDeletion(ctx context.Context, id string) (Deletion, error) {
	var value Deletion
	err := s.db.QueryRowContext(ctx, `SELECT id,status,artifact_manifest,deleted_artifact_ids,attempts,last_error,
		backup_purge_after,created_at,updated_at,completed_at FROM user_deletion_requests WHERE id=$1`, id).Scan(
		&value.ID, &value.Status, &value.ArtifactManifest, &value.DeletedArtifactIDs, &value.Attempts,
		&value.LastError, &value.BackupPurgeAfter, &value.CreatedAt, &value.UpdatedAt, &value.CompletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Deletion{}, apperror.New(apperror.CodeNotFound, "deletion request was not found")
	}
	if err != nil {
		return Deletion{}, fmt.Errorf("load user deletion request: %w", err)
	}
	return value, nil
}

func (s *Service) RetryDeletion(ctx context.Context, id string) (Deletion, error) {
	result, err := s.db.ExecContext(ctx, `WITH eligible AS (
		SELECT id FROM jobs WHERE dedupe_key='user-delete:'||$1::text
			AND status IN ('retry','dead') FOR UPDATE
	), changed AS (
		UPDATE user_deletion_requests SET status='pending',last_error='',updated_at=clock_timestamp()
		WHERE id=$1::uuid AND status='failed' AND EXISTS(SELECT 1 FROM eligible) RETURNING id
	) UPDATE jobs SET status='queued',attempt=0,available_at=clock_timestamp(),last_error='',
		lease_id=NULL,lease_owner=NULL,lease_until=NULL,updated_at=clock_timestamp()
		WHERE id IN (SELECT id FROM eligible) AND EXISTS(SELECT 1 FROM changed)`, id)
	if err != nil {
		return Deletion{}, fmt.Errorf("retry user deletion: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		current, loadErr := s.GetDeletion(ctx, id)
		if loadErr != nil {
			return Deletion{}, loadErr
		}
		return current, apperror.New(apperror.CodeConflict, "only failed deletion requests can be retried")
	}
	return s.GetDeletion(ctx, id)
}

func (s *Service) ProcessDeletion(ctx context.Context, id string) error {
	var ownerID, status string
	err := s.db.QueryRowContext(ctx, `UPDATE user_deletion_requests
		SET status='processing',attempts=attempts+1,last_error='',updated_at=clock_timestamp()
		WHERE id=$1 AND status IN ('pending','failed','processing')
		RETURNING COALESCE(owner_id::text,''),status`, id).Scan(&ownerID, &status)
	if errors.Is(err, sql.ErrNoRows) {
		var current string
		if loadErr := s.db.QueryRowContext(ctx, `SELECT status FROM user_deletion_requests WHERE id=$1`, id).Scan(&current); loadErr == nil && current == "completed" {
			return nil
		}
		return apperror.New(apperror.CodeNotFound, "deletion request was not found")
	}
	if err != nil {
		return fmt.Errorf("start user deletion: %w", err)
	}
	if ownerID == "" {
		return s.completeOrphanedDeletion(ctx, id)
	}
	metas, err := listArtifacts(ctx, s.db, ownerID)
	if err != nil {
		return s.failDeletion(ctx, id, err)
	}
	manifest, _ := json.Marshal(metas)
	if _, err := s.db.ExecContext(ctx, `UPDATE user_deletion_requests SET artifact_manifest=CASE WHEN artifact_manifest='[]'::jsonb THEN $2 ELSE artifact_manifest END,updated_at=clock_timestamp() WHERE id=$1`, id, manifest); err != nil {
		return s.failDeletion(ctx, id, fmt.Errorf("persist artifact deletion manifest: %w", err))
	}
	var storedManifest, deletedRaw []byte
	if err := s.db.QueryRowContext(ctx, `SELECT artifact_manifest,deleted_artifact_ids FROM user_deletion_requests WHERE id=$1`, id).Scan(&storedManifest, &deletedRaw); err != nil {
		return s.failDeletion(ctx, id, fmt.Errorf("load artifact deletion progress: %w", err))
	}
	var original []artifact.Meta
	var deleted []string
	if err := json.Unmarshal(storedManifest, &original); err != nil {
		return s.failDeletion(ctx, id, fmt.Errorf("decode artifact deletion manifest: %w", err))
	}
	if err := json.Unmarshal(deletedRaw, &deleted); err != nil {
		return s.failDeletion(ctx, id, fmt.Errorf("decode artifact deletion progress: %w", err))
	}
	done := make(map[string]bool, len(deleted))
	for _, artifactID := range deleted {
		done[artifactID] = true
	}
	remaining := make(map[string]bool, len(metas))
	for _, item := range metas {
		remaining[item.ID] = true
	}
	progressChanged := false
	for _, item := range original {
		if !remaining[item.ID] && !done[item.ID] {
			deleted = append(deleted, item.ID)
			done[item.ID] = true
			progressChanged = true
		}
	}
	if progressChanged {
		encoded, _ := json.Marshal(deleted)
		if _, err := s.db.ExecContext(ctx, `UPDATE user_deletion_requests SET deleted_artifact_ids=$2,updated_at=clock_timestamp() WHERE id=$1`, id, encoded); err != nil {
			return s.failDeletion(ctx, id, fmt.Errorf("reconcile deleted artifact progress: %w", err))
		}
	}
	for _, item := range metas {
		if done[item.ID] {
			continue
		}
		if err := s.artifacts.Delete(ctx, item.ID); err != nil {
			return s.failDeletion(ctx, id, fmt.Errorf("delete artifact %s: %w", item.ID, err))
		}
		deleted = append(deleted, item.ID)
		encoded, _ := json.Marshal(deleted)
		if _, err := s.db.ExecContext(ctx, `UPDATE user_deletion_requests SET deleted_artifact_ids=$2,updated_at=clock_timestamp() WHERE id=$1`, id, encoded); err != nil {
			return s.failDeletion(ctx, id, fmt.Errorf("persist deleted artifact progress: %w", err))
		}
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return s.failDeletion(ctx, id, fmt.Errorf("begin final user deletion: %w", err))
	}
	defer tx.Rollback()
	finalFail := func(cause error) error {
		_ = tx.Rollback()
		return s.failDeletion(ctx, id, cause)
	}
	if err := tx.QueryRowContext(ctx, `SELECT id::text FROM users WHERE id=$1 FOR UPDATE`, ownerID).Scan(&status); errors.Is(err, sql.ErrNoRows) {
		if err := tx.Rollback(); err != nil {
			return s.failDeletion(ctx, id, err)
		}
		return s.completeOrphanedDeletion(ctx, id)
	} else if err != nil {
		return finalFail(fmt.Errorf("lock user for final deletion: %w", err))
	}
	var active int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE run_id IN
		(SELECT id FROM runs WHERE owner_id=$1) AND status='leased' AND lease_until>clock_timestamp()`, ownerID).Scan(&active); err != nil {
		return finalFail(fmt.Errorf("check active user jobs: %w", err))
	}
	if active != 0 {
		return finalFail(fmt.Errorf("user still has %d actively leased jobs", active))
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM outbox WHERE payload->>'runId' IN
		(SELECT id::text FROM runs WHERE owner_id=$1)`, ownerID); err != nil {
		return finalFail(fmt.Errorf("delete user outbox records: %w", err))
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM audit_log WHERE actor_id=$1`, ownerID); err != nil {
		return finalFail(fmt.Errorf("delete user audit records: %w", err))
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id=$1`, ownerID); err != nil {
		return finalFail(fmt.Errorf("delete user database records: %w", err))
	}
	if _, err := tx.ExecContext(ctx, `UPDATE user_deletion_requests SET owner_id=NULL,status='completed',last_error='',
		completed_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=$1`, id); err != nil {
		return finalFail(fmt.Errorf("complete user deletion request: %w", err))
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_log(actor_id,action,resource_type,resource_id,request_id,source_ip,details)
		VALUES(NULL,'account.delete.complete','user_deletion',$1,$2,'','{}'::jsonb)`, id, "background:"+id); err != nil {
		return finalFail(fmt.Errorf("audit completed user deletion: %w", err))
	}
	if err := tx.Commit(); err != nil {
		return finalFail(fmt.Errorf("commit user deletion: %w", err))
	}
	return nil
}

func (s *Service) completeOrphanedDeletion(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE user_deletion_requests SET owner_id=NULL,status='completed',
		last_error='',completed_at=COALESCE(completed_at,clock_timestamp()),updated_at=clock_timestamp() WHERE id=$1`, id)
	if err != nil {
		return fmt.Errorf("complete orphaned user deletion: %w", err)
	}
	return nil
}

func (s *Service) failDeletion(ctx context.Context, id string, cause error) error {
	message := strings.TrimSpace(cause.Error())
	if len(message) > 4000 {
		message = message[:4000]
	}
	_, updateErr := s.db.ExecContext(ctx, `UPDATE user_deletion_requests SET status='failed',last_error=$2,
		updated_at=clock_timestamp() WHERE id=$1 AND status<>'completed'`, id, message)
	return errors.Join(cause, updateErr)
}
