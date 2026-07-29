package reconciliation

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type PostgresRepository struct {
	db *sql.DB
}

func NewPostgresRepository(db *sql.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) CreateRun(ctx context.Context, run Run) (Run, error) {
	if run.ID == uuid.Nil {
		run.ID = uuid.New()
	}
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now().UTC()
	}
	if run.Status == "" {
		run.Status = RunStatusRunning
	}
	scope, err := json.Marshal(run.Scope)
	if err != nil {
		return Run{}, fmt.Errorf("encode run scope: %w", err)
	}

	const stmt = `
		INSERT INTO reconciliation_runs (
			id, level, comparator, status, scope, started_at,
			checkpoint_key, checkpoint_from, checkpoint_to
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	if _, err := r.db.ExecContext(ctx, stmt,
		run.ID,
		string(run.Level),
		run.Comparator,
		string(run.Status),
		scope,
		run.StartedAt.UTC(),
		nullText(run.CheckpointKey),
		nullText(run.CheckpointFrom),
		nullText(run.CheckpointTo),
	); err != nil {
		return Run{}, fmt.Errorf("create reconciliation run: %w", err)
	}
	return run, nil
}

func (r *PostgresRepository) AddFinding(ctx context.Context, finding Finding) (Finding, error) {
	if finding.ID == uuid.Nil {
		finding.ID = uuid.New()
	}
	if finding.ObservedAt.IsZero() {
		finding.ObservedAt = time.Now().UTC()
	}
	if finding.ResolutionState == "" {
		finding.ResolutionState = ResolutionOpen
	}
	details, err := json.Marshal(finding.Details)
	if err != nil {
		return Finding{}, fmt.Errorf("encode finding details: %w", err)
	}

	const stmt = `
		INSERT INTO reconciliation_findings (
			id, run_id, level, type, severity, entity_type, entity_id,
			recorded_value, on_chain_value, difference, tolerance,
			observed_at, details, resolution_state
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		ON CONFLICT (run_id, level, type, entity_type, entity_id)
		DO UPDATE SET
			severity = EXCLUDED.severity,
			recorded_value = EXCLUDED.recorded_value,
			on_chain_value = EXCLUDED.on_chain_value,
			difference = EXCLUDED.difference,
			tolerance = EXCLUDED.tolerance,
			observed_at = EXCLUDED.observed_at,
			details = EXCLUDED.details,
			updated_at = NOW()
	`
	if _, err := r.db.ExecContext(ctx, stmt,
		finding.ID,
		finding.RunID,
		string(finding.Level),
		string(finding.Type),
		string(finding.Severity),
		finding.EntityType,
		finding.EntityID,
		decimalText(finding.RecordedValue),
		decimalText(finding.OnChainValue),
		decimalText(finding.Difference),
		finding.Tolerance.String(),
		finding.ObservedAt.UTC(),
		details,
		string(finding.ResolutionState),
	); err != nil {
		return Finding{}, fmt.Errorf("add reconciliation finding: %w", err)
	}
	return finding, nil
}

func (r *PostgresRepository) CompleteRun(ctx context.Context, runID uuid.UUID, stats Stats) error {
	const stmt = `
		UPDATE reconciliation_runs
		SET status = $2,
			completed_at = NOW(),
			checked_count = $3,
			finding_count = $4,
			critical_count = $5,
			updated_at = NOW()
		WHERE id = $1
	`
	_, err := r.db.ExecContext(ctx, stmt, runID, string(RunStatusCompleted), stats.Checked, stats.Findings, stats.Critical)
	return err
}

func (r *PostgresRepository) FailRun(ctx context.Context, runID uuid.UUID, errText string) error {
	const stmt = `
		UPDATE reconciliation_runs
		SET status = $2, completed_at = NOW(), error = $3, updated_at = NOW()
		WHERE id = $1
	`
	_, err := r.db.ExecContext(ctx, stmt, runID, string(RunStatusFailed), errText)
	return err
}

func (r *PostgresRepository) GetCheckpoint(ctx context.Context, key string) (string, bool, error) {
	const stmt = `SELECT value FROM reconciliation_checkpoints WHERE key = $1`
	var value string
	if err := r.db.QueryRowContext(ctx, stmt, key).Scan(&value); err != nil {
		if err == sql.ErrNoRows {
			return "", false, nil
		}
		return "", false, err
	}
	return value, true, nil
}

func (r *PostgresRepository) SetCheckpoint(ctx context.Context, key, value string) error {
	const stmt = `
		INSERT INTO reconciliation_checkpoints (key, value, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (key)
		DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()
	`
	_, err := r.db.ExecContext(ctx, stmt, key, value)
	return err
}

func (r *PostgresRepository) RecordCorrection(ctx context.Context, findingID uuid.UUID, reason string) error {
	const stmt = `
		UPDATE reconciliation_findings
		SET resolution_state = $2,
			resolution_note = $3,
			resolved_at = NOW(),
			updated_at = NOW()
		WHERE id = $1
	`
	_, err := r.db.ExecContext(ctx, stmt, findingID, string(ResolutionResolved), reason)
	return err
}

func nullText(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func decimalText(value *decimal.Decimal) any {
	if value == nil {
		return nil
	}
	return value.String()
}
