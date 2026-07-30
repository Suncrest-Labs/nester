package oracle_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/oracle"
)

func defaultOpts() oracle.AggregationOptions {
	return oracle.AggregationOptions{
		MaxDeviationBPS:    300, // 3%
		MinAgreeingSources: 2,
		PerSourceTimeout:   time.Second,
	}
}

func TestAggregate_AgreeingSourcesProduceConsensus(t *testing.T) {
	sources := []oracle.Provider{
		&stubProvider{name: "a", rate: 100.0},
		&stubProvider{name: "b", rate: 100.5},
		&stubProvider{name: "c", rate: 99.5},
	}
	health := oracle.NewHealthTracker()

	result, err := oracle.Aggregate(context.Background(), "X", "Y", sources, health, defaultOpts())
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	if result.Unavailable {
		t.Fatal("expected a usable consensus value")
	}
	if result.Value != 100.0 {
		t.Errorf("expected median 100.0, got %v", result.Value)
	}
	if len(result.SourcesUsed) != 3 {
		t.Errorf("expected all 3 agreeing sources used, got %v", result.SourcesUsed)
	}
	if result.Confidence != 1.0 {
		t.Errorf("expected full confidence with all sources agreeing, got %v", result.Confidence)
	}
}

func TestAggregate_OutlierRejectedConsensusUnaffected(t *testing.T) {
	sources := []oracle.Provider{
		&stubProvider{name: "a", rate: 100.0},
		&stubProvider{name: "b", rate: 100.2},
		&stubProvider{name: "manipulated", rate: 500.0}, // wildly off
	}
	health := oracle.NewHealthTracker()

	result, err := oracle.Aggregate(context.Background(), "X", "Y", sources, health, defaultOpts())
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	if result.Value > 101 || result.Value < 99 {
		t.Errorf("expected consensus near 100, got %v (outlier must not move it)", result.Value)
	}
	found := false
	for _, r := range result.SourcesRejected {
		if r == "manipulated" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'manipulated' to be rejected as an outlier, got rejected=%v", result.SourcesRejected)
	}
	for _, u := range result.SourcesUsed {
		if u == "manipulated" {
			t.Error("manipulated source must not be counted among SourcesUsed")
		}
	}
}

func TestAggregate_AllButOneDown_LowConfidenceNotUnavailable(t *testing.T) {
	sources := []oracle.Provider{
		&stubProvider{name: "a", err: errors.New("down")},
		&stubProvider{name: "b", err: errors.New("down")},
		&stubProvider{name: "c", rate: 42.0},
	}
	health := oracle.NewHealthTracker()

	result, err := oracle.Aggregate(context.Background(), "X", "Y", sources, health, defaultOpts())
	if err != nil {
		t.Fatalf("expected a value despite two sources down, got error: %v", err)
	}
	if result.Unavailable {
		t.Fatal("a lone responding source must not be reported Unavailable")
	}
	if result.Value != 42.0 {
		t.Errorf("expected the lone source's value, got %v", result.Value)
	}
	// MinAgreeingSources is 2 in defaultOpts; only 1 responded, so
	// confidence must be reduced below what full agreement would give.
	if result.Confidence >= 1.0 {
		t.Errorf("expected reduced confidence with only 1 of 3 sources responding, got %v", result.Confidence)
	}
	if result.Confidence <= 0 {
		t.Errorf("expected a nonzero confidence for a usable lone response, got %v", result.Confidence)
	}
}

func TestAggregate_AllSourcesDown_Unavailable(t *testing.T) {
	sources := []oracle.Provider{
		&stubProvider{name: "a", err: errors.New("down")},
		&stubProvider{name: "b", err: errors.New("down")},
	}
	health := oracle.NewHealthTracker()

	result, err := oracle.Aggregate(context.Background(), "X", "Y", sources, health, defaultOpts())
	if err == nil {
		t.Fatal("expected an error when every source is down")
	}
	if !result.Unavailable {
		t.Error("expected Unavailable=true when every source is down")
	}
}

func TestAggregate_SlowSourceTimesOutAndDoesNotStallResult(t *testing.T) {
	slow := &blockingProvider{name: "slow", delay: 5 * time.Second}
	fast := &stubProvider{name: "fast", rate: 10.0}
	health := oracle.NewHealthTracker()

	opts := defaultOpts()
	opts.PerSourceTimeout = 50 * time.Millisecond
	opts.MinAgreeingSources = 1

	start := time.Now()
	result, err := oracle.Aggregate(context.Background(), "X", "Y", []oracle.Provider{slow, fast}, health, opts)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("expected the slow source's timeout to bound total latency, took %v", elapsed)
	}
	if result.Value != 10.0 {
		t.Errorf("expected the fast source's value, got %v", result.Value)
	}
	found := false
	for _, f := range result.SourcesFailed {
		if f == "slow" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected the timed-out source recorded as failed, got %v", result.SourcesFailed)
	}
}

func TestHealthTracker_RepeatedFailuresMarkUnhealthyAndSkipped(t *testing.T) {
	h := oracle.NewHealthTracker()
	h.RecordFailure("flaky", errors.New("boom"))

	if h.IsHealthy("flaky") {
		t.Fatal("expected source to be unhealthy immediately after a failure (backoff just started)")
	}

	failing := &stubProvider{name: "flaky", err: errors.New("boom")}
	ok := &stubProvider{name: "ok", rate: 5.0}

	result, err := oracle.Aggregate(context.Background(), "X", "Y", []oracle.Provider{failing, ok}, h, oracle.AggregationOptions{
		MaxDeviationBPS: 300, MinAgreeingSources: 1, PerSourceTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	if failing.callCount != 0 {
		t.Errorf("expected the unhealthy source to be skipped (not queried), got %d calls", failing.callCount)
	}
	found := false
	for _, s := range result.SourcesSkipped {
		if s == "flaky" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'flaky' recorded as skipped, got %v", result.SourcesSkipped)
	}
}

func TestHealthTracker_BackoffExpiresAndSourceIsProbedAgain(t *testing.T) {
	h := oracle.NewHealthTracker()
	fakeNow := time.Now()
	h.SetNowFuncForTest(func() time.Time { return fakeNow })

	h.RecordFailure("flaky", errors.New("boom"))
	if h.IsHealthy("flaky") {
		t.Fatal("expected unhealthy immediately after failure")
	}

	// Advance past the first backoff window (base backoff is 5s).
	fakeNow = fakeNow.Add(10 * time.Second)
	if !h.IsHealthy("flaky") {
		t.Fatal("expected the source to be probed again once its backoff window elapsed")
	}
}

func TestHealthTracker_BackoffGrowsExponentiallyWithConsecutiveFailures(t *testing.T) {
	h := oracle.NewHealthTracker()
	fakeNow := time.Now()
	h.SetNowFuncForTest(func() time.Time { return fakeNow })

	h.RecordFailure("flaky", errors.New("1"))
	snap1 := h.Snapshot()["flaky"]
	firstBackoff := snap1.BackoffUntil.Sub(fakeNow)

	// Immediately fail again (still within the first backoff window) to
	// accumulate a second consecutive failure.
	h.RecordFailure("flaky", errors.New("2"))
	snap2 := h.Snapshot()["flaky"]
	secondBackoff := snap2.BackoffUntil.Sub(fakeNow)

	if secondBackoff <= firstBackoff {
		t.Errorf("expected backoff to grow with consecutive failures: first=%v second=%v", firstBackoff, secondBackoff)
	}
}

func TestHealthTracker_SuccessClearsFailureHistory(t *testing.T) {
	h := oracle.NewHealthTracker()
	h.RecordFailure("recovering", errors.New("boom"))
	h.RecordSuccess("recovering")

	if !h.IsHealthy("recovering") {
		t.Fatal("expected a source to be healthy immediately after a recorded success")
	}
	snap := h.Snapshot()["recovering"]
	if snap.ConsecutiveFailures != 0 {
		t.Errorf("expected ConsecutiveFailures reset to 0, got %d", snap.ConsecutiveFailures)
	}
}

func TestDecayConfidenceForStaleness_DecaysTowardHalfAtMaxAge(t *testing.T) {
	full := oracle.DecayConfidenceForStaleness(1.0, 0, time.Minute)
	if full != 1.0 {
		t.Errorf("zero age must not decay confidence, got %v", full)
	}
	atMax := oracle.DecayConfidenceForStaleness(1.0, time.Minute, time.Minute)
	if atMax != 0.5 {
		t.Errorf("expected confidence*0.5 at maxAge, got %v", atMax)
	}
	beyondMax := oracle.DecayConfidenceForStaleness(1.0, 10*time.Minute, time.Minute)
	if beyondMax != 0.5 {
		t.Errorf("expected age beyond maxAge to clamp at *0.5, got %v", beyondMax)
	}
}

// blockingProvider simulates a slow source that never returns before its
// context is cancelled.
type blockingProvider struct {
	name  string
	delay time.Duration
}

func (p *blockingProvider) Name() string { return p.name }

func (p *blockingProvider) Fetch(ctx context.Context, _, _ string) (float64, error) {
	select {
	case <-time.After(p.delay):
		return 0, errors.New("should not reach here in the timeout test")
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}
