// Package breaker implements the circuit breaker that protects Nester's chain
// dependencies — Soroban RPC and Horizon (nester#1087).
//
// The problem it solves is load amplification. When a chain endpoint degrades,
// every in-flight request sits on it until its own timeout, holding a
// connection the whole time. Requests arrive faster than they drain, the pool
// saturates, and a partial upstream outage becomes a total application outage
// — including for the endpoints that never needed the chain at all.
//
// The breaker cuts that feedback loop: once an upstream is demonstrably
// unhealthy, calls to it fail immediately and locally, holding no connection
// and waiting on nothing.
//
// # States
//
//	CLOSED     calls pass through; outcomes are counted in a rolling window.
//	           Exceeding the failure ratio (with enough samples) opens it.
//	OPEN       calls are rejected immediately with ErrOpen. No connection is
//	           opened and no timeout is waited on.
//	HALF-OPEN  entered once the open period elapses. Exactly one probe call is
//	           admitted; every other caller keeps failing fast. The probe's
//	           outcome decides: success closes, failure re-opens.
//
// # What counts as a failure
//
// This type is transport-agnostic — the caller classifies each outcome and
// reports it via Record. See transport.go for the HTTP classification, which
// is where the "a 404 is not an unhealthy upstream" reasoning lives.
//
// # Concurrency
//
// One Breaker is shared by every goroutine calling its upstream. All state
// lives under a single mutex held only for bookkeeping — never across the
// network call itself, which happens between Allow and Record.
package breaker

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrOpen is returned by Allow when the breaker is not admitting calls.
//
// Callers match it with errors.Is. It survives the wrapping that
// http.Client.Do applies (*url.Error implements Unwrap), so a handler can
// distinguish "we declined to call the chain" from "the chain call failed".
var ErrOpen = errors.New("circuit breaker is open")

// OpenError carries which upstream rejected the call and how long until the
// next probe is admitted, so an API layer can answer with a Retry-After rather
// than an opaque failure.
type OpenError struct {
	Name    string
	RetryIn time.Duration
}

func (e *OpenError) Error() string {
	return fmt.Sprintf("%s: %s (retry in %s)", e.Name, ErrOpen.Error(), e.RetryIn.Round(time.Second))
}

func (e *OpenError) Unwrap() error { return ErrOpen }

// State is the breaker's current mode.
//
// The values ascend with severity — closed < half-open < open — so a metric
// threshold or a max() across replicas reads monotonically as "how bad is it",
// which is not true of an arbitrary ordering.
type State int

const (
	StateClosed State = iota
	StateHalfOpen
	StateOpen
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateHalfOpen:
		return "half_open"
	case StateOpen:
		return "open"
	default:
		return "unknown"
	}
}

// windowBuckets is how finely the rolling window is subdivided.
//
// Not configurable: it trades memory for the granularity at which old
// outcomes expire, and ten is fine for every window length this is used with.
// Exposing it would be an implementation detail masquerading as an operational
// control.
const windowBuckets = 10

// Config is the breaker's policy. Every field has a default; the zero Config
// is usable.
type Config struct {
	// FailureRatio is the share of failed calls, within Window, at or above
	// which the breaker opens. 0.5 means "half of the calls are failing".
	FailureRatio float64

	// MinRequests is how many calls must be observed within Window before the
	// ratio is allowed to open the breaker.
	//
	// A ratio alone is unusable at small samples: one failed call out of one
	// is a 100% failure ratio, and without this floor a single timeout on an
	// idle upstream would open the breaker. The cost of the floor is that a
	// very quiet upstream may never accumulate enough calls to trip — which is
	// acceptable, because with no traffic there is no pile-on to prevent.
	MinRequests int

	// Window is how far back outcomes are counted. Older outcomes expire, so
	// a burst of failures cannot hold the breaker open indefinitely after the
	// upstream recovers.
	Window time.Duration

	// OpenDuration is how long the breaker stays open before admitting a
	// probe.
	OpenDuration time.Duration
}

// Defaults for the chain upstreams. See docs/observability/circuit-breakers.md
// for how these were chosen.
const (
	DefaultFailureRatio = 0.5
	DefaultMinRequests  = 10
	DefaultWindow       = 60 * time.Second
	DefaultOpenDuration = 15 * time.Second
)

func (c Config) withDefaults() Config {
	if c.FailureRatio <= 0 {
		c.FailureRatio = DefaultFailureRatio
	}
	if c.MinRequests <= 0 {
		c.MinRequests = DefaultMinRequests
	}
	if c.Window <= 0 {
		c.Window = DefaultWindow
	}
	if c.OpenDuration <= 0 {
		c.OpenDuration = DefaultOpenDuration
	}
	return c
}

// Validate reports a policy that cannot behave sensibly. Startup rejects such
// a configuration rather than letting it become an incident.
func (c Config) Validate() error {
	if c.FailureRatio <= 0 || c.FailureRatio > 1 {
		return fmt.Errorf("failure ratio must be greater than 0 and at most 1, got %v", c.FailureRatio)
	}
	if c.MinRequests <= 0 {
		return fmt.Errorf("minimum requests must be greater than 0, got %d", c.MinRequests)
	}
	if c.Window <= 0 {
		return fmt.Errorf("window must be greater than 0, got %v", c.Window)
	}
	if c.OpenDuration <= 0 {
		return fmt.Errorf("open duration must be greater than 0, got %v", c.OpenDuration)
	}
	return nil
}

// Outcome is how a completed call is counted.
type Outcome int

const (
	// Success: the upstream answered and behaved.
	Success Outcome = iota

	// Failure: the upstream is implicated — no response, or one that says it
	// is unwell.
	Failure

	// Ignored: the call ended for a reason that says nothing about upstream
	// health, such as the caller cancelling. It is counted neither way, but it
	// still releases a half-open probe slot — without that, a cancelled probe
	// would strand the breaker in half-open with no probe ever admitted again.
	Ignored
)

// Permit authorises one call. It carries the generation it was issued in, so
// an outcome arriving after the breaker has moved on is discarded rather than
// applied to a state it does not describe.
type Permit struct {
	generation uint64
	probe      bool
	valid      bool
}

// TransitionFunc is called after each state change, outside the breaker's
// lock. Transitions are rare, so this is a reasonable place to log; per-call
// rejections are deliberately not reported here, because an open breaker
// rejecting thousands of calls must not turn an upstream outage into a logging
// outage. Use the rejection counter for that.
type TransitionFunc func(name string, from, to State, snapshot Snapshot)

type bucket struct {
	total    int
	failures int
}

// Breaker is a single upstream's circuit breaker.
type Breaker struct {
	name         string
	cfg          Config
	now          func() time.Time
	onTransition TransitionFunc

	mu            sync.Mutex
	state         State
	generation    uint64
	openedAt      time.Time
	probeInFlight bool

	buckets     [windowBuckets]bucket
	bucketIndex int
	bucketStart time.Time
	bucketDur   time.Duration

	rejected uint64
}

// New returns a breaker for the named upstream. The name appears in errors,
// logs, and the metric label, so it must come from a bounded set.
func New(name string, cfg Config, onTransition TransitionFunc) *Breaker {
	return NewWithClock(name, cfg, onTransition, time.Now)
}

// NewWithClock is New with an injectable clock, so the open period and the
// rolling window can be driven deterministically in tests instead of slept
// through.
func NewWithClock(name string, cfg Config, onTransition TransitionFunc, now func() time.Time) *Breaker {
	if now == nil {
		now = time.Now
	}
	cfg = cfg.withDefaults()

	return &Breaker{
		name:         name,
		cfg:          cfg,
		now:          now,
		onTransition: onTransition,
		state:        StateClosed,
		bucketStart:  now(),
		bucketDur:    cfg.Window / windowBuckets,
	}
}

// Name returns the upstream this breaker guards.
func (b *Breaker) Name() string { return b.name }

// Allow asks permission to make one call.
//
// It returns ErrOpen without touching the network when the breaker is open, or
// when it is half-open and another caller already holds the probe. On success
// the caller MUST report the outcome via Record, or the half-open probe slot
// leaks.
func (b *Breaker) Allow() (Permit, error) {
	b.mu.Lock()
	permit, err, event := b.allowLocked()
	b.mu.Unlock()

	b.emit(event)
	return permit, err
}

func (b *Breaker) allowLocked() (Permit, error, *transition) {
	now := b.now()
	b.advanceLocked(now)

	var event *transition

	// The open period is measured, not timed: there is no goroutine or timer
	// waiting to move the breaker to half-open, so a breaker that is never
	// called again costs nothing and leaks nothing.
	if b.state == StateOpen && now.Sub(b.openedAt) >= b.cfg.OpenDuration {
		event = b.setStateLocked(StateHalfOpen, now)
	}

	switch b.state {
	case StateOpen:
		b.rejected++
		return Permit{}, b.openErrorLocked(now), event

	case StateHalfOpen:
		// Single-flight probe. Every other caller keeps failing fast, so a
		// recovering upstream is not hit by the entire backlog the moment the
		// open period ends.
		if b.probeInFlight {
			b.rejected++
			return Permit{}, b.openErrorLocked(now), event
		}
		b.probeInFlight = true
		return Permit{generation: b.generation, probe: true, valid: true}, nil, event

	default:
		return Permit{generation: b.generation, valid: true}, nil, event
	}
}

// Record reports how a permitted call ended.
//
// An outcome from a superseded generation is discarded: the breaker has
// already changed state on other evidence, and applying a stale result would
// let a call that started before an incident close a breaker opened during it.
func (b *Breaker) Record(permit Permit, outcome Outcome) {
	if !permit.valid {
		return
	}

	b.mu.Lock()
	event := b.recordLocked(permit, outcome)
	b.mu.Unlock()

	b.emit(event)
}

func (b *Breaker) recordLocked(permit Permit, outcome Outcome) *transition {
	if permit.generation != b.generation {
		// Superseded. setStateLocked already cleared probeInFlight, so a
		// stale probe cannot leave the slot held.
		return nil
	}

	if permit.probe {
		b.probeInFlight = false
	}

	// Ignored says nothing about the upstream, so it is neither counted nor
	// allowed to decide a half-open probe. Releasing the slot above is the
	// whole point: the next caller gets to probe.
	if outcome == Ignored {
		return nil
	}

	now := b.now()
	b.advanceLocked(now)

	if b.state == StateHalfOpen {
		if outcome == Failure {
			return b.setStateLocked(StateOpen, now)
		}
		return b.setStateLocked(StateClosed, now)
	}

	b.buckets[b.bucketIndex].total++
	if outcome == Failure {
		b.buckets[b.bucketIndex].failures++
	}

	if b.state == StateClosed && b.trippedLocked() {
		return b.setStateLocked(StateOpen, now)
	}
	return nil
}

// trippedLocked reports whether the rolling window justifies opening.
func (b *Breaker) trippedLocked() bool {
	total, failures := b.countsLocked()
	if total < b.cfg.MinRequests {
		return false
	}
	return float64(failures)/float64(total) >= b.cfg.FailureRatio
}

func (b *Breaker) countsLocked() (total, failures int) {
	for _, bkt := range b.buckets {
		total += bkt.total
		failures += bkt.failures
	}
	return total, failures
}

// advanceLocked rolls the window forward to now, clearing the buckets that
// have aged out. This is what stops failures from an incident half an hour ago
// contributing to the current ratio.
func (b *Breaker) advanceLocked(now time.Time) {
	elapsed := now.Sub(b.bucketStart)
	if elapsed < b.bucketDur {
		return
	}

	steps := int(elapsed / b.bucketDur)
	if steps >= windowBuckets {
		// Nothing in the window is still current.
		b.buckets = [windowBuckets]bucket{}
		b.bucketIndex = 0
	} else {
		for i := 0; i < steps; i++ {
			b.bucketIndex = (b.bucketIndex + 1) % windowBuckets
			b.buckets[b.bucketIndex] = bucket{}
		}
	}
	b.bucketStart = b.bucketStart.Add(time.Duration(steps) * b.bucketDur)
}

type transition struct {
	from, to State
	snapshot Snapshot
}

// setStateLocked moves the breaker and returns the event to emit once the lock
// is released. It never calls out while holding the mutex: the callback logs,
// and a blocked log write must not serialise every caller of the upstream.
func (b *Breaker) setStateLocked(to State, now time.Time) *transition {
	from := b.state
	if from == to {
		return nil
	}

	b.state = to

	// Bumping the generation invalidates every permit issued before this
	// point, so calls still in flight cannot apply their outcome to the new
	// state.
	b.generation++
	b.probeInFlight = false

	switch to {
	case StateOpen:
		b.openedAt = now
	case StateClosed:
		// Start the recovered upstream with a clean window. Carrying the
		// failures that opened the breaker into the closed state would
		// immediately re-trip it on the first new failure.
		b.buckets = [windowBuckets]bucket{}
		b.bucketIndex = 0
		b.bucketStart = now
	}

	return &transition{from: from, to: to, snapshot: b.snapshotLocked(now)}
}

func (b *Breaker) emit(event *transition) {
	if event == nil || b.onTransition == nil {
		return
	}
	b.onTransition(b.name, event.from, event.to, event.snapshot)
}

func (b *Breaker) openErrorLocked(now time.Time) error {
	retryIn := b.cfg.OpenDuration - now.Sub(b.openedAt)
	if retryIn < 0 {
		retryIn = 0
	}
	return &OpenError{Name: b.name, RetryIn: retryIn}
}

// Snapshot is a breaker's state at one instant, for metrics and the health
// response.
type Snapshot struct {
	Name         string
	State        State
	Total        int
	Failures     int
	FailureRatio float64
	Rejected     uint64

	// RetryIn is how long until a probe is admitted. Zero unless open.
	RetryIn time.Duration
}

// Snapshot reports the breaker's current state without side effects.
//
// An open breaker whose open period has elapsed is reported as half-open even
// though no caller has arrived to make the transition, because that is the
// truthful answer to "would a call be admitted right now". Reporting it as
// open would tell an operator the upstream is still being shed when it is in
// fact ready to probe.
func (b *Breaker) Snapshot() Snapshot {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Roll the window forward before reading it. Without this a breaker that
	// stopped being called would keep reporting the failures that were current
	// when the last call happened, so the health endpoint and the metric would
	// show an upstream still failing long after its traffic stopped.
	now := b.now()
	b.advanceLocked(now)

	return b.snapshotLocked(now)
}

// State is the effective state, following Snapshot's semantics.
func (b *Breaker) State() State {
	return b.Snapshot().State
}

func (b *Breaker) snapshotLocked(now time.Time) Snapshot {
	total, failures := b.countsLocked()

	ratio := 0.0
	if total > 0 {
		ratio = float64(failures) / float64(total)
	}

	state := b.state
	retryIn := time.Duration(0)
	if state == StateOpen {
		if remaining := b.cfg.OpenDuration - now.Sub(b.openedAt); remaining > 0 {
			retryIn = remaining
		} else {
			state = StateHalfOpen
		}
	}

	return Snapshot{
		Name:         b.name,
		State:        state,
		Total:        total,
		Failures:     failures,
		FailureRatio: ratio,
		Rejected:     b.rejected,
		RetryIn:      retryIn,
	}
}
