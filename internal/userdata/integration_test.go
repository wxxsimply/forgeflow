package userdata_test

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"forgeflow/internal/apperror"
	"forgeflow/internal/artifact"
	"forgeflow/internal/domain"
	pg "forgeflow/internal/postgres"
	"forgeflow/internal/userdata"
	"forgeflow/migrations"
)

func TestExportAndResumableDeletion(t *testing.T) {
	db := openDatabase(t)
	ctx := context.Background()
	ownerID, otherAdminID := domain.NewID(), domain.NewID()
	for _, user := range []struct{ id, email string }{{ownerID, "owner@example.com"}, {otherAdminID, "other@example.com"}} {
		if _, err := db.ExecContext(ctx, `INSERT INTO users(id,email,normalized_email,password_hash,role,status)
			VALUES($1,$2,$2,'not-exported','admin','active')`, user.id, user.email); err != nil {
			t.Fatal(err)
		}
	}
	store := &deletionStore{db: db}
	service, err := userdata.New(db, store, userdata.Options{ExportTTL: 10 * time.Minute, MaxExportBytes: 16 * 1024 * 1024, BackupRetention: 7 * 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := service.CreateExport(ctx, ownerID)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := service.BuildExport(ctx, ownerID, ticket.ID)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(archive.Data), int64(len(archive.Data)))
	if err != nil {
		t.Fatal(err)
	}
	if len(reader.File) != 1 || reader.File[0].Name != "database.json" {
		t.Fatalf("archive entries=%v", archiveEntries(reader))
	}
	body, err := reader.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	databaseJSON, err := io.ReadAll(body)
	if closeErr := body.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(databaseJSON, []byte("owner@example.com")) || bytes.Contains(databaseJSON, []byte("not-exported")) {
		t.Fatalf("export did not include safe owner data: %s", databaseJSON)
	}
	if _, err := service.BuildExport(ctx, otherAdminID, ticket.ID); apperror.CodeOf(err) != apperror.CodeNotFound {
		t.Fatalf("cross-owner export error=%v", err)
	}

	repositoryID, runID := domain.NewID(), domain.NewID()
	if _, err := db.ExecContext(ctx, `INSERT INTO repositories(id,owner_id,name,local_path) VALUES($1,$2,'owner-repo','.')`, repositoryID, ownerID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO runs(id,owner_id,repository_id,status,version,current_node_id,task,repository_path,base_revision,budget,state_json,created_at,updated_at)
		VALUES($1,$2,$3,'created',0,'start','delete me','.','HEAD','{}','{}',now(),now())`, runID, ownerID, repositoryID); err != nil {
		t.Fatal(err)
	}
	firstArtifact, secondArtifact := domain.NewID(), domain.NewID()
	for index, id := range []string{firstArtifact, secondArtifact} {
		createdAt := time.Date(2026, 9, 20, 0, index, 0, 0, time.UTC)
		if _, err := db.ExecContext(ctx, `INSERT INTO artifacts(id,run_id,kind,storage_key,sha256,size_bytes,content_type,metadata,created_at)
			VALUES($1,$2,'log',$1::text,repeat('a',64),1,'text/plain','{}',$3)`, id, runID, createdAt); err != nil {
			t.Fatal(err)
		}
	}
	store.failID = secondArtifact
	evalID, releaseID := domain.NewID(), domain.NewID()
	if _, err := db.ExecContext(ctx, `INSERT INTO eval_runs(id,created_by,dataset,dataset_version,status,report_json)
		VALUES($1,$2,'fixture','v1','completed','{}')`, evalID, ownerID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO prompt_releases(id,agent,version,prompt_sha256,model,eval_run_id,promoted_by,comment)
		VALUES($1,'planner','v1',repeat('b',64),'fixture-model',$2,$3,'fixture')`, releaseID, evalID, ownerID); err != nil {
		t.Fatal(err)
	}
	deletion, err := service.RequestDeletion(ctx, ownerID)
	if err != nil {
		t.Fatal(err)
	}
	var userStatus string
	if err := db.QueryRowContext(ctx, `SELECT status FROM users WHERE id=$1`, ownerID).Scan(&userStatus); err != nil || userStatus != "deletion_pending" {
		t.Fatalf("status=%q err=%v", userStatus, err)
	}
	if err := service.ProcessDeletion(ctx, deletion.ID); err == nil {
		t.Fatal("expected the injected Artifact deletion failure")
	}
	failed, err := service.GetDeletion(ctx, deletion.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != "failed" || !bytes.Contains(failed.ArtifactManifest, []byte(firstArtifact)) ||
		!bytes.Contains(failed.ArtifactManifest, []byte(secondArtifact)) ||
		!bytes.Contains(failed.DeletedArtifactIDs, []byte(firstArtifact)) {
		t.Fatalf("failed deletion did not preserve progress: %+v", failed)
	}
	if _, err := db.ExecContext(ctx, `UPDATE jobs SET status='retry' WHERE dedupe_key=$1`, "user-delete:"+deletion.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RetryDeletion(ctx, deletion.ID); err != nil {
		t.Fatal(err)
	}
	if err := service.ProcessDeletion(ctx, deletion.ID); err != nil {
		t.Fatal(err)
	}
	if err := service.ProcessDeletion(ctx, deletion.ID); err != nil {
		t.Fatalf("completed deletion is not idempotent: %v", err)
	}
	completed, err := service.GetDeletion(ctx, deletion.ID)
	if err != nil || completed.Status != "completed" || completed.CompletedAt == nil ||
		!bytes.Contains(completed.ArtifactManifest, []byte(firstArtifact)) ||
		!bytes.Contains(completed.ArtifactManifest, []byte(secondArtifact)) {
		t.Fatalf("completed deletion=%+v err=%v", completed, err)
	}
	assertCount(t, db, `SELECT count(*) FROM users WHERE id=$1`, ownerID, 0)
	assertCount(t, db, `SELECT count(*) FROM runs WHERE owner_id=$1`, ownerID, 0)
	assertCount(t, db, `SELECT count(*) FROM repositories WHERE owner_id=$1`, ownerID, 0)
	assertCount(t, db, `SELECT count(*) FROM audit_log WHERE action='account.delete.complete' AND resource_id=$1`, deletion.ID, 1)
	var createdBy, promotedBy sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT created_by::text FROM eval_runs WHERE id=$1`, evalID).Scan(&createdBy); err != nil || createdBy.Valid {
		t.Fatalf("eval actor=%v err=%v", createdBy, err)
	}
	if err := db.QueryRowContext(ctx, `SELECT promoted_by::text FROM prompt_releases WHERE id=$1`, releaseID).Scan(&promotedBy); err != nil || promotedBy.Valid {
		t.Fatalf("release actor=%v err=%v", promotedBy, err)
	}
	if _, err := service.RequestDeletion(ctx, otherAdminID); apperror.CodeOf(err) != apperror.CodeConflict {
		t.Fatalf("last administrator deletion error=%v", err)
	}
}

type deletionStore struct {
	db        *sql.DB
	failID    string
	failedOne bool
}

func (s *deletionStore) Put(context.Context, artifact.PutRequest, io.Reader) (artifact.Meta, error) {
	return artifact.Meta{}, errors.New("put is not implemented by the deletion fixture")
}
func (s *deletionStore) Open(context.Context, string) (io.ReadCloser, artifact.Meta, error) {
	return nil, artifact.Meta{}, errors.New("open is not implemented by the deletion fixture")
}
func (s *deletionStore) Delete(ctx context.Context, id string) error {
	if id == s.failID && !s.failedOne {
		s.failedOne = true
		return errors.New("injected object storage failure")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM artifacts WHERE id=$1`, id)
	return err
}
func (s *deletionStore) Check(context.Context) error { return nil }

func archiveEntries(reader *zip.Reader) []string {
	result := make([]string, 0, len(reader.File))
	for _, file := range reader.File {
		result = append(result, file.Name)
	}
	return result
}

func assertCount(t *testing.T, db *sql.DB, query, id string, expected int) {
	t.Helper()
	var count int
	if err := db.QueryRow(query, id).Scan(&count); err != nil || count != expected {
		t.Fatalf("count=%d expected=%d err=%v query=%s", count, expected, err, query)
	}
}

func openDatabase(t *testing.T) *sql.DB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("FORGEFLOW_TEST_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("FORGEFLOW_TEST_POSTGRES_DSN is not configured")
	}
	db, err := pg.Open(context.Background(), pg.Config{DSN: dsn, MaxOpenConns: 10, MaxIdleConns: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	guard, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := guard.ExecContext(context.Background(), `SELECT pg_advisory_lock(7308441907124661110)`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = guard.ExecContext(context.Background(), `SELECT pg_advisory_unlock(7308441907124661110)`)
		_ = guard.Close()
	})
	var databaseName string
	if err := db.QueryRow(`SELECT current_database()`).Scan(&databaseName); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(databaseName), "test") {
		t.Fatalf("refusing destructive integration setup against non-test database %q", databaseName)
	}
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`TRUNCATE TABLE user_deletion_requests,user_data_exports,prompt_releases,eval_runs,
		audit_log,idempotency_keys,sessions,tool_calls,model_calls,artifacts,jobs,outbox,node_executions,
		approvals,run_events,checkpoints,runs,repositories,users CASCADE`); err != nil {
		t.Fatal(err)
	}
	return db
}
