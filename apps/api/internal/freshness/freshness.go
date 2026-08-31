// Package freshness owns the single definition of how stale Nester's indexed
// view of the chain is (nester#1088).
//
// Three consumers read the same model, and that is the point: the Prometheus
// metrics, the staleness alert, and the freshness headers the API returns to
// clients must never disagree about whether balances are current. Before this
// package the lag threshold lived only in a Prometheus expression, so the API
// had no way to tell a client that the balance it was serving was stale.
//
// The model has two inputs, both taken from the indexer:
//
//	indexedLedger — the cursor the indexer has committed (system_state)
//	networkLedger — the tip the Soroban RPC reported at that moment
//
// and one derived quantity, staleness in wall time:
//
//	lag = (now - sampledAt) + (networkLedger - indexedLedger) * LedgerCloseInterval
//
// The first term is how long it has been since freshness was verified at all;
// the second is how far behind the chain the indexer was when it last looked.
// Both terms are required. Dropping the first would let the number freeze at a
// healthy value the moment the indexer died — the exact failure this issue
// exists to eliminate — and dropping the second would report a busily-polling
// but hopelessly-behind indexer as perfectly fresh.
//
// Because the first term grows with the clock, lag is computed on read rather
// than pushed on a timer. A dead indexer therefore produces a climbing number
// with no goroutine of ours still running to produce it.
package freshness

import (
	"math"
	"sync"
	"time"
)

// LedgerCloseInterval is the nominal Stellar ledger close time. It converts
// ledger lag into wall time so that one budget, stated in seconds, can govern
// both units.
//
// Real close times vary by a few hundred milliseconds, so this is an estimate
// and the derived seconds figure inherits that error. At the scale the budget
// operates on — minutes — the error is immaterial, and it is the same 5s
// figure the SLO documentation and the flow-latency buckets already assume.
const LedgerCloseInterval = 5 * time.Second

// DefaultBudget is the staleness budget: how far behind the chain indexed data
// may fall before it is served as stale and the alert pages.
//
// Five minutes is the existing balance-freshness SLO restated in seconds. That
// SLO permits 60 ledgers of lag, and 60 * 5s is exactly 300s, so this
// introduces no second, contradictory threshold — it expresses the same budget
// in the unit the API contract and the seconds-based alert both need.
//
// Healthy operation sits an order of magnitude inside it: the indexer polls
// every 6s and advances its cursor to the tip it just read, so a healthy
// sample is a handful of ledgers behind and no more than one poll old, i.e.
// well under 20s of staleness. The gap between that and 300s absorbs a slow
// RPC round trip, a restart, and the 12-ledger cold-start rewind without
// paging.
const DefaultBudget = 5 * time.Minute

// Snapshot is the freshness of indexed data at one instant.
//
// It is a value, not a live view: everything needed to render a metric, an
// alert, or an HTTP header is resolved at the moment it is taken, so no caller
// has to hold a lock or re-derive the staleness rule.
type Snapshot struct {
	// Sampled reports whether the indexer has ever published a position.
	// Until it has, IndexedLedger, NetworkLedger and LagLedgers are
	// meaningless and must not be published as zero: "zero ledgers behind"
	// and "we have no idea" are opposite claims.
	Sampled bool

	IndexedLedger uint64
	NetworkLedger uint64

	// LagLedgers is NetworkLedger - IndexedLedger, floored at zero. An
	// indexed ledger ahead of the reported tip is normal for a moment after
	// the cursor advances, and is not negative lag.
	LagLedgers uint64

	// Lag is how stale the indexed data is in wall time. It grows with the
	// clock while the indexer is not reporting, so it is the quantity both
	// the alert and the API contract are defined against.
	Lag time.Duration

	// SampleAge is how long ago the last successful sample was taken, or how
	// long the tracker has existed if there has never been one.
	SampleAge time.Duration

	Budget time.Duration

	// Stale is Lag > Budget. Strictly greater: data exactly at the budget is
	// still within it.
	Stale bool

	// SampleErrors counts failed sampling attempts since process start.
	// Reported separately rather than folded into Lag, because a sentinel lag
	// written on error is indistinguishable from a real stall.
	SampleErrors uint64
}

// Reader reports the current freshness of indexed data. The metrics collector
// and the API middleware depend on this rather than on *Tracker so both can be
// tested against a fixed snapshot.
type Reader interface {
	Snapshot() Snapshot
}

// Tracker is the process-wide freshness state. The indexer writes to it, the
// metrics collector and the API read from it, concurrently and continuously.
type Tracker struct {
	budget time.Duration
	now    func() time.Time

	mu            sync.RWMutex
	sampled       bool
	sampledAt     time.Time
	indexedLedger uint64
	networkLedger uint64
	sampleErrors  uint64
}

// NewTracker returns a tracker enforcing the given staleness budget. A
// non-positive budget falls back to DefaultBudget so a misconfigured value
// cannot make every response report itself stale.
func NewTracker(budget time.Duration) *Tracker {
	return NewTrackerWithClock(budget, time.Now)
}

// NewTrackerWithClock is NewTracker with an injectable clock, so staleness can
// be tested at exact boundaries without sleeping.
func NewTrackerWithClock(budget time.Duration, now func() time.Time) *Tracker {
	if budget <= 0 {
		budget = DefaultBudget
	}
	if now == nil {
		now = time.Now
	}

	return &Tracker{
		budget: budget,
		now:    now,
		// Age is measured from construction until the first sample lands, so
		// an indexer that never starts goes stale after one budget rather
		// than reporting perfect freshness forever. That also gives a fresh
		// deployment exactly one budget of grace before it can page, which is
		// what keeps restarts off the pager.
		sampledAt: now(),
	}
}

// Observe records a successful freshness sample.
func (t *Tracker) Observe(indexedLedger, networkLedger uint64) {
	if t == nil {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	t.sampled = true
	t.sampledAt = t.now()
	t.indexedLedger = indexedLedger
	t.networkLedger = networkLedger
}

// ObserveFailure records a sampling attempt that failed.
//
// It deliberately does not touch sampledAt: a failed sample proves nothing
// about freshness, so the previous reading must keep ageing. Resetting the age
// here would let a permanently-failing sampler hold the freshness signal at
// zero and silence the alert.
func (t *Tracker) ObserveFailure() {
	if t == nil {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	t.sampleErrors++
}

// Budget returns the staleness budget this tracker enforces.
func (t *Tracker) Budget() time.Duration {
	if t == nil {
		return DefaultBudget
	}
	return t.budget
}

// Snapshot computes freshness as of now.
func (t *Tracker) Snapshot() Snapshot {
	if t == nil {
		return Snapshot{Budget: DefaultBudget}
	}

	t.mu.RLock()
	sampled := t.sampled
	sampledAt := t.sampledAt
	indexed := t.indexedLedger
	network := t.networkLedger
	errors := t.sampleErrors
	t.mu.RUnlock()

	age := t.now().Sub(sampledAt)
	if age < 0 {
		// A clock stepping backwards must not report data as fresher than it
		// is; treat the sample as current instead of negative-aged.
		age = 0
	}

	snapshot := Snapshot{
		Sampled:      sampled,
		Budget:       t.budget,
		SampleAge:    age,
		SampleErrors: errors,
		Lag:          age,
	}

	if sampled {
		snapshot.IndexedLedger = indexed
		snapshot.NetworkLedger = network
		if network > indexed {
			snapshot.LagLedgers = network - indexed
		}
		snapshot.Lag = addSaturating(age, ledgerLagDuration(snapshot.LagLedgers))
	}

	snapshot.Stale = snapshot.Lag > t.budget

	return snapshot
}

// ledgerLagDuration converts a ledger count to wall time, saturating instead
// of overflowing. Ledger sequences are 32-bit on Stellar but arrive here as
// uint64 from the RPC and from a text column in system_state; a corrupt value
// multiplied by 5s would wrap int64 and produce a *negative* duration, which
// would report a completely dead indexer as fresh.
func ledgerLagDuration(ledgers uint64) time.Duration {
	const maxLedgers = uint64(math.MaxInt64) / uint64(LedgerCloseInterval)
	if ledgers > maxLedgers {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(ledgers) * LedgerCloseInterval
}

// addSaturating adds two non-negative durations, clamping at the maximum
// rather than wrapping negative. Same reasoning as ledgerLagDuration.
func addSaturating(a, b time.Duration) time.Duration {
	const max = time.Duration(math.MaxInt64)
	if b > max-a {
		return max
	}
	return a + b
}
