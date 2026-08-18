package graph

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"forgeflow/internal/checkpoint"
	"forgeflow/internal/domain"
)

func TestRuntimeRetriesTransientFailure(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	node := NodeFunc{
		NodeID:          "work",
		ExecutionPolicy: NodePolicy{MaxAttempts: 3, Backoff: time.Millisecond},
		Run: func(_ context.Context, state *domain.RunState) Result {
			if calls.Add(1) < 3 {
				return Result{Type: ResultRetryableError, State: state, Err: errors.New("temporary")}
			}
			return Result{Type: ResultCompleted, State: state}
		},
	}
	state := runStateAt("work")
	runtime := mustRuntime(t, Definition{EntryNodeID: "work", Nodes: []Node{node}}, checkpoint.NewFileStore(t.TempDir()))

	completed, err := runtime.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if completed.Status != domain.StatusCompleted || calls.Load() != 3 {
		t.Fatalf("status = %q, calls = %d", completed.Status, calls.Load())
	}
	execution := onlyExecution(t, completed)
	if execution.Attempts != 3 || execution.Status != domain.NodeExecutionSucceeded {
		t.Fatalf("execution = %+v", execution)
	}
}

func TestRuntimeDoesNotRetryFatalFailure(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	node := NodeFunc{
		NodeID: "work", ExecutionPolicy: NodePolicy{MaxAttempts: 3},
		Run: func(_ context.Context, state *domain.RunState) Result {
			calls.Add(1)
			return Result{Type: ResultFatalError, State: state, Err: errors.New("permanent")}
		},
	}
	runtime := mustRuntime(t, Definition{EntryNodeID: "work", Nodes: []Node{node}}, checkpoint.NewFileStore(t.TempDir()))

	failed, err := runtime.Execute(context.Background(), runStateAt("work"))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if failed.Status != domain.StatusFailed || calls.Load() != 1 {
		t.Fatalf("status = %q, calls = %d", failed.Status, calls.Load())
	}
}

func TestRuntimeHonorsRetryFilter(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	node := NodeFunc{
		NodeID:          "work",
		ExecutionPolicy: NodePolicy{MaxAttempts: 3, Retryable: func(error) bool { return false }},
		Run: func(_ context.Context, state *domain.RunState) Result {
			calls.Add(1)
			return Result{Type: ResultRetryableError, State: state, Err: errors.New("not safe to retry")}
		},
	}
	runtime := mustRuntime(t, Definition{EntryNodeID: "work", Nodes: []Node{node}}, checkpoint.NewFileStore(t.TempDir()))

	failed, err := runtime.Execute(context.Background(), runStateAt("work"))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if failed.Status != domain.StatusFailed || calls.Load() != 1 {
		t.Fatalf("status = %q, calls = %d", failed.Status, calls.Load())
	}
}

func TestRuntimeTimesOutAndStopsAtAttemptLimit(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	node := NodeFunc{
		NodeID:          "slow",
		ExecutionPolicy: NodePolicy{Timeout: 10 * time.Millisecond, MaxAttempts: 2},
		Run: func(ctx context.Context, state *domain.RunState) Result {
			calls.Add(1)
			<-ctx.Done()
			return Result{Type: ResultRetryableError, State: state, Err: ctx.Err()}
		},
	}
	runtime := mustRuntime(t, Definition{EntryNodeID: "slow", Nodes: []Node{node}}, checkpoint.NewFileStore(t.TempDir()))

	failed, err := runtime.Execute(context.Background(), runStateAt("slow"))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if failed.Status != domain.StatusFailed || calls.Load() != 2 {
		t.Fatalf("status = %q, calls = %d", failed.Status, calls.Load())
	}
	if failed.Error == nil || !strings.Contains(failed.Error.Message, "timed out") {
		t.Fatalf("run error = %+v", failed.Error)
	}
}

func TestRuntimeReusesSucceededIdempotentExecution(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	node := NodeFunc{
		NodeID: "work", Key: func(*domain.RunState) string { return "stable" },
		Run: func(_ context.Context, state *domain.RunState) Result {
			calls.Add(1)
			return Result{Type: ResultCompleted, State: state}
		},
	}
	state := runStateAt("work")
	key := "work:0:stable"
	state.NodeExecutions[key] = domain.NodeExecution{
		Key: key, NodeID: "work", IdempotencyKey: "stable", Status: domain.NodeExecutionSucceeded, Attempts: 1,
	}
	runtime := mustRuntime(t, Definition{EntryNodeID: "work", Nodes: []Node{node}}, checkpoint.NewFileStore(t.TempDir()))

	completed, err := runtime.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if completed.Status != domain.StatusCompleted || calls.Load() != 0 {
		t.Fatalf("status = %q, calls = %d", completed.Status, calls.Load())
	}
}

func TestRuntimeDoesNotReplayIndeterminateExecution(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	node := NodeFunc{
		NodeID: "write",
		Run: func(_ context.Context, state *domain.RunState) Result {
			calls.Add(1)
			return Result{Type: ResultCompleted, State: state}
		},
	}
	state := runStateAt("write")
	key := "write:0:write"
	state.NodeExecutions[key] = domain.NodeExecution{
		Key: key, NodeID: "write", IdempotencyKey: "write", Status: domain.NodeExecutionRunning, Attempts: 1,
	}
	runtime := mustRuntime(t, Definition{EntryNodeID: "write", Nodes: []Node{node}}, checkpoint.NewFileStore(t.TempDir()))

	failed, err := runtime.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if failed.Status != domain.StatusFailed || calls.Load() != 0 {
		t.Fatalf("status = %q, calls = %d", failed.Status, calls.Load())
	}
	if failed.Error == nil || failed.Error.Code != "indeterminate_node_execution" {
		t.Fatalf("run error = %+v", failed.Error)
	}
}

func TestRuntimeStopsWhenBudgetIsExhausted(t *testing.T) {
	t.Parallel()
	var secondCalls atomic.Int32
	first := NodeFunc{NodeID: "first", Run: completedNode}
	second := NodeFunc{NodeID: "second", Run: func(_ context.Context, state *domain.RunState) Result {
		secondCalls.Add(1)
		return Result{Type: ResultCompleted, State: state}
	}}
	definition := Definition{
		EntryNodeID: "first", Nodes: []Node{first, second},
		Edges: []Edge{{From: "first", To: "second"}},
	}
	state := runStateAt("first")
	state.Budget.MaxNodeCalls = 1
	runtime := mustRuntime(t, definition, checkpoint.NewFileStore(t.TempDir()))

	failed, err := runtime.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if failed.Status != domain.StatusFailed || failed.Error == nil || failed.Error.Code != "budget_exhausted" {
		t.Fatalf("state = %+v", failed)
	}
	if secondCalls.Load() != 0 {
		t.Fatalf("second node ran %d times", secondCalls.Load())
	}
}

func TestRuntimeStopsWhenIterationBudgetIsExhausted(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	node := NodeFunc{NodeID: "repair", Run: func(_ context.Context, state *domain.RunState) Result {
		calls.Add(1)
		return Result{Type: ResultCompleted, State: state}
	}}
	state := runStateAt("repair")
	state.Iteration = state.Budget.MaxIterations
	runtime := mustRuntime(t, Definition{EntryNodeID: "repair", Nodes: []Node{node}}, checkpoint.NewFileStore(t.TempDir()))

	failed, err := runtime.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if failed.Status != domain.StatusFailed || failed.Error == nil || failed.Error.Code != "budget_exhausted" {
		t.Fatalf("state = %+v", failed)
	}
	if calls.Load() != 0 {
		t.Fatalf("repair node ran %d times", calls.Load())
	}
}

func TestRuntimeCancellationStopsActiveNode(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32
	node := NodeFunc{
		NodeID: "work", ExecutionPolicy: NodePolicy{Timeout: time.Second, MaxAttempts: 2},
		Run: func(ctx context.Context, state *domain.RunState) Result {
			calls.Add(1)
			cancel()
			<-ctx.Done()
			return Result{Type: ResultRetryableError, State: state, Err: ctx.Err()}
		},
	}
	runtime := mustRuntime(t, Definition{EntryNodeID: "work", Nodes: []Node{node}}, checkpoint.NewFileStore(t.TempDir()))

	cancelled, err := runtime.Execute(ctx, runStateAt("work"))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if cancelled.Status != domain.StatusCancelled || calls.Load() != 1 {
		t.Fatalf("status = %q, calls = %d", cancelled.Status, calls.Load())
	}
}

func runStateAt(nodeID string) *domain.RunState {
	state := domain.NewRunState(domain.NewRunInput{Task: "test", RepositoryPath: "."})
	state.CurrentNodeID = nodeID
	return state
}

func mustRuntime(t *testing.T, definition Definition, store checkpoint.Store) *Runtime {
	t.Helper()
	runtime, err := NewRuntime(definition, store)
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	return runtime
}

func onlyExecution(t *testing.T, state *domain.RunState) domain.NodeExecution {
	t.Helper()
	if len(state.NodeExecutions) != 1 {
		t.Fatalf("node executions = %d, want 1", len(state.NodeExecutions))
	}
	for _, execution := range state.NodeExecutions {
		return execution
	}
	return domain.NodeExecution{}
}

func completedNode(_ context.Context, state *domain.RunState) Result {
	return Result{Type: ResultCompleted, State: state}
}
