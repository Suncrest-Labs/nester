package valuation

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/portfolio"
	"github.com/suncrestlabs/nester/apps/api/internal/freshness"
)

type fakePositions struct {
	calls int
	list  []Position
}

func (f *fakePositions) Positions(context.Context, uuid.UUID) ([]Position, error) {
	f.calls++
	return f.list, nil
}

type recordingNotifier struct {
	mu     sync.Mutex
	pushed []portfolio.Valuation
}

func (n *recordingNotifier) PushValuation(_ uuid.UUID, v portfolio.Valuation) {
	n.mu.Lock()
	n.pushed = append(n.pushed, v)
	n.mu.Unlock()
}

func (n *recordingNotifier) count() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.pushed)
}

func newTestService(pos *fakePositions, notifier Notifier) *Service {
	return NewService(Deps{
		Positions: pos,
		Oracle:    NewStaticOracle(nil),
		Cache:     NewCache(time.Minute),
		Notifier:  notifier,
	})
}

func TestService_GetValuationCaches(t *testing.T) {
	pos := &fakePositions{list: []Position{{VaultID: uuid.New(), Asset: "USDC", Principal: dec("10")}}}
	svc := newTestService(pos, nil)
	uid := uuid.New()

	if _, err := svc.GetValuation(context.Background(), uid); err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, err := svc.GetValuation(context.Background(), uid); err != nil {
		t.Fatalf("second: %v", err)
	}
	if pos.calls != 1 {
		t.Fatalf("position source hit %d times, want 1 (second served from cache)", pos.calls)
	}
}

func TestService_InvalidateRecomputesAndPushes(t *testing.T) {
	pos := &fakePositions{list: []Position{{VaultID: uuid.New(), Asset: "USDC", Principal: dec("10")}}}
	notifier := &recordingNotifier{}
	svc := newTestService(pos, notifier)
	uid := uuid.New()

	// Prime the cache.
	if _, err := svc.GetValuation(context.Background(), uid); err != nil {
		t.Fatalf("prime: %v", err)
	}

	svc.Invalidate(uid)

	// Invalidate recomputes+pushes asynchronously.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && notifier.count() == 0 {
		time.Sleep(2 * time.Millisecond)
	}
	if notifier.count() != 1 {
		t.Fatalf("expected 1 push after invalidation, got %d", notifier.count())
	}

	// The cache was refreshed by the recompute (calls: prime=1, recompute=1).
	if pos.calls != 2 {
		t.Fatalf("position source hit %d times, want 2", pos.calls)
	}
	if _, ok := svc.cache.Get(uid); !ok {
		t.Fatal("expected cache to be repopulated after invalidation recompute")
	}
}

// TestService_ValuationStaleness_Unknown covers nester#1109: a Service with
// no freshness reader configured must report StalenessUnknown, not a
// fabricated "fresh" — absence of a staleness signal is not evidence of
// freshness.
func TestService_ValuationStaleness_Unknown(t *testing.T) {
	pos := &fakePositions{list: []Position{{VaultID: uuid.New(), Asset: "USDC", Principal: dec("10")}}}
	svc := newTestService(pos, nil)

	val, err := svc.GetValuation(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("GetValuation: %v", err)
	}
	if val.Staleness != portfolio.StalenessUnknown {
		t.Fatalf("Staleness = %q, want %q", val.Staleness, portfolio.StalenessUnknown)
	}
	if val.AsOfLedger != 0 {
		t.Fatalf("AsOfLedger = %d, want 0 when unknown", val.AsOfLedger)
	}
}

// TestService_ValuationStaleness_Fresh covers the fresh case: a freshness
// tracker sampled within budget must report StalenessFresh and stamp the
// real indexed ledger onto the valuation.
func TestService_ValuationStaleness_Fresh(t *testing.T) {
	tracker := freshness.NewTracker(5 * time.Minute)
	tracker.Observe(1_000, 1_002)

	pos := &fakePositions{list: []Position{{VaultID: uuid.New(), Asset: "USDC", Principal: dec("10")}}}
	svc := NewService(Deps{
		Positions: pos,
		Oracle:    NewStaticOracle(nil),
		Cache:     NewCache(time.Minute),
		Freshness: tracker,
	})

	val, err := svc.GetValuation(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("GetValuation: %v", err)
	}
	if val.Staleness != portfolio.StalenessFresh {
		t.Fatalf("Staleness = %q, want %q", val.Staleness, portfolio.StalenessFresh)
	}
	if val.AsOfLedger != 1_000 {
		t.Fatalf("AsOfLedger = %d, want 1000", val.AsOfLedger)
	}
}

// TestService_ValuationStaleness_Stale covers the stale case: a freshness
// tracker sampled long enough ago (beyond its budget) must report
// StalenessStale while still surfacing the real (old) ledger it was last
// sampled at, rather than withholding it.
func TestService_ValuationStaleness_Stale(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }
	tracker := freshness.NewTrackerWithClock(time.Minute, clock)
	tracker.Observe(1_000, 1_000)
	now = now.Add(5 * time.Minute) // advance the clock well past the 1-minute budget

	pos := &fakePositions{list: []Position{{VaultID: uuid.New(), Asset: "USDC", Principal: dec("10")}}}
	svc := NewService(Deps{
		Positions: pos,
		Oracle:    NewStaticOracle(nil),
		Cache:     NewCache(time.Minute),
		Freshness: tracker,
	})

	val, err := svc.GetValuation(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("GetValuation: %v", err)
	}
	if val.Staleness != portfolio.StalenessStale {
		t.Fatalf("Staleness = %q, want %q", val.Staleness, portfolio.StalenessStale)
	}
	if val.AsOfLedger != 1_000 {
		t.Fatalf("AsOfLedger = %d, want 1000 (the last real sample, even though stale)", val.AsOfLedger)
	}
}
