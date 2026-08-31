package scheduler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/protocoltvl"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

// ── Fakes ────────────────────────────────────────────────────────────────

type fakeActiveVaults struct {
	vaults []vault.Vault
	err    error
}

func (f fakeActiveVaults) ListActive(_ context.Context) ([]vault.Vault, error) {
	return f.vaults, f.err
}

type fakeTVL struct{}

func (fakeTVL) ProtocolTVL(_ context.Context, _ string) (float64, error) {
	// No TVL data — the degraded-source path must run regardless.
	return 0, errors.New("no tvl")
}

type degradedFakeTVLRepo struct{}

func (degradedFakeTVLRepo) InsertSnapshot(context.Context, string, float64) error { return nil }
func (degradedFakeTVLRepo) SnapshotAt(context.Context, string, time.Time) (*protocoltvl.Snapshot, error) {
	return nil, nil
}
func (degradedFakeTVLRepo) LatestSnapshot(context.Context, string) (*protocoltvl.Snapshot, error) {
	return nil, nil
}
func (degradedFakeTVLRepo) ListSince(context.Context, string, time.Time) ([]protocoltvl.Snapshot, error) {
	return nil, nil
}
func (degradedFakeTVLRepo) CanAlert(context.Context, string) (bool, error) { return true, nil }
func (degradedFakeTVLRepo) RecordAlert(context.Context, string) error      { return nil }

type noopHealthNotifier struct{}

func (noopHealthNotifier) NotifyProtocolHealth(context.Context, uuid.UUID, string, float64, float64) error {
	return nil
}

type fakeDegradedLister struct {
	mu      sync.Mutex
	sources []DegradedSource
	err     error
}

func (f *fakeDegradedLister) ListDegradedSources(_ context.Context) ([]DegradedSource, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sources, f.err
}

func (f *fakeDegradedLister) set(sources []DegradedSource) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sources = sources
}

type recordingDegradedNotifier struct {
	mu    sync.Mutex
	calls []string
}

func (r *recordingDegradedNotifier) NotifySourceDegraded(_ context.Context, _ uuid.UUID, slug string, _ uint32) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, slug)
	return nil
}

func (r *recordingDegradedNotifier) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

// ── Helpers ──────────────────────────────────────────────────────────────

func checkerWithDegraded(
	lister DegradedSourceLister,
	notifier DegradedSourceNotifier,
) (*ProtocolHealthChecker, uuid.UUID) {
	userID := uuid.New()
	vaults := fakeActiveVaults{vaults: []vault.Vault{{
		ID:     uuid.New(),
		UserID: userID,
		Allocations: []vault.Allocation{
			{Protocol: "blend", Amount: decimal.NewFromInt(1000)},
		},
	}}}

	j := NewProtocolHealthChecker(
		ProtocolHealthConfig{Enabled: true, Interval: time.Minute},
		vaults, fakeTVL{}, degradedFakeTVLRepo{}, noopHealthNotifier{}, nil,
	).WithDegradedSources(lister, notifier)

	return j, userID
}

// waitForCount polls until the expected notification count is reached, since
// notifications are dispatched on goroutines.
func waitForCount(t *testing.T, n *recordingDegradedNotifier, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if n.count() >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d notifications, got %d", want, n.count())
}

// ── Tests ────────────────────────────────────────────────────────────────

func TestDegradedSourceAlertsAffectedUsers(t *testing.T) {
	lister := &fakeDegradedLister{sources: []DegradedSource{
		{SourceID: "blend", FailureCount: 4, LastFailureAt: time.Now()},
	}}
	notifier := &recordingDegradedNotifier{}
	j, _ := checkerWithDegraded(lister, notifier)

	j.Tick(context.Background())
	waitForCount(t, notifier, 1)
}

// A source that stays degraded must not re-alert on every 30-minute tick:
// recovery is a manual admin action, so the condition persists by design.
func TestDegradedSourceAlertsOncePerEpisode(t *testing.T) {
	lister := &fakeDegradedLister{sources: []DegradedSource{
		{SourceID: "blend", FailureCount: 4},
	}}
	notifier := &recordingDegradedNotifier{}
	j, _ := checkerWithDegraded(lister, notifier)

	ctx := context.Background()
	j.Tick(ctx)
	waitForCount(t, notifier, 1)
	j.Tick(ctx)
	j.Tick(ctx)

	time.Sleep(50 * time.Millisecond)
	if got := notifier.count(); got != 1 {
		t.Fatalf("expected 1 alert for a persistent degradation, got %d", got)
	}
}

// Once an admin recovers a source it leaves the degraded list; a later
// degradation of the same source must alert again.
func TestRecoveredSourceCanAlertAgain(t *testing.T) {
	lister := &fakeDegradedLister{sources: []DegradedSource{
		{SourceID: "blend", FailureCount: 4},
	}}
	notifier := &recordingDegradedNotifier{}
	j, _ := checkerWithDegraded(lister, notifier)

	ctx := context.Background()
	j.Tick(ctx)
	waitForCount(t, notifier, 1)

	// Admin recovers the source.
	lister.set(nil)
	j.Tick(ctx)

	// It degrades again later.
	lister.set([]DegradedSource{{SourceID: "blend", FailureCount: 4}})
	j.Tick(ctx)
	waitForCount(t, notifier, 2)
}

func TestDegradedListerErrorDoesNotPanic(t *testing.T) {
	lister := &fakeDegradedLister{err: errors.New("rpc down")}
	notifier := &recordingDegradedNotifier{}
	j, _ := checkerWithDegraded(lister, notifier)

	j.Tick(context.Background())
	if notifier.count() != 0 {
		t.Fatal("no alerts expected when the degraded feed is unavailable")
	}
}

// Without the on-chain feed wired, the checker keeps doing TVL-only work.
func TestCheckerWorksWithoutDegradedFeed(t *testing.T) {
	j := NewProtocolHealthChecker(
		ProtocolHealthConfig{Enabled: true, Interval: time.Minute},
		fakeActiveVaults{vaults: []vault.Vault{{
			ID:          uuid.New(),
			UserID:      uuid.New(),
			Allocations: []vault.Allocation{{Protocol: "blend"}},
		}}},
		fakeTVL{}, degradedFakeTVLRepo{}, noopHealthNotifier{}, nil,
	)
	j.Tick(context.Background())
}
