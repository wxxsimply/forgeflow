package graph

import (
	"context"
	"errors"
	"fmt"
	"time"

	"forgeflow/internal/apperror"
	"forgeflow/internal/checkpoint"
	"forgeflow/internal/domain"
	"forgeflow/internal/lifecycle"
	"forgeflow/internal/observability"
)

type Runtime struct {
	definition      Definition
	store           checkpoint.Store
	nodes           map[string]Node
	resumeValidator lifecycle.Validator
}

func NewRuntime(definition Definition, store checkpoint.Store) (*Runtime, error) {
	nodes := make(map[string]Node, len(definition.Nodes))
	for _, node := range definition.Nodes {
		if node == nil || node.ID() == "" {
			return nil, errors.New("graph contains a node without an id")
		}
		if _, exists := nodes[node.ID()]; exists {
			return nil, fmt.Errorf("duplicate node %q", node.ID())
		}
		nodes[node.ID()] = node
	}
	if _, exists := nodes[definition.EntryNodeID]; !exists {
		return nil, fmt.Errorf("entry node %q not found", definition.EntryNodeID)
	}
	for _, edge := range definition.Edges {
		if _, exists := nodes[edge.From]; !exists {
			return nil, fmt.Errorf("edge source node %q not found", edge.From)
		}
		if _, exists := nodes[edge.To]; !exists {
			return nil, fmt.Errorf("edge target node %q not found", edge.To)
		}
	}
	validator, err := lifecycle.NewValidator(lifecycle.Options{})
	if err != nil {
		return nil, err
	}
	return &Runtime{definition: definition, store: store, nodes: nodes, resumeValidator: validator}, nil
}

func (r *Runtime) Execute(ctx context.Context, state *domain.RunState) (resultState *domain.RunState, resultErr error) {
	ctx, runSpan := observability.StartRunSpan(ctx, state.RunID, state.TraceID)
	wasTerminal := state.Status.Terminal()
	defer func() {
		if !wasTerminal && state.Status.Terminal() {
			observability.DefaultMetrics().Run(string(state.Status), state.Status == domain.StatusCompleted && state.RepairCount == 0, hasEvent(state, domain.EventRunResumed), hasEvent(state, domain.EventBudgetExhausted), state.RepairCount)
		}
		observability.EndSpan(runSpan, resultErr, string(state.Status))
	}()
	r.ensureState(state)
	for !state.Status.Terminal() {
		if ctx.Err() != nil || state.Cancellation.Requested() {
			return r.cancel(ctx, state)
		}
		if state.Status == domain.StatusPaused {
			return state, nil
		}
		if state.Pause.Requested() {
			return r.pause(ctx, state)
		}
		if allowed, reason := state.Budget.NodeCallAllowed(state.CreatedAt, time.Now().UTC(), state.Iteration); !allowed {
			return r.exhaustBudget(ctx, state, reason)
		}

		node, exists := r.nodes[state.CurrentNodeID]
		if !exists {
			return state, apperror.New(apperror.CodeValidation, fmt.Sprintf("node %q not found", state.CurrentNodeID))
		}
		executionKey := r.executionKey(node, state)
		if execution, found := state.NodeExecutions[executionKey]; found {
			if execution.Status == domain.NodeExecutionSucceeded {
				state.MarkNodeCompleted(node.ID())
				state.AppendEvent(domain.EventNodeReused, node.ID(), "Reused completed node execution")
				if err := r.advanceAndSave(context.WithoutCancel(ctx), node.ID(), state); err != nil {
					return state, err
				}
				continue
			}
			if execution.Status == domain.NodeExecutionRunning && !node.Policy().ReplaySafe {
				state.Status = domain.StatusFailed
				state.Error = &domain.RunError{
					Code:    "indeterminate_node_execution",
					Message: "node has an indeterminate prior execution and is not safe to replay",
				}
				state.AppendEvent(domain.EventNodeFailed, node.ID(), state.Error.Message)
				return state, r.save(context.WithoutCancel(ctx), state)
			}
		}

		result, updatedState, execution, terminal := r.executeNode(ctx, node, executionKey, state)
		state = updatedState
		if terminal {
			return state, nil
		}

		switch result.Type {
		case ResultInterrupted:
			if result.Approval == nil {
				return state, apperror.New(apperror.CodeInternal, "interrupted node did not provide an approval request")
			}
			now := time.Now().UTC()
			execution.Status = domain.NodeExecutionInterrupted
			execution.CompletedAt = &now
			state.NodeExecutions[executionKey] = execution
			state.PendingApproval = result.Approval
			state.AppendEvent(domain.EventNodeInterrupted, node.ID(), "Node "+node.ID()+" is waiting for approval")
			return state, r.save(context.WithoutCancel(ctx), state)
		case ResultRetryableError, ResultFatalError:
			if result.Err == nil {
				result.Err = errors.New("node failed without an error")
			}
			now := time.Now().UTC()
			execution.Status = domain.NodeExecutionFailed
			execution.CompletedAt = &now
			execution.LastError = result.Err.Error()
			state.NodeExecutions[executionKey] = execution
			state.Status = domain.StatusFailed
			errorCode := string(result.Type)
			if applicationCode := apperror.CodeOf(result.Err); applicationCode != apperror.CodeInternal {
				errorCode = string(applicationCode)
			}
			state.Error = &domain.RunError{Code: errorCode, Message: result.Err.Error()}
			state.AppendEvent(domain.EventNodeFailed, node.ID(), result.Err.Error())
			return state, r.save(context.WithoutCancel(ctx), state)
		case ResultCompleted:
			now := time.Now().UTC()
			execution.Status = domain.NodeExecutionSucceeded
			execution.CompletedAt = &now
			execution.LastError = ""
			state.NodeExecutions[executionKey] = execution
			state.MarkNodeCompleted(node.ID())
			state.AppendEvent(domain.EventNodeCompleted, node.ID(), "Node "+node.ID()+" completed")
			if err := r.advanceAndSave(context.WithoutCancel(ctx), node.ID(), state); err != nil {
				return state, err
			}
		default:
			return state, apperror.New(apperror.CodeInternal, fmt.Sprintf("node %q returned invalid result %q", node.ID(), result.Type))
		}
	}
	return state, nil
}

func hasEvent(state *domain.RunState, eventType domain.EventType) bool {
	for _, event := range state.Events {
		if event.Type == eventType {
			return true
		}
	}
	return false
}

func (r *Runtime) pause(ctx context.Context, state *domain.RunState) (*domain.RunState, error) {
	now := time.Now().UTC()
	state.Status = domain.StatusPaused
	state.Pause.PausedAt = &now
	state.ResumeGuard = ptrResumeGuard(r.resumeValidator.Capture(state))
	state.AppendEvent(domain.EventRunPaused, state.CurrentNodeID, "Run paused at a safe checkpoint")
	return state, r.save(context.WithoutCancel(ctx), state)
}

func ptrResumeGuard(guard domain.ResumeGuard) *domain.ResumeGuard { return &guard }

func (r *Runtime) executeNode(
	ctx context.Context,
	node Node,
	executionKey string,
	state *domain.RunState,
) (Result, *domain.RunState, domain.NodeExecution, bool) {
	policy := node.Policy().normalized()
	execution := state.NodeExecutions[executionKey]
	if execution.Key == "" {
		execution = domain.NodeExecution{
			Key: executionKey, NodeID: node.ID(), Iteration: state.Iteration,
			IdempotencyKey: node.IdempotencyKey(state), StartedAt: time.Now().UTC(),
		}
	} else if execution.Status == domain.NodeExecutionInterrupted {
		execution.Attempts = 0
		execution.CompletedAt = nil
		execution.LastError = ""
	}

	for execution.Attempts < policy.MaxAttempts {
		if ctx.Err() != nil || state.Cancellation.Requested() {
			cancelled, err := r.cancel(ctx, state)
			if err != nil {
				cancelled.Error = &domain.RunError{Code: string(apperror.CodeInternal), Message: err.Error()}
			}
			return Result{}, cancelled, execution, true
		}
		if allowed, reason := state.Budget.NodeCallAllowed(state.CreatedAt, time.Now().UTC(), state.Iteration); !allowed {
			exhausted, err := r.exhaustBudget(ctx, state, reason)
			if err != nil {
				exhausted.Error = &domain.RunError{Code: string(apperror.CodeInternal), Message: err.Error()}
			}
			return Result{}, exhausted, execution, true
		}

		execution.Attempts++
		execution.Status = domain.NodeExecutionRunning
		state.Budget.NodeCalls++
		state.NodeExecutions[executionKey] = execution
		state.AppendEvent(domain.EventNodeStarted, node.ID(), fmt.Sprintf("Node %s attempt %d started", node.ID(), execution.Attempts))
		if err := r.save(context.WithoutCancel(ctx), state); err != nil {
			return Result{Type: ResultFatalError, State: state, Err: err}, state, execution, false
		}

		attemptContext, cancel := context.WithTimeout(ctx, policy.Timeout)
		attemptContext, nodeSpan := observability.StartNodeSpan(attemptContext, state.RunID, state.TraceID, node.ID(), execution.Attempts)
		nodeStarted := time.Now()
		result := node.Execute(attemptContext, state)
		attemptErr := attemptContext.Err()
		cancel()
		if result.State == nil {
			result.State = state
		}
		state = result.State
		r.ensureState(state)

		if errors.Is(attemptErr, context.DeadlineExceeded) {
			result = Result{Type: ResultRetryableError, State: state, Err: fmt.Errorf("node %s timed out after %s", node.ID(), policy.Timeout)}
		}
		observability.DefaultMetrics().Node(node.ID(), nodeMetricStatus(result.Type), time.Since(nodeStarted))
		observability.EndSpan(nodeSpan, result.Err, string(result.Type))

		if ctx.Err() != nil || state.Cancellation.Requested() {
			cancelled, err := r.cancel(ctx, state)
			if err != nil {
				cancelled.Error = &domain.RunError{Code: string(apperror.CodeInternal), Message: err.Error()}
			}
			return Result{}, cancelled, execution, true
		}
		if result.Type != ResultRetryableError || execution.Attempts >= policy.MaxAttempts || !retryAllowed(policy, result.Err) {
			return result, state, execution, false
		}

		if result.Err == nil {
			result.Err = errors.New("retryable node failure")
		}
		execution.LastError = result.Err.Error()
		state.NodeExecutions[executionKey] = execution
		state.AppendEvent(domain.EventNodeRetrying, node.ID(), fmt.Sprintf("Node %s will retry after attempt %d", node.ID(), execution.Attempts))
		if err := r.save(context.WithoutCancel(ctx), state); err != nil {
			return Result{Type: ResultFatalError, State: state, Err: err}, state, execution, false
		}
		if !waitForRetry(ctx, policy.Backoff) {
			cancelled, _ := r.cancel(ctx, state)
			return Result{}, cancelled, execution, true
		}
	}
	return Result{Type: ResultFatalError, State: state, Err: errors.New("node attempt limit exhausted")}, state, execution, false
}

func nodeMetricStatus(result ResultType) string {
	switch result {
	case ResultCompleted:
		return "completed"
	case ResultInterrupted:
		return "interrupted"
	case ResultRetryableError, ResultFatalError:
		return "failed"
	default:
		return "cancelled"
	}
}

func (r *Runtime) advanceAndSave(ctx context.Context, nodeID string, state *domain.RunState) error {
	next, found := r.nextNode(nodeID, state)
	if !found {
		if !state.Status.Terminal() {
			state.Status = domain.StatusCompleted
		}
		return r.save(ctx, state)
	}
	state.CurrentNodeID = next
	return r.save(ctx, state)
}

func (r *Runtime) nextNode(from string, state *domain.RunState) (string, bool) {
	for _, edge := range r.definition.Edges {
		if edge.From == from && (edge.When == nil || edge.When(state)) {
			return edge.To, true
		}
	}
	return "", false
}

func (r *Runtime) executionKey(node Node, state *domain.RunState) string {
	return fmt.Sprintf("%s:%d:%s", node.ID(), state.Iteration, node.IdempotencyKey(state))
}

func (r *Runtime) ensureState(state *domain.RunState) {
	if state.NodeExecutions == nil {
		state.NodeExecutions = map[string]domain.NodeExecution{}
	}
	if state.PendingBranches == nil {
		state.PendingBranches = map[string]domain.BranchState{}
	}
	if state.ToolCallAudits == nil {
		state.ToolCallAudits = []domain.ToolCallAudit{}
	}
	if state.AssessmentErrors == nil {
		state.AssessmentErrors = map[string]string{}
	}
	if state.JudgeDecisions == nil {
		state.JudgeDecisions = []domain.JudgeDecision{}
	}
}

func (r *Runtime) cancel(ctx context.Context, state *domain.RunState) (*domain.RunState, error) {
	state.Status = domain.StatusCancelled
	state.AppendEvent(domain.EventRunCancelled, state.CurrentNodeID, "Run cancelled")
	return state, r.save(context.WithoutCancel(ctx), state)
}

func (r *Runtime) exhaustBudget(ctx context.Context, state *domain.RunState, reason string) (*domain.RunState, error) {
	state.Status = domain.StatusFailed
	state.Error = &domain.RunError{Code: string(apperror.CodeBudget), Message: reason}
	state.AppendEvent(domain.EventBudgetExhausted, state.CurrentNodeID, reason)
	return state, r.save(context.WithoutCancel(ctx), state)
}

func (r *Runtime) save(ctx context.Context, state *domain.RunState) error {
	expectedVersion := state.Version
	if err := r.store.Save(ctx, state, expectedVersion); err != nil {
		if errors.Is(err, checkpoint.ErrConflict) {
			return apperror.Wrap(err, apperror.CodeConflict, "graph.checkpoint.save", "run state changed concurrently")
		}
		return apperror.Wrap(err, apperror.CodeInternal, "graph.checkpoint.save", "could not save graph checkpoint")
	}
	return nil
}

func retryAllowed(policy NodePolicy, err error) bool {
	if policy.Retryable == nil {
		return true
	}
	return policy.Retryable(err)
}

func waitForRetry(ctx context.Context, duration time.Duration) bool {
	if duration <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
