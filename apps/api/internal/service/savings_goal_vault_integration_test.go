package service

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
	"github.com/suncrestlabs/nester/apps/api/internal/repository/postgres"
)

func applySavingsGoalVaultMigrations(t *testing.T, db *sql.DB) {
	t.Helper()
	applySavingsGoalIntegrationMigrations(t, db)
	for _, name := range []string{
		"026_create_savings_goals.up.sql",
		"037_add_savings_goal_category.up.sql",
		"038_add_savings_goal_notified_milestones.up.sql",
		"053_add_savings_goal_vault_id.up.sql",
	} {
		path := filepath.Join("..", "..", "migrations", name)
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) error = %v", path, err)
		}
		if _, err := db.Exec(string(contents)); err != nil {
			t.Fatalf("applying migration %q failed: %v", name, err)
		}
	}
}

// TestSavingsGoalVaultLinking_Integration exercises the full persistence path
// for issue #688: two vaults in the same currency with two goals, each linked
// to its own vault, must report only their own vault's balance.
func TestSavingsGoalVaultLinking_Integration(t *testing.T) {
	db := openSavingsGoalIntegrationDB(t)
	applySavingsGoalVaultMigrations(t, db)
	if _, err := db.Exec(`TRUNCATE TABLE savings_goals, allocations, vaults, users RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("TRUNCATE failed: %v", err)
	}

	ctx := context.Background()
	userID := seedSavingsGoalIntegrationUser(t, db)
	vaultRepo := postgres.NewVaultRepository(db)
	goalRepo := postgres.NewSavingsGoalRepository(db)
	svc := NewSavingsGoalService(goalRepo, vaultRepo, nil)

	flexible, err := vaultRepo.CreateVault(ctx, vault.Vault{
		ID:              uuid.New(),
		UserID:          userID,
		ContractAddress: "CA-FLEX",
		TotalDeposited:  decimal.NewFromInt(200),
		CurrentBalance:  decimal.NewFromInt(200),
		Currency:        "USDC",
		Status:          vault.StatusActive,
	})
	if err != nil {
		t.Fatalf("CreateVault(flexible) error = %v", err)
	}
	compound, err := vaultRepo.CreateVault(ctx, vault.Vault{
		ID:              uuid.New(),
		UserID:          userID,
		ContractAddress: "CA-COMPOUND",
		TotalDeposited:  decimal.NewFromInt(800),
		CurrentBalance:  decimal.NewFromInt(800),
		Currency:        "USDC",
		Status:          vault.StatusActive,
	})
	if err != nil {
		t.Fatalf("CreateVault(compound) error = %v", err)
	}

	deadline := time.Now().UTC().Add(365 * 24 * time.Hour)
	goalFlex, err := svc.Create(ctx, userID, CreateSavingsGoalInput{
		TargetAmount: decimal.NewFromInt(1000),
		Currency:     "USDC",
		Deadline:     deadline,
		VaultID:      &flexible.ID,
	})
	if err != nil {
		t.Fatalf("Create(goalFlex) error = %v", err)
	}
	goalCompound, err := svc.Create(ctx, userID, CreateSavingsGoalInput{
		TargetAmount: decimal.NewFromInt(1000),
		Currency:     "USDC",
		Deadline:     deadline,
		VaultID:      &compound.ID,
	})
	if err != nil {
		t.Fatalf("Create(goalCompound) error = %v", err)
	}

	if !goalFlex.CurrentAmount.Equal(decimal.NewFromInt(200)) {
		t.Fatalf("goalFlex current_amount = %s, want 200", goalFlex.CurrentAmount)
	}
	if !goalCompound.CurrentAmount.Equal(decimal.NewFromInt(800)) {
		t.Fatalf("goalCompound current_amount = %s, want 800", goalCompound.CurrentAmount)
	}

	// The link must survive a round-trip through the database.
	reread, err := svc.Get(ctx, userID, goalFlex.ID)
	if err != nil {
		t.Fatalf("Get(goalFlex) error = %v", err)
	}
	if reread.VaultID == nil || *reread.VaultID != flexible.ID {
		t.Fatalf("persisted vault_id = %v, want %v", reread.VaultID, flexible.ID)
	}
	if !reread.CurrentAmount.Equal(decimal.NewFromInt(200)) {
		t.Fatalf("reread current_amount = %s, want 200", reread.CurrentAmount)
	}
}

// TestSavingsGoalVaultLinking_ForeignVault_Integration verifies a vault owned by
// another user cannot be linked.
func TestSavingsGoalVaultLinking_ForeignVault_Integration(t *testing.T) {
	db := openSavingsGoalIntegrationDB(t)
	applySavingsGoalVaultMigrations(t, db)
	if _, err := db.Exec(`TRUNCATE TABLE savings_goals, allocations, vaults, users RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("TRUNCATE failed: %v", err)
	}

	ctx := context.Background()
	owner := seedSavingsGoalIntegrationUser(t, db)
	intruder := seedSavingsGoalIntegrationUser(t, db)
	vaultRepo := postgres.NewVaultRepository(db)
	goalRepo := postgres.NewSavingsGoalRepository(db)
	svc := NewSavingsGoalService(goalRepo, vaultRepo, nil)

	ownersVault, err := vaultRepo.CreateVault(ctx, vault.Vault{
		ID:              uuid.New(),
		UserID:          owner,
		ContractAddress: "CA-OWNER",
		TotalDeposited:  decimal.NewFromInt(100),
		CurrentBalance:  decimal.NewFromInt(100),
		Currency:        "USDC",
		Status:          vault.StatusActive,
	})
	if err != nil {
		t.Fatalf("CreateVault() error = %v", err)
	}

	_, err = svc.Create(ctx, intruder, CreateSavingsGoalInput{
		TargetAmount: decimal.NewFromInt(1000),
		Currency:     "USDC",
		Deadline:     time.Now().UTC().Add(365 * 24 * time.Hour),
		VaultID:      &ownersVault.ID,
	})
	if err == nil {
		t.Fatal("Create() error = nil, want unauthorized vault link")
	}
}
