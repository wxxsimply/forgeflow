package checkpoint

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"forgeflow/internal/domain"
)

type PostgresOptions struct {
	PublishWakeups bool
}

type PostgresStore struct {
	db             *sql.DB
	publishWakeups bool
}

func NewPostgresStore(db *sql.DB) *PostgresStore {
	return NewPostgresStoreWithOptions(db, PostgresOptions{PublishWakeups: true})
}

func NewPostgresStoreWithOptions(db *sql.DB, options PostgresOptions) *PostgresStore {
	return &PostgresStore{db: db, publishWakeups: options.PublishWakeups}
}

func (s *PostgresStore) Save(ctx context.Context, state *domain.RunState, expectedVersion int64) error {
	if s == nil || s.db == nil || state == nil {
		return fmt.Errorf("PostgreSQL checkpoint store is not configured")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if state.Version != expectedVersion {
		return ErrConflict
	}

	snapshot := *state
	snapshot.Version = expectedVersion + 1
	stateJSON, err := json.Marshal(&snapshot)
	if err != nil {
		return fmt.Errorf("encode PostgreSQL checkpoint: %w", err)
	}
	budgetJSON, err := json.Marshal(snapshot.Budget)
	if err != nil {
		return fmt.Errorf("encode run budget: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("begin checkpoint transaction: %w", err)
	}
	defer tx.Rollback()

	if expectedVersion == 0 {
		result, err := tx.ExecContext(ctx, `INSERT INTO runs(
            id, owner_id, repository_id, status, version, current_node_id, task, repository_path, base_revision,
            budget, state_json, created_at, updated_at
        ) VALUES ($1,NULLIF($2,'')::uuid,NULLIF($3,'')::uuid,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
        ON CONFLICT (id) DO NOTHING`,
			snapshot.RunID, snapshot.OwnerID, snapshot.RepositoryID, snapshot.Status, snapshot.Version, snapshot.CurrentNodeID,
			snapshot.Task, snapshot.RepositoryPath, snapshot.BaseRevision,
			budgetJSON, stateJSON, snapshot.CreatedAt, snapshot.UpdatedAt)
		if err != nil {
			return fmt.Errorf("insert run projection: %w", err)
		}
		if rows, _ := result.RowsAffected(); rows != 1 {
			return ErrConflict
		}
	} else {
		result, err := tx.ExecContext(ctx, `UPDATE runs SET
			owner_id=NULLIF($1,'')::uuid, repository_id=NULLIF($2,'')::uuid,
            status=$3, version=$4, current_node_id=$5, task=$6, repository_path=$7,
            base_revision=$8, budget=$9, state_json=$10, updated_at=$11
            WHERE id=$12 AND version=$13`,
			snapshot.OwnerID, snapshot.RepositoryID, snapshot.Status, snapshot.Version, snapshot.CurrentNodeID, snapshot.Task,
			snapshot.RepositoryPath, snapshot.BaseRevision, budgetJSON, stateJSON,
			snapshot.UpdatedAt, snapshot.RunID, expectedVersion)
		if err != nil {
			return fmt.Errorf("update run projection: %w", err)
		}
		if rows, _ := result.RowsAffected(); rows != 1 {
			return ErrConflict
		}
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO checkpoints(run_id, version, node_id, state_json)
        VALUES ($1,$2,$3,$4)`, snapshot.RunID, snapshot.Version, snapshot.CurrentNodeID, stateJSON); err != nil {
		return fmt.Errorf("insert checkpoint: %w", err)
	}
	if err := appendEvents(ctx, tx, &snapshot); err != nil {
		return err
	}
	if err := syncApproval(ctx, tx, &snapshot); err != nil {
		return err
	}
	if err := syncNodeExecutions(ctx, tx, &snapshot); err != nil {
		return err
	}
	if err := syncModelCalls(ctx, tx, &snapshot); err != nil {
		return err
	}
	if err := syncToolCalls(ctx, tx, &snapshot); err != nil {
		return err
	}
	if s.publishWakeups && shouldPublishWakeup(&snapshot) {
		payload, _ := json.Marshal(map[string]any{"runId": snapshot.RunID, "version": snapshot.Version})
		if _, err := tx.ExecContext(ctx, `INSERT INTO outbox(id, topic, dedupe_key, payload)
            VALUES ($1,'run.wakeup',$2,$3) ON CONFLICT (dedupe_key) DO NOTHING`,
			domain.NewID(), fmt.Sprintf("run:%s:version:%d", snapshot.RunID, snapshot.Version), payload); err != nil {
			return fmt.Errorf("insert checkpoint outbox: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit checkpoint transaction: %w", err)
	}
	state.Version = snapshot.Version
	return nil
}

func (s *PostgresStore) Load(ctx context.Context, runID string) (*domain.RunState, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("PostgreSQL checkpoint store is not configured")
	}
	if !runIDPattern.MatchString(runID) {
		return nil, fmt.Errorf("invalid run id")
	}
	var encoded []byte
	if err := s.db.QueryRowContext(ctx, `SELECT state_json FROM runs WHERE id=$1`, runID).Scan(&encoded); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("load PostgreSQL checkpoint: %w", err)
	}
	var state domain.RunState
	if err := json.Unmarshal(encoded, &state); err != nil {
		return nil, fmt.Errorf("decode PostgreSQL checkpoint: %w", err)
	}
	normalizeRunState(&state)
	return &state, nil
}

func appendEvents(ctx context.Context, tx *sql.Tx, state *domain.RunState) error {
	rows, err := tx.QueryContext(ctx, `SELECT sequence, event_id FROM run_events WHERE run_id=$1 ORDER BY sequence`, state.RunID)
	if err != nil {
		return fmt.Errorf("read persisted event prefix: %w", err)
	}
	persisted := 0
	for rows.Next() {
		var sequence int
		var eventID string
		if err := rows.Scan(&sequence, &eventID); err != nil {
			rows.Close()
			return fmt.Errorf("scan persisted event prefix: %w", err)
		}
		if sequence != persisted+1 || persisted >= len(state.Events) || state.Events[persisted].EventID != eventID {
			rows.Close()
			return ErrConflict
		}
		persisted++
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for index := persisted; index < len(state.Events); index++ {
		event := state.Events[index]
		payload, _ := json.Marshal(event)
		if _, err := tx.ExecContext(ctx, `INSERT INTO run_events(
            run_id, sequence, event_id, trace_id, type, node_id, message, payload, created_at
        ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			state.RunID, index+1, event.EventID, event.TraceID, event.Type,
			event.NodeID, event.Message, payload, event.CreatedAt); err != nil {
			return fmt.Errorf("append run event: %w", err)
		}
	}
	return nil
}

func syncApproval(ctx context.Context, tx *sql.Tx, state *domain.RunState) error {
	approval := state.PendingApproval
	if approval == nil {
		return nil
	}
	payload, _ := json.Marshal(approval)
	_, err := tx.ExecContext(ctx, `INSERT INTO approvals(
        id, run_id, type, risk, status, request_json, decision_comment, requested_at, resolved_at
    ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
    ON CONFLICT (id) DO UPDATE SET status=EXCLUDED.status, request_json=EXCLUDED.request_json,
        decision_comment=EXCLUDED.decision_comment, resolved_at=EXCLUDED.resolved_at, updated_at=now()`,
		approval.ApprovalID, state.RunID, approval.ActionType, approval.Risk, approval.Status,
		payload, approval.Comment, approval.RequestedAt, approval.ResolvedAt)
	if err != nil {
		return fmt.Errorf("sync approval: %w", err)
	}
	return nil
}

func syncNodeExecutions(ctx context.Context, tx *sql.Tx, state *domain.RunState) error {
	for _, execution := range state.NodeExecutions {
		payload, _ := json.Marshal(execution)
		if _, err := tx.ExecContext(ctx, `INSERT INTO node_executions(
            run_id,node_id,iteration,idempotency_key,status,attempts,execution_json
        ) VALUES ($1,$2,$3,$4,$5,$6,$7)
        ON CONFLICT (run_id,node_id,iteration,idempotency_key) DO UPDATE SET
            status=EXCLUDED.status, attempts=EXCLUDED.attempts,
            execution_json=EXCLUDED.execution_json, updated_at=now()`,
			state.RunID, execution.NodeID, execution.Iteration, execution.IdempotencyKey,
			execution.Status, execution.Attempts, payload); err != nil {
			return fmt.Errorf("sync node execution: %w", err)
		}
	}
	return nil
}

func syncModelCalls(ctx context.Context, tx *sql.Tx, state *domain.RunState) error {
	for index, call := range state.ModelInvocations {
		payload, _ := json.Marshal(call)
		if _, err := tx.ExecContext(ctx, `INSERT INTO model_calls(
            run_id,sequence,node_id,agent,model,status,call_json,created_at
        ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT (run_id,sequence) DO NOTHING`,
			state.RunID, index+1, call.NodeID, call.Agent, call.Model, call.Status, payload, call.CreatedAt); err != nil {
			return fmt.Errorf("sync model call: %w", err)
		}
	}
	return nil
}

func syncToolCalls(ctx context.Context, tx *sql.Tx, state *domain.RunState) error {
	for _, call := range state.ToolCallAudits {
		payload, _ := json.Marshal(call)
		if _, err := tx.ExecContext(ctx, `INSERT INTO tool_calls(
            call_id,run_id,node_id,tool_name,status,call_json,created_at
        ) VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (call_id) DO NOTHING`,
			call.CallID, state.RunID, call.NodeID, call.ToolName, call.Status, payload, call.CreatedAt); err != nil {
			return fmt.Errorf("sync tool call: %w", err)
		}
	}
	return nil
}

func shouldPublishWakeup(state *domain.RunState) bool {
	if state.Status.Terminal() {
		return false
	}
	if state.Cancellation.Requested() || state.Pause.Requested() {
		return true
	}
	if state.Status == domain.StatusWaitingPlanApproval || state.Status == domain.StatusWaitingActionApproval {
		return state.PendingApproval != nil && state.PendingApproval.Status != domain.ApprovalPending
	}
	if state.Status == domain.StatusPaused {
		return false
	}
	return true
}

func normalizeRunState(state *domain.RunState) {
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

var _ Store = (*PostgresStore)(nil)
