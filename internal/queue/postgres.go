package queue

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"forgeflow/internal/domain"
)

type PostgresQueue struct {
	db *sql.DB
}

func NewPostgresQueue(db *sql.DB) *PostgresQueue {
	return &PostgresQueue{db: db}
}

func (q *PostgresQueue) Enqueue(ctx context.Context, job Job) error {
	if q == nil || q.db == nil {
		return fmt.Errorf("PostgreSQL queue is not configured")
	}
	if err := job.Validate(); err != nil {
		return err
	}
	if job.AvailableAt.IsZero() {
		job.AvailableAt = time.Now().UTC()
	}
	result, err := q.db.ExecContext(ctx, `INSERT INTO jobs(
        id,type,run_id,dedupe_key,payload,status,max_attempts,available_at
    ) VALUES ($1,$2,NULLIF($3,'')::uuid,$4,$5,'queued',$6,$7)
    ON CONFLICT (dedupe_key) DO NOTHING`,
		job.ID, job.Type, job.RunID, job.DedupeKey, job.Payload, job.MaxAttempts, job.AvailableAt)
	if err != nil {
		return fmt.Errorf("enqueue job: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrDuplicate
	}
	return nil
}

func (q *PostgresQueue) Lease(ctx context.Context, workerID string, ttl time.Duration) (LeasedJob, error) {
	if q == nil || q.db == nil {
		return LeasedJob{}, fmt.Errorf("PostgreSQL queue is not configured")
	}
	if strings.TrimSpace(workerID) == "" || ttl < time.Second || ttl > time.Hour {
		return LeasedJob{}, fmt.Errorf("worker id and lease ttl are invalid")
	}
	leaseID := domain.NewID()
	tx, err := q.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return LeasedJob{}, fmt.Errorf("begin job lease: %w", err)
	}
	defer tx.Rollback()
	var now time.Time
	if err := tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return LeasedJob{}, fmt.Errorf("read database lease clock: %w", err)
	}
	leaseUntil := now.Add(ttl)
	if _, err := tx.ExecContext(ctx, `UPDATE jobs SET status='dead', lease_id=NULL,
        lease_owner=NULL, lease_until=NULL, updated_at=$1,
        last_error=CASE WHEN last_error='' THEN 'lease expired after maximum attempts' ELSE last_error END
        WHERE status='leased' AND lease_until <= $1 AND attempt >= max_attempts`, now); err != nil {
		return LeasedJob{}, fmt.Errorf("expire exhausted jobs: %w", err)
	}
	row := tx.QueryRowContext(ctx, `WITH candidate AS (
        SELECT id FROM jobs
        WHERE attempt < max_attempts AND (
            (status IN ('queued','retry') AND available_at <= $1)
            OR (status='leased' AND lease_until <= $1)
        )
        ORDER BY available_at, created_at, id
        FOR UPDATE SKIP LOCKED
        LIMIT 1
    )
    UPDATE jobs AS job SET status='leased', attempt=job.attempt+1,
        lease_id=$2, lease_owner=$3, lease_until=$4, updated_at=$1
    FROM candidate WHERE job.id=candidate.id
    RETURNING job.id,job.type,COALESCE(job.run_id::text,''),job.dedupe_key,
        job.payload,job.max_attempts,job.available_at,job.attempt`,
		now, leaseID, workerID, leaseUntil)
	leased := LeasedJob{LeaseID: leaseID, WorkerID: workerID, LeaseUntil: leaseUntil}
	if err := row.Scan(&leased.ID, &leased.Type, &leased.RunID, &leased.DedupeKey,
		&leased.Payload, &leased.MaxAttempts, &leased.AvailableAt, &leased.Attempt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return LeasedJob{}, ErrEmpty
		}
		return LeasedJob{}, fmt.Errorf("lease job: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return LeasedJob{}, fmt.Errorf("commit job lease: %w", err)
	}
	return leased, nil
}

func (q *PostgresQueue) Heartbeat(ctx context.Context, leaseID string, ttl time.Duration) error {
	if strings.TrimSpace(leaseID) == "" || ttl < time.Second || ttl > time.Hour {
		return fmt.Errorf("lease id and ttl are invalid")
	}
	result, err := q.db.ExecContext(ctx, `UPDATE jobs SET
        lease_until=clock_timestamp()+($1::double precision*interval '1 second'),updated_at=clock_timestamp()
        WHERE lease_id=$2 AND status='leased' AND lease_until > clock_timestamp()`, ttl.Seconds(), leaseID)
	if err != nil {
		return fmt.Errorf("heartbeat job: %w", err)
	}
	return requireLeaseRow(result)
}

func (q *PostgresQueue) Complete(ctx context.Context, leaseID string) error {
	result, err := q.db.ExecContext(ctx, `UPDATE jobs SET status='completed',completed_at=clock_timestamp(),
        lease_id=NULL,lease_owner=NULL,lease_until=NULL,updated_at=clock_timestamp()
        WHERE lease_id=$1 AND status='leased' AND lease_until > clock_timestamp()`, leaseID)
	if err != nil {
		return fmt.Errorf("complete job: %w", err)
	}
	return requireLeaseRow(result)
}

func (q *PostgresQueue) Fail(ctx context.Context, leaseID string, cause error, retryAt *time.Time) error {
	if strings.TrimSpace(leaseID) == "" {
		return fmt.Errorf("lease id is required")
	}
	message := "job failed"
	if cause != nil {
		message = cause.Error()
	}
	if len(message) > 4_000 {
		message = message[:4_000]
	}
	tx, err := q.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var now time.Time
	if err := tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return fmt.Errorf("read database failure clock: %w", err)
	}
	var attempt, maxAttempts int
	if err := tx.QueryRowContext(ctx, `SELECT attempt,max_attempts FROM jobs
        WHERE lease_id=$1 AND status='leased' AND lease_until > $2 FOR UPDATE`, leaseID, now).Scan(&attempt, &maxAttempts); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrLeaseLost
		}
		return fmt.Errorf("lock failed job: %w", err)
	}
	status := StatusDead
	availableAt := now
	if attempt < maxAttempts {
		status = StatusRetry
		if retryAt != nil && retryAt.After(now) {
			availableAt = retryAt.UTC()
		} else {
			availableAt = now.Add(retryBackoff(attempt))
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE jobs SET status=$1,available_at=$2,last_error=$3,
        lease_id=NULL,lease_owner=NULL,lease_until=NULL,updated_at=$4 WHERE lease_id=$5`,
		status, availableAt, message, now, leaseID); err != nil {
		return fmt.Errorf("fail job: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit failed job: %w", err)
	}
	return nil
}

func (q *PostgresQueue) DispatchOutbox(ctx context.Context, limit int, maxAttempts int) (int, error) {
	if limit <= 0 || limit > 1_000 || maxAttempts <= 0 || maxAttempts > 100 {
		return 0, fmt.Errorf("outbox dispatch limits are invalid")
	}
	tx, err := q.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id,topic,dedupe_key,payload,available_at
        FROM outbox WHERE published_at IS NULL AND available_at <= now()
        ORDER BY available_at,created_at FOR UPDATE SKIP LOCKED LIMIT $1`, limit)
	if err != nil {
		return 0, fmt.Errorf("select outbox: %w", err)
	}
	type message struct {
		id, topic, dedupe string
		payload           json.RawMessage
		available         time.Time
	}
	messages := make([]message, 0, limit)
	for rows.Next() {
		var item message
		if err := rows.Scan(&item.id, &item.topic, &item.dedupe, &item.payload, &item.available); err != nil {
			rows.Close()
			return 0, err
		}
		messages = append(messages, item)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	for _, item := range messages {
		var envelope struct {
			RunID string `json:"runId"`
		}
		if err := json.Unmarshal(item.payload, &envelope); err != nil {
			return 0, fmt.Errorf("decode outbox %s: %w", item.id, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO jobs(
            id,type,run_id,dedupe_key,payload,status,max_attempts,available_at
        ) VALUES ($1,$2,NULLIF($3,'')::uuid,$4,$5,'queued',$6,$7)
        ON CONFLICT (dedupe_key) DO NOTHING`,
			domain.NewID(), item.topic, envelope.RunID, "outbox:"+item.dedupe,
			item.payload, maxAttempts, item.available); err != nil {
			return 0, fmt.Errorf("publish outbox %s: %w", item.id, err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE outbox SET published_at=now(),attempts=attempts+1 WHERE id=$1`, item.id); err != nil {
			return 0, fmt.Errorf("mark outbox %s published: %w", item.id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(messages), nil
}

func (q *PostgresQueue) Depth(ctx context.Context) (int, error) {
	if q == nil || q.db == nil {
		return 0, fmt.Errorf("PostgreSQL queue is not configured")
	}
	var depth int
	err := q.db.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE
		status IN ('queued','retry') OR (status='leased' AND lease_until <= clock_timestamp())`).Scan(&depth)
	if err != nil {
		return 0, fmt.Errorf("read queue depth: %w", err)
	}
	return depth, nil
}

func requireLeaseRow(result sql.Result) error {
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrLeaseLost
	}
	return nil
}

func retryBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	duration := time.Second << min(attempt-1, 8)
	if duration > 5*time.Minute {
		return 5 * time.Minute
	}
	return duration
}

var _ Queue = (*PostgresQueue)(nil)
var _ DepthSource = (*PostgresQueue)(nil)
