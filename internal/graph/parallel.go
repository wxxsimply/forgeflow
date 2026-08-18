package graph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"forgeflow/internal/domain"
)

type Branch interface {
	ID() string
	Execute(context.Context, *domain.RunState) (json.RawMessage, error)
}

type BranchFunc struct {
	BranchID string
	Run      func(context.Context, *domain.RunState) (json.RawMessage, error)
}

func (b BranchFunc) ID() string { return b.BranchID }

func (b BranchFunc) Execute(ctx context.Context, state *domain.RunState) (json.RawMessage, error) {
	return b.Run(ctx, state)
}

type ParallelNode struct {
	NodeID          string
	Branches        []Branch
	MaxConcurrency  int
	ExecutionPolicy NodePolicy
}

func (n ParallelNode) ID() string { return n.NodeID }

func (n ParallelNode) Policy() NodePolicy { return n.ExecutionPolicy.normalized() }

func (n ParallelNode) IdempotencyKey(*domain.RunState) string { return n.NodeID }

func (n ParallelNode) Execute(ctx context.Context, state *domain.RunState) Result {
	if len(n.Branches) == 0 {
		return Result{Type: ResultFatalError, State: state, Err: errors.New("parallel node has no branches")}
	}
	seen := make(map[string]struct{}, len(n.Branches))
	for _, branch := range n.Branches {
		if branch == nil || branch.ID() == "" {
			return Result{Type: ResultFatalError, State: state, Err: errors.New("parallel node contains a branch without an id")}
		}
		if _, exists := seen[branch.ID()]; exists {
			return Result{Type: ResultFatalError, State: state, Err: fmt.Errorf("duplicate branch %q", branch.ID())}
		}
		seen[branch.ID()] = struct{}{}
	}

	maxConcurrency := n.MaxConcurrency
	if maxConcurrency <= 0 || maxConcurrency > len(n.Branches) {
		maxConcurrency = len(n.Branches)
	}
	baseSnapshot, err := state.Clone()
	if err != nil {
		return Result{Type: ResultFatalError, State: state, Err: err}
	}
	type branchResult struct {
		id        string
		result    json.RawMessage
		err       error
		startedAt time.Time
		endedAt   time.Time
	}
	results := make(chan branchResult, len(n.Branches))
	semaphore := make(chan struct{}, maxConcurrency)

	for _, branch := range n.Branches {
		branch := branch
		startedAt := time.Now().UTC()
		state.PendingBranches[branchStateKey(n.NodeID, branch.ID())] = domain.BranchState{
			ID: branch.ID(), Status: domain.BranchPending, StartedAt: startedAt,
		}
		go func() {
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				results <- branchResult{id: branch.ID(), err: ctx.Err(), startedAt: startedAt, endedAt: time.Now().UTC()}
				return
			}
			snapshot, err := baseSnapshot.Clone()
			if err != nil {
				results <- branchResult{id: branch.ID(), err: err, startedAt: startedAt, endedAt: time.Now().UTC()}
				return
			}
			output, err := branch.Execute(ctx, snapshot)
			results <- branchResult{id: branch.ID(), result: output, err: err, startedAt: startedAt, endedAt: time.Now().UTC()}
		}()
	}

	remaining := len(n.Branches)
	for remaining > 0 {
		select {
		case result := <-results:
			remaining--
			status := domain.BranchSucceeded
			errorMessage := ""
			if result.err != nil {
				status = domain.BranchFailed
				errorMessage = result.err.Error()
				if errors.Is(result.err, context.Canceled) || errors.Is(result.err, context.DeadlineExceeded) {
					status = domain.BranchCancelled
				}
			}
			completedAt := result.endedAt
			state.PendingBranches[branchStateKey(n.NodeID, result.id)] = domain.BranchState{
				ID: result.id, Status: status, Result: result.result, Error: errorMessage,
				StartedAt: result.startedAt, CompletedAt: &completedAt,
			}
		case <-ctx.Done():
			now := time.Now().UTC()
			for _, branch := range n.Branches {
				key := branchStateKey(n.NodeID, branch.ID())
				branchState := state.PendingBranches[key]
				if !branchState.Status.Terminal() {
					branchState.Status = domain.BranchCancelled
					branchState.Error = ctx.Err().Error()
					branchState.CompletedAt = &now
					state.PendingBranches[key] = branchState
				}
			}
			state.AppendEvent(domain.EventParallelCompleted, n.NodeID, "Parallel branches stopped by context cancellation")
			return Result{Type: ResultCompleted, State: state}
		}
	}
	state.AppendEvent(domain.EventParallelCompleted, n.NodeID, "Parallel branches completed")
	return Result{Type: ResultCompleted, State: state}
}

type JoinNode struct {
	NodeID          string
	SourceNodeID    string
	BranchIDs       []string
	ExecutionPolicy NodePolicy
	Decide          func(*domain.RunState, map[string]domain.BranchState) Result
}

func (n JoinNode) ID() string { return n.NodeID }

func (n JoinNode) Policy() NodePolicy { return n.ExecutionPolicy.normalized() }

func (n JoinNode) IdempotencyKey(*domain.RunState) string { return n.NodeID }

func (n JoinNode) Execute(_ context.Context, state *domain.RunState) Result {
	branches := make(map[string]domain.BranchState, len(n.BranchIDs))
	for _, branchID := range n.BranchIDs {
		branch, exists := state.PendingBranches[branchStateKey(n.SourceNodeID, branchID)]
		if !exists {
			return Result{Type: ResultFatalError, State: state, Err: fmt.Errorf("join branch %q is missing", branchID)}
		}
		if !branch.Status.Terminal() {
			return Result{Type: ResultFatalError, State: state, Err: fmt.Errorf("join branch %q is not terminal", branchID)}
		}
		branches[branchID] = branch
	}
	if n.Decide != nil {
		return n.Decide(state, branches)
	}
	return Result{Type: ResultCompleted, State: state}
}

func branchStateKey(nodeID, branchID string) string {
	return nodeID + ":" + branchID
}
