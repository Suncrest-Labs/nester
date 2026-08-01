package postgres

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/backfill"
)

func TestBackfillRepository_Create(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewBackfillRepository(db)
	run := &backfill.Run{
		FromLedger:  100,
		ToLedger:    200,
		ContractIDs: []string{"C123"},
		Mode:        backfill.ModeBackfill,
		DryRun:      false,
		InitiatedBy: "omarima-10",
	}

	mock.ExpectQuery(regexp.QuoteMeta(`
		INSERT INTO backfill_runs (id, from_ledger, to_ledger, contract_ids, mode, dry_run, status, initiated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING created_at, updated_at
	`)).
		WithArgs(sqlmock.AnyArg(), int64(100), int64(200), sqlmock.AnyArg(), "backfill", false, "running", "omarima-10").
		WillReturnRows(sqlmock.NewRows([]string{"created_at", "updated_at"}).AddRow(time.Now(), time.Now()))

	if err := repo.Create(context.Background(), run); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if run.ID == uuid.Nil {
		t.Error("expected an id to be assigned")
	}
	if run.Status != backfill.StatusRunning {
		t.Errorf("expected default status running, got %q", run.Status)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestBackfillRepository_GetByID_NotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewBackfillRepository(db)
	id := uuid.New()

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, from_ledger, to_ledger, contract_ids, mode, dry_run, status,
		       last_ledger_done, events_processed, events_skipped_duplicate, last_error,
		       initiated_by, created_at, updated_at, completed_at
		FROM backfill_runs WHERE id = $1
	`)).WithArgs(id).WillReturnError(sql.ErrNoRows)

	_, err = repo.GetByID(context.Background(), id)
	if !errors.Is(err, backfill.ErrRunNotFound) {
		t.Fatalf("expected ErrRunNotFound, got %v", err)
	}
}

func TestBackfillRepository_GetByID_ReturnsResumeCheckpoint(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewBackfillRepository(db)
	id := uuid.New()
	now := time.Now()

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, from_ledger, to_ledger, contract_ids, mode, dry_run, status,
		       last_ledger_done, events_processed, events_skipped_duplicate, last_error,
		       initiated_by, created_at, updated_at, completed_at
		FROM backfill_runs WHERE id = $1
	`)).WithArgs(id).WillReturnRows(sqlmock.NewRows([]string{
		"id", "from_ledger", "to_ledger", "contract_ids", "mode", "dry_run", "status",
		"last_ledger_done", "events_processed", "events_skipped_duplicate", "last_error",
		"initiated_by", "created_at", "updated_at", "completed_at",
	}).AddRow(
		id.String(), int64(100), int64(200), "{C123}", "backfill", false, "running",
		int64(150), int64(42), int64(3), nil,
		"omarima-10", now, now, nil,
	))

	run, err := repo.GetByID(context.Background(), id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if run.LastLedgerDone == nil || *run.LastLedgerDone != 150 {
		t.Fatalf("expected last_ledger_done=150, got %+v", run.LastLedgerDone)
	}
	if run.ResumeFrom() != 151 {
		t.Errorf("ResumeFrom() = %d, want 151", run.ResumeFrom())
	}
	if run.EventsProcessed != 42 || run.EventsSkippedDuplicate != 3 {
		t.Errorf("unexpected counts: %+v", run)
	}
}

func TestBackfillRepository_UpdateProgress(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewBackfillRepository(db)
	id := uuid.New()

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE backfill_runs
		SET last_ledger_done = $2,
		    events_processed = $3,
		    events_skipped_duplicate = $4,
		    updated_at = NOW()
		WHERE id = $1
	`)).WithArgs(id, int64(150), int64(42), int64(3)).WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.UpdateProgress(context.Background(), id, 150, 42, 3); err != nil {
		t.Fatalf("UpdateProgress: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestBackfillRepository_Complete(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewBackfillRepository(db)
	id := uuid.New()

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE backfill_runs SET status = 'completed', completed_at = NOW(), updated_at = NOW() WHERE id = $1
	`)).WithArgs(id).WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.Complete(context.Background(), id); err != nil {
		t.Fatalf("Complete: %v", err)
	}
}

func TestBackfillRepository_Fail(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewBackfillRepository(db)
	id := uuid.New()

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE backfill_runs SET status = 'failed', last_error = $2, updated_at = NOW() WHERE id = $1
	`)).WithArgs(id, "rpc timeout").WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.Fail(context.Background(), id, "rpc timeout"); err != nil {
		t.Fatalf("Fail: %v", err)
	}
}
