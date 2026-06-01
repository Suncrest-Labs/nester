package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/systemstate"
)

func newSystemStateMock(t *testing.T) (*SystemStateRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewSystemStateRepository(db), mock
}

func TestSystemStateRepository_Get_found(t *testing.T) {
	repo, mock := newSystemStateMock(t)

	mock.ExpectQuery(`SELECT value FROM system_state WHERE key = \$1`).
		WithArgs("event_indexer.last_ledger").
		WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow("42"))

	val, err := repo.Get(context.Background(), "event_indexer.last_ledger")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "42" {
		t.Fatalf("expected '42', got %q", val)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSystemStateRepository_Get_missingKey(t *testing.T) {
	repo, mock := newSystemStateMock(t)

	mock.ExpectQuery(`SELECT value FROM system_state WHERE key = \$1`).
		WithArgs("nonexistent.key").
		WillReturnError(sql.ErrNoRows)

	val, err := repo.Get(context.Background(), "nonexistent.key")
	if !errors.Is(err, systemstate.ErrKeyNotFound) {
		t.Fatalf("expected ErrKeyNotFound, got err=%v val=%q", err, val)
	}
	if val != "" {
		t.Fatalf("expected empty string on missing key, got %q", val)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSystemStateRepository_Set_insert(t *testing.T) {
	repo, mock := newSystemStateMock(t)

	mock.ExpectExec(`INSERT INTO system_state`).
		WithArgs("event_indexer.last_ledger", "100").
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := repo.Set(context.Background(), "event_indexer.last_ledger", "100"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSystemStateRepository_Set_upsert(t *testing.T) {
	repo, mock := newSystemStateMock(t)

	// First Set
	mock.ExpectExec(`INSERT INTO system_state`).
		WithArgs("event_indexer.last_ledger", "50").
		WillReturnResult(sqlmock.NewResult(1, 1))
	// Second Set (conflict → update)
	mock.ExpectExec(`INSERT INTO system_state`).
		WithArgs("event_indexer.last_ledger", "75").
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := repo.Set(context.Background(), "event_indexer.last_ledger", "50"); err != nil {
		t.Fatalf("first Set: %v", err)
	}
	if err := repo.Set(context.Background(), "event_indexer.last_ledger", "75"); err != nil {
		t.Fatalf("second Set: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}
