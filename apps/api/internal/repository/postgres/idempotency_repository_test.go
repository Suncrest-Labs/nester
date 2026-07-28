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

	"github.com/suncrestlabs/nester/apps/api/internal/middleware"
)

func TestIdempotencyRepository_Claim_NewKeyIsClaimed(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewIdempotencyRepository(db)
	userID := uuid.New()

	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO idempotency_keys (user_id, key, request_fingerprint, status, expires_at)
		VALUES ($1, $2, $3, 'in_progress', $4)
		ON CONFLICT (user_id, key) DO NOTHING
	`)).
		WithArgs(userID, "key-1", "fp-1", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	claimed, _, err := repo.Claim(context.Background(), userID, "key-1", "fp-1", 24*time.Hour)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if !claimed {
		t.Fatal("expected the new key to be claimed")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestIdempotencyRepository_Claim_ExistingKeyReturnsCurrentState(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewIdempotencyRepository(db)
	userID := uuid.New()

	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO idempotency_keys (user_id, key, request_fingerprint, status, expires_at)
		VALUES ($1, $2, $3, 'in_progress', $4)
		ON CONFLICT (user_id, key) DO NOTHING
	`)).
		WithArgs(userID, "key-1", "fp-1", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 0)) // 0 rows affected: conflict

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT request_fingerprint, status, response_status, response_body, response_content_type, completed_at
		FROM idempotency_keys
		WHERE user_id = $1 AND key = $2
	`)).
		WithArgs(userID, "key-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"request_fingerprint", "status", "response_status", "response_body", "response_content_type", "completed_at",
		}).AddRow("fp-1", "completed", 201, []byte(`{"id":"tx-1"}`), "application/json", time.Now()))

	claimed, existing, err := repo.Claim(context.Background(), userID, "key-1", "fp-1", 24*time.Hour)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if claimed {
		t.Fatal("expected the already-existing key to not be claimed")
	}
	if existing.Status != "completed" || existing.ResponseStatus != 201 {
		t.Fatalf("existing = %+v", existing)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestIdempotencyRepository_Get_NotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewIdempotencyRepository(db)
	userID := uuid.New()

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT request_fingerprint, status, response_status, response_body, response_content_type, completed_at
		FROM idempotency_keys
		WHERE user_id = $1 AND key = $2
	`)).
		WithArgs(userID, "missing-key").
		WillReturnError(sql.ErrNoRows)

	_, err = repo.Get(context.Background(), userID, "missing-key")
	if !errors.Is(err, middleware.ErrIdempotencyKeyNotFound) {
		t.Fatalf("expected ErrIdempotencyKeyNotFound, got %v", err)
	}
}

func TestIdempotencyRepository_Complete(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewIdempotencyRepository(db)
	userID := uuid.New()

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE idempotency_keys
		SET status = 'completed',
		    response_status = $3,
		    response_body = $4,
		    response_content_type = $5,
		    completed_at = NOW()
		WHERE user_id = $1 AND key = $2
	`)).
		WithArgs(userID, "key-1", 201, []byte(`{"id":"tx-1"}`), "application/json").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.Complete(context.Background(), userID, "key-1", 201, []byte(`{"id":"tx-1"}`), "application/json"); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestIdempotencyRepository_Release(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewIdempotencyRepository(db)
	userID := uuid.New()

	mock.ExpectExec(regexp.QuoteMeta(`
		DELETE FROM idempotency_keys WHERE user_id = $1 AND key = $2 AND status = 'in_progress'
	`)).
		WithArgs(userID, "key-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.Release(context.Background(), userID, "key-1"); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestIdempotencyRepository_PurgeExpired(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewIdempotencyRepository(db)

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM idempotency_keys WHERE expires_at < NOW()`)).
		WillReturnResult(sqlmock.NewResult(0, 3))

	n, err := repo.PurgeExpired(context.Background())
	if err != nil {
		t.Fatalf("PurgeExpired: %v", err)
	}
	if n != 3 {
		t.Fatalf("expected 3 rows purged, got %d", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}
