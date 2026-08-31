package outbox

import (
	"sync"
	"time"
)

// Metrics is the observability sink for the relay. All methods must be safe
// for concurrent use. The default (NoopMetrics) discards everything.
type Metrics interface {
	// IncRelayed records that an event was handed to the job queue.
	IncRelayed(eventType string)
	// IncDispatched records that an event's delivery job succeeded.
	IncDispatched(eventType string)
	// IncDeadLettered records a poison event. This is the counter worth
	// alerting on: it means a side effect will never happen.
	IncDeadLettered(eventType string)
	// IncPruned records rows removed by the retention job.
	IncPruned(status string, n int64)
	// SetPendingDepth publishes the undispatched-and-due gauge.
	SetPendingDepth(n int64)
	// SetDispatchingDepth publishes the in-flight gauge.
	SetDispatchingDepth(n int64)
	// SetDeadDepth publishes the poison-backlog gauge.
	SetDeadDepth(n int64)
	// SetOldestPendingAge publishes how long the oldest undispatched event
	// has waited — a relay that has stopped relaying shows up here first,
	// while the depth gauges can still look healthy.
	SetOldestPendingAge(d time.Duration)
}

// NoopMetrics implements Metrics and does nothing.
type NoopMetrics struct{}

func (NoopMetrics) IncRelayed(string)                 {}
func (NoopMetrics) IncDispatched(string)              {}
func (NoopMetrics) IncDeadLettered(string)            {}
func (NoopMetrics) IncPruned(string, int64)           {}
func (NoopMetrics) SetPendingDepth(int64)             {}
func (NoopMetrics) SetDispatchingDepth(int64)         {}
func (NoopMetrics) SetDeadDepth(int64)                {}
func (NoopMetrics) SetOldestPendingAge(time.Duration) {}

// StdMetrics is a lightweight in-process Metrics implementation, mirroring
// jobqueue.StdMetrics so the two subsystems are read the same way.
type StdMetrics struct {
	mu sync.Mutex

	relayed      map[string]int64
	dispatched   map[string]int64
	deadLettered map[string]int64
	pruned       map[string]int64

	pendingDepth     int64
	dispatchingDepth int64
	deadDepth        int64
	oldestPendingAge time.Duration
}

// NewStdMetrics constructs an empty StdMetrics.
func NewStdMetrics() *StdMetrics {
	return &StdMetrics{
		relayed:      map[string]int64{},
		dispatched:   map[string]int64{},
		deadLettered: map[string]int64{},
		pruned:       map[string]int64{},
	}
}

func (m *StdMetrics) inc(counter map[string]int64, key string, n int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	counter[key] += n
}

func (m *StdMetrics) IncRelayed(eventType string)      { m.inc(m.relayed, eventType, 1) }
func (m *StdMetrics) IncDispatched(eventType string)   { m.inc(m.dispatched, eventType, 1) }
func (m *StdMetrics) IncDeadLettered(eventType string) { m.inc(m.deadLettered, eventType, 1) }
func (m *StdMetrics) IncPruned(status string, n int64) { m.inc(m.pruned, status, n) }

func (m *StdMetrics) SetPendingDepth(n int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pendingDepth = n
}

func (m *StdMetrics) SetDispatchingDepth(n int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dispatchingDepth = n
}

func (m *StdMetrics) SetDeadDepth(n int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deadDepth = n
}

func (m *StdMetrics) SetOldestPendingAge(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.oldestPendingAge = d
}

// Snapshot is a point-in-time copy of the counters, for the metrics endpoint
// and for assertions in tests.
type Snapshot struct {
	Relayed          map[string]int64 `json:"relayed"`
	Dispatched       map[string]int64 `json:"dispatched"`
	DeadLettered     map[string]int64 `json:"dead_lettered"`
	Pruned           map[string]int64 `json:"pruned"`
	PendingDepth     int64            `json:"pending_depth"`
	DispatchingDepth int64            `json:"dispatching_depth"`
	DeadDepth        int64            `json:"dead_depth"`
	OldestPendingAge time.Duration    `json:"oldest_pending_age"`
}

// Snapshot returns a copy of the current counters.
func (m *StdMetrics) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return Snapshot{
		Relayed:          copyCounts(m.relayed),
		Dispatched:       copyCounts(m.dispatched),
		DeadLettered:     copyCounts(m.deadLettered),
		Pruned:           copyCounts(m.pruned),
		PendingDepth:     m.pendingDepth,
		DispatchingDepth: m.dispatchingDepth,
		DeadDepth:        m.deadDepth,
		OldestPendingAge: m.oldestPendingAge,
	}
}

func copyCounts(src map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}
