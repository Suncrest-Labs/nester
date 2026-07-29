package postgres

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
)

const softDeleteQuery = `
		UPDATE savings_goals
		SET deleted_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL
	`

func TestSavingsGoalDeleteSoftArchivesRow(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repository := NewSavingsGoalRepository(db)
	goalID := uuid.New()
	userID := uuid.New()

	mock.ExpectExec(regexp.QuoteMeta(softDeleteQuery)).
		WithArgs(goalID, userID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repository.Delete(context.Background(), goalID, userID); err != nil {
		t.Fatalf("Delete() error = %v, want nil", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSavingsGoalDeleteAlreadyArchivedReturnsNotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repository := NewSavingsGoalRepository(db)
	goalID := uuid.New()
	userID := uuid.New()

	// The deleted_at IS NULL guard means an already-deleted (or missing)
	// goal matches zero rows.
	mock.ExpectExec(regexp.QuoteMeta(softDeleteQuery)).
		WithArgs(goalID, userID).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err = repository.Delete(context.Background(), goalID, userID)
	if !errors.Is(err, savingsgoal.ErrGoalNotFound) {
		t.Fatalf("Delete() error = %v, want ErrGoalNotFound", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

const restoreQuery = `
		UPDATE savings_goals
		SET deleted_at = NULL, updated_at = NOW()
		WHERE id = $1 AND user_id = $2 AND deleted_at IS NOT NULL
	`

func TestSavingsGoalRestoreClearsDeletedAt(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repository := NewSavingsGoalRepository(db)
	goalID := uuid.New()
	userID := uuid.New()

	mock.ExpectExec(regexp.QuoteMeta(restoreQuery)).
		WithArgs(goalID, userID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repository.Restore(context.Background(), goalID, userID); err != nil {
		t.Fatalf("Restore() error = %v, want nil", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSavingsGoalRestoreNotDeletedReturnsNotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repository := NewSavingsGoalRepository(db)
	goalID := uuid.New()
	userID := uuid.New()

	// The deleted_at IS NOT NULL guard means a goal that was never deleted
	// (or doesn't exist / isn't owned by userID) matches zero rows.
	mock.ExpectExec(regexp.QuoteMeta(restoreQuery)).
		WithArgs(goalID, userID).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err = repository.Restore(context.Background(), goalID, userID)
	if !errors.Is(err, savingsgoal.ErrGoalNotFound) {
		t.Fatalf("Restore() error = %v, want ErrGoalNotFound", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

const hardDeleteQuery = `DELETE FROM savings_goals WHERE id = $1 AND deleted_at IS NOT NULL`

func TestSavingsGoalHardDeleteRemovesRow(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repository := NewSavingsGoalRepository(db)
	goalID := uuid.New()

	mock.ExpectExec(regexp.QuoteMeta(hardDeleteQuery)).
		WithArgs(goalID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repository.HardDelete(context.Background(), goalID); err != nil {
		t.Fatalf("HardDelete() error = %v, want nil", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSavingsGoalHardDeleteNotDeletedReturnsNotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repository := NewSavingsGoalRepository(db)
	goalID := uuid.New()

	mock.ExpectExec(regexp.QuoteMeta(hardDeleteQuery)).
		WithArgs(goalID).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err = repository.HardDelete(context.Background(), goalID)
	if !errors.Is(err, savingsgoal.ErrGoalNotFound) {
		t.Fatalf("HardDelete() error = %v, want ErrGoalNotFound", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}
