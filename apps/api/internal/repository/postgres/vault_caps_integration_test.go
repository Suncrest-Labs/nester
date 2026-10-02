package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/caps"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

// TestVaultRepositoryIntegrationRecordDepositVaultCapConcurrent is the
// acceptance test for nester#1317: N concurrent deposits that would together
// exceed a vault's soft_capacity must not all succeed. Without the FOR UPDATE
// re-check inside RecordDeposit, every goroutine reads the same pre-deposit
// balance, all pass, and the vault ends up over its cap.
func TestVaultRepositoryIntegrationRecordDepositVaultCapConcurrent(t *testing.T) {
	db := openIntegrationDB(t)
	applyIntegrationMigrations(t, db)
	resetIntegrationTables(t, db)

	repository := NewVaultRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)

	created, err := repository.CreateVault(ctx, vault.Vault{
		ID:              uuid.New(),
		UserID:          userID,
		ContractAddress: "CA-INT-CAP-RACE",
		Currency:        "USDC",
		Status:          vault.StatusActive,
	})
	if err != nil {
		t.Fatalf("CreateVault() error = %v", err)
	}

	// soft_capacity is not yet wired through the CreateVault DTO, so it is
	// set directly for this test.
	const capAmount = "100"
	if _, err := db.Exec(`UPDATE vaults SET soft_capacity = $1 WHERE id = $2`, capAmount, created.ID.String()); err != nil {
		t.Fatalf("set soft_capacity failed: %v", err)
	}

	const n = 8
	const depositAmount = "20" // n * 20 = 160, well over the cap of 100
	results := make(chan error, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			results <- repository.RecordDeposit(ctx, created.ID, vault.TransactionRecord{
				UserID:               userID,
				Amount:               decimal.RequireFromString(depositAmount),
				TransactionHash:      uuid.New().String(),
				SharesMintedOrBurned: decimal.RequireFromString(depositAmount),
				SharePriceAtTime:     decimal.NewFromInt(1),
			})
			_ = i
		}(i)
	}
	wg.Wait()
	close(results)

	var successes, capExceeded int
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, caps.ErrVaultCapExceeded):
			capExceeded++
		default:
			t.Errorf("RecordDeposit() unexpected error = %v", err)
		}
	}

	// 5 deposits of 20 reach exactly 100 (at the cap, allowed); the 6th would
	// push to 120 and must be refused, so exactly 5 succeed.
	const wantSuccesses = 5
	if successes != wantSuccesses {
		t.Fatalf("expected exactly %d successful deposits, got %d (capExceeded=%d)", wantSuccesses, successes, capExceeded)
	}
	if capExceeded != n-wantSuccesses {
		t.Fatalf("expected %d ErrVaultCapExceeded, got %d", n-wantSuccesses, capExceeded)
	}

	fetched, err := repository.GetVault(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetVault() error = %v", err)
	}
	if !fetched.CurrentBalance.Equal(decimal.RequireFromString("100")) {
		t.Fatalf("expected final balance 100, got %s", fetched.CurrentBalance)
	}
}

// TestVaultRepositoryIntegrationRecordDepositUserDailyCapConcurrent is the
// acceptance test for nester#1317's per-user cap: N concurrent deposits by
// the same user, across two different vaults with no vault-level cap, must
// still be bounded by the user's rolling 24h total once it crosses
// daily_deposit_cap.
func TestVaultRepositoryIntegrationRecordDepositUserDailyCapConcurrent(t *testing.T) {
	db := openIntegrationDB(t)
	applyIntegrationMigrations(t, db)
	resetIntegrationTables(t, db)

	repository := NewVaultRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)

	const dailyCap = "100"
	if _, err := db.Exec(`UPDATE users SET daily_deposit_cap = $1 WHERE id = $2`, dailyCap, userID.String()); err != nil {
		t.Fatalf("set daily_deposit_cap failed: %v", err)
	}

	const n = 8
	vaultIDs := make([]uuid.UUID, n)
	for i := range n {
		created, err := repository.CreateVault(ctx, vault.Vault{
			ID:              uuid.New(),
			UserID:          userID,
			ContractAddress: "CA-INT-USER-CAP-RACE-" + uuid.New().String(),
			Currency:        "USDC",
			Status:          vault.StatusActive,
		})
		if err != nil {
			t.Fatalf("CreateVault() error = %v", err)
		}
		vaultIDs[i] = created.ID
	}

	const depositAmount = "20" // n * 20 = 160, well over the daily cap of 100
	results := make(chan error, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func(vaultID uuid.UUID) {
			defer wg.Done()
			results <- repository.RecordDeposit(ctx, vaultID, vault.TransactionRecord{
				UserID:               userID,
				Amount:               decimal.RequireFromString(depositAmount),
				TransactionHash:      uuid.New().String(),
				SharesMintedOrBurned: decimal.RequireFromString(depositAmount),
				SharePriceAtTime:     decimal.NewFromInt(1),
			})
		}(vaultIDs[i])
	}
	wg.Wait()
	close(results)

	var successes, capExceeded int
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, caps.ErrUserDailyCapExceeded):
			capExceeded++
		default:
			t.Errorf("RecordDeposit() unexpected error = %v", err)
		}
	}

	const wantSuccesses = 5
	if successes != wantSuccesses {
		t.Fatalf("expected exactly %d successful deposits under the daily cap, got %d (capExceeded=%d)", wantSuccesses, successes, capExceeded)
	}
	if capExceeded != n-wantSuccesses {
		t.Fatalf("expected %d ErrUserDailyCapExceeded, got %d", n-wantSuccesses, capExceeded)
	}
}
