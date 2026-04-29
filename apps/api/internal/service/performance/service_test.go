package performance

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	perfdom "github.com/suncrestlabs/nester/apps/api/internal/domain/performance"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

func TestCalculateRealizedAPY_BasicAnnualization(t *testing.T) {
	// 5% gain over 30 days annualizes to ~79% APY.
	current := decimal.NewFromFloat(105)
	deposited := decimal.NewFromFloat(100)
	apy := CalculateRealizedAPY(current, deposited, 30)

	got, _ := apy.Float64()
	expected := (math.Pow(1.05, 365.0/30.0) - 1) * 100
	if math.Abs(got-expected) > 0.01 {
		t.Fatalf("expected %.4f, got %.4f", expected, got)
	}
}

func TestCalculateRealizedAPY_ZeroDeposit(t *testing.T) {
	got := CalculateRealizedAPY(decimal.NewFromFloat(100), decimal.Zero, 30)
	if !got.IsZero() {
		t.Fatalf("expected 0 for zero deposit, got %s", got)
	}
}

func TestCalculateRealizedAPY_ZeroElapsed(t *testing.T) {
	got := CalculateRealizedAPY(decimal.NewFromFloat(105), decimal.NewFromFloat(100), 0)
	if !got.IsZero() {
		t.Fatalf("expected 0 for zero elapsed days, got %s", got)
	}
}

func TestCalculateRealizedAPY_NegativeReturn(t *testing.T) {
	// 10% loss over 30 days annualizes to a large negative number; we cap at -1000.
	got := CalculateRealizedAPY(decimal.NewFromFloat(90), decimal.NewFromFloat(100), 30)
	v, _ := got.Float64()
	if v >= 0 {
		t.Fatalf("expected negative APY for losses, got %.4f", v)
	}
	if v < -1001 {
		t.Fatalf("expected APY clamped to -1000, got %.4f", v)
	}
}

func TestCalculateRealizedAPY_ExtremeRatioClamped(t *testing.T) {
	// Tiny denominator could blow up the result; clamp at +10000.
	got := CalculateRealizedAPY(decimal.NewFromFloat(1_000_000), decimal.NewFromFloat(1), 1)
	v, _ := got.Float64()
	if v != 10000 {
		t.Fatalf("expected APY clamped to 10000, got %.4f", v)
	}
}

// --- snapshot worker tests below ---

type stubVaultLister struct {
	vaults []vault.Vault
	err    error
}

func (s *stubVaultLister) ListActive(_ context.Context) ([]vault.Vault, error) {
	return s.vaults, s.err
}

type stubBalanceProvider struct {
	balances map[string]decimal.Decimal
	err      error
}

func (s *stubBalanceProvider) VaultBalance(_ context.Context, addr string) (decimal.Decimal, error) {
	if s.err != nil {
		return decimal.Zero, s.err
	}
	return s.balances[addr], nil
}

type fakeRepo struct {
	snapshots []perfdom.Snapshot
	apyRows   map[perfdom.Period]perfdom.APYRecord
	insertErr error
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{apyRows: map[perfdom.Period]perfdom.APYRecord{}}
}

func (f *fakeRepo) Insert(_ context.Context, s perfdom.Snapshot) (perfdom.Snapshot, error) {
	if f.insertErr != nil {
		return perfdom.Snapshot{}, f.insertErr
	}
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	f.snapshots = append(f.snapshots, s)
	return s, nil
}

func (f *fakeRepo) LatestForVault(_ context.Context, vaultID uuid.UUID) (perfdom.Snapshot, error) {
	var latest perfdom.Snapshot
	found := false
	for _, s := range f.snapshots {
		if s.VaultID != vaultID {
			continue
		}
		if !found || s.SnapshotAt.After(latest.SnapshotAt) {
			latest = s
			found = true
		}
	}
	if !found {
		return perfdom.Snapshot{}, perfdom.ErrSnapshotNotFound
	}
	return latest, nil
}

func (f *fakeRepo) HistoryForVault(_ context.Context, vaultID uuid.UUID, since time.Time) ([]perfdom.Snapshot, error) {
	out := []perfdom.Snapshot{}
	for _, s := range f.snapshots {
		if s.VaultID == vaultID && !s.SnapshotAt.Before(since) {
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *fakeRepo) FirstAtOrAfter(_ context.Context, vaultID uuid.UUID, since time.Time) (perfdom.Snapshot, error) {
	var first perfdom.Snapshot
	found := false
	for _, s := range f.snapshots {
		if s.VaultID != vaultID {
			continue
		}
		if s.SnapshotAt.Before(since) {
			continue
		}
		if !found || s.SnapshotAt.Before(first.SnapshotAt) {
			first = s
			found = true
		}
	}
	if !found {
		return perfdom.Snapshot{}, perfdom.ErrSnapshotNotFound
	}
	return first, nil
}

func (f *fakeRepo) UpsertAPY(_ context.Context, rec perfdom.APYRecord) error {
	f.apyRows[rec.Period] = rec
	return nil
}

func (f *fakeRepo) ListAPY(_ context.Context, _ uuid.UUID) ([]perfdom.APYRecord, error) {
	out := []perfdom.APYRecord{}
	for _, v := range f.apyRows {
		out = append(out, v)
	}
	return out, nil
}

func TestTracker_TakeSnapshots_WritesSnapshotPerVault(t *testing.T) {
	now := time.Date(2026, 4, 26, 0, 0, 0, 0, time.UTC)
	id1, id2 := uuid.New(), uuid.New()

	vaults := &stubVaultLister{
		vaults: []vault.Vault{
			{ID: id1, ContractAddress: "C1", TotalDeposited: decimal.NewFromInt(100), CurrentBalance: decimal.NewFromInt(100), CreatedAt: now.Add(-90 * 24 * time.Hour)},
			{ID: id2, ContractAddress: "C2", TotalDeposited: decimal.NewFromInt(200), CurrentBalance: decimal.NewFromInt(200), CreatedAt: now.Add(-30 * 24 * time.Hour)},
		},
	}
	chain := &stubBalanceProvider{
		balances: map[string]decimal.Decimal{
			"C1": decimal.NewFromInt(110),
			"C2": decimal.NewFromInt(220),
		},
	}
	repo := newFakeRepo()

	tr := NewTracker(repo, vaults, chain, time.Hour)
	tr.SetClock(func() time.Time { return now })

	if err := tr.TakeSnapshots(context.Background()); err != nil {
		t.Fatalf("TakeSnapshots: %v", err)
	}
	if len(repo.snapshots) != 2 {
		t.Fatalf("expected 2 snapshots, got %d", len(repo.snapshots))
	}
	for _, s := range repo.snapshots {
		switch s.VaultID {
		case id1:
			if !s.TotalBalance.Equal(decimal.NewFromInt(110)) {
				t.Errorf("vault 1 balance: %s", s.TotalBalance)
			}
			if !s.TotalYieldEarned.Equal(decimal.NewFromInt(10)) {
				t.Errorf("vault 1 yield: %s", s.TotalYieldEarned)
			}
		case id2:
			if !s.TotalBalance.Equal(decimal.NewFromInt(220)) {
				t.Errorf("vault 2 balance: %s", s.TotalBalance)
			}
		}
	}
}

func TestTracker_TakeSnapshots_FallsBackOnChainError(t *testing.T) {
	now := time.Now().UTC()
	id := uuid.New()
	vaults := &stubVaultLister{
		vaults: []vault.Vault{
			{ID: id, ContractAddress: "C1", TotalDeposited: decimal.NewFromInt(100), CurrentBalance: decimal.NewFromInt(100), CreatedAt: now.Add(-30 * 24 * time.Hour)},
		},
	}
	chain := &stubBalanceProvider{err: errors.New("rpc unavailable")}

	repo := newFakeRepo()
	tr := NewTracker(repo, vaults, chain, time.Hour)
	tr.SetClock(func() time.Time { return now })

	if err := tr.TakeSnapshots(context.Background()); err != nil {
		t.Fatalf("TakeSnapshots: %v", err)
	}
	if len(repo.snapshots) != 1 {
		t.Fatalf("expected fallback snapshot to still be written, got %d", len(repo.snapshots))
	}
}

func TestService_Summary_NoSnapshotsReturnsZeros(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo)
	out, err := svc.Summary(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if !out.CurrentBalance.IsZero() {
		t.Fatalf("expected zero balance, got %s", out.CurrentBalance)
	}
	if out.LastSnapshotAt != nil {
		t.Fatalf("expected nil last_snapshot_at, got %v", out.LastSnapshotAt)
	}
}
