package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

func TestVaultRepositoryIntegrationPersistence(t *testing.T) {
	db := openIntegrationDB(t)
	applyIntegrationMigrations(t, db)
	resetIntegrationTables(t, db)

	repository := NewVaultRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)

	// Create vault with all fields
	original := vault.Vault{
		ID:              uuid.New(),
		UserID:          userID,
		ContractAddress: "CA-PERSIST-001",
		TotalDeposited:  decimal.RequireFromString("1234.5678"),
		CurrentBalance:  decimal.RequireFromString("1235.7890"),
		Currency:        "USDC",
		Status:          vault.StatusActive,
	}

	created, err := repository.CreateVault(ctx, original)
	if err != nil {
		t.Fatalf("CreateVault() error = %v", err)
	}

	// Verify all fields are persisted correctly
	if created.ID != original.ID {
		t.Fatalf("ID = %v, want %v", created.ID, original.ID)
	}
	if created.UserID != original.UserID {
		t.Fatalf("UserID = %v, want %v", created.UserID, original.UserID)
	}
	if created.ContractAddress != original.ContractAddress {
		t.Fatalf("ContractAddress = %q, want %q", created.ContractAddress, original.ContractAddress)
	}
	if !created.TotalDeposited.Equal(original.TotalDeposited) {
		t.Fatalf("TotalDeposited = %s, want %s", created.TotalDeposited, original.TotalDeposited)
	}
	if !created.CurrentBalance.Equal(original.CurrentBalance) {
		t.Fatalf("CurrentBalance = %s, want %s", created.CurrentBalance, original.CurrentBalance)
	}
	if created.Currency != original.Currency {
		t.Fatalf("Currency = %q, want %q", created.Currency, original.Currency)
	}
	if created.Status != original.Status {
		t.Fatalf("Status = %q, want %q", created.Status, original.Status)
	}
	if created.CreatedAt.IsZero() {
		t.Fatal("CreatedAt should be set")
	}
	if created.UpdatedAt.IsZero() {
		t.Fatal("UpdatedAt should be set")
	}

	// Retrieve and verify persistence
	fetched, err := repository.GetVault(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetVault() error = %v", err)
	}

	if fetched.ID != original.ID {
		t.Fatalf("fetched ID = %v, want %v", fetched.ID, original.ID)
	}
	if fetched.UserID != original.UserID {
		t.Fatalf("fetched UserID = %v, want %v", fetched.UserID, original.UserID)
	}
	if fetched.ContractAddress != original.ContractAddress {
		t.Fatalf("fetched ContractAddress = %q, want %q", fetched.ContractAddress, original.ContractAddress)
	}
	if !fetched.TotalDeposited.Equal(original.TotalDeposited) {
		t.Fatalf("fetched TotalDeposited = %s, want %s", fetched.TotalDeposited, original.TotalDeposited)
	}
	if !fetched.CurrentBalance.Equal(original.CurrentBalance) {
		t.Fatalf("fetched CurrentBalance = %s, want %s", fetched.CurrentBalance, original.CurrentBalance)
	}
	if fetched.Currency != original.Currency {
		t.Fatalf("fetched Currency = %q, want %q", fetched.Currency, original.Currency)
	}
	if fetched.Status != original.Status {
		t.Fatalf("fetched Status = %q, want %q", fetched.Status, original.Status)
	}
}

func TestVaultRepositoryIntegrationAllocationPersistence(t *testing.T) {
	db := openIntegrationDB(t)
	applyIntegrationMigrations(t, db)
	resetIntegrationTables(t, db)

	repository := NewVaultRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)

	created, err := repository.CreateVault(ctx, vault.Vault{
		ID:              uuid.New(),
		UserID:          userID,
		ContractAddress: "CA-ALLOC-PERSIST-001",
		TotalDeposited:  decimal.Zero,
		CurrentBalance:  decimal.Zero,
		Currency:        "USDC",
		Status:          vault.StatusActive,
	})
	if err != nil {
		t.Fatalf("CreateVault() error = %v", err)
	}

	// Create allocations with all fields
	allocations := []vault.Allocation{
		{
			ID:          uuid.New(),
			VaultID:     created.ID,
			Protocol:    "aave",
			Amount:      decimal.RequireFromString("40.50"),
			APY:         decimal.RequireFromString("4.1234"),
			AllocatedAt: time.Now().UTC().Truncate(time.Microsecond),
		},
		{
			ID:          uuid.New(),
			VaultID:     created.ID,
			Protocol:    "blend",
			Amount:      decimal.RequireFromString("59.50"),
			APY:         decimal.RequireFromString("5.5678"),
			AllocatedAt: time.Now().UTC().Add(time.Second).Truncate(time.Microsecond),
		},
	}

	err = repository.ReplaceAllocations(ctx, created.ID, allocations)
	if err != nil {
		t.Fatalf("ReplaceAllocations() error = %v", err)
	}

	// Retrieve and verify allocations
	fetched, err := repository.GetVault(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetVault() error = %v", err)
	}

	if len(fetched.Allocations) != 2 {
		t.Fatalf("fetched %d allocations, want 2", len(fetched.Allocations))
	}

	// Verify each allocation
	for i, alloc := range fetched.Allocations {
		expected := allocations[i]
		if alloc.ID != expected.ID {
			t.Fatalf("allocation[%d] ID = %v, want %v", i, alloc.ID, expected.ID)
		}
		if alloc.VaultID != expected.VaultID {
			t.Fatalf("allocation[%d] VaultID = %v, want %v", i, alloc.VaultID, expected.VaultID)
		}
		if alloc.Protocol != expected.Protocol {
			t.Fatalf("allocation[%d] Protocol = %q, want %q", i, alloc.Protocol, expected.Protocol)
		}
		if !alloc.Amount.Equal(expected.Amount) {
			t.Fatalf("allocation[%d] Amount = %s, want %s", i, alloc.Amount, expected.Amount)
		}
		if !alloc.APY.Equal(expected.APY) {
			t.Fatalf("allocation[%d] APY = %s, want %s", i, alloc.APY, expected.APY)
		}
		if !alloc.AllocatedAt.Equal(expected.AllocatedAt) {
			t.Fatalf("allocation[%d] AllocatedAt = %v, want %v", i, alloc.AllocatedAt, expected.AllocatedAt)
		}
	}
}

func TestVaultRepositoryIntegrationReplaceAllocations(t *testing.T) {
	db := openIntegrationDB(t)
	applyIntegrationMigrations(t, db)
	resetIntegrationTables(t, db)

	repository := NewVaultRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)

	created, err := repository.CreateVault(ctx, vault.Vault{
		ID:              uuid.New(),
		UserID:          userID,
		ContractAddress: "CA-ALLOC-REPLACE-001",
		TotalDeposited:  decimal.Zero,
		CurrentBalance:  decimal.Zero,
		Currency:        "USDC",
		Status:          vault.StatusActive,
	})
	if err != nil {
		t.Fatalf("CreateVault() error = %v", err)
	}

	// Initial allocations
	initialAllocations := []vault.Allocation{
		{
			ID:          uuid.New(),
			VaultID:     created.ID,
			Protocol:    "aave",
			Amount:      decimal.RequireFromString("100"),
			APY:         decimal.RequireFromString("4.5"),
			AllocatedAt: time.Now().UTC(),
		},
	}

	err = repository.ReplaceAllocations(ctx, created.ID, initialAllocations)
	if err != nil {
		t.Fatalf("ReplaceAllocations() error = %v", err)
	}

	fetched, err := repository.GetVault(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetVault() error = %v", err)
	}

	if len(fetched.Allocations) != 1 {
		t.Fatalf("fetched %d allocations, want 1", len(fetched.Allocations))
	}

	// Replace with new allocations
	newAllocations := []vault.Allocation{
		{
			ID:          uuid.New(),
			VaultID:     created.ID,
			Protocol:    "blend",
			Amount:      decimal.RequireFromString("60"),
			APY:         decimal.RequireFromString("5.2"),
			AllocatedAt: time.Now().UTC(),
		},
		{
			ID:          uuid.New(),
			VaultID:     created.ID,
			Protocol:    "compound",
			Amount:      decimal.RequireFromString("40"),
			APY:         decimal.RequireFromString("3.8"),
			AllocatedAt: time.Now().UTC().Add(time.Second),
		},
	}

	err = repository.ReplaceAllocations(ctx, created.ID, newAllocations)
	if err != nil {
		t.Fatalf("ReplaceAllocations() error = %v", err)
	}

	fetched, err = repository.GetVault(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetVault() error = %v", err)
	}

	if len(fetched.Allocations) != 2 {
		t.Fatalf("fetched %d allocations, want 2", len(fetched.Allocations))
	}

	// Verify old allocation is gone and new ones are present
	protocols := make(map[string]bool)
	for _, alloc := range fetched.Allocations {
		protocols[alloc.Protocol] = true
	}
	if protocols["aave"] {
		t.Fatal("old allocation 'aave' should be replaced")
	}
	if !protocols["blend"] {
		t.Fatal("new allocation 'blend' should be present")
	}
	if !protocols["compound"] {
		t.Fatal("new allocation 'compound' should be present")
	}
}

func TestVaultRepositoryIntegrationGetUserVaultsWithAllocations(t *testing.T) {
	db := openIntegrationDB(t)
	applyIntegrationMigrations(t, db)
	resetIntegrationTables(t, db)

	repository := NewVaultRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)

	// Create multiple vaults with allocations
	vault1, err := repository.CreateVault(ctx, vault.Vault{
		ID:              uuid.New(),
		UserID:          userID,
		ContractAddress: "CA-USER-ALLOC-001",
		TotalDeposited:  decimal.RequireFromString("100"),
		CurrentBalance:  decimal.RequireFromString("105"),
		Currency:        "USDC",
		Status:          vault.StatusActive,
	})
	if err != nil {
		t.Fatalf("CreateVault() error = %v", err)
	}

	err = repository.ReplaceAllocations(ctx, vault1.ID, []vault.Allocation{
		{
			ID:          uuid.New(),
			VaultID:     vault1.ID,
			Protocol:    "aave",
			Amount:      decimal.RequireFromString("100"),
			APY:         decimal.RequireFromString("4.5"),
			AllocatedAt: time.Now().UTC(),
		},
	})
	if err != nil {
		t.Fatalf("ReplaceAllocations() error = %v", err)
	}

	vault2, err := repository.CreateVault(ctx, vault.Vault{
		ID:              uuid.New(),
		UserID:          userID,
		ContractAddress: "CA-USER-ALLOC-002",
		TotalDeposited:  decimal.RequireFromString("200"),
		CurrentBalance:  decimal.RequireFromString("210"),
		Currency:        "USDC",
		Status:          vault.StatusActive,
	})
	if err != nil {
		t.Fatalf("CreateVault() error = %v", err)
	}

	err = repository.ReplaceAllocations(ctx, vault2.ID, []vault.Allocation{
		{
			ID:          uuid.New(),
			VaultID:     vault2.ID,
			Protocol:    "blend",
			Amount:      decimal.RequireFromString("60"),
			APY:         decimal.RequireFromString("5.2"),
			AllocatedAt: time.Now().UTC(),
		},
		{
			ID:          uuid.New(),
			VaultID:     vault2.ID,
			Protocol:    "compound",
			Amount:      decimal.RequireFromString("40"),
			APY:         decimal.RequireFromString("3.8"),
			AllocatedAt: time.Now().UTC().Add(time.Second),
		},
	})
	if err != nil {
		t.Fatalf("ReplaceAllocations() error = %v", err)
	}

	// Get user vaults
	vaults, err := repository.GetUserVaults(ctx, userID)
	if err != nil {
		t.Fatalf("GetUserVaults() error = %v", err)
	}

	if len(vaults) != 2 {
		t.Fatalf("fetched %d vaults, want 2", len(vaults))
	}

	// Verify allocations are loaded for each vault
	for _, v := range vaults {
		if v.ID == vault1.ID {
			if len(v.Allocations) != 1 {
				t.Fatalf("vault1 has %d allocations, want 1", len(v.Allocations))
			}
			if v.Allocations[0].Protocol != "aave" {
				t.Fatalf("vault1 allocation protocol = %q, want %q", v.Allocations[0].Protocol, "aave")
			}
		} else if v.ID == vault2.ID {
			if len(v.Allocations) != 2 {
				t.Fatalf("vault2 has %d allocations, want 2", len(v.Allocations))
			}
		}
	}
}

func TestVaultRepositoryIntegrationUpdateVaultBalances(t *testing.T) {
	db := openIntegrationDB(t)
	applyIntegrationMigrations(t, db)
	resetIntegrationTables(t, db)

	repository := NewVaultRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)

	created, err := repository.CreateVault(ctx, vault.Vault{
		ID:              uuid.New(),
		UserID:          userID,
		ContractAddress: "CA-BALANCE-001",
		TotalDeposited:  decimal.Zero,
		CurrentBalance:  decimal.Zero,
		Currency:        "USDC",
		Status:          vault.StatusActive,
	})
	if err != nil {
		t.Fatalf("CreateVault() error = %v", err)
	}

	// Update balances
	err = repository.UpdateVaultBalances(
		ctx,
		created.ID,
		decimal.RequireFromString("500.25"),
		decimal.RequireFromString("525.50"),
	)
	if err != nil {
		t.Fatalf("UpdateVaultBalances() error = %v", err)
	}

	// Verify update
	fetched, err := repository.GetVault(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetVault() error = %v", err)
	}

	if !fetched.TotalDeposited.Equal(decimal.RequireFromString("500.25")) {
		t.Fatalf("TotalDeposited = %s, want 500.25", fetched.TotalDeposited)
	}
	if !fetched.CurrentBalance.Equal(decimal.RequireFromString("525.50")) {
		t.Fatalf("CurrentBalance = %s, want 525.50", fetched.CurrentBalance)
	}
}

func TestVaultRepositoryIntegrationRecordDeposit(t *testing.T) {
	db := openIntegrationDB(t)
	applyIntegrationMigrations(t, db)
	resetIntegrationTables(t, db)

	repository := NewVaultRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)

	created, err := repository.CreateVault(ctx, vault.Vault{
		ID:              uuid.New(),
		UserID:          userID,
		ContractAddress: "CA-DEPOSIT-INT-001",
		TotalDeposited:  decimal.Zero,
		CurrentBalance:  decimal.Zero,
		Currency:        "USDC",
		Status:          vault.StatusActive,
	})
	if err != nil {
		t.Fatalf("CreateVault() error = %v", err)
	}

	// Record deposits
	err = repository.RecordDeposit(ctx, created.ID, decimal.RequireFromString("100.50"))
	if err != nil {
		t.Fatalf("RecordDeposit() error = %v", err)
	}

	err = repository.RecordDeposit(ctx, created.ID, decimal.RequireFromString("50.25"))
	if err != nil {
		t.Fatalf("RecordDeposit() error = %v", err)
	}

	// Verify deposits
	fetched, err := repository.GetVault(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetVault() error = %v", err)
	}

	if !fetched.TotalDeposited.Equal(decimal.RequireFromString("150.75")) {
		t.Fatalf("TotalDeposited = %s, want 150.75", fetched.TotalDeposited)
	}
	if !fetched.CurrentBalance.Equal(decimal.RequireFromString("150.75")) {
		t.Fatalf("CurrentBalance = %s, want 150.75", fetched.CurrentBalance)
	}
}

func TestVaultRepositoryIntegrationEmptyAllocations(t *testing.T) {
	db := openIntegrationDB(t)
	applyIntegrationMigrations(t, db)
	resetIntegrationTables(t, db)

	repository := NewVaultRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)

	created, err := repository.CreateVault(ctx, vault.Vault{
		ID:              uuid.New(),
		UserID:          userID,
		ContractAddress: "CA-EMPTY-ALLOC-001",
		TotalDeposited:  decimal.Zero,
		CurrentBalance:  decimal.Zero,
		Currency:        "USDC",
		Status:          vault.StatusActive,
	})
	if err != nil {
		t.Fatalf("CreateVault() error = %v", err)
	}

	// Replace with empty allocations
	err = repository.ReplaceAllocations(ctx, created.ID, []vault.Allocation{})
	if err != nil {
		t.Fatalf("ReplaceAllocations() error = %v", err)
	}

	// Verify empty allocations
	fetched, err := repository.GetVault(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetVault() error = %v", err)
	}

	if len(fetched.Allocations) != 0 {
		t.Fatalf("fetched %d allocations, want 0", len(fetched.Allocations))
	}
}
