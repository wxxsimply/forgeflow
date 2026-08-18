package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"forgeflow/internal/domain"
	"forgeflow/internal/queue"
)

func TestWorkerCompletesAndRetriesJobs(t *testing.T) {
	q := queue.NewMemoryQueue()
	job := queue.Job{ID: "job-1", Type: "test", DedupeKey: "one", Payload: []byte(`{}`), MaxAttempts: 2}
	if err := q.Enqueue(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	calls := 0
	w, err := New(Options{ID: "worker-1", Queue: q, LeaseTTL: 3 * time.Second, HeartbeatInterval: time.Second, Handler: HandlerFunc(func(context.Context, queue.LeasedJob) error {
		calls++
		if calls == 1 {
			return errors.New("transient")
		}
		return nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	if processed, err := w.RunOne(context.Background()); !processed || err == nil {
		t.Fatalf("first processed=%v err=%v", processed, err)
	}
	status, attempts, _ := q.Status(job.ID)
	if status != queue.StatusRetry || attempts != 1 {
		t.Fatalf("status=%s attempts=%d", status, attempts)
	}
}

type cancellationSource struct{ state *domain.RunState }

func (s cancellationSource) Load(context.Context, string) (*domain.RunState, error) {
	return s.state, nil
}

func TestWorkerPropagatesPersistedCancellationToHandler(t *testing.T) {
	q := queue.NewMemoryQueue()
	job := queue.Job{ID: "job-cancel", Type: "run", RunID: "run-1", DedupeKey: "cancel", Payload: []byte(`{}`), MaxAttempts: 2}
	if err := q.Enqueue(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	state := domain.NewRunState(domain.NewRunInput{Task: "x", RepositoryPath: "."})
	state.RequestCancellation("test", "cancel")
	handlerCancelled := make(chan struct{})
	w, err := New(Options{
		ID: "worker-1", Queue: q, StateSource: cancellationSource{state: state},
		LeaseTTL: 3 * time.Second, HeartbeatInterval: time.Second, CancellationPoll: 10 * time.Millisecond,
		Handler: HandlerFunc(func(ctx context.Context, _ queue.LeasedJob) error {
			<-ctx.Done()
			close(handlerCancelled)
			return ctx.Err()
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if processed, err := w.RunOne(context.Background()); !processed || !errors.Is(err, context.Canceled) {
		t.Fatalf("processed=%v err=%v", processed, err)
	}
	select {
	case <-handlerCancelled:
	default:
		t.Fatal("handler did not receive cancellation")
	}
}
