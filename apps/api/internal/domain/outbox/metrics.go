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

	// ObserveTimeToFirstAttempt records how long an event waited between
	// being written (Event.CreatedAt) and being handed to the job queue for
	// the first time (nester#1311). A growing figure here means the relay is
	// falling behind production even though nothing has dead-lettered yet —
	// the earliest signal that a backlog is forming, ahead of the depth
	// gauges and long before it becomes an incident.
	ObserveTimeToFirstAttempt(eventType string, d time.Duration)

	// ObserveTimeToDelivery records how long an event took from being
	// written to reaching the terminal delivered state (nester#1311). This is
	// the end-to-end side-effect latency users and integrators actually feel
	// — the figure worth an alerting threshold on, distinct from
	// TimeToFirstAttempt, which only says the relay itself is keeping up.
	ObserveTimeToDelivery(eventType string, d time.Duration)
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
func (NoopMetrics) SetOldestPendingAge(time.Duration)              {}
func (NoopMetrics) ObserveTimeToFirstAttempt(string, time.Duration) {}
func (NoopMetrics) ObserveTimeToDelivery(string, time.Duration)     {}

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

	timeToFirstAttempt map[string]*durationAgg
	timeToDelivery     map[string]*durationAgg
}

// durationAgg is a running count/sum/max for one event type's latency
// observations — enough to derive a mean and a worst case without keeping
// every sample, which is the same trade-off the rest of StdMetrics makes.
type durationAgg struct {
	count int64
	sum   time.Duration
	max   time.Duration
}

func (a *durationAgg) observe(d time.Duration) {
	a.count++
	a.sum += d
	if d > a.max {
		a.max = d
	}
}

// DurationStats is a point-in-time snapshot of one durationAgg.
type DurationStats struct {
	Count int64         `json:"count"`
	Mean  time.Duration `json:"mean"`
	Max   time.Duration `json:"max"`
}

// NewStdMetrics constructs an empty StdMetrics.
func NewStdMetrics() *StdMetrics {
	return &StdMetrics{
		relayed:            map[string]int64{},
		dispatched:         map[string]int64{},
		deadLettered:       map[string]int64{},
		pruned:             map[string]int64{},
		timeToFirstAttempt: map[string]*durationAgg{},
		timeToDelivery:     map[string]*durationAgg{},
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

func (m *StdMetrics) ObserveTimeToFirstAttempt(eventType string, d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	agg, ok := m.timeToFirstAttempt[eventType]
	if !ok {
		agg = &durationAgg{}
		m.timeToFirstAttempt[eventType] = agg
	}
	agg.observe(d)
}

func (m *StdMetrics) ObserveTimeToDelivery(eventType string, d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	agg, ok := m.timeToDelivery[eventType]
	if !ok {
		agg = &durationAgg{}
		m.timeToDelivery[eventType] = agg
	}
	agg.observe(d)
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

	// TimeToFirstAttempt and TimeToDelivery are keyed by event type
	// (nester#1311). See ObserveTimeToFirstAttempt/ObserveTimeToDelivery for
	// what each measures and why they are tracked separately.
	TimeToFirstAttempt map[string]DurationStats `json:"time_to_first_attempt"`
	TimeToDelivery     map[string]DurationStats `json:"time_to_delivery"`
}

// Snapshot returns a copy of the current counters.
func (m *StdMetrics) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return Snapshot{
		Relayed:            copyCounts(m.relayed),
		Dispatched:         copyCounts(m.dispatched),
		DeadLettered:       copyCounts(m.deadLettered),
		Pruned:             copyCounts(m.pruned),
		PendingDepth:       m.pendingDepth,
		DispatchingDepth:   m.dispatchingDepth,
		DeadDepth:          m.deadDepth,
		OldestPendingAge:   m.oldestPendingAge,
		TimeToFirstAttempt: copyDurationAggs(m.timeToFirstAttempt),
		TimeToDelivery:     copyDurationAggs(m.timeToDelivery),
	}
}

func copyCounts(src map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func copyDurationAggs(src map[string]*durationAgg) map[string]DurationStats {
	out := make(map[string]DurationStats, len(src))
	for k, agg := range src {
		stats := DurationStats{Count: agg.count, Max: agg.max}
		if agg.count > 0 {
			stats.Mean = agg.sum / time.Duration(agg.count)
		}
		out[k] = stats
	}
	return out
}
