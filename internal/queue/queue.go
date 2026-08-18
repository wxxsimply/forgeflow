package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrEmpty = errors.New("job queue is empty")
var ErrLeaseLost = errors.New("job lease is missing, expired, or owned by another worker")
var ErrDuplicate = errors.New("job dedupe key already exists")

type Status string

const (
	StatusQueued    Status = "queued"
	StatusLeased    Status = "leased"
	StatusRetry     Status = "retry"
	StatusCompleted Status = "completed"
	StatusDead      Status = "dead"
)

type Job struct {
	ID          string          `json:"id"`
	Type        string          `json:"type"`
	RunID       string          `json:"runId,omitempty"`
	DedupeKey   string          `json:"dedupeKey"`
	Payload     json.RawMessage `json:"payload"`
	MaxAttempts int             `json:"maxAttempts"`
	AvailableAt time.Time       `json:"availableAt"`
}

func (j Job) Validate() error {
	if strings.TrimSpace(j.ID) == "" || strings.TrimSpace(j.Type) == "" || strings.TrimSpace(j.DedupeKey) == "" {
		return fmt.Errorf("job id, type, and dedupe key are required")
	}
	if j.MaxAttempts <= 0 || j.MaxAttempts > 100 {
		return fmt.Errorf("job max attempts must be between 1 and 100")
	}
	if len(j.Payload) == 0 || !json.Valid(j.Payload) || len(j.Payload) > 1024*1024 {
		return fmt.Errorf("job payload must be bounded valid JSON")
	}
	return nil
}

type LeasedJob struct {
	Job
	LeaseID    string    `json:"leaseId"`
	WorkerID   string    `json:"workerId"`
	LeaseUntil time.Time `json:"leaseUntil"`
	Attempt    int       `json:"attempt"`
}

type Queue interface {
	Enqueue(context.Context, Job) error
	Lease(context.Context, string, time.Duration) (LeasedJob, error)
	Heartbeat(context.Context, string, time.Duration) error
	Complete(context.Context, string) error
	Fail(context.Context, string, error, *time.Time) error
}

type DepthSource interface {
	Depth(context.Context) (int, error)
}
