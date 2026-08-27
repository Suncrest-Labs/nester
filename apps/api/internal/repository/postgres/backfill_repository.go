package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/backfill"
)

// BackfillRepository is the Postgres-backed backfill.Repository (#840).
type BackfillRepository struct {
	db *sql.DB
}

func NewBackfillRepository(db *sql.DB) *BackfillRepository {
	return &BackfillRepository{db: db}
}

func (r *BackfillRepository) Create(ctx context.Context, run *backfill.Run) error {
	if run.ID == uuid.Nil {
		run.ID = uuid.New()
	}
	if run.Status == "" {
		run.Status = backfill.StatusRunning
	}
	if run.Mode == "" {
		run.Mode = backfill.ModeBackfill
	}
	return r.db.QueryRowContext(ctx, `
		INSERT INTO backfill_runs (id, from_ledger, to_ledger, contract_ids, mode, dry_run, status, initiated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING created_at, updated_at
	`,
		run.ID, run.FromLedger, run.ToLedger, pq.Array(run.ContractIDs), string(run.Mode), run.DryRun, string(run.Status), run.InitiatedBy,
	).Scan(&run.CreatedAt, &run.UpdatedAt)
}

func (r *BackfillRepository) GetByID(ctx context.Context, id uuid.UUID) (*backfill.Run, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, from_ledger, to_ledger, contract_ids, mode, dry_run, status,
		       last_ledger_done, events_processed, events_skipped_duplicate, last_error,
		       initiated_by, created_at, updated_at, completed_at
		FROM backfill_runs WHERE id = $1
	`, id)
	run, err := scanBackfillRun(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, backfill.ErrRunNotFound
		}
		return nil, err
	}
	return &run, nil
}

// UpdateProgress persists the resumability checkpoint. Called after each
// processed batch, not just at the end, so a crash mid-run resumes from the
// last committed batch rather than restarting the whole range (#840).
func (r *BackfillRepository) UpdateProgress(ctx context.Context, id uuid.UUID, lastLedgerDone uint64, eventsProcessed, eventsSkippedDuplicate int64) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE backfill_runs
		SET last_ledger_done = $2,
		    events_processed = $3,
		    events_skipped_duplicate = $4,
		    updated_at = NOW()
		WHERE id = $1
	`, id, lastLedgerDone, eventsProcessed, eventsSkippedDuplicate)
	return err
}

func (r *BackfillRepository) Complete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE backfill_runs SET status = 'completed', completed_at = NOW(), updated_at = NOW() WHERE id = $1
	`, id)
	return err
}

func (r *BackfillRepository) Fail(ctx context.Context, id uuid.UUID, errMsg string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE backfill_runs SET status = 'failed', last_error = $2, updated_at = NOW() WHERE id = $1
	`, id, errMsg)
	return err
}

func (r *BackfillRepository) List(ctx context.Context, limit int) ([]backfill.Run, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, from_ledger, to_ledger, contract_ids, mode, dry_run, status,
		       last_ledger_done, events_processed, events_skipped_duplicate, last_error,
		       initiated_by, created_at, updated_at, completed_at
		FROM backfill_runs
		ORDER BY created_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []backfill.Run
	for rows.Next() {
		run, err := scanBackfillRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

type backfillRunScanner interface {
	Scan(dest ...any) error
}

func scanBackfillRun(row backfillRunScanner) (backfill.Run, error) {
	var (
		idStr           string
		fromLedger      int64
		toLedger        int64
		contractIDs     pq.StringArray
		mode            string
		dryRun          bool
		status          string
		lastLedgerDone  sql.NullInt64
		eventsProcessed int64
		eventsSkipped   int64
		lastError       sql.NullString
		initiatedBy     string
		createdAt       time.Time
		updatedAt       time.Time
		completedAt     sql.NullTime
	)
	if err := row.Scan(
		&idStr, &fromLedger, &toLedger, &contractIDs, &mode, &dryRun, &status,
		&lastLedgerDone, &eventsProcessed, &eventsSkipped, &lastError,
		&initiatedBy, &createdAt, &updatedAt, &completedAt,
	); err != nil {
		return backfill.Run{}, err
	}

	run := backfill.Run{
		ID: uuid.MustParse(idStr),
		// Ledger sequence numbers are non-negative by definition and are
		// written by this application; the columns are bounded well below
		// int64 max (nester#1035, G115).
		FromLedger:             uint64(fromLedger), // #nosec G115 -- ledger sequences are non-negative
		ToLedger:               uint64(toLedger),   // #nosec G115 -- ledger sequences are non-negative
		ContractIDs:            []string(contractIDs),
		Mode:                   backfill.Mode(mode),
		DryRun:                 dryRun,
		Status:                 backfill.Status(status),
		EventsProcessed:        eventsProcessed,
		EventsSkippedDuplicate: eventsSkipped,
		InitiatedBy:            initiatedBy,
		CreatedAt:              createdAt,
		UpdatedAt:              updatedAt,
	}
	if lastLedgerDone.Valid {
		v := uint64(lastLedgerDone.Int64) // #nosec G115 -- ledger sequences are non-negative
		run.LastLedgerDone = &v
	}
	if lastError.Valid {
		run.LastError = lastError.String
	}
	if completedAt.Valid {
		t := completedAt.Time
		run.CompletedAt = &t
	}
	return run, nil
}
