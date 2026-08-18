package graph

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"forgeflow/internal/checkpoint"
	"forgeflow/internal/domain"
)

func TestParallelNodeIsolatesBranchesAndJoinSeesFailures(t *testing.T) {
	t.Parallel()
	parallel := ParallelNode{
		NodeID: "checks",
		Branches: []Branch{
			BranchFunc{BranchID: "test", Run: func(_ context.Context, snapshot *domain.RunState) (json.RawMessage, error) {
				snapshot.Task = "branch-local mutation"
				return json.RawMessage(`{"passed":true}`), nil
			}},
			BranchFunc{BranchID: "security", Run: func(_ context.Context, _ *domain.RunState) (json.RawMessage, error) {
				return nil, errors.New("blocking finding")
			}},
		},
	}
	join := JoinNode{
		NodeID: "join", SourceNodeID: "checks", BranchIDs: []string{"test", "security"},
		Decide: func(state *domain.RunState, branches map[string]domain.BranchState) Result {
			if branches["test"].Status != domain.BranchSucceeded {
				return Result{Type: ResultFatalError, State: state, Err: errors.New("test branch did not succeed")}
			}
			if branches["security"].Status != domain.BranchFailed {
				return Result{Type: ResultFatalError, State: state, Err: errors.New("security failure was not preserved")}
			}
			return Result{Type: ResultCompleted, State: state}
		},
	}
	definition := Definition{
		EntryNodeID: "checks", Nodes: []Node{parallel, join},
		Edges: []Edge{{From: "checks", To: "join"}},
	}
	state := runStateAt("checks")
	state.Task = "original task"
	runtime := mustRuntime(t, definition, checkpoint.NewFileStore(t.TempDir()))

	completed, err := runtime.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if completed.Status != domain.StatusCompleted {
		t.Fatalf("status = %q", completed.Status)
	}
	if completed.Task != "original task" {
		t.Fatalf("parallel branch mutated shared state: %q", completed.Task)
	}
	if got := completed.PendingBranches["checks:security"].Error; got != "blocking finding" {
		t.Fatalf("security branch error = %q", got)
	}
}

func TestJoinFailsWhenBranchIsMissing(t *testing.T) {
	t.Parallel()
	join := JoinNode{NodeID: "join", SourceNodeID: "checks", BranchIDs: []string{"missing"}}
	runtime := mustRuntime(t, Definition{EntryNodeID: "join", Nodes: []Node{join}}, checkpoint.NewFileStore(t.TempDir()))

	failed, err := runtime.Execute(context.Background(), runStateAt("join"))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if failed.Status != domain.StatusFailed {
		t.Fatalf("status = %q, want failed", failed.Status)
	}
}
