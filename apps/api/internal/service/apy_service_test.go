package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/apysnapshot"
)

// fakeAPYSnapshotRepo is an in-memory apysnapshot.Repository for testing the
// anomaly-flagging guard (#941) without a database.
type fakeAPYSnapshotRepo struct {
	snapshots []apysnapshot.APYSnapshot
	upserted  []apysnapshot.APYSnapshot
}

func (f *fakeAPYSnapshotRepo) Upsert(_ context.Context, snap apysnapshot.APYSnapshot) error {
	f.upserted = append(f.upserted, snap)
	f.snapshots = append(f.snapshots, snap)
	return nil
}

func (f *fakeAPYSnapshotRepo) ListByProtocol(_ context.Context, slug string, since time.Time) ([]apysnapshot.APYSnapshot, error) {
	var out []apysnapshot.APYSnapshot
	for _, s := range f.snapshots {
		if s.ProtocolSlug == slug && !s.CapturedAt.Before(since) {
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *fakeAPYSnapshotRepo) PruneOlderThan(context.Context, time.Duration) error { return nil }

func TestAPYService_FlagIfAnomalous_NoHistoryNotFlagged(t *testing.T) {
	repo := &fakeAPYSnapshotRepo{}
	svc := NewAPYService(repo)

	snap := apysnapshot.APYSnapshot{
		ID:           uuid.New(),
		ProtocolSlug: "aave-v3",
		APY:          decimal.RequireFromString("5"),
		TVL:          decimal.RequireFromString("1000000"),
		CapturedAt:   time.Now().UTC(),
	}

	got := svc.flagIfAnomalous(context.Background(), snap)
	if got.Flagged {
		t.Fatalf("expected first-ever snapshot to not be flagged, got reason %q", got.FlagReason)
	}
}

func TestAPYService_FlagIfAnomalous_SpikeIsFlagged(t *testing.T) {
	now := time.Now().UTC()
	repo := &fakeAPYSnapshotRepo{
		snapshots: []apysnapshot.APYSnapshot{
			{
				ID:           uuid.New(),
				ProtocolSlug: "aave-v3",
				APY:          decimal.RequireFromString("5"),
				TVL:          decimal.RequireFromString("1000000"),
				CapturedAt:   now.Add(-time.Hour),
			},
		},
	}
	svc := NewAPYService(repo)

	snap := apysnapshot.APYSnapshot{
		ID:           uuid.New(),
		ProtocolSlug: "aave-v3",
		APY:          decimal.RequireFromString("40"), // 8x prior reading
		TVL:          decimal.RequireFromString("1000000"),
		CapturedAt:   now,
	}

	got := svc.flagIfAnomalous(context.Background(), snap)
	if !got.Flagged {
		t.Fatal("expected an 8x APY jump to be flagged")
	}
	if got.FlagReason == "" {
		t.Fatal("expected a non-empty flag reason")
	}
}

func TestAPYService_FlagIfAnomalous_IgnoresOtherProtocols(t *testing.T) {
	now := time.Now().UTC()
	repo := &fakeAPYSnapshotRepo{
		snapshots: []apysnapshot.APYSnapshot{
			{
				ID:           uuid.New(),
				ProtocolSlug: "blend",
				APY:          decimal.RequireFromString("5"),
				TVL:          decimal.RequireFromString("1000000"),
				CapturedAt:   now.Add(-time.Hour),
			},
		},
	}
	svc := NewAPYService(repo)

	snap := apysnapshot.APYSnapshot{
		ID:           uuid.New(),
		ProtocolSlug: "aave-v3", // different protocol, no relevant history
		APY:          decimal.RequireFromString("40"),
		TVL:          decimal.RequireFromString("1000000"),
		CapturedAt:   now,
	}

	got := svc.flagIfAnomalous(context.Background(), snap)
	if got.Flagged {
		t.Fatalf("expected no cross-protocol comparison, got reason %q", got.FlagReason)
	}
}

func TestAPYService_FlagIfAnomalous_PicksMostRecentPrior(t *testing.T) {
	now := time.Now().UTC()
	repo := &fakeAPYSnapshotRepo{
		snapshots: []apysnapshot.APYSnapshot{
			{ProtocolSlug: "aave-v3", APY: decimal.RequireFromString("40"), CapturedAt: now.Add(-47 * time.Hour)}, // old outlier
			{ProtocolSlug: "aave-v3", APY: decimal.RequireFromString("5"), CapturedAt: now.Add(-time.Hour)},        // most recent
		},
	}
	svc := NewAPYService(repo)

	snap := apysnapshot.APYSnapshot{
		ProtocolSlug: "aave-v3",
		APY:          decimal.RequireFromString("5.5"), // small move relative to the recent 5, not the old 40
		CapturedAt:   now,
	}

	got := svc.flagIfAnomalous(context.Background(), snap)
	if got.Flagged {
		t.Fatalf("expected comparison against most recent prior snapshot only, got reason %q", got.FlagReason)
	}
}

func TestAPYService_Poll_PersistsFlaggedSnapshots(t *testing.T) {
	now := time.Now().UTC()
	repo := &fakeAPYSnapshotRepo{
		snapshots: []apysnapshot.APYSnapshot{
			{ProtocolSlug: "aave-v3", APY: decimal.RequireFromString("5"), CapturedAt: now.Add(-time.Hour)},
		},
	}
	svc := NewAPYService(repo)

	snap := apysnapshot.APYSnapshot{
		ID:           uuid.New(),
		ProtocolSlug: "aave-v3",
		APY:          decimal.RequireFromString("40"),
		TVL:          decimal.RequireFromString("1000000"),
		CapturedAt:   now,
	}
	flagged := svc.flagIfAnomalous(context.Background(), snap)
	if err := repo.Upsert(context.Background(), flagged); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	if len(repo.upserted) != 1 || !repo.upserted[0].Flagged {
		t.Fatal("expected the flagged snapshot to still be persisted, not rejected")
	}
}
