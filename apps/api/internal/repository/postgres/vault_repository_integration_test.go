package postgres

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

func TestVaultRepositoryIntegrationCRUD(t *testing.T) {
	db := openIntegrationDB(t)
	applyIntegrationMigrations(t, db)
	resetIntegrationTables(t, db)

	repository := NewVaultRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)
	otherUserID := seedIntegrationUser(t, db)

	created, err := repository.CreateVault(ctx, vault.Vault{
		ID:              uuid.New(),
		UserID:          userID,
		ContractAddress: "CA-INT-001",
		TotalDeposited:  decimal.Zero,
		CurrentBalance:  decimal.Zero,
		Currency:        "USDC",
		Status:          vault.StatusActive,
	})
	if err != nil {
		t.Fatalf("CreateVault() error = %v", err)
	}

	if err := repository.UpdateVaultBalances(
		ctx,
		created.ID,
		decimal.RequireFromString("120.50"),
		decimal.RequireFromString("123.75"),
	); err != nil {
		t.Fatalf("UpdateVaultBalances() error = %v", err)
	}

	if err := repository.ReplaceAllocations(ctx, created.ID, []vault.Allocation{
		{
			ID:          uuid.New(),
			VaultID:     created.ID,
			Protocol:    "aave",
			Amount:      decimal.RequireFromString("50.00"),
			APY:         decimal.RequireFromString("4.10"),
			AllocatedAt: time.Now().UTC(),
		},
		{
			ID:          uuid.New(),
			VaultID:     created.ID,
			Protocol:    "blend",
			Amount:      decimal.RequireFromString("73.75"),
			APY:         decimal.RequireFromString("5.20"),
			AllocatedAt: time.Now().UTC().Add(time.Second),
		},
	}); err != nil {
		t.Fatalf("ReplaceAllocations() error = %v", err)
	}

	_, err = repository.CreateVault(ctx, vault.Vault{
		ID:              uuid.New(),
		UserID:          otherUserID,
		ContractAddress: "CA-INT-002",
		TotalDeposited:  decimal.Zero,
		CurrentBalance:  decimal.Zero,
		Currency:        "USDC",
		Status:          vault.StatusPaused,
	})
	if err != nil {
		t.Fatalf("CreateVault(other user) error = %v", err)
	}

	fetched, err := repository.GetVault(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetVault() error = %v", err)
	}

	if !fetched.TotalDeposited.Equal(decimal.RequireFromString("120.50")) {
		t.Fatalf("expected total deposited 120.50, got %s", fetched.TotalDeposited)
	}
	if len(fetched.Allocations) != 2 {
		t.Fatalf("expected 2 allocations, got %d", len(fetched.Allocations))
	}

	userVaults, err := repository.GetUserVaults(ctx, userID)
	if err != nil {
		t.Fatalf("GetUserVaults() error = %v", err)
	}
	if len(userVaults) != 1 {
		t.Fatalf("expected 1 vault for user, got %d", len(userVaults))
	}
}

func TestVaultRepositoryIntegrationEdgeCases(t *testing.T) {
	db := openIntegrationDB(t)
	applyIntegrationMigrations(t, db)
	resetIntegrationTables(t, db)

	repository := NewVaultRepository(db)
	ctx := context.Background()

	_, err := repository.GetVault(ctx, uuid.New())
	if err != vault.ErrVaultNotFound {
		t.Fatalf("expected ErrVaultNotFound, got %v", err)
	}

	err = repository.UpdateVaultBalances(
		ctx,
		uuid.New(),
		decimal.RequireFromString("1"),
		decimal.RequireFromString("1"),
	)
	if err != vault.ErrVaultNotFound {
		t.Fatalf("expected ErrVaultNotFound on update, got %v", err)
	}

	err = repository.ReplaceAllocations(ctx, uuid.New(), []vault.Allocation{
		{
			ID:          uuid.New(),
			Protocol:    "aave",
			Amount:      decimal.RequireFromString("1"),
			APY:         decimal.RequireFromString("1"),
			AllocatedAt: time.Now().UTC(),
		},
	})
	if err != vault.ErrVaultNotFound {
		t.Fatalf("expected ErrVaultNotFound on allocation replace, got %v", err)
	}

	_, err = repository.CreateVault(ctx, vault.Vault{
		ID:              uuid.New(),
		UserID:          uuid.New(),
		ContractAddress: "CA-INT-404",
		TotalDeposited:  decimal.Zero,
		CurrentBalance:  decimal.Zero,
		Currency:        "USDC",
		Status:          vault.StatusActive,
	})
	if err != vault.ErrUserNotFound {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

func TestVaultRepositoryIntegrationRecordDepositConcurrent(t *testing.T) {
	db := openIntegrationDB(t)
	applyIntegrationMigrations(t, db)
	resetIntegrationTables(t, db)

	repository := NewVaultRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)

	created, err := repository.CreateVault(ctx, vault.Vault{
		ID:              uuid.New(),
		UserID:          userID,
		ContractAddress: "CA-INT-RACE",
		TotalDeposited:  decimal.Zero,
		CurrentBalance:  decimal.Zero,
		Currency:        "USDC",
		Status:          vault.StatusActive,
	})
	if err != nil {
		t.Fatalf("CreateVault() error = %v", err)
	}

	var wg sync.WaitGroup
	deposit := func() {
		defer wg.Done()
		if err := repository.RecordDeposit(ctx, created.ID, decimal.RequireFromString("10")); err != nil {
			t.Errorf("RecordDeposit() error = %v", err)
		}
	}

	wg.Add(2)
	go deposit()
	go deposit()
	wg.Wait()

	fetched, err := repository.GetVault(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetVault() error = %v", err)
	}
	if !fetched.TotalDeposited.Equal(decimal.RequireFromString("20")) {
		t.Fatalf("expected total deposited 20, got %s", fetched.TotalDeposited)
	}
	if !fetched.CurrentBalance.Equal(decimal.RequireFromString("20")) {
		t.Fatalf("expected current balance 20, got %s", fetched.CurrentBalance)
	}
}

func openIntegrationDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_DSN")
	if strings.TrimSpace(dsn) == "" {
		t.Skip("TEST_DATABASE_DSN is not set")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })
	return db
}

func applyIntegrationMigrations(t *testing.T, db *sql.DB) {
	t.Helper()

	for _, name := range []string{
		"001_create_users_table.up.sql",
		"004_create_vaults_table.up.sql",
		"005_create_allocations_table.up.sql",
		"006_create_settlements_table.up.sql",
	} {
		path := filepath.Join("..", "..", "..", "migrations", name)
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) error = %v", path, err)
		}
		if _, err := db.Exec(string(contents)); err != nil {
			t.Fatalf("applying migration %q failed: %v", name, err)
		}
	}
}

func resetIntegrationTables(t *testing.T, db *sql.DB) {
	t.Helper()

	if _, err := db.Exec(`TRUNCATE TABLE settlements, allocations, vaults, users RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("TRUNCATE failed: %v", err)
	}
}

func seedIntegrationUser(t *testing.T, db *sql.DB) uuid.UUID {
	t.Helper()

	userID := uuid.New()
	if _, err := db.Exec(
		`INSERT INTO users (id, email, name) VALUES ($1, $2, $3)`,
		userID.String(),
		userID.String()+"@example.com",
		"Integration User",
	); err != nil {
		t.Fatalf("seed user failed: %v", err)
	}
	return userID
}
