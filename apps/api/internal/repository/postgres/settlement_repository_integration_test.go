package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/offramp"
)

func TestSettlementRepositoryIntegration_CreateGetUpdateAndFilter(t *testing.T) {
	db := openIntegrationDB(t)
	applyIntegrationMigrations(t, db)
	resetIntegrationTables(t, db)

	repo := NewSettlementRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)
	vaultID := seedIntegrationVault(t, db, userID)

	dest := offramp.Destination{
		Type:          "bank_transfer",
		Provider:      "bank",
		AccountNumber: "0123456789",
		AccountName:   "Ada Lovelace",
		BankCode:      "058",
	}

	model := offramp.Settlement{
		ID:           uuid.New(),
		UserID:       userID,
		VaultID:      vaultID,
		Amount:       decimal.RequireFromString("100.5"),
		Currency:     "USDC",
		FiatCurrency: "NGN",
		FiatAmount:   decimal.RequireFromString("150000"),
		ExchangeRate: decimal.RequireFromString("1492.5373"),
		Destination:  dest,
		Status:       offramp.StatusInitiated,
	}

	created, err := repo.Create(ctx, model)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.CreatedAt.IsZero() {
		t.Fatal("expected created_at from DB")
	}
	if created.ID != model.ID {
		t.Fatalf("ID mismatch")
	}
	if created.Destination.BankCode != "058" {
		t.Fatalf("bank_code mismatch")
	}

	fetched, err := repo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if !fetched.Amount.Equal(model.Amount) || fetched.FiatCurrency != "NGN" {
		t.Fatalf("round-trip fields mismatch: %+v", fetched)
	}

	if err := repo.UpdateStatus(ctx, created.ID, offramp.StatusLiquidityMatched, nil); err != nil {
		t.Fatalf("UpdateStatus(liquidity_matched) error = %v", err)
	}
	mid, _ := repo.GetByID(ctx, created.ID)
	if mid.Status != offramp.StatusLiquidityMatched || mid.CompletedAt != nil {
		t.Fatalf("expected liquidity_matched without completed_at, got %+v", mid)
	}

	completed := time.Now().UTC()
	if err := repo.UpdateStatus(ctx, created.ID, offramp.StatusConfirmed, &completed); err != nil {
		t.Fatalf("UpdateStatus(confirmed) error = %v", err)
	}
	done, _ := repo.GetByID(ctx, created.ID)
	if done.Status != offramp.StatusConfirmed || done.CompletedAt == nil {
		t.Fatalf("expected confirmed with completed_at, got %+v", done)
	}
	if done.CompletedAt.IsZero() {
		t.Fatal("completed_at should be set for confirmed")
	}

	// second settlement same user, stays initiated
	model2 := model
	model2.ID = uuid.New()
	if _, err := repo.Create(ctx, model2); err != nil {
		t.Fatalf("Create second error = %v", err)
	}

	all, err := repo.GetByUserID(ctx, userID, "")
	if err != nil || len(all) != 2 {
		t.Fatalf("GetByUserID all: err=%v len=%d", err, len(all))
	}
	initiatedOnly, err := repo.GetByUserID(ctx, userID, offramp.StatusInitiated)
	if err != nil || len(initiatedOnly) != 1 {
		t.Fatalf("GetByUserID initiated: err=%v len=%d", err, len(initiatedOnly))
	}
	if initiatedOnly[0].ID != model2.ID {
		t.Fatalf("wrong settlement in initiated filter")
	}
}

func TestSettlementRepositoryIntegration_NotFound(t *testing.T) {
	db := openIntegrationDB(t)
	applyIntegrationMigrations(t, db)
	resetIntegrationTables(t, db)

	repo := NewSettlementRepository(db)
	ctx := context.Background()

	_, err := repo.GetByID(ctx, uuid.New())
	if !errors.Is(err, offramp.ErrSettlementNotFound) {
		t.Fatalf("GetByID unknown: want ErrSettlementNotFound, got %v", err)
	}

	err = repo.UpdateStatus(ctx, uuid.New(), offramp.StatusFailed, nil)
	if !errors.Is(err, offramp.ErrSettlementNotFound) {
		t.Fatalf("UpdateStatus unknown id: want ErrSettlementNotFound, got %v", err)
	}
}

func seedIntegrationVault(t *testing.T, db *sql.DB, userID uuid.UUID) uuid.UUID {
	t.Helper()
	vaultID := uuid.New()
	_, err := db.ExecContext(
		context.Background(),
		`INSERT INTO vaults (id, user_id, contract_address, total_deposited, current_balance, currency, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		vaultID.String(),
		userID.String(),
		"CA-SETTLE-INT",
		"0",
		"0",
		"USDC",
		"active",
	)
	if err != nil {
		t.Fatalf("seed vault: %v", err)
	}
	return vaultID
}
