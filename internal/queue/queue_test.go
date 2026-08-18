package queue

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestMemoryQueueProvidesExclusiveLease(t *testing.T) {
	q := NewMemoryQueue()
	job := Job{ID: "job-1", Type: "run.wakeup", DedupeKey: "one", Payload: []byte(`{"runId":"run-1"}`), MaxAttempts: 2}
	if err := q.Enqueue(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	winners := 0
	var mu sync.Mutex
	for _, workerID := range []string{"worker-a", "worker-b"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := q.Lease(context.Background(), workerID, time.Minute)
			if err == nil {
				mu.Lock()
				winners++
				mu.Unlock()
			} else if !errors.Is(err, ErrEmpty) {
				t.Errorf("Lease error=%v", err)
			}
		}()
	}
	wg.Wait()
	if winners != 1 {
		t.Fatalf("lease winners=%d", winners)
	}
}

func TestMemoryQueueRetriesThenDeadLetters(t *testing.T) {
	q := NewMemoryQueue()
	now := time.Now().UTC()
	q.now = func() time.Time { return now }
	job := Job{ID: "job-1", Type: "run", DedupeKey: "one", Payload: []byte(`{}`), MaxAttempts: 2}
	if err := q.Enqueue(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	first, err := q.Lease(context.Background(), "worker-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	retryAt := now.Add(time.Second)
	if err := q.Fail(context.Background(), first.LeaseID, errors.New("boom"), &retryAt); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Lease(context.Background(), "worker-b", time.Minute); !errors.Is(err, ErrEmpty) {
		t.Fatalf("early lease error=%v", err)
	}
	now = retryAt
	second, err := q.Lease(context.Background(), "worker-b", time.Minute)
	if err != nil || second.Attempt != 2 {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	if err := q.Fail(context.Background(), second.LeaseID, errors.New("again"), nil); err != nil {
		t.Fatal(err)
	}
	status, attempts, _ := q.Status(job.ID)
	if status != StatusDead || attempts != 2 {
		t.Fatalf("status=%s attempts=%d", status, attempts)
	}
}
