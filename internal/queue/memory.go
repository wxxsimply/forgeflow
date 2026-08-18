package queue

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"forgeflow/internal/domain"
)

type memoryRecord struct {
	job        Job
	status     Status
	attempt    int
	leaseID    string
	workerID   string
	leaseUntil time.Time
	createdAt  time.Time
	lastError  string
}

type MemoryQueue struct {
	mu      sync.Mutex
	records map[string]*memoryRecord
	dedupe  map[string]string
	now     func() time.Time
}

func NewMemoryQueue() *MemoryQueue {
	return &MemoryQueue{
		records: map[string]*memoryRecord{}, dedupe: map[string]string{},
		now: func() time.Time { return time.Now().UTC() },
	}
}

func (q *MemoryQueue) Enqueue(ctx context.Context, job Job) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := job.Validate(); err != nil {
		return err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if _, exists := q.dedupe[job.DedupeKey]; exists {
		return ErrDuplicate
	}
	now := q.now()
	if job.AvailableAt.IsZero() {
		job.AvailableAt = now
	}
	q.records[job.ID] = &memoryRecord{job: job, status: StatusQueued, createdAt: now}
	q.dedupe[job.DedupeKey] = job.ID
	return nil
}

func (q *MemoryQueue) Lease(ctx context.Context, workerID string, ttl time.Duration) (LeasedJob, error) {
	if err := ctx.Err(); err != nil {
		return LeasedJob{}, err
	}
	if strings.TrimSpace(workerID) == "" || ttl < time.Second || ttl > time.Hour {
		return LeasedJob{}, errors.New("worker id and lease ttl are invalid")
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	now := q.now()
	candidates := make([]*memoryRecord, 0)
	for _, record := range q.records {
		if record.status == StatusLeased && !record.leaseUntil.After(now) && record.attempt >= record.job.MaxAttempts {
			record.status = StatusDead
			record.leaseID, record.workerID = "", ""
		}
		ready := (record.status == StatusQueued || record.status == StatusRetry) && !record.job.AvailableAt.After(now)
		expired := record.status == StatusLeased && !record.leaseUntil.After(now)
		if record.attempt < record.job.MaxAttempts && (ready || expired) {
			candidates = append(candidates, record)
		}
	}
	if len(candidates) == 0 {
		return LeasedJob{}, ErrEmpty
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].job.AvailableAt.Equal(candidates[j].job.AvailableAt) {
			return candidates[i].createdAt.Before(candidates[j].createdAt)
		}
		return candidates[i].job.AvailableAt.Before(candidates[j].job.AvailableAt)
	})
	record := candidates[0]
	record.status = StatusLeased
	record.attempt++
	record.leaseID = domain.NewID()
	record.workerID = workerID
	record.leaseUntil = now.Add(ttl)
	return LeasedJob{Job: record.job, LeaseID: record.leaseID, WorkerID: workerID, LeaseUntil: record.leaseUntil, Attempt: record.attempt}, nil
}

func (q *MemoryQueue) Heartbeat(ctx context.Context, leaseID string, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	record := q.byLease(leaseID)
	now := q.now()
	if record == nil || record.status != StatusLeased || !record.leaseUntil.After(now) {
		return ErrLeaseLost
	}
	record.leaseUntil = now.Add(ttl)
	return nil
}

func (q *MemoryQueue) Complete(ctx context.Context, leaseID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	record := q.byLease(leaseID)
	if record == nil || record.status != StatusLeased || !record.leaseUntil.After(q.now()) {
		return ErrLeaseLost
	}
	record.status = StatusCompleted
	record.leaseID, record.workerID = "", ""
	return nil
}

func (q *MemoryQueue) Fail(ctx context.Context, leaseID string, cause error, retryAt *time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	record := q.byLease(leaseID)
	now := q.now()
	if record == nil || record.status != StatusLeased || !record.leaseUntil.After(now) {
		return ErrLeaseLost
	}
	if cause != nil {
		record.lastError = cause.Error()
	}
	record.leaseID, record.workerID = "", ""
	if record.attempt >= record.job.MaxAttempts {
		record.status = StatusDead
		return nil
	}
	record.status = StatusRetry
	record.job.AvailableAt = now.Add(retryBackoff(record.attempt))
	if retryAt != nil && retryAt.After(now) {
		record.job.AvailableAt = retryAt.UTC()
	}
	return nil
}

func (q *MemoryQueue) byLease(leaseID string) *memoryRecord {
	for _, record := range q.records {
		if record.leaseID == leaseID {
			return record
		}
	}
	return nil
}

func (q *MemoryQueue) Status(jobID string) (Status, int, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	record, exists := q.records[jobID]
	if !exists {
		return "", 0, false
	}
	return record.status, record.attempt, true
}

func (q *MemoryQueue) Depth(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	depth := 0
	for _, record := range q.records {
		if record.status == StatusQueued || record.status == StatusRetry || (record.status == StatusLeased && !record.leaseUntil.After(q.now())) {
			depth++
		}
	}
	return depth, nil
}

var _ Queue = (*MemoryQueue)(nil)
var _ DepthSource = (*MemoryQueue)(nil)
