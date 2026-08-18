package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"forgeflow/internal/application"
	"forgeflow/internal/artifact"
	"forgeflow/internal/checkpoint"
	"forgeflow/internal/domain"
	"forgeflow/internal/planner"
	pg "forgeflow/internal/postgres"
	"forgeflow/internal/queue"
	"forgeflow/migrations"
)

func TestPostgresCheckpointOutboxQueueAndArtifact(t *testing.T) {
	db := openTestDatabase(t)
	ctx := context.Background()
	store := checkpoint.NewPostgresStore(db)
	state := domain.NewRunState(domain.NewRunInput{Task: "postgres integration", RepositoryPath: "."})
	if err := store.Save(ctx, state, 0); err != nil {
		t.Fatal(err)
	}
	stale, err := state.Clone()
	if err != nil {
		t.Fatal(err)
	}
	state.AppendEvent(domain.EventStatusChanged, "test", "second transaction")
	if err := store.Save(ctx, state, state.Version); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, stale, stale.Version); !errors.Is(err, checkpoint.ErrConflict) {
		t.Fatalf("stale save error=%v", err)
	}
	loaded, err := checkpoint.NewPostgresStore(db).Load(ctx, state.RunID)
	if err != nil || loaded.Version != state.Version || len(loaded.Events) != len(state.Events) {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
	var eventCount, outboxCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM run_events WHERE run_id=$1`, state.RunID).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM outbox WHERE payload->>'runId'=$1`, state.RunID).Scan(&outboxCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != len(state.Events) || outboxCount != 2 {
		t.Fatalf("events=%d/%d outbox=%d", eventCount, len(state.Events), outboxCount)
	}

	queueStore := queue.NewPostgresQueue(db)
	published, err := queueStore.DispatchOutbox(ctx, 10, 3)
	if err != nil || published != 2 {
		t.Fatalf("published=%d err=%v", published, err)
	}
	var queuedCount int
	var readyCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*),count(*) FILTER (WHERE available_at <= now()) FROM jobs`).Scan(&queuedCount, &readyCount); err != nil {
		t.Fatal(err)
	}
	if queuedCount != 2 || readyCount != 2 {
		t.Fatalf("queued=%d ready=%d", queuedCount, readyCount)
	}
	leased, err := queueStore.Lease(ctx, "worker-a", 5*time.Second)
	if err != nil || leased.RunID != state.RunID {
		t.Fatalf("leased=%+v err=%v", leased, err)
	}
	if err := queueStore.Heartbeat(ctx, leased.LeaseID, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := queueStore.Complete(ctx, leased.LeaseID); err != nil {
		t.Fatal(err)
	}

	metadata := artifact.NewPostgresMetadata(db)
	artifactStore, err := artifact.NewFileStore(t.TempDir(), metadata, 1024)
	if err != nil {
		t.Fatal(err)
	}
	created, err := artifactStore.Put(ctx, artifact.PutRequest{RunID: state.RunID, Kind: artifact.KindPatch, ContentType: "text/x-diff"}, strings.NewReader("patch"))
	if err != nil {
		t.Fatal(err)
	}
	listed, err := metadata.List(ctx, state.RunID)
	if err != nil || len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("listed=%+v err=%v", listed, err)
	}
}

func TestPostgresServiceResumesAfterProcessRestart(t *testing.T) {
	db := openTestDatabase(t)
	store := checkpoint.NewPostgresStore(db)
	firstProcess := application.NewService(store, planner.Mock{})
	waiting, err := firstProcess.Create(context.Background(), application.CreateInput{Task: "restart", RepositoryPath: "."})
	if err != nil {
		t.Fatal(err)
	}
	if waiting.Status != domain.StatusWaitingPlanApproval {
		t.Fatalf("status=%s", waiting.Status)
	}
	secondProcess := application.NewService(checkpoint.NewPostgresStore(db), planner.Mock{})
	completed, err := secondProcess.ResolveApproval(context.Background(), waiting.RunID, true, "approved after restart")
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != domain.StatusCompleted {
		t.Fatalf("status=%s error=%+v", completed.Status, completed.Error)
	}
}

func TestPostgresQueueAllowsOnlyOneWorkerAndRecoversExpiredLease(t *testing.T) {
	db := openTestDatabase(t)
	ctx := context.Background()
	q := queue.NewPostgresQueue(db)
	job := queue.Job{ID: domain.NewID(), Type: "fixture", DedupeKey: domain.NewID(), Payload: []byte(`{}`), MaxAttempts: 2}
	if err := q.Enqueue(ctx, job); err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	var mu sync.Mutex
	winners := make([]queue.LeasedJob, 0, 1)
	for _, workerID := range []string{"worker-a", "worker-b"} {
		wait.Add(1)
		go func() {
			defer wait.Done()
			leased, err := q.Lease(ctx, workerID, 5*time.Second)
			if err == nil {
				mu.Lock()
				winners = append(winners, leased)
				mu.Unlock()
			} else if !errors.Is(err, queue.ErrEmpty) {
				t.Errorf("lease: %v", err)
			}
		}()
	}
	wait.Wait()
	if len(winners) != 1 {
		t.Fatalf("lease winners=%d", len(winners))
	}
	if _, err := db.ExecContext(ctx, `UPDATE jobs SET lease_until=now()-interval '1 second' WHERE id=$1`, job.ID); err != nil {
		t.Fatal(err)
	}
	recovered, err := q.Lease(ctx, "worker-c", 5*time.Second)
	if err != nil || recovered.ID != job.ID || recovered.Attempt != 2 {
		t.Fatalf("recovered=%+v err=%v", recovered, err)
	}
}

func TestCheckpointTransactionRollsBackProjectionWhenSnapshotInsertFails(t *testing.T) {
	db := openTestDatabase(t)
	ctx := context.Background()
	store := checkpoint.NewPostgresStoreWithOptions(db, checkpoint.PostgresOptions{})
	state := domain.NewRunState(domain.NewRunInput{Task: "rollback", RepositoryPath: "."})
	if err := store.Save(ctx, state, 0); err != nil {
		t.Fatal(err)
	}
	before := state.Version
	if _, err := db.ExecContext(ctx, `INSERT INTO checkpoints(run_id,version,node_id,state_json) VALUES($1,$2,'collision','{}')`, state.RunID, before+1); err != nil {
		t.Fatal(err)
	}
	state.Task = "must rollback"
	if err := store.Save(ctx, state, before); err == nil {
		t.Fatal("checkpoint collision did not fail")
	}
	var version int64
	var task string
	if err := db.QueryRowContext(ctx, `SELECT version,task FROM runs WHERE id=$1`, state.RunID).Scan(&version, &task); err != nil {
		t.Fatal(err)
	}
	if version != before || task == "must rollback" {
		t.Fatalf("projection was partially committed: version=%d task=%q", version, task)
	}
}

func openTestDatabase(t *testing.T) *sql.DB {
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
		_ = guard.Close()
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
	if _, err := db.Exec(`TRUNCATE TABLE audit_log,idempotency_keys,sessions,tool_calls,model_calls,artifacts,jobs,outbox,
		node_executions,approvals,run_events,checkpoints,runs,repositories,users CASCADE`); err != nil {
		t.Fatal(err)
	}
	return db
}
