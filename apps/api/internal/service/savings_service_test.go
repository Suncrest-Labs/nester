package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/savings"
)

type fakeSavingsChain struct {
	state    savings.ChainState
	stateErr error
	balance  decimal.Decimal
	rate     decimal.Decimal
}

func (f *fakeSavingsChain) State(context.Context) (savings.ChainState, error) {
	return f.state, f.stateErr
}

func (f *fakeSavingsChain) Balance(context.Context, string) (decimal.Decimal, error) {
	return f.balance, nil
}

func (f *fakeSavingsChain) ConvertToAssets(_ context.Context, shares decimal.Decimal) (decimal.Decimal, error) {
	return shares.Mul(f.rate), nil
}

type fakeSavingsRepo struct {
	snaps    []savings.Snapshot
	inserted []savings.Snapshot
}

func (f *fakeSavingsRepo) Insert(_ context.Context, s savings.Snapshot) error {
	f.inserted = append(f.inserted, s)
	return nil
}

func (f *fakeSavingsRepo) Since(_ context.Context, t time.Time) ([]savings.Snapshot, error) {
	var out []savings.Snapshot
	for _, s := range f.snaps {
		if !s.TakenAt.Before(t) {
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *fakeSavingsRepo) Latest(context.Context) (savings.Snapshot, error) {
	return savings.Snapshot{}, savings.ErrSnapshotNotFound
}

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func sampleChainState() savings.ChainState {
	return savings.ChainState{
		SharePrice:    dec("10010000"),
		TotalAssets:   dec("50000000000"),
		TotalSupply:   dec("49950000000"),
		Idle:          dec("10000000000"),
		ReserveShares: dec("20000000"),
		FeeBps:        1000,
		Timelock:      72 * time.Hour,
		Adapters: []savings.AdapterState{
			{Address: "CADAPTER", Name: "Blend", Value: dec("40000000000"), AbsCap: dec("90000000000"), RelCapBps: 5000},
		},
		Pending: []savings.PendingChange{{ID: 3, Kind: "SetFee", ETA: time.Unix(1_800_000_000, 0).UTC()}},
	}
}

func TestSavingsService_Snapshot_InsertsChainState(t *testing.T) {
	repo := &fakeSavingsRepo{}
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	svc := NewSavingsService(&fakeSavingsChain{state: sampleChainState()}, repo)
	svc.clock = func() time.Time { return now }

	if err := svc.Snapshot(context.Background()); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(repo.inserted) != 1 {
		t.Fatalf("inserted %d snapshots, want 1", len(repo.inserted))
	}
	got := repo.inserted[0]
	if !got.TakenAt.Equal(now) || !got.SharePrice.Equal(dec("10010000")) ||
		!got.TotalAssets.Equal(dec("50000000000")) || !got.TotalSupply.Equal(dec("49950000000")) ||
		!got.ReserveShares.Equal(dec("20000000")) {
		t.Fatalf("snapshot = %+v", got)
	}
}

func TestSavingsService_Snapshot_ChainErrorInsertsNothing(t *testing.T) {
	repo := &fakeSavingsRepo{}
	svc := NewSavingsService(&fakeSavingsChain{stateErr: errors.New("rpc down")}, repo)
	if err := svc.Snapshot(context.Background()); err == nil {
		t.Fatal("want error")
	}
	if len(repo.inserted) != 0 {
		t.Fatalf("inserted %d snapshots on chain error", len(repo.inserted))
	}
}

func TestSavingsService_Summary_UnitsAndShares(t *testing.T) {
	now := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	repo := &fakeSavingsRepo{snaps: []savings.Snapshot{
		{TakenAt: now.Add(-7 * 24 * time.Hour), SharePrice: dec("10000000")},
		{TakenAt: now, SharePrice: dec("10010000")},
	}}
	svc := NewSavingsService(&fakeSavingsChain{state: sampleChainState(), rate: dec("1.001")}, repo)
	svc.clock = func() time.Time { return now }

	got, err := svc.Summary(context.Background())
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if got.Rate7d == nil || got.Rate30d == nil {
		t.Fatalf("rates = %v / %v, want both set", got.Rate7d, got.Rate30d)
	}
	if got.SharePrice != "1.001" || got.TotalAssets != "5000" || got.Idle != "1000" {
		t.Errorf("price/assets/idle = %s/%s/%s", got.SharePrice, got.TotalAssets, got.Idle)
	}
	if got.FeeBps != 1000 || got.TimelockSeconds != 259200 {
		t.Errorf("fee/timelock = %d/%d", got.FeeBps, got.TimelockSeconds)
	}
	if got.Reserve.Shares != "2" || got.Reserve.Value != "2.002" {
		t.Errorf("reserve = %+v", got.Reserve)
	}
	if len(got.Adapters) != 1 {
		t.Fatalf("adapters = %d", len(got.Adapters))
	}
	a := got.Adapters[0]
	if a.Name != "Blend" || a.Value != "4000" || a.AbsCap != "9000" || a.RelCapBps != 5000 || a.Share != "0.8" {
		t.Errorf("adapter = %+v", a)
	}
	if len(got.Pending) != 1 || got.Pending[0].ID != 3 || got.Pending[0].Kind != "SetFee" || got.Pending[0].ETA != 1_800_000_000 {
		t.Errorf("pending = %+v", got.Pending)
	}
}

func TestSavingsService_Summary_ZeroAssetsAdapterShareZero(t *testing.T) {
	st := sampleChainState()
	st.TotalAssets = decimal.Zero
	st.Adapters[0].Value = decimal.Zero
	svc := NewSavingsService(&fakeSavingsChain{state: st, rate: dec("1")}, &fakeSavingsRepo{})
	got, err := svc.Summary(context.Background())
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if got.Adapters[0].Share != "0" {
		t.Errorf("share = %s, want 0", got.Adapters[0].Share)
	}
	if got.Rate7d != nil || got.Rate30d != nil {
		t.Errorf("fresh deploy rates should be nil")
	}
}

func TestSavingsService_History(t *testing.T) {
	now := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	repo := &fakeSavingsRepo{snaps: []savings.Snapshot{
		{TakenAt: now.Add(-10 * 24 * time.Hour), SharePrice: dec("10000000")},
		{TakenAt: now.Add(-time.Hour), SharePrice: dec("10010000")},
	}}
	svc := NewSavingsService(&fakeSavingsChain{}, repo)
	svc.clock = func() time.Time { return now }

	pts, err := svc.History(context.Background(), "7d")
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(pts) != 1 || pts[0].SharePrice != "1.001" || !pts[0].TakenAt.Equal(now.Add(-time.Hour)) {
		t.Fatalf("points = %+v", pts)
	}
	if _, err := svc.History(context.Background(), "1y"); !errors.Is(err, ErrInvalidSavingsPeriod) {
		t.Fatalf("bad period err = %v", err)
	}
}

func TestSavingsService_Position(t *testing.T) {
	svc := NewSavingsService(&fakeSavingsChain{balance: dec("25000000"), rate: dec("1.001")}, &fakeSavingsRepo{})
	got, err := svc.Position(context.Background(), "GWALLET")
	if err != nil {
		t.Fatalf("Position: %v", err)
	}
	if got.Shares != "2.5" || got.Value != "2.5025" || got.Deposited != nil {
		t.Fatalf("position = %+v", got)
	}
}

type fixedLeader struct{ leader bool }

func (c fixedLeader) IsLeader() bool { return c.leader }

func TestSavingsService_SnapshotTick_SkipsWhenNotLeader(t *testing.T) {
	repo := &fakeSavingsRepo{}
	svc := NewSavingsService(&fakeSavingsChain{state: sampleChainState()}, repo)
	svc.SetLeaderChecker(fixedLeader{leader: false})
	svc.SnapshotTick(context.Background())
	if len(repo.inserted) != 0 {
		t.Fatal("non-leader inserted a snapshot")
	}
	svc.SetLeaderChecker(fixedLeader{leader: true})
	svc.SnapshotTick(context.Background())
	if len(repo.inserted) != 1 {
		t.Fatal("leader did not insert a snapshot")
	}
}
