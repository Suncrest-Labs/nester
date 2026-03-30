package postgres

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

func TestCreateVaultMapsForeignKeyViolationToUserNotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repository := NewVaultRepository(db)
	model := vault.Vault{
		ID:              uuid.New(),
		UserID:          uuid.New(),
		ContractAddress: "CA-001",
		TotalDeposited:  decimal.Zero,
		CurrentBalance:  decimal.Zero,
		Currency:        "USDC",
		Status:          vault.StatusActive,
	}

	mock.ExpectQuery(regexp.QuoteMeta(`
		INSERT INTO vaults (
			id, user_id, contract_address, total_deposited, current_balance, currency, status
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING created_at, updated_at
	`)).
		WillReturnError(&pgconn.PgError{Code: "23503", ConstraintName: "vaults_user_id_fkey"})

	_, err = repository.CreateVault(context.Background(), model)
	if !errors.Is(err, vault.ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

func TestRecordDepositUpdatesBalancesAtomically(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repository := NewVaultRepository(db)
	vaultID := uuid.New()

	// RecordDeposit now runs inside a transaction and also inserts a ledger entry.
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE vaults
		 SET total_deposited = total_deposited + $2::numeric,
		     current_balance = current_balance + $2::numeric,
		     updated_at = NOW()
		 WHERE id = $1 AND deleted_at IS NULL`)).
		WithArgs(vaultID.String(), "25.5").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO vault_transactions (vault_id, type, amount) VALUES ($1, 'deposit', $2::numeric)`)).
		WithArgs(vaultID.String(), "25.5").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := repository.RecordDeposit(context.Background(), vaultID, decimal.RequireFromString("25.5")); err != nil {
		t.Fatalf("RecordDeposit() error = %v", err)
	}
}

func TestGetVaultLoadsAllocations(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repository := NewVaultRepository(db)
	vaultID := uuid.New()
	userID := uuid.New()
	createdAt := time.Now().UTC()

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, user_id, contract_address, total_deposited, current_balance, currency, status, created_at, updated_at
		FROM vaults
		WHERE id = $1 AND deleted_at IS NULL
	`)).
		WithArgs(vaultID.String()).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "user_id", "contract_address", "total_deposited", "current_balance", "currency", "status", "created_at", "updated_at",
		}).AddRow(vaultID.String(), userID.String(), "CA-001", "100.00", "105.50", "USDC", "active", createdAt, createdAt))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, vault_id, protocol, amount, apy, allocated_at FROM allocations WHERE vault_id = $1 ORDER BY allocated_at DESC`)).
		WithArgs(vaultID.String()).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "vault_id", "protocol", "amount", "apy", "allocated_at",
		}).AddRow(uuid.New().String(), vaultID.String(), "aave", "40.00", "4.10", createdAt))

	model, err := repository.GetVault(context.Background(), vaultID)
	if err != nil {
		t.Fatalf("GetVault() error = %v", err)
	}

	if len(model.Allocations) != 1 {
		t.Fatalf("expected 1 allocation, got %d", len(model.Allocations))
	}
	if !model.CurrentBalance.Equal(decimal.RequireFromString("105.50")) {
		t.Fatalf("expected current balance 105.50, got %s", model.CurrentBalance)
	}
}
