package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/performance"
	"github.com/suncrestlabs/nester/apps/api/internal/notifications"
)

// --- test doubles ---

type recordingAPYNotifier struct {
	calls []apyNotifierCall
}

type apyNotifierCall struct {
	userID uuid.UUID
	title  string
	body   string
}

func (r *recordingAPYNotifier) Send(_ context.Context, userID uuid.UUID, _ notifications.EventType, title, body string, _ map[string]any) error {
	r.calls = append(r.calls, apyNotifierCall{userID: userID, title: title, body: body})
	return nil
}

type recordingAPYUpdater struct {
	calls []uuid.UUID
}

func (r *recordingAPYUpdater) UpdateAPYAlertSentAt(_ context.Context, vaultID uuid.UUID, _ time.Time) error {
	r.calls = append(r.calls, vaultID)
	return nil
}

// makeSnapshots creates n snapshots with linearly increasing share prices.
// The APY of every consecutive pair is roughly the same (controlled by priceDelta).
func makeSnapshots(n int, basePrice, priceDelta float64) []performance.Snapshot {
	snaps := make([]performance.Snapshot, n)
	t := time.Now().UTC().Add(-time.Duration(n) * 24 * time.Hour)
	for i := range snaps {
		price, _ := decimal.NewFromFloat(basePrice + float64(i)*priceDelta).Float64()
		snaps[i] = performance.Snapshot{
			ID:         uuid.New(),
			VaultID:    uuid.New(),
			SharePrice: decimal.NewFromFloat(price),
			SnapshotAt: t.Add(time.Duration(i) * 24 * time.Hour),
		}
	}
	return snaps
}

// --- computeAPYStats ---

func TestComputeAPYStats_TooFewSnapshots(t *testing.T) {
	_, _, ok := computeAPYStats(nil)
	if ok {
		t.Fatal("expected ok=false for nil snapshots")
	}
	_, _, ok = computeAPYStats(makeSnapshots(1, 1.0, 0.01))
	if ok {
		t.Fatal("expected ok=false for single snapshot")
	}
}

func TestComputeAPYStats_StableGrowth(t *testing.T) {
	// 31 snapshots of steady growth → all daily APYs are equal.
	snaps := makeSnapshots(31, 1.0, 0.001)
	mean, latest, ok := computeAPYStats(snaps)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if mean <= 0 {
		t.Fatalf("mean=%v, want > 0", mean)
	}
	// For uniform growth, mean and latest APY should be very close.
	diff := mean - latest
	if diff < 0 {
		diff = -diff
	}
	if diff > 1.0 {
		t.Fatalf("mean=%v latest=%v: expected roughly equal for uniform growth", mean, latest)
	}
}

// --- threshold comparison ---

func TestAPYDeviationJob_FiresWhenDropExceedsThreshold(t *testing.T) {
	ctx := context.Background()
	vaultID := uuid.New()
	userID := uuid.New()

	// Build snapshots where the last interval has zero growth (APY ≈ 0)
	// vs a mean close to a positive value.
	// 30 high-growth days + 1 flat day = latest APY ≈ 0, mean >> 0
	snaps := makeSnapshots(30, 1.0, 0.01) // 30 days of growth
	// add a flat snapshot (same price as last)
	lastPrice := snaps[len(snaps)-1].SharePrice
	snaps = append(snaps, performance.Snapshot{
		SharePrice: lastPrice,
		SnapshotAt: snaps[len(snaps)-1].SnapshotAt.Add(24 * time.Hour),
	})

	notifier := &recordingAPYNotifier{}
	updater := &recordingAPYUpdater{}

	job := NewAPYDeviationJob(
		APYDeviationConfig{Enabled: true, Interval: time.Hour, ThresholdPct: 20},
		VaultAPYListerFunc(func(_ context.Context) ([]APYVaultInfo, error) {
			return []APYVaultInfo{{ID: vaultID, UserID: userID, Currency: "USDC"}}, nil
		}),
		APYSnapshotReaderFunc(func(_ context.Context, _ uuid.UUID, _ time.Time) ([]performance.Snapshot, error) {
			return snaps, nil
		}),
		VaultAPYUpdaterFunc(updater.UpdateAPYAlertSentAt),
		notifier,
		nil,
	)

	job.tick(ctx)

	if len(notifier.calls) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(notifier.calls))
	}
	if notifier.calls[0].userID != userID {
		t.Fatalf("notification sent to wrong user")
	}
	if len(updater.calls) != 1 || updater.calls[0] != vaultID {
		t.Fatalf("expected alert timestamp to be recorded for vault")
	}
}

func TestAPYDeviationJob_SkipsWhenDropBelowThreshold(t *testing.T) {
	ctx := context.Background()

	// All steady growth → drop = 0, should not fire.
	snaps := makeSnapshots(31, 1.0, 0.001)
	notifier := &recordingAPYNotifier{}

	job := NewAPYDeviationJob(
		APYDeviationConfig{Enabled: true, Interval: time.Hour, ThresholdPct: 20},
		VaultAPYListerFunc(func(_ context.Context) ([]APYVaultInfo, error) {
			return []APYVaultInfo{{ID: uuid.New(), UserID: uuid.New(), Currency: "USDC"}}, nil
		}),
		APYSnapshotReaderFunc(func(_ context.Context, _ uuid.UUID, _ time.Time) ([]performance.Snapshot, error) {
			return snaps, nil
		}),
		VaultAPYUpdaterFunc(func(_ context.Context, _ uuid.UUID, _ time.Time) error { return nil }),
		notifier,
		nil,
	)

	job.tick(ctx)

	if len(notifier.calls) != 0 {
		t.Fatalf("expected 0 notifications, got %d", len(notifier.calls))
	}
}

// --- deduplication ---

func TestAPYDeviationJob_Deduplication_SkipsWithinCooldown(t *testing.T) {
	ctx := context.Background()

	// Simulate a drop that would fire, but last alert was 1h ago (< 24h cooldown).
	recentAlert := time.Now().UTC().Add(-1 * time.Hour)
	snaps := makeSnapshots(30, 1.0, 0.01)
	lastPrice := snaps[len(snaps)-1].SharePrice
	snaps = append(snaps, performance.Snapshot{
		SharePrice: lastPrice,
		SnapshotAt: snaps[len(snaps)-1].SnapshotAt.Add(24 * time.Hour),
	})

	notifier := &recordingAPYNotifier{}

	job := NewAPYDeviationJob(
		APYDeviationConfig{Enabled: true, Interval: time.Hour, ThresholdPct: 20},
		VaultAPYListerFunc(func(_ context.Context) ([]APYVaultInfo, error) {
			return []APYVaultInfo{{
				ID:                 uuid.New(),
				UserID:             uuid.New(),
				Currency:           "USDC",
				LastAPYAlertSentAt: &recentAlert,
			}}, nil
		}),
		APYSnapshotReaderFunc(func(_ context.Context, _ uuid.UUID, _ time.Time) ([]performance.Snapshot, error) {
			return snaps, nil
		}),
		VaultAPYUpdaterFunc(func(_ context.Context, _ uuid.UUID, _ time.Time) error { return nil }),
		notifier,
		nil,
	)

	job.tick(ctx)

	if len(notifier.calls) != 0 {
		t.Fatalf("expected 0 notifications within cooldown, got %d", len(notifier.calls))
	}
}

func TestAPYDeviationJob_Deduplication_FiresAfterCooldown(t *testing.T) {
	ctx := context.Background()

	// Last alert was 25h ago → cooldown expired → should fire.
	oldAlert := time.Now().UTC().Add(-25 * time.Hour)
	snaps := makeSnapshots(30, 1.0, 0.01)
	lastPrice := snaps[len(snaps)-1].SharePrice
	snaps = append(snaps, performance.Snapshot{
		SharePrice: lastPrice,
		SnapshotAt: snaps[len(snaps)-1].SnapshotAt.Add(24 * time.Hour),
	})

	notifier := &recordingAPYNotifier{}

	job := NewAPYDeviationJob(
		APYDeviationConfig{Enabled: true, Interval: time.Hour, ThresholdPct: 20},
		VaultAPYListerFunc(func(_ context.Context) ([]APYVaultInfo, error) {
			return []APYVaultInfo{{
				ID:                 uuid.New(),
				UserID:             uuid.New(),
				Currency:           "USDC",
				LastAPYAlertSentAt: &oldAlert,
			}}, nil
		}),
		APYSnapshotReaderFunc(func(_ context.Context, _ uuid.UUID, _ time.Time) ([]performance.Snapshot, error) {
			return snaps, nil
		}),
		VaultAPYUpdaterFunc(func(_ context.Context, _ uuid.UUID, _ time.Time) error { return nil }),
		notifier,
		nil,
	)

	job.tick(ctx)

	if len(notifier.calls) != 1 {
		t.Fatalf("expected 1 notification after cooldown, got %d", len(notifier.calls))
	}
}
