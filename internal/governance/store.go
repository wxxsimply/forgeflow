package governance

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"forgeflow/internal/checkpoint"
	fulleval "forgeflow/internal/eval"
)

type EvalRun struct {
	ID             string                    `json:"id"`
	CreatedBy      string                    `json:"createdBy"`
	Dataset        string                    `json:"dataset"`
	DatasetVersion string                    `json:"datasetVersion"`
	Status         string                    `json:"status"`
	Report         fulleval.ComparisonReport `json:"report"`
	CreatedAt      time.Time                 `json:"createdAt"`
}

type PromptRelease struct {
	ID           string    `json:"id"`
	Agent        string    `json:"agent"`
	Version      string    `json:"version"`
	PromptSHA256 string    `json:"promptSha256"`
	Model        string    `json:"model"`
	EvalRunID    string    `json:"evalRunId"`
	PromotedBy   string    `json:"promotedBy"`
	RollbackOf   string    `json:"rollbackOf,omitempty"`
	Comment      string    `json:"comment"`
	Active       bool      `json:"active"`
	CreatedAt    time.Time `json:"createdAt"`
}

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) CreateEvalRun(ctx context.Context, run EvalRun) error {
	encoded, err := json.Marshal(run.Report)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO eval_runs(id,created_by,dataset,dataset_version,status,report_json,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, run.ID, run.CreatedBy, run.Dataset, run.DatasetVersion, run.Status, encoded, run.CreatedAt)
	return err
}

func (s *Store) ListEvalRuns(ctx context.Context, limit int) ([]EvalRun, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,created_by,dataset,dataset_version,status,report_json,created_at FROM eval_runs ORDER BY created_at DESC,id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EvalRun{}
	for rows.Next() {
		run, err := scanEvalRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

func (s *Store) GetEvalRun(ctx context.Context, id string) (EvalRun, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,created_by,dataset,dataset_version,status,report_json,created_at FROM eval_runs WHERE id=$1`, id)
	run, err := scanEvalRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return EvalRun{}, checkpoint.ErrNotFound
	}
	return run, err
}

type scanner interface{ Scan(...any) error }

func scanEvalRun(row scanner) (EvalRun, error) {
	var run EvalRun
	var encoded []byte
	if err := row.Scan(&run.ID, &run.CreatedBy, &run.Dataset, &run.DatasetVersion, &run.Status, &encoded, &run.CreatedAt); err != nil {
		return run, err
	}
	if err := json.Unmarshal(encoded, &run.Report); err != nil {
		return run, fmt.Errorf("decode stored eval report: %w", err)
	}
	return run, nil
}

func ForgeFlowReport(run EvalRun) (fulleval.Report, error) {
	for _, report := range run.Report.Reports {
		if report.Configuration.Mode == fulleval.ModeForgeFlow {
			return report, nil
		}
	}
	return fulleval.Report{}, fmt.Errorf("eval report has no forgeflow baseline")
}

func InitialPromotionAllowed(report fulleval.Report) error {
	if report.Total < 30 || report.Passed != report.Total || len(report.Unavailable) > 0 || report.Metrics.AverageCostUSD == nil || report.Metrics.P95LatencyMS == nil {
		return fmt.Errorf("initial promotion requires 30 passing cases and complete cost/latency measurements")
	}
	return nil
}

func (s *Store) ActiveRelease(ctx context.Context, agent string) (PromptRelease, error) {
	release, err := scanRelease(s.db.QueryRowContext(ctx, `SELECT id,agent,version,prompt_sha256,model,eval_run_id,promoted_by,COALESCE(rollback_of::text,''),comment,active,created_at FROM prompt_releases WHERE agent=$1 AND active`, agent))
	if errors.Is(err, sql.ErrNoRows) {
		return PromptRelease{}, checkpoint.ErrNotFound
	}
	return release, err
}

func (s *Store) ListReleases(ctx context.Context) ([]PromptRelease, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,agent,version,prompt_sha256,model,eval_run_id,promoted_by,COALESCE(rollback_of::text,''),comment,active,created_at FROM prompt_releases ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PromptRelease{}
	for rows.Next() {
		release, err := scanRelease(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, release)
	}
	return out, rows.Err()
}

func (s *Store) GetRelease(ctx context.Context, id string) (PromptRelease, error) {
	release, err := scanRelease(s.db.QueryRowContext(ctx, `SELECT id,agent,version,prompt_sha256,model,eval_run_id,promoted_by,COALESCE(rollback_of::text,''),comment,active,created_at FROM prompt_releases WHERE id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return PromptRelease{}, checkpoint.ErrNotFound
	}
	return release, err
}

func scanRelease(row scanner) (PromptRelease, error) {
	var release PromptRelease
	err := row.Scan(&release.ID, &release.Agent, &release.Version, &release.PromptSHA256, &release.Model, &release.EvalRunID, &release.PromotedBy, &release.RollbackOf, &release.Comment, &release.Active, &release.CreatedAt)
	return release, err
}

func (s *Store) Promote(ctx context.Context, release PromptRelease) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE prompt_releases SET active=false WHERE agent=$1 AND active`, release.Agent); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO prompt_releases(id,agent,version,prompt_sha256,model,eval_run_id,promoted_by,rollback_of,comment,active,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,NULLIF($8,'')::uuid,$9,true,$10)`, release.ID, release.Agent, release.Version, release.PromptSHA256, release.Model, release.EvalRunID, release.PromotedBy, release.RollbackOf, release.Comment, release.CreatedAt)
	if err != nil {
		return err
	}
	return tx.Commit()
}
