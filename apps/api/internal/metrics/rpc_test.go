package metrics

import (
	"testing"
	"time"
)

func rpcLabels(upstream Upstream) map[string]string {
	return map[string]string{"upstream": string(upstream)}
}

// The acceptance criterion: attempts, exhaustions, and latency, per upstream.
func TestRPCRecorderRecordsAttemptsExhaustionsAndLatency(t *testing.T) {
	m := New()
	recorder := m.RPCRecorderFor(UpstreamSorobanRPC)

	// A first-try success.
	recorder.RecordRPCCall(1, 120*time.Millisecond, false)
	// One that needed two retries but got there.
	recorder.RecordRPCCall(3, 900*time.Millisecond, false)
	// One that never did.
	recorder.RecordRPCCall(3, 2*time.Second, true)

	labels := rpcLabels(UpstreamSorobanRPC)

	if got := counterValue(t, m.Registry(), "nester_rpc_attempts_total", labels); got != 7 {
		t.Errorf("attempts = %v, want 7 (1 + 3 + 3)", got)
	}
	if got := counterValue(t, m.Registry(), "nester_rpc_exhaustions_total", labels); got != 1 {
		t.Errorf("exhaustions = %v, want 1", got)
	}
	if got := histogramCount(t, m.Registry(), "nester_rpc_call_duration_seconds", labels); got != 3 {
		t.Errorf("duration observations = %d, want 3", got)
	}
}

// Attempts and call count are read together as "average attempts per call",
// which is the earliest signal that an upstream is degrading. That reading
// only works if every call is observed exactly once.
func TestAttemptsAndCallCountSupportTheRatio(t *testing.T) {
	m := New()
	recorder := m.RPCRecorderFor(UpstreamSorobanRPC)

	for i := 0; i < 4; i++ {
		recorder.RecordRPCCall(2, time.Second, false)
	}

	labels := rpcLabels(UpstreamSorobanRPC)
	attempts := counterValue(t, m.Registry(), "nester_rpc_attempts_total", labels)
	calls := float64(histogramCount(t, m.Registry(), "nester_rpc_call_duration_seconds", labels))

	if calls == 0 {
		t.Fatal("no calls observed")
	}
	if ratio := attempts / calls; ratio != 2 {
		t.Fatalf("attempts per call = %v, want 2", ratio)
	}
}

// Each upstream is counted separately, so a flaky Soroban endpoint does not
// show up as Horizon retrying.
func TestRPCMetricsAreSeparatedByUpstream(t *testing.T) {
	m := New()

	m.RPCRecorderFor(UpstreamSorobanRPC).RecordRPCCall(3, time.Second, true)
	m.RPCRecorderFor(UpstreamHorizon).RecordRPCCall(1, time.Millisecond, false)

	if got := counterValue(t, m.Registry(), "nester_rpc_attempts_total", rpcLabels(UpstreamSorobanRPC)); got != 3 {
		t.Errorf("soroban attempts = %v, want 3", got)
	}
	if got := counterValue(t, m.Registry(), "nester_rpc_attempts_total", rpcLabels(UpstreamHorizon)); got != 1 {
		t.Errorf("horizon attempts = %v, want 1", got)
	}
	if got := counterValue(t, m.Registry(), "nester_rpc_exhaustions_total", rpcLabels(UpstreamHorizon)); got != 0 {
		t.Errorf("horizon exhaustions = %v, want 0: one upstream's failure moved the other's counter", got)
	}
}

// A recorder bound to a nil *Metrics must be safe on every path, so a service
// constructed without metrics behaves exactly as before rather than panicking
// mid-call.
func TestNilMetricsRPCRecorderIsSafe(t *testing.T) {
	var m *Metrics

	recorder := m.RPCRecorderFor(UpstreamSorobanRPC)
	recorder.RecordRPCCall(3, time.Second, true)

	var nilRecorder *RPCRecorder
	nilRecorder.RecordRPCCall(1, time.Second, false)
}

// A caller reporting zero attempts must not decrement or pollute the counter;
// the histogram still records the call so the denominator stays honest.
func TestZeroAttemptsDoesNotCorruptTheCounter(t *testing.T) {
	m := New()
	recorder := m.RPCRecorderFor(UpstreamSorobanRPC)

	recorder.RecordRPCCall(0, time.Second, false)

	labels := rpcLabels(UpstreamSorobanRPC)
	if got := counterValue(t, m.Registry(), "nester_rpc_attempts_total", labels); got != 0 {
		t.Fatalf("attempts = %v, want 0", got)
	}
	if got := histogramCount(t, m.Registry(), "nester_rpc_call_duration_seconds", labels); got != 1 {
		t.Fatalf("duration observations = %d, want 1", got)
	}
}

// The label set is the bounded Upstream constants and nothing else. A label
// that a caller could move is the cardinality leak the package policy exists
// to prevent.
func TestRPCMetricCardinalityIsBounded(t *testing.T) {
	m := New()

	// One exhausted call per upstream as well as one successful one, so all
	// three families have a series for both and the count below is comparing
	// like with like. A CounterVec with no observed label value reports no
	// family at all.
	for _, upstream := range []Upstream{UpstreamSorobanRPC, UpstreamHorizon} {
		recorder := m.RPCRecorderFor(upstream)
		recorder.RecordRPCCall(1, time.Second, false)
		recorder.RecordRPCCall(3, 2*time.Second, true)
	}

	families, err := m.Registry().Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	found := 0
	const prefix = "nester_rpc_"
	for _, family := range families {
		name := family.GetName()
		if len(name) < len(prefix) || name[:len(prefix)] != prefix {
			continue
		}
		found++

		if got := len(family.GetMetric()); got != 2 {
			t.Fatalf("%s has %d series, want 2 (one per upstream used)", name, got)
		}
		for _, metric := range family.GetMetric() {
			labels := metric.GetLabel()
			if len(labels) != 1 || labels[0].GetName() != "upstream" {
				t.Fatalf("%s carries labels %v, want exactly one named upstream", name, labels)
			}
		}
	}

	if found != 3 {
		t.Fatalf("found %d nester_rpc_* families, want 3", found)
	}
}
