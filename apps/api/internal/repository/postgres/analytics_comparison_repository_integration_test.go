package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

func resetAnalyticsComparisonIntegrationTables(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`TRUNCATE TABLE apy_history, allocations, vaults, users RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("TRUNCATE failed: %v", err)
	}
}

func seedAnalyticsVaultWithAllocation(t *testing.T, db *sql.DB, vaultRepo *VaultRepository, userID uuid.UUID, protocol string) uuid.UUID {
	t.Helper()
	ctx := context.Background()

	v, err := vaultRepo.CreateVault(ctx, vault.Vault{
		ID:              uuid.New(),
		UserID:          userID,
		ContractAddress: "CA-" + uuid.New().String(),
		TotalDeposited:  decimal.Zero,
		CurrentBalance:  decimal.Zero,
		Currency:        "USDC",
		Status:          vault.StatusActive,
	})
	if err != nil {
		t.Fatalf("create vault: %v", err)
	}

	if err := vaultRepo.ReplaceAllocations(ctx, v.ID, []vault.Allocation{
		{
			ID:          uuid.New(),
			VaultID:     v.ID,
			Protocol:    protocol,
			Amount:      decimal.RequireFromString("100"),
			APY:         decimal.RequireFromString("5.0"),
			AllocatedAt: time.Now().UTC(),
		},
	}); err != nil {
		t.Fatalf("replace allocations: %v", err)
	}
	return v.ID
}

func insertAPYHistory(t *testing.T, db *sql.DB, vaultID uuid.UUID, realizedAPY float64, calculatedAt time.Time) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO apy_history (id, vault_id, period, realized_apy, calculated_at)
		VALUES ($1, $2, 'all', $3, $4)`,
		uuid.New(), vaultID, realizedAPY, calculatedAt,
	); err != nil {
		t.Fatalf("insert apy_history: %v", err)
	}
}

func TestAnalyticsComparisonRepository_ProtocolYieldHistory(t *testing.T) {
	db := openIntegrationDB(t)
	applyIntegrationMigrations(t, db)
	resetAnalyticsComparisonIntegrationTables(t, db)

	vaultRepo := NewVaultRepository(db)
	userID := seedIntegrationUser(t, db)

	aaveVault := seedAnalyticsVaultWithAllocation(t, db, vaultRepo, userID, "aave")
	compoundVault := seedAnalyticsVaultWithAllocation(t, db, vaultRepo, userID, "compound")

	day1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	day2 := day1.Add(24 * time.Hour)

	insertAPYHistory(t, db, aaveVault, 4.0, day1)
	insertAPYHistory(t, db, aaveVault, 6.0, day1.Add(time.Hour)) // same day, averages with the point above
	insertAPYHistory(t, db, aaveVault, 5.0, day2)
	insertAPYHistory(t, db, compoundVault, 3.0, day1)

	// A point far outside the window must not appear.
	insertAPYHistory(t, db, aaveVault, 99.0, day1.AddDate(-1, 0, 0))

	repo := NewAnalyticsComparisonRepository(db)
	series, err := repo.ProtocolYieldHistory(context.Background(), day1.Add(-time.Hour))
	if err != nil {
		t.Fatalf("ProtocolYieldHistory: %v", err)
	}

	byProtocol := map[string][]float64{}
	for _, s := range series {
		for _, p := range s.Points {
			byProtocol[s.Protocol] = append(byProtocol[s.Protocol], p.APY)
		}
	}

	aavePoints, ok := byProtocol["aave"]
	if !ok || len(aavePoints) != 2 {
		t.Fatalf("aave points = %v, want 2 daily buckets", aavePoints)
	}
	if aavePoints[0] != 5.0 { // (4.0 + 6.0) / 2
		t.Errorf("aave day1 avg = %v, want 5.0", aavePoints[0])
	}
	if aavePoints[1] != 5.0 {
		t.Errorf("aave day2 avg = %v, want 5.0", aavePoints[1])
	}

	compoundPoints, ok := byProtocol["compound"]
	if !ok || len(compoundPoints) != 1 || compoundPoints[0] != 3.0 {
		t.Fatalf("compound points = %v, want [3.0]", compoundPoints)
	}
}
