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
		SET archived_at = NOW(), status = 'archived', updated_at = NOW()
		WHERE id = $1 AND user_id = $2 AND status <> 'archived'
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

	// The status <> 'archived' guard means an already-archived (or missing)
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
