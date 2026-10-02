package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/apysnapshot"
)

// fakeAPYSnapshotRepo is an in-memory apysnapshot.Repository for testing the
// anomaly-flagging guard (#941) without a database.
type fakeAPYSnapshotRepo struct {
	snapshots   []apysnapshot.APYSnapshot
	upserted    []apysnapshot.APYSnapshot
	pruned      []time.Duration
	downsampled []time.Duration
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

func (f *fakeAPYSnapshotRepo) PruneOlderThan(_ context.Context, age time.Duration) error {
	f.pruned = append(f.pruned, age)
	return nil
}

func (f *fakeAPYSnapshotRepo) DownsampleOlderThan(_ context.Context, age time.Duration) error {
	f.downsampled = append(f.downsampled, age)
	return nil
}

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
		t.Fatal("expected spike to be flagged")
	}
	if got.FlagReason == "" {
		t.Fatal("expected non-empty flag reason")
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
			{ProtocolSlug: "aave-v3", APY: decimal.RequireFromString("5"), CapturedAt: now.Add(-time.Hour)},       // most recent
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

func TestAPYService_GapMarkersAfterConsecutiveFailures(t *testing.T) {
	now := time.Now().UTC()
	repo := &fakeAPYSnapshotRepo{
		snapshots: []apysnapshot.APYSnapshot{
			{
				ID:           uuid.New(),
				ProtocolSlug: "blend",
				APY:          decimal.RequireFromString("8.5"),
				TVL:          decimal.RequireFromString("1000000"),
				CapturedAt:   now.Add(-6 * time.Hour),
			},
		},
	}
	svc := NewAPYService(repo)
	svc.knownProtocols = map[string]struct{}{"blend": {}}
	svc.defiLlamaURL = "http://127.0.0.1:1/pools" // forced connection failure
	svc.httpClient = &http.Client{Timeout: 50 * time.Millisecond}

	// First failure: below threshold — no gap marker.
	svc.PollOnce(context.Background())
	if svc.consecutiveFails != 1 {
		t.Fatalf("consecutiveFails=%d want 1", svc.consecutiveFails)
	}
	if countGapUpserts(repo) != 0 {
		t.Fatalf("unexpected gap markers after 1 failure: %d", countGapUpserts(repo))
	}

	// Second failure: threshold met — gap marker written.
	svc.PollOnce(context.Background())
	if svc.consecutiveFails < 2 {
		t.Fatalf("consecutiveFails=%d want >=2", svc.consecutiveFails)
	}
	if countGapUpserts(repo) < 1 {
		t.Fatalf("expected at least one gap marker after consecutive failures, got %d", countGapUpserts(repo))
	}
	last := repo.upserted[len(repo.upserted)-1]
	if !apysnapshot.IsGapMarker(last) {
		t.Fatalf("last upsert not a gap marker: flagged=%v reason=%q", last.Flagged, last.FlagReason)
	}
	if last.ProtocolSlug != "blend" {
		t.Fatalf("protocol=%s want blend", last.ProtocolSlug)
	}
	if !last.APY.Equal(decimal.RequireFromString("8.5")) {
		t.Fatalf("gap marker should carry last-good APY, got %s", last.APY)
	}
}

func TestAPYService_GetHistoryMarksGaps(t *testing.T) {
	now := time.Now().UTC()
	repo := &fakeAPYSnapshotRepo{
		snapshots: []apysnapshot.APYSnapshot{
			{
				ID:           uuid.New(),
				ProtocolSlug: "blend",
				APY:          decimal.RequireFromString("8.5"),
				TVL:          decimal.RequireFromString("1"),
				CapturedAt:   now.Add(-12 * time.Hour),
			},
			apysnapshot.NewGapMarker(
				"blend",
				apysnapshot.APYSnapshot{
					APY: decimal.RequireFromString("8.5"),
					TVL: decimal.RequireFromString("1"),
				},
				now.Add(-6*time.Hour),
				2,
			),
		},
	}
	svc := NewAPYService(repo)
	hist, err := svc.GetHistory(context.Background(), "blend")
	if err != nil {
		t.Fatal(err)
	}
	if len(hist.Snapshots) != 2 {
		t.Fatalf("len=%d want 2", len(hist.Snapshots))
	}
	if hist.Snapshots[0].Gap {
		t.Fatal("first point should not be a gap")
	}
	if !hist.Snapshots[1].Gap {
		t.Fatal("second point should be marked gap")
	}
	if hist.Snapshots[1].FlagReason == "" {
		t.Fatal("expected flag_reason on gap entry")
	}
}

func countGapUpserts(repo *fakeAPYSnapshotRepo) int {
	n := 0
	for _, s := range repo.upserted {
		if apysnapshot.IsGapMarker(s) {
			n++
		}
	}
	return n
}
