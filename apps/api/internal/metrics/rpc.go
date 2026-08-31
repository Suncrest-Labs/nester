package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// rpcCallDurationBuckets cover a whole logical RPC call — every attempt plus
// the backoff between them — not a single round trip.
//
// That is why they reach further than dependencyDurationBuckets: a healthy
// Soroban read lands well under 500ms, but a call that retried twice
// legitimately spends seconds, and the whole point of this histogram is to
// show the difference between "fast" and "succeeded, eventually". The tail at
// 15s brackets the default 12s retry budget, so a call cut off by the budget
// is visibly distinct from one that merely took a while.
var rpcCallDurationBuckets = []float64{
	0.05, 0.1, 0.25, 0.5, 1, 2, 3, 5, 8, 12, 15,
}

// rpcCollectors instrument the shared retry helper (nester#1086).
//
// These describe *logical* calls, where the outbound metrics describe HTTP
// attempts. The pair is deliberate and the difference is the useful signal:
// when attempts_total climbs faster than outbound requests would suggest, the
// upstream is flaky but recovering; when exhaustions_total starts moving, the
// retries have stopped being enough and users are seeing errors.
type rpcCollectors struct {
	attempts    *prometheus.CounterVec
	exhaustions *prometheus.CounterVec
	duration    *prometheus.HistogramVec
}

func newRPCCollectors() *rpcCollectors {
	labels := []string{"upstream"}

	return &rpcCollectors{
		// Counts every attempt, including the first. Rated against the count
		// of logical calls (duration_seconds_count), this is the average
		// attempts per call — the cleanest early warning that an upstream is
		// degrading, because it moves long before anything fails outright.
		attempts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: Namespace,
			Subsystem: "rpc",
			Name:      "attempts_total",
			Help:      "Individual RPC attempts made, including retries, by upstream.",
		}, labels),

		// Logical calls that used up their attempts or their budget without
		// an answer. These are the ones that reach the user as an error, so
		// this is the counter an alert would watch.
		exhaustions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: Namespace,
			Subsystem: "rpc",
			Name:      "exhaustions_total",
			Help:      "Logical RPC calls that exhausted the retry policy without succeeding, by upstream.",
		}, labels),

		// Wall time for the whole logical call, retries and backoff included.
		// This is what a user waited, which the per-attempt outbound histogram
		// cannot show.
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: Namespace,
			Subsystem: "rpc",
			Name:      "call_duration_seconds",
			Help:      "End-to-end latency of a logical RPC call including retries and backoff, by upstream.",
			Buckets:   rpcCallDurationBuckets,
		}, labels),
	}
}

func (c *rpcCollectors) collectors() []prometheus.Collector {
	return []prometheus.Collector{c.attempts, c.exhaustions, c.duration}
}

// RPCRecorder reports completed logical RPC calls for one upstream.
//
// It is bound to an upstream at construction rather than taking one per call,
// so a call site physically cannot report against the wrong label — and the
// label set stays the bounded Upstream constants rather than anything a
// caller passes in.
type RPCRecorder struct {
	metrics  *Metrics
	upstream Upstream
}

// RPCRecorderFor returns a recorder bound to the given upstream. A nil
// *Metrics yields a recorder whose methods are no-ops, so a service
// constructed without metrics behaves exactly as before.
func (m *Metrics) RPCRecorderFor(upstream Upstream) *RPCRecorder {
	return &RPCRecorder{metrics: m, upstream: upstream}
}

// RecordRPCCall records one completed logical call.
//
// attempts counts every try including the first; elapsed is the wall time
// across all of them; exhausted reports whether the retry policy ran out
// rather than the call reaching a decision.
func (r *RPCRecorder) RecordRPCCall(attempts int, elapsed time.Duration, exhausted bool) {
	if r == nil || r.metrics == nil {
		return
	}

	upstream := string(r.upstream)

	// A call always makes at least one attempt; guarding here keeps a caller
	// that reports zero from silently under-counting the denominator.
	if attempts > 0 {
		r.metrics.rpc.attempts.WithLabelValues(upstream).Add(float64(attempts))
	}
	if exhausted {
		r.metrics.rpc.exhaustions.WithLabelValues(upstream).Inc()
	}
	if elapsed >= 0 {
		r.metrics.rpc.duration.WithLabelValues(upstream).Observe(elapsed.Seconds())
	}
}
