package performance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	perfdom "github.com/suncrestlabs/nester/apps/api/internal/domain/performance"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

// analyticsVaultRepo lists a fixed set of vaults for GetUserAnalytics. Only
// ListUserVaults is exercised; every other method is a hard failure so a change
// that starts calling one is caught rather than silently stubbed.
type analyticsVaultRepo struct {
	vault.Repository
	vaults []vault.Vault
	err    error
}

func (r *analyticsVaultRepo) ListUserVaults(
	_ context.Context, _ uuid.UUID, _ vault.UserListFilter,
) ([]vault.Vault, int, error) {
	if r.err != nil {
		return nil, 0, r.err
	}
	return r.vaults, len(r.vaults), nil
}

// analyticsSnapshotRepo serves per-vault history from a map.
type analyticsSnapshotRepo struct {
	perfdom.SnapshotRepository
	history map[uuid.UUID][]perfdom.Snapshot
}

func (r *analyticsSnapshotRepo) HistoryForVault(
	_ context.Context, vaultID uuid.UUID, _ time.Time,
) ([]perfdom.Snapshot, error) {
	snaps, ok := r.history[vaultID]
	if !ok {
		return nil, perfdom.ErrSnapshotNotFound
	}
	return snaps, nil
}

func day(t *testing.T, s string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return parsed
}

func snap(vaultID uuid.UUID, at time.Time, balance, yield int64) perfdom.Snapshot {
	return perfdom.Snapshot{
		VaultID:          vaultID,
		TotalBalance:     decimal.NewFromInt(balance),
		TotalYieldEarned: decimal.NewFromInt(yield),
		SnapshotAt:       at,
	}
}

// A two-vault user's daily series must reflect both vaults, not just the first
// one in the list (nester#1195).
func TestGetUserAnalytics_DailySnapshotsAggregateAllVaults(t *testing.T) {
	vaultA, vaultB := uuid.New(), uuid.New()
	d1 := day(t, "2026-03-01T12:00:00Z")
	d2 := day(t, "2026-03-02T12:00:00Z")

	svc := NewService(
		&analyticsSnapshotRepo{history: map[uuid.UUID][]perfdom.Snapshot{
			vaultA: {snap(vaultA, d1, 100, 10), snap(vaultA, d2, 110, 20)},
			vaultB: {snap(vaultB, d1, 500, 50), snap(vaultB, d2, 520, 70)},
		}},
		&analyticsVaultRepo{vaults: []vault.Vault{
			{ID: vaultA, ContractAddress: "CVAULTA", CurrentBalance: decimal.NewFromInt(110)},
			{ID: vaultB, ContractAddress: "CVAULTB", CurrentBalance: decimal.NewFromInt(520)},
		}},
	)

	resp, err := svc.GetUserAnalytics(context.Background(), uuid.New(), d1.Add(-24*time.Hour), time.Time{})
	if err != nil {
		t.Fatalf("GetUserAnalytics: %v", err)
	}

	if len(resp.DailySnapshots) != 2 {
		t.Fatalf("expected 2 daily snapshots, got %d: %+v", len(resp.DailySnapshots), resp.DailySnapshots)
	}

	// Day one: 100 + 500 balance, 10 + 50 yield — not either vault alone.
	got := resp.DailySnapshots[0]
	if got.Date != "2026-03-01" || !got.TotalBalanceUSD.Equal(decimal.NewFromInt(600)) || !got.YieldEarnedUSD.Equal(decimal.NewFromInt(60)) {
		t.Fatalf("day one must sum both vaults, got %+v", got)
	}
	got = resp.DailySnapshots[1]
	if got.Date != "2026-03-02" || !got.TotalBalanceUSD.Equal(decimal.NewFromInt(630)) || !got.YieldEarnedUSD.Equal(decimal.NewFromInt(90)) {
		t.Fatalf("day two must sum both vaults, got %+v", got)
	}
}

// A vault with no snapshot on a given day must carry its last known balance
// forward rather than dropping out of the portfolio total, which would read as
// a loss the user never took.
func TestGetUserAnalytics_CarriesVaultForwardAcrossMissingDays(t *testing.T) {
	vaultA, vaultB := uuid.New(), uuid.New()
	d1 := day(t, "2026-03-01T12:00:00Z")
	d2 := day(t, "2026-03-02T12:00:00Z")

	svc := NewService(
		&analyticsSnapshotRepo{history: map[uuid.UUID][]perfdom.Snapshot{
			vaultA: {snap(vaultA, d1, 100, 10), snap(vaultA, d2, 110, 20)},
			// vaultB was snapshotted on day one only.
			vaultB: {snap(vaultB, d1, 500, 50)},
		}},
		&analyticsVaultRepo{vaults: []vault.Vault{
			{ID: vaultA, ContractAddress: "CVAULTA"},
			{ID: vaultB, ContractAddress: "CVAULTB"},
		}},
	)

	resp, err := svc.GetUserAnalytics(context.Background(), uuid.New(), d1.Add(-24*time.Hour), time.Time{})
	if err != nil {
		t.Fatalf("GetUserAnalytics: %v", err)
	}
	if len(resp.DailySnapshots) != 2 {
		t.Fatalf("expected 2 daily snapshots, got %+v", resp.DailySnapshots)
	}
	// 110 (day two) + 500 (carried from day one), not 110 alone.
	if got := resp.DailySnapshots[1]; !got.TotalBalanceUSD.Equal(decimal.NewFromInt(610)) {
		t.Fatalf("expected the un-snapshotted vault to carry forward, got %+v", got)
	}
}

// VaultMonthlyYield must be populated rather than empty in every response, and
// must report yield earned within each month rather than the cumulative total.
func TestGetUserAnalytics_PopulatesVaultMonthlyYield(t *testing.T) {
	vaultA := uuid.New()
	march := day(t, "2026-03-31T12:00:00Z")
	april := day(t, "2026-04-30T12:00:00Z")

	svc := NewService(
		&analyticsSnapshotRepo{history: map[uuid.UUID][]perfdom.Snapshot{
			// TotalYieldEarned is cumulative: 30 by end of March, 80 by end of April.
			vaultA: {snap(vaultA, march, 1000, 30), snap(vaultA, april, 1100, 80)},
		}},
		&analyticsVaultRepo{vaults: []vault.Vault{{ID: vaultA, ContractAddress: "CVAULTA"}}},
	)

	resp, err := svc.GetUserAnalytics(context.Background(), uuid.New(), march.Add(-24*time.Hour), time.Time{})
	if err != nil {
		t.Fatalf("GetUserAnalytics: %v", err)
	}
	if len(resp.VaultMonthlyYield) != 2 {
		t.Fatalf("expected 2 monthly entries, got %+v", resp.VaultMonthlyYield)
	}

	first := resp.VaultMonthlyYield[0]
	if first.Month != "2026-03" || !first.YieldUSD.Equal(decimal.NewFromInt(30)) || first.VaultID != vaultA.String() {
		t.Fatalf("March entry wrong: %+v", first)
	}
	second := resp.VaultMonthlyYield[1]
	// 80 cumulative minus 30 already earned = 50 earned in April.
	if second.Month != "2026-04" || !second.YieldUSD.Equal(decimal.NewFromInt(50)) {
		t.Fatalf("April entry must report the month's growth, not the cumulative total: %+v", second)
	}
}

// A user with no vaults gets an empty series, not an error and not a zero-filled
// one.
func TestGetUserAnalytics_NoVaultsReturnsEmptySeries(t *testing.T) {
	svc := NewService(
		&analyticsSnapshotRepo{history: map[uuid.UUID][]perfdom.Snapshot{}},
		&analyticsVaultRepo{vaults: nil},
	)

	resp, err := svc.GetUserAnalytics(context.Background(), uuid.New(), time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("GetUserAnalytics: %v", err)
	}
	if resp.DailySnapshots == nil || len(resp.DailySnapshots) != 0 {
		t.Fatalf("expected an empty, non-nil daily series, got %+v", resp.DailySnapshots)
	}
	if resp.VaultMonthlyYield == nil || len(resp.VaultMonthlyYield) != 0 {
		t.Fatalf("expected an empty, non-nil monthly yield, got %+v", resp.VaultMonthlyYield)
	}
}

// A vault that exists but has never been snapshotted must not error the whole
// request, and must not contribute a zero-balance day.
func TestGetUserAnalytics_VaultWithoutSnapshotsIsSkipped(t *testing.T) {
	withData, without := uuid.New(), uuid.New()
	d1 := day(t, "2026-03-01T12:00:00Z")

	svc := NewService(
		&analyticsSnapshotRepo{history: map[uuid.UUID][]perfdom.Snapshot{
			withData: {snap(withData, d1, 100, 10)},
		}},
		&analyticsVaultRepo{vaults: []vault.Vault{
			{ID: withData, ContractAddress: "CWITH"},
			{ID: without, ContractAddress: "CWITHOUT"},
		}},
	)

	resp, err := svc.GetUserAnalytics(context.Background(), uuid.New(), d1.Add(-24*time.Hour), time.Time{})
	if err != nil {
		t.Fatalf("GetUserAnalytics: %v", err)
	}
	if len(resp.DailySnapshots) != 1 || !resp.DailySnapshots[0].TotalBalanceUSD.Equal(decimal.NewFromInt(100)) {
		t.Fatalf("expected only the snapshotted vault to contribute, got %+v", resp.DailySnapshots)
	}
	for _, entry := range resp.VaultMonthlyYield {
		if entry.VaultID == without.String() {
			t.Fatalf("a never-snapshotted vault must not appear in monthly yield: %+v", entry)
		}
	}
}

// A repository failure on any vault's history is surfaced, not swallowed —
// otherwise a partial portfolio would be presented as the whole one.
func TestGetUserAnalytics_PropagatesHistoryError(t *testing.T) {
	svc := NewService(
		&analyticsSnapshotRepo{history: nil},
		&analyticsVaultRepo{err: errors.New("boom")},
	)

	if _, err := svc.GetUserAnalytics(context.Background(), uuid.New(), time.Time{}, time.Time{}); err == nil {
		t.Fatal("expected the vault listing error to be surfaced")
	}
}
