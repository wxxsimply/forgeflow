package worker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"forgeflow/internal/domain"
	"forgeflow/internal/observability"
	"forgeflow/internal/queue"
)

type Handler interface {
	Handle(context.Context, queue.LeasedJob) error
}

type HandlerFunc func(context.Context, queue.LeasedJob) error

func (f HandlerFunc) Handle(ctx context.Context, job queue.LeasedJob) error { return f(ctx, job) }

type RunStateSource interface {
	Load(context.Context, string) (*domain.RunState, error)
}

type Options struct {
	ID                string
	Queue             queue.Queue
	Handler           Handler
	StateSource       RunStateSource
	LeaseTTL          time.Duration
	HeartbeatInterval time.Duration
	CancellationPoll  time.Duration
	EmptyPollInterval time.Duration
	RetryDelay        time.Duration
}

type Worker struct {
	id                string
	queue             queue.Queue
	handler           Handler
	stateSource       RunStateSource
	leaseTTL          time.Duration
	heartbeatInterval time.Duration
	cancellationPoll  time.Duration
	emptyPollInterval time.Duration
	retryDelay        time.Duration
}

func New(options Options) (*Worker, error) {
	if strings.TrimSpace(options.ID) == "" || options.Queue == nil || options.Handler == nil {
		return nil, fmt.Errorf("worker id, queue, and handler are required")
	}
	if options.LeaseTTL <= 0 {
		options.LeaseTTL = 30 * time.Second
	}
	if options.HeartbeatInterval <= 0 {
		options.HeartbeatInterval = options.LeaseTTL / 3
	}
	if options.CancellationPoll <= 0 {
		options.CancellationPoll = time.Second
	}
	if options.EmptyPollInterval <= 0 {
		options.EmptyPollInterval = 500 * time.Millisecond
	}
	if options.RetryDelay <= 0 {
		options.RetryDelay = time.Second
	}
	if options.LeaseTTL < time.Second || options.LeaseTTL > time.Hour || options.HeartbeatInterval >= options.LeaseTTL {
		return nil, fmt.Errorf("worker lease timing is invalid")
	}
	return &Worker{
		id: options.ID, queue: options.Queue, handler: options.Handler, stateSource: options.StateSource,
		leaseTTL: options.LeaseTTL, heartbeatInterval: options.HeartbeatInterval,
		cancellationPoll: options.CancellationPoll, emptyPollInterval: options.EmptyPollInterval,
		retryDelay: options.RetryDelay,
	}, nil
}

func (w *Worker) Run(ctx context.Context) error {
	for ctx.Err() == nil {
		processed, err := w.RunOne(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			if !wait(ctx, w.retryDelay) {
				break
			}
			continue
		}
		if !processed && !wait(ctx, w.emptyPollInterval) {
			break
		}
	}
	return ctx.Err()
}

func (w *Worker) RunOne(ctx context.Context) (bool, error) {
	job, err := w.queue.Lease(ctx, w.id, w.leaseTTL)
	if errors.Is(err, queue.ErrEmpty) {
		observability.DefaultMetrics().Queue("empty")
		w.recordQueueDepth(ctx)
		return false, nil
	}
	if err != nil {
		return false, err
	}
	observability.DefaultMetrics().Queue("leased")
	w.recordQueueDepth(ctx)
	jobContext, cancel := context.WithCancel(ctx)
	defer cancel()
	handlerResult := make(chan error, 1)
	go func() { handlerResult <- w.handler.Handle(jobContext, job) }()
	heartbeat := time.NewTicker(w.heartbeatInterval)
	defer heartbeat.Stop()
	var cancellation *time.Ticker
	if w.stateSource != nil && job.RunID != "" {
		cancellation = time.NewTicker(w.cancellationPoll)
		defer cancellation.Stop()
	}
	for {
		var cancellationChannel <-chan time.Time
		if cancellation != nil {
			cancellationChannel = cancellation.C
		}
		select {
		case handlerErr := <-handlerResult:
			operationContext, operationCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer operationCancel()
			if handlerErr == nil {
				if err := w.queue.Complete(operationContext, job.LeaseID); err != nil {
					if errors.Is(err, queue.ErrLeaseLost) {
						observability.DefaultMetrics().Queue("lease_lost")
					}
					return true, err
				}
				observability.DefaultMetrics().Queue("completed")
				w.recordQueueDepth(operationContext)
				return true, nil
			}
			retryAt := time.Now().UTC().Add(w.retryDelay)
			if err := w.queue.Fail(operationContext, job.LeaseID, handlerErr, &retryAt); err != nil {
				if errors.Is(err, queue.ErrLeaseLost) {
					observability.DefaultMetrics().Queue("lease_lost")
				}
				return true, err
			}
			observability.DefaultMetrics().Queue("failed")
			w.recordQueueDepth(operationContext)
			return true, handlerErr
		case <-heartbeat.C:
			if err := w.queue.Heartbeat(ctx, job.LeaseID, w.leaseTTL); err != nil {
				cancel()
				return true, err
			}
		case <-cancellationChannel:
			state, err := w.stateSource.Load(ctx, job.RunID)
			if err == nil && state.Cancellation.Requested() {
				cancel()
			}
		case <-ctx.Done():
			cancel()
			return true, ctx.Err()
		}
	}
}

func (w *Worker) recordQueueDepth(ctx context.Context) {
	source, ok := w.queue.(queue.DepthSource)
	if !ok {
		return
	}
	if depth, err := source.Depth(ctx); err == nil {
		observability.DefaultMetrics().QueueDepth(depth)
	}
}

func wait(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
