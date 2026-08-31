package metrics

import (
	"testing"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/breaker"
)

func breakerLabels(upstream Upstream) map[string]string {
	return map[string]string{"upstream": string(upstream)}
}

// gaugeValueFor is gaugeValue with a label selector, which the shared helper
// does not offer and which every assertion here needs.
func gaugeValueFor(t *testing.T, m *Metrics, name string, labels map[string]string) float64 {
	t.Helper()

	families, err := m.Registry().Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			if labelsMatch(metric, labels) {
				return metric.GetGauge().GetValue()
			}
		}
	}
	t.Fatalf("%s%v not found in registry", name, labels)
	return 0
}

func newBreakerMetrics(t *testing.T, clock func() time.Time, cfg breaker.Config) (*Metrics, *breaker.Breaker, *breaker.Breaker) {
	t.Helper()

	soroban := breaker.NewWithClock(string(UpstreamSorobanRPC), cfg, nil, clock)
	horizon := breaker.NewWithClock(string(UpstreamHorizon), cfg, nil, clock)

	m := New()
	if err := m.RegisterBreakers(map[Upstream]BreakerReader{
		UpstreamSorobanRPC: soroban,
		UpstreamHorizon:    horizon,
	}); err != nil {
		t.Fatalf("RegisterBreakers: %v", err)
	}
	return m, soroban, horizon
}

func tripBreaker(t *testing.T, b *breaker.Breaker) {
	t.Helper()

	for i := 0; i < 200; i++ {
		if b.State() == breaker.StateOpen {
			return
		}
		permit, err := b.Allow()
		if err != nil {
			break
		}
		b.Record(permit, breaker.Failure)
	}
	if b.State() != breaker.StateOpen {
		t.Fatal("breaker did not open")
	}
}

// The acceptance criterion: breaker state is a metric, for both upstreams, and
// it tracks the state machine through every transition.
func TestBreakerStateMetricFollowsEveryTransition(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	cfg := breaker.Config{FailureRatio: 0.5, MinRequests: 4, Window: time.Minute, OpenDuration: 15 * time.Second}
	m, soroban, _ := newBreakerMetrics(t, clock, cfg)

	// CLOSED
	if got := gaugeValueFor(t, m, "nester_circuit_breaker_state", breakerLabels(UpstreamSorobanRPC)); got != 0 {
		t.Fatalf("state = %v, want 0 (closed)", got)
	}

	// OPEN
	tripBreaker(t, soroban)
	if got := gaugeValueFor(t, m, "nester_circuit_breaker_state", breakerLabels(UpstreamSorobanRPC)); got != 2 {
		t.Fatalf("state = %v, want 2 (open)", got)
	}

	// HALF-OPEN, derived from the clock with no call in between. A pushed
	// gauge would still read "open" here and disagree with what the breaker
	// would actually do — which is why this is a pull collector.
	now = now.Add(15 * time.Second)
	if got := gaugeValueFor(t, m, "nester_circuit_breaker_state", breakerLabels(UpstreamSorobanRPC)); got != 1 {
		t.Fatalf("state = %v, want 1 (half-open)", got)
	}

	// CLOSED again, after a successful probe.
	permit, err := soroban.Allow()
	if err != nil {
		t.Fatalf("probe refused: %v", err)
	}
	soroban.Record(permit, breaker.Success)

	if got := gaugeValueFor(t, m, "nester_circuit_breaker_state", breakerLabels(UpstreamSorobanRPC)); got != 0 {
		t.Fatalf("state = %v, want 0 (closed)", got)
	}
}

// Both upstreams are reported, independently.
func TestBreakerMetricsAreReportedPerUpstream(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	cfg := breaker.Config{FailureRatio: 0.5, MinRequests: 4, Window: time.Minute, OpenDuration: 15 * time.Second}
	m, soroban, _ := newBreakerMetrics(t, func() time.Time { return now }, cfg)

	tripBreaker(t, soroban)

	if got := gaugeValueFor(t, m, "nester_circuit_breaker_state", breakerLabels(UpstreamSorobanRPC)); got != 2 {
		t.Fatalf("soroban state = %v, want 2 (open)", got)
	}
	if got := gaugeValueFor(t, m, "nester_circuit_breaker_state", breakerLabels(UpstreamHorizon)); got != 0 {
		t.Fatalf("horizon state = %v, want 0 (closed): one upstream's outage moved the other's metric", got)
	}
}

func TestBreakerRejectionAndRatioMetrics(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	cfg := breaker.Config{FailureRatio: 0.5, MinRequests: 4, Window: time.Minute, OpenDuration: 15 * time.Second}
	m, soroban, _ := newBreakerMetrics(t, func() time.Time { return now }, cfg)

	tripBreaker(t, soroban)

	if got := gaugeValueFor(t, m, "nester_circuit_breaker_failure_ratio", breakerLabels(UpstreamSorobanRPC)); got != 1 {
		t.Fatalf("failure ratio = %v, want 1", got)
	}

	for i := 0; i < 7; i++ {
		if _, err := soroban.Allow(); err == nil {
			t.Fatal("Allow() granted permission while open")
		}
	}

	if got := counterValue(t, m.Registry(), "nester_circuit_breaker_rejected_total", breakerLabels(UpstreamSorobanRPC)); got != 7 {
		t.Fatalf("rejected = %v, want 7", got)
	}
}

// Only the bounded Upstream constants may appear as labels, and there is
// exactly one series per upstream per metric. Nothing about a request can move
// the series count.
func TestBreakerMetricCardinalityIsFixed(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	m, soroban, _ := newBreakerMetrics(t, func() time.Time { return now }, breaker.Config{})

	tripBreaker(t, soroban)

	families, err := m.Registry().Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	found := 0
	for _, family := range families {
		name := family.GetName()
		if len(name) < len("nester_circuit_breaker_") || name[:len("nester_circuit_breaker_")] != "nester_circuit_breaker_" {
			continue
		}
		found++

		if got := len(family.GetMetric()); got != 2 {
			t.Fatalf("%s has %d series, want 2 (one per upstream)", name, got)
		}
		for _, metric := range family.GetMetric() {
			labels := metric.GetLabel()
			if len(labels) != 1 || labels[0].GetName() != "upstream" {
				t.Fatalf("%s carries labels %v, want exactly one named upstream", name, labels)
			}
			switch value := labels[0].GetValue(); value {
			case string(UpstreamSorobanRPC), string(UpstreamHorizon):
			default:
				t.Fatalf("%s has upstream=%q, which is not a bounded constant", name, value)
			}
		}
	}

	if found != 3 {
		t.Fatalf("found %d nester_circuit_breaker_* families, want 3", found)
	}
}

// A deployment with the breakers disabled registers no collector and no
// series, rather than reporting a permanently-closed breaker that does not
// exist.
func TestRegisterBreakersWithNoReadersIsANoOp(t *testing.T) {
	m := New()

	if err := m.RegisterBreakers(nil); err != nil {
		t.Fatalf("RegisterBreakers(nil) = %v, want nil", err)
	}
	if err := m.RegisterBreakers(map[Upstream]BreakerReader{}); err != nil {
		t.Fatalf("RegisterBreakers(empty) = %v, want nil", err)
	}

	families, err := m.Registry().Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, family := range families {
		if name := family.GetName(); len(name) >= len("nester_circuit_breaker_") &&
			name[:len("nester_circuit_breaker_")] == "nester_circuit_breaker_" {
			t.Fatalf("%s was registered with no breakers", name)
		}
	}
}

func TestRegisterBreakersRejectsDuplicates(t *testing.T) {
	m := New()
	readers := map[Upstream]BreakerReader{
		UpstreamSorobanRPC: breaker.New("soroban_rpc", breaker.Config{}, nil),
	}

	if err := m.RegisterBreakers(readers); err != nil {
		t.Fatalf("first RegisterBreakers: %v", err)
	}
	if err := m.RegisterBreakers(readers); err == nil {
		t.Fatal("duplicate registration succeeded; the series would be reported twice")
	}
}
