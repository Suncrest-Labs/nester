package freshness

import (
	"math"
	"sync"
	"testing"
	"time"
)

// fakeClock is a manually advanced clock. Every staleness assertion below is
// about a boundary measured in minutes; sleeping for them would make the suite
// unusable and the boundaries themselves untestable.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newTestTracker(budget time.Duration) (*Tracker, *fakeClock) {
	clock := newFakeClock()
	return NewTrackerWithClock(budget, clock.Now), clock
}

func TestFreshSampleIsNotStale(t *testing.T) {
	tracker, _ := newTestTracker(DefaultBudget)

	tracker.Observe(1_000, 1_002)

	got := tracker.Snapshot()
	if !got.Sampled {
		t.Fatal("snapshot reports no sample after Observe")
	}
	if got.LagLedgers != 2 {
		t.Fatalf("lag ledgers = %d, want 2", got.LagLedgers)
	}
	if want := 2 * LedgerCloseInterval; got.Lag != want {
		t.Fatalf("lag = %v, want %v", got.Lag, want)
	}
	if got.Stale {
		t.Fatalf("2 ledgers behind reported stale against a %v budget", got.Budget)
	}
}

// An indexed ledger ahead of the reported tip happens for a moment whenever
// the cursor advances between the tip read and the next sample. It is not
// negative lag, and it must not underflow uint64 into an enormous value.
func TestIndexedAheadOfTipIsZeroLag(t *testing.T) {
	tracker, _ := newTestTracker(DefaultBudget)

	tracker.Observe(1_005, 1_000)

	got := tracker.Snapshot()
	if got.LagLedgers != 0 {
		t.Fatalf("lag ledgers = %d, want 0", got.LagLedgers)
	}
	if got.Lag != 0 {
		t.Fatalf("lag = %v, want 0", got.Lag)
	}
}

// The staleness boundary, stated exactly: at the budget the data is still
// fresh, one nanosecond past it is stale. `>` not `>=`, matching the alert
// expression and the documented rule.
func TestStalenessBoundary(t *testing.T) {
	const budget = 5 * time.Minute

	cases := []struct {
		name      string
		elapsed   time.Duration
		wantStale bool
	}{
		{"below budget", budget - time.Second, false},
		{"exactly at budget", budget, false},
		{"one nanosecond past budget", budget + time.Nanosecond, true},
		{"far past budget", 2 * budget, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tracker, clock := newTestTracker(budget)
			// Zero ledger lag, so the whole of Lag is elapsed time and the
			// boundary is exercised precisely.
			tracker.Observe(1_000, 1_000)

			clock.Advance(tc.elapsed)

			got := tracker.Snapshot()
			if got.Lag != tc.elapsed {
				t.Fatalf("lag = %v, want %v", got.Lag, tc.elapsed)
			}
			if got.Stale != tc.wantStale {
				t.Fatalf("stale = %v, want %v (lag %v, budget %v)", got.Stale, tc.wantStale, got.Lag, budget)
			}
		})
	}
}

// The failure this whole issue exists to make visible: the indexer stops and
// nothing updates the freshness signal again. Lag must keep climbing on the
// clock alone, and must eventually cross the budget.
func TestLagClimbsWhileTheIndexerIsSilent(t *testing.T) {
	tracker, clock := newTestTracker(DefaultBudget)

	tracker.Observe(1_000, 1_001)
	first := tracker.Snapshot()

	clock.Advance(time.Minute)
	second := tracker.Snapshot()
	if second.Lag <= first.Lag {
		t.Fatalf("lag did not grow while the indexer was silent: %v then %v", first.Lag, second.Lag)
	}
	if second.Stale {
		t.Fatal("one minute of silence exceeded a five minute budget")
	}

	clock.Advance(10 * time.Minute)
	third := tracker.Snapshot()
	if !third.Stale {
		t.Fatalf("eleven minutes of silence reported fresh (lag %v, budget %v)", third.Lag, third.Budget)
	}
	// The ledger figures are still the last observed ones — the indexer never
	// said otherwise — which is exactly why the seconds view is the one that
	// pages.
	if third.LagLedgers != 1 {
		t.Fatalf("lag ledgers = %d, want the last observed 1", third.LagLedgers)
	}
}

// Before the first sample there is no position to report, and reporting zero
// ledgers of lag would claim the indexer is exactly at the tip. The seconds
// view still ages from process start, so an indexer that never runs at all
// goes stale rather than looking perfect forever.
func TestUnsampledTrackerAgesFromConstruction(t *testing.T) {
	tracker, clock := newTestTracker(DefaultBudget)

	initial := tracker.Snapshot()
	if initial.Sampled {
		t.Fatal("a tracker with no samples reports Sampled")
	}
	if initial.Stale {
		t.Fatal("a freshly constructed tracker is stale, which would page on every deploy")
	}

	clock.Advance(DefaultBudget + time.Second)

	got := tracker.Snapshot()
	if got.Sampled {
		t.Fatal("still reports Sampled with no observation")
	}
	if !got.Stale {
		t.Fatalf("an indexer that never reported is not stale after %v", got.Lag)
	}
	if got.LagLedgers != 0 || got.IndexedLedger != 0 || got.NetworkLedger != 0 {
		t.Fatalf("ledger fields populated without a sample: %+v", got)
	}
}

// A failed sample must not reset the age. If it did, a permanently failing
// sampler would hold the freshness signal at zero and silence the alert
// forever — the same frozen-value failure, arrived at from the error path.
func TestSampleFailureDoesNotRefreshTheSignal(t *testing.T) {
	tracker, clock := newTestTracker(DefaultBudget)

	tracker.Observe(1_000, 1_000)
	clock.Advance(4 * time.Minute)

	tracker.ObserveFailure()
	tracker.ObserveFailure()

	got := tracker.Snapshot()
	if got.SampleAge != 4*time.Minute {
		t.Fatalf("sample age = %v, want 4m: a failure reset the age", got.SampleAge)
	}
	if got.SampleErrors != 2 {
		t.Fatalf("sample errors = %d, want 2", got.SampleErrors)
	}

	clock.Advance(2 * time.Minute)
	if !tracker.Snapshot().Stale {
		t.Fatal("six minutes of failed samples reported fresh")
	}
}

func TestObserveRefreshesTheSignal(t *testing.T) {
	tracker, clock := newTestTracker(DefaultBudget)

	tracker.Observe(1_000, 1_000)
	clock.Advance(10 * time.Minute)
	if !tracker.Snapshot().Stale {
		t.Fatal("precondition: tracker should be stale before recovery")
	}

	tracker.Observe(1_100, 1_100)

	got := tracker.Snapshot()
	if got.Stale {
		t.Fatalf("a fresh sample did not clear staleness (lag %v)", got.Lag)
	}
	if got.SampleAge != 0 {
		t.Fatalf("sample age = %v, want 0 immediately after Observe", got.SampleAge)
	}
	if got.IndexedLedger != 1_100 {
		t.Fatalf("indexed ledger = %d, want 1100", got.IndexedLedger)
	}
}

// Ledger lag contributes to the seconds figure at the nominal close interval,
// so the two units the issue asks for describe the same budget.
func TestLedgerLagContributesToSeconds(t *testing.T) {
	tracker, _ := newTestTracker(DefaultBudget)

	// 60 ledgers is the documented ledger-lag SLO; at a 5s close interval it
	// is exactly the 5 minute budget, so it must sit on the boundary rather
	// than over it.
	tracker.Observe(1_000, 1_060)

	got := tracker.Snapshot()
	if want := 5 * time.Minute; got.Lag != want {
		t.Fatalf("lag = %v, want %v", got.Lag, want)
	}
	if got.Stale {
		t.Fatal("60 ledgers of lag is exactly the budget and must not be stale")
	}
}

// A corrupt cursor must not wrap the duration arithmetic negative, which would
// report a completely dead indexer as fresh.
func TestAbsurdLedgerLagSaturatesRatherThanWrapping(t *testing.T) {
	tracker, _ := newTestTracker(DefaultBudget)

	tracker.Observe(1, math.MaxUint64)

	got := tracker.Snapshot()
	if got.Lag < 0 {
		t.Fatalf("lag wrapped negative: %v", got.Lag)
	}
	if !got.Stale {
		t.Fatalf("an absurd lag of %v was reported fresh", got.Lag)
	}
}

// A clock that steps backwards (NTP correction, VM restore) must not make the
// data look fresher than it is.
func TestBackwardsClockDoesNotReportNegativeAge(t *testing.T) {
	tracker, clock := newTestTracker(DefaultBudget)

	tracker.Observe(1_000, 1_000)
	clock.Advance(-time.Hour)

	got := tracker.Snapshot()
	if got.SampleAge != 0 {
		t.Fatalf("sample age = %v, want 0 after the clock stepped backwards", got.SampleAge)
	}
	if got.Lag < 0 {
		t.Fatalf("lag = %v, want a non-negative duration", got.Lag)
	}
}

func TestNonPositiveBudgetFallsBackToDefault(t *testing.T) {
	for _, budget := range []time.Duration{0, -time.Second} {
		tracker := NewTracker(budget)
		if got := tracker.Budget(); got != DefaultBudget {
			t.Fatalf("budget for %v = %v, want %v", budget, got, DefaultBudget)
		}
	}
}

// A nil tracker is safe on every path, so a deployment wired without an
// indexer degrades to "no freshness information" rather than panicking in the
// request path.
func TestNilTrackerIsSafe(t *testing.T) {
	var tracker *Tracker

	tracker.Observe(1, 2)
	tracker.ObserveFailure()

	got := tracker.Snapshot()
	if got.Budget != DefaultBudget {
		t.Fatalf("nil tracker budget = %v, want %v", got.Budget, DefaultBudget)
	}
	if got.Sampled {
		t.Fatal("nil tracker reports a sample")
	}
}

// The indexer writes while the metrics scrape and every API request read. Run
// under -race, this is what proves that is safe.
func TestConcurrentObserveAndSnapshot(t *testing.T) {
	tracker := NewTracker(DefaultBudget)

	var wg sync.WaitGroup
	for writer := 0; writer < 4; writer++ {
		wg.Add(1)
		go func(base uint64) {
			defer wg.Done()
			for i := uint64(0); i < 500; i++ {
				tracker.Observe(base+i, base+i+3)
				tracker.ObserveFailure()
			}
		}(uint64(writer) * 1000)
	}
	for reader := 0; reader < 4; reader++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				_ = tracker.Snapshot()
			}
		}()
	}
	wg.Wait()

	if got := tracker.Snapshot().SampleErrors; got != 2000 {
		t.Fatalf("sample errors = %d, want 2000", got)
	}
}
