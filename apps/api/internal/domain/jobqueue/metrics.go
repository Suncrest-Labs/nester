package jobqueue

import (
	"maps"
	"sync"
	"time"
)

// Metrics is the observability sink for the worker pool. All methods must be
// safe for concurrent use. The default (NoopMetrics) discards everything.
type Metrics interface {
	// IncEnqueued records that a job of the given type was enqueued.
	IncEnqueued(jobType string)
	// IncProcessed records a terminal or retry outcome for one execution.
	// outcome is one of: "succeeded", "retried", "dead".
	IncProcessed(jobType, outcome string)
	// ObserveLatency records handler execution latency.
	ObserveLatency(jobType string, d time.Duration)
	// SetDepth publishes the current queue depth gauge (runnable pending jobs).
	SetDepth(depth int64)
	// SetDeadLetterDepth publishes the current DLQ depth gauge.
	SetDeadLetterDepth(depth int64)
}

// NoopMetrics implements Metrics and does nothing.
type NoopMetrics struct{}

func (NoopMetrics) IncEnqueued(string)                   {}
func (NoopMetrics) IncProcessed(string, string)          {}
func (NoopMetrics) ObserveLatency(string, time.Duration) {}
func (NoopMetrics) SetDepth(int64)                       {}
func (NoopMetrics) SetDeadLetterDepth(int64)             {}

// StdMetrics is a lightweight in-process Metrics implementation backed by
// counters. It is intended for exposure on the health/metrics endpoint and for
// assertions in tests; production deployments can supply a Prometheus-backed
// implementation of the same interface.
type StdMetrics struct {
	mu sync.Mutex

	enqueued   map[string]int64
	succeeded  map[string]int64
	retried    map[string]int64
	dead       map[string]int64
	latencySum map[string]time.Duration
	latencyN   map[string]int64
	depth      int64
	dlqDepth   int64
}

// NewStdMetrics constructs an empty StdMetrics.
func NewStdMetrics() *StdMetrics {
	return &StdMetrics{
		enqueued:   map[string]int64{},
		succeeded:  map[string]int64{},
		retried:    map[string]int64{},
		dead:       map[string]int64{},
		latencySum: map[string]time.Duration{},
		latencyN:   map[string]int64{},
	}
}

func (m *StdMetrics) IncEnqueued(jobType string) {
	m.mu.Lock()
	m.enqueued[jobType]++
	m.mu.Unlock()
}

func (m *StdMetrics) IncProcessed(jobType, outcome string) {
	m.mu.Lock()
	switch outcome {
	case "succeeded":
		m.succeeded[jobType]++
	case "retried":
		m.retried[jobType]++
	case "dead":
		m.dead[jobType]++
	}
	m.mu.Unlock()
}

func (m *StdMetrics) ObserveLatency(jobType string, d time.Duration) {
	m.mu.Lock()
	m.latencySum[jobType] += d
	m.latencyN[jobType]++
	m.mu.Unlock()
}

func (m *StdMetrics) SetDepth(depth int64) {
	m.mu.Lock()
	m.depth = depth
	m.mu.Unlock()
}

func (m *StdMetrics) SetDeadLetterDepth(depth int64) {
	m.mu.Lock()
	m.dlqDepth = depth
	m.mu.Unlock()
}

// Snapshot is a point-in-time copy of StdMetrics counters.
type Snapshot struct {
	Enqueued         map[string]int64 `json:"enqueued"`
	Succeeded        map[string]int64 `json:"succeeded"`
	Retried          map[string]int64 `json:"retried"`
	Dead             map[string]int64 `json:"dead"`
	AvgLatencyMillis map[string]int64 `json:"avg_latency_ms"`
	Depth            int64            `json:"depth"`
	DeadLetterDepth  int64            `json:"dead_letter_depth"`
}

// Snapshot returns a copy of the current counters.
func (m *StdMetrics) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()

	avg := make(map[string]int64, len(m.latencyN))
	for t, n := range m.latencyN {
		if n > 0 {
			avg[t] = (m.latencySum[t] / time.Duration(n)).Milliseconds()
		}
	}
	return Snapshot{
		Enqueued:         copyCounts(m.enqueued),
		Succeeded:        copyCounts(m.succeeded),
		Retried:          copyCounts(m.retried),
		Dead:             copyCounts(m.dead),
		AvgLatencyMillis: avg,
		Depth:            m.depth,
		DeadLetterDepth:  m.dlqDepth,
	}
}

func copyCounts(src map[string]int64) map[string]int64 {
	dst := make(map[string]int64, len(src))
	maps.Copy(dst, src)
	return dst
}
