package metrics

import (
	"testing"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/freshness"
)

// stubReader returns a fixed snapshot, so the collector is asserted against a
// known freshness state rather than against a live clock.
type stubReader struct {
	snapshot freshness.Snapshot
}

func (s stubReader) Snapshot() freshness.Snapshot { return s.snapshot }

func registryWithFreshness(t *testing.T, reader freshness.Reader) *Metrics {
	t.Helper()

	m := New()
	if err := m.RegisterFreshness(reader); err != nil {
		t.Fatalf("RegisterFreshness: %v", err)
	}
	return m
}

// hasSeries reports whether a metric family exists at all, which gaugeValue
// cannot: it returns 0 both for "the gauge reads zero" and for "the series is
// not there", and the difference is the whole point of the cold-start case.
func hasSeries(t *testing.T, m *Metrics, name string) bool {
	t.Helper()

	families, err := m.Registry().Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, family := range families {
		if family.GetName() == name {
			return len(family.GetMetric()) > 0
		}
	}
	return false
}

func TestFreshnessCollectorExportsLagInLedgersAndSeconds(t *testing.T) {
	m := registryWithFreshness(t, stubReader{snapshot: freshness.Snapshot{
		Sampled:       true,
		IndexedLedger: 1_000,
		NetworkLedger: 1_007,
		LagLedgers:    7,
		Lag:           41 * time.Second,
		SampleAge:     6 * time.Second,
		Budget:        5 * time.Minute,
		SampleErrors:  3,
	}})

	if got := gaugeValue(t, m.Registry(), "nester_indexer_lag_ledgers"); got != 7 {
		t.Fatalf("lag ledgers = %v, want 7", got)
	}
	if got := gaugeValue(t, m.Registry(), "nester_indexer_lag_seconds"); got != 41 {
		t.Fatalf("lag seconds = %v, want 41", got)
	}
	if got := gaugeValue(t, m.Registry(), "nester_indexer_lag_last_sample_age_seconds"); got != 6 {
		t.Fatalf("sample age = %v, want 6", got)
	}
	if got := gaugeValue(t, m.Registry(), "nester_indexer_staleness_budget_seconds"); got != 300 {
		t.Fatalf("budget = %v, want 300", got)
	}
	if got := counterValue(t, m.Registry(), "nester_indexer_lag_sample_errors_total", nil); got != 3 {
		t.Fatalf("sample errors = %v, want 3", got)
	}
}

// A caught-up indexer reports zero lag in both units. The series must exist
// and read zero, not vanish: an absent series cannot be alerted on, and the
// dashboard would show a gap rather than a healthy line.
func TestFreshnessCollectorReportsZeroLagWhenCaughtUp(t *testing.T) {
	m := registryWithFreshness(t, stubReader{snapshot: freshness.Snapshot{
		Sampled: true,
		Budget:  5 * time.Minute,
	}})

	if !hasSeries(t, m, "nester_indexer_lag_ledgers") {
		t.Fatal("lag_ledgers series missing for a caught-up indexer")
	}
	if got := gaugeValue(t, m.Registry(), "nester_indexer_lag_ledgers"); got != 0 {
		t.Fatalf("lag ledgers = %v, want 0", got)
	}
	if got := gaugeValue(t, m.Registry(), "nester_indexer_lag_seconds"); got != 0 {
		t.Fatalf("lag seconds = %v, want 0", got)
	}
}

// Before the first sample the ledger lag is unknown. Publishing zero would be
// the single most dangerous value the gauge can hold, because it is the one
// that looks perfect — so the series is withheld while the seconds view, which
// is always present, carries the truth.
func TestLedgerLagIsWithheldUntilTheIndexerReports(t *testing.T) {
	m := registryWithFreshness(t, stubReader{snapshot: freshness.Snapshot{
		Sampled: false,
		Lag:     9 * time.Minute,
		Budget:  5 * time.Minute,
	}})

	if hasSeries(t, m, "nester_indexer_lag_ledgers") {
		t.Fatal("lag_ledgers published before the indexer reported a position")
	}
	if !hasSeries(t, m, "nester_indexer_lag_seconds") {
		t.Fatal("lag_seconds missing: nothing would page for an indexer that never started")
	}
	if got := gaugeValue(t, m.Registry(), "nester_indexer_lag_seconds"); got != 540 {
		t.Fatalf("lag seconds = %v, want 540", got)
	}
}

// The reason this is a pull collector rather than gauges the indexer pushes.
// Nothing observes anything here after registration; the numbers must still
// grow between scrapes, because they are derived from the clock at scrape
// time. A pushed gauge would freeze at its last healthy value the moment the
// indexer died, and the alert would never fire.
func TestLagSecondsGrowsBetweenScrapesWithoutTheIndexer(t *testing.T) {
	clock := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	tracker := freshness.NewTrackerWithClock(5*time.Minute, func() time.Time { return clock })

	m := registryWithFreshness(t, tracker)
	tracker.Observe(1_000, 1_000)

	if got := gaugeValue(t, m.Registry(), "nester_indexer_lag_seconds"); got != 0 {
		t.Fatalf("lag seconds immediately after a sample = %v, want 0", got)
	}

	// The indexer is now dead: no further Observe calls, only time passing.
	clock = clock.Add(10 * time.Minute)

	lag := gaugeValue(t, m.Registry(), "nester_indexer_lag_seconds")
	if lag != 600 {
		t.Fatalf("lag seconds after 10m of silence = %v, want 600", lag)
	}
	if budget := gaugeValue(t, m.Registry(), "nester_indexer_staleness_budget_seconds"); lag <= budget {
		t.Fatalf("lag %v did not exceed budget %v, so the alert would not fire", lag, budget)
	}
	if got := gaugeValue(t, m.Registry(), "nester_indexer_lag_last_sample_age_seconds"); got != 600 {
		t.Fatalf("sample age = %v, want 600", got)
	}
}

// Recovery has to be visible too, or an operator cannot tell a fixed indexer
// from a still-broken one.
func TestLagSecondsFallsAfterTheIndexerCatchesUp(t *testing.T) {
	clock := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	tracker := freshness.NewTrackerWithClock(5*time.Minute, func() time.Time { return clock })

	m := registryWithFreshness(t, tracker)
	tracker.Observe(1_000, 1_000)
	clock = clock.Add(10 * time.Minute)

	if got := gaugeValue(t, m.Registry(), "nester_indexer_lag_seconds"); got != 600 {
		t.Fatalf("precondition: lag seconds = %v, want 600", got)
	}

	tracker.Observe(1_120, 1_122)

	if got := gaugeValue(t, m.Registry(), "nester_indexer_lag_seconds"); got != 10 {
		t.Fatalf("lag seconds after catching up = %v, want 10 (2 ledgers)", got)
	}
	if got := gaugeValue(t, m.Registry(), "nester_indexer_lag_ledgers"); got != 2 {
		t.Fatalf("lag ledgers after catching up = %v, want 2", got)
	}
}

// Registering twice must fail rather than panic or silently double-count, and
// a nil reader must be a no-op so the API can boot without an indexer.
func TestRegisterFreshnessGuards(t *testing.T) {
	m := New()

	if err := m.RegisterFreshness(nil); err != nil {
		t.Fatalf("RegisterFreshness(nil) = %v, want nil", err)
	}
	if hasSeries(t, m, "nester_indexer_lag_seconds") {
		t.Fatal("a nil reader registered a collector")
	}

	tracker := freshness.NewTracker(time.Minute)
	if err := m.RegisterFreshness(tracker); err != nil {
		t.Fatalf("first RegisterFreshness: %v", err)
	}
	if err := m.RegisterFreshness(tracker); err == nil {
		t.Fatal("duplicate registration succeeded; a second collector would double the series")
	}
}

// Freshness is one process-wide fact, so none of its series may carry a label.
// A label here could only come from something request-scoped, which is exactly
// the cardinality leak the package policy forbids.
func TestFreshnessSeriesCarryNoLabels(t *testing.T) {
	m := registryWithFreshness(t, stubReader{snapshot: freshness.Snapshot{
		Sampled: true,
		Budget:  5 * time.Minute,
	}})

	families, err := m.Registry().Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	found := 0
	for _, family := range families {
		if len(family.GetName()) < len("nester_indexer_") || family.GetName()[:len("nester_indexer_")] != "nester_indexer_" {
			continue
		}
		found++
		if got := len(family.GetMetric()); got != 1 {
			t.Fatalf("%s has %d series, want exactly 1", family.GetName(), got)
		}
		if got := len(family.GetMetric()[0].GetLabel()); got != 0 {
			t.Fatalf("%s carries %d labels, want none", family.GetName(), got)
		}
	}

	if found != 5 {
		t.Fatalf("found %d nester_indexer_* families, want 5", found)
	}
}
