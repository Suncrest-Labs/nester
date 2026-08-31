package breaker

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeClock drives the open period and the rolling window without sleeping.
// Every timing assertion below is about a boundary measured in seconds or
// minutes; sleeping through them would make the suite slow and, worse, flaky.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func testConfig() Config {
	return Config{
		FailureRatio: 0.5,
		MinRequests:  4,
		Window:       60 * time.Second,
		OpenDuration: 15 * time.Second,
	}
}

func newTestBreaker(t *testing.T, cfg Config) (*Breaker, *fakeClock) {
	t.Helper()
	clock := newFakeClock()
	return NewWithClock("test_upstream", cfg, nil, clock.Now), clock
}

// call runs one permitted call with the given outcome. It fails the test if
// the breaker refused, so a test that means to exercise the closed path cannot
// silently pass while the breaker was rejecting.
func call(t *testing.T, b *Breaker, outcome Outcome) {
	t.Helper()
	permit, err := b.Allow()
	if err != nil {
		t.Fatalf("Allow() = %v, want permission", err)
	}
	b.Record(permit, outcome)
}

func mustReject(t *testing.T, b *Breaker) error {
	t.Helper()
	permit, err := b.Allow()
	if err == nil {
		b.Record(permit, Success)
		t.Fatal("Allow() granted permission, want rejection")
	}
	if !errors.Is(err, ErrOpen) {
		t.Fatalf("Allow() = %v, want an error matching ErrOpen", err)
	}
	return err
}

func assertState(t *testing.T, b *Breaker, want State) {
	t.Helper()
	if got := b.State(); got != want {
		t.Fatalf("state = %s, want %s", got, want)
	}
}

// ---------------------------------------------------------------------------
// CLOSED
// ---------------------------------------------------------------------------

func TestClosedBreakerPassesCallsThrough(t *testing.T) {
	b, _ := newTestBreaker(t, testConfig())

	for i := 0; i < 50; i++ {
		call(t, b, Success)
	}

	assertState(t, b, StateClosed)
	if got := b.Snapshot().Rejected; got != 0 {
		t.Fatalf("rejected = %d, want 0", got)
	}
}

// Failures below the ratio must not open the breaker. A quarter of calls
// failing is a bad day for the upstream, not an outage, and shedding traffic
// there would cost more availability than it saves.
func TestFailuresBelowTheRatioKeepTheBreakerClosed(t *testing.T) {
	b, _ := newTestBreaker(t, testConfig())

	for i := 0; i < 5; i++ {
		call(t, b, Failure)
		call(t, b, Success)
		call(t, b, Success)
		call(t, b, Success)
	}

	assertState(t, b, StateClosed)
	if got := b.Snapshot().FailureRatio; got != 0.25 {
		t.Fatalf("failure ratio = %v, want 0.25", got)
	}
}

// The floor that stops a ratio from being meaningless at tiny sample sizes.
// Three failures out of three is a 100% failure ratio, and without MinRequests
// a single blip on an idle upstream would shed all its traffic.
func TestRatioAloneCannotOpenTheBreakerBelowMinRequests(t *testing.T) {
	cfg := testConfig()
	cfg.MinRequests = 4
	b, _ := newTestBreaker(t, cfg)

	for i := 0; i < 3; i++ {
		call(t, b, Failure)
	}
	assertState(t, b, StateClosed)

	// The fourth failure reaches MinRequests, and the ratio is 1.0.
	call(t, b, Failure)
	assertState(t, b, StateOpen)
}

// The ratio is configurable, and the configured value is what decides.
func TestFailureRatioIsConfigurable(t *testing.T) {
	cases := []struct {
		name      string
		ratio     float64
		failures  int
		successes int
		wantState State
	}{
		{"strict ratio trips on a fifth of calls", 0.2, 2, 8, StateOpen},
		{"default ratio does not trip on a fifth of calls", 0.5, 2, 8, StateClosed},
		{"tolerant ratio survives three quarters failing", 0.8, 6, 2, StateClosed},
		{"tolerant ratio trips once four fifths fail", 0.8, 8, 2, StateOpen},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testConfig()
			cfg.FailureRatio = tc.ratio
			b, _ := newTestBreaker(t, cfg)

			// Successes first, then failures, so the running ratio only ever
			// approaches its final value from below. Driving the failures
			// first would trip every case on the opening burst and prove
			// nothing about the configured ratio.
			for i := 0; i < tc.successes; i++ {
				call(t, b, Success)
			}
			for i := 0; i < tc.failures; i++ {
				if b.State() != StateClosed {
					break
				}
				call(t, b, Failure)
			}

			assertState(t, b, tc.wantState)
		})
	}
}

// Exactly at the ratio opens: the threshold is "at or above", and an upstream
// failing precisely half its calls is not serving.
func TestBreakerOpensAtExactlyTheRatio(t *testing.T) {
	b, _ := newTestBreaker(t, testConfig())

	call(t, b, Success)
	call(t, b, Success)
	call(t, b, Failure)
	assertState(t, b, StateClosed)

	call(t, b, Failure) // 2/4 = 0.5, and MinRequests is 4.
	assertState(t, b, StateOpen)
}

// Old failures must not hold the breaker near its threshold forever. Once the
// window has rolled past them they are gone.
func TestOutcomesExpireOutOfTheRollingWindow(t *testing.T) {
	b, clock := newTestBreaker(t, testConfig())

	call(t, b, Failure)
	call(t, b, Failure)
	call(t, b, Failure)
	if got := b.Snapshot().Failures; got != 3 {
		t.Fatalf("failures = %d, want 3", got)
	}

	clock.Advance(61 * time.Second)

	snapshot := b.Snapshot()
	if snapshot.Total != 0 || snapshot.Failures != 0 {
		t.Fatalf("window still holds %d/%d after it elapsed, want 0/0", snapshot.Failures, snapshot.Total)
	}

	// And those expired failures cannot combine with new ones to trip it.
	call(t, b, Failure)
	call(t, b, Failure)
	call(t, b, Failure)
	assertState(t, b, StateClosed)
}

// ---------------------------------------------------------------------------
// OPEN
// ---------------------------------------------------------------------------

// The core promise: while open, no call is attempted and the caller is told
// immediately.
func TestOpenBreakerRejectsWithoutCallingTheUpstream(t *testing.T) {
	b, _ := newTestBreaker(t, testConfig())
	tripBreaker(t, b)

	for i := 0; i < 100; i++ {
		mustReject(t, b)
	}

	if got := b.Snapshot().Rejected; got != 100 {
		t.Fatalf("rejected = %d, want 100", got)
	}
}

// The rejection is recognisable, so an API layer can answer 503 with a
// Retry-After rather than a generic 500.
func TestOpenErrorIsTypedAndCarriesRetryHint(t *testing.T) {
	b, clock := newTestBreaker(t, testConfig())
	tripBreaker(t, b)

	err := mustReject(t, b)

	var openErr *OpenError
	if !errors.As(err, &openErr) {
		t.Fatalf("error %v is not an *OpenError", err)
	}
	if openErr.Name != "test_upstream" {
		t.Fatalf("name = %q, want %q", openErr.Name, "test_upstream")
	}
	if openErr.RetryIn != 15*time.Second {
		t.Fatalf("retry in = %v, want 15s", openErr.RetryIn)
	}

	clock.Advance(10 * time.Second)
	err = mustReject(t, b)
	_ = errors.As(err, &openErr)
	if openErr.RetryIn != 5*time.Second {
		t.Fatalf("retry in after 10s = %v, want 5s", openErr.RetryIn)
	}
}

// Before the open period elapses, nothing gets through — not even one probe.
func TestBreakerStaysOpenForTheFullOpenDuration(t *testing.T) {
	b, clock := newTestBreaker(t, testConfig())
	tripBreaker(t, b)

	clock.Advance(14 * time.Second)
	mustReject(t, b)
	assertState(t, b, StateOpen)
}

// ---------------------------------------------------------------------------
// HALF-OPEN
// ---------------------------------------------------------------------------

// The full recovery path the acceptance criteria name:
// CLOSED -> OPEN -> HALF-OPEN -> CLOSED.
func TestBreakerRecoversThroughHalfOpen(t *testing.T) {
	b, clock := newTestBreaker(t, testConfig())

	assertState(t, b, StateClosed)

	tripBreaker(t, b)
	assertState(t, b, StateOpen)
	mustReject(t, b)

	clock.Advance(15 * time.Second)
	assertState(t, b, StateHalfOpen)

	// The probe is admitted and succeeds.
	permit, err := b.Allow()
	if err != nil {
		t.Fatalf("probe was refused after the open period: %v", err)
	}
	b.Record(permit, Success)

	assertState(t, b, StateClosed)

	// And normal service resumes.
	call(t, b, Success)
	assertState(t, b, StateClosed)
}

// The failure path: a probe that fails puts the breaker straight back to open
// for another full period, rather than letting the next caller through.
func TestFailedProbeReopensTheBreaker(t *testing.T) {
	b, clock := newTestBreaker(t, testConfig())
	tripBreaker(t, b)

	clock.Advance(15 * time.Second)

	permit, err := b.Allow()
	if err != nil {
		t.Fatalf("probe was refused: %v", err)
	}
	b.Record(permit, Failure)

	assertState(t, b, StateOpen)
	mustReject(t, b)

	// The open period restarts from the failed probe, not from the original
	// trip: another 15s must pass before the next probe.
	clock.Advance(14 * time.Second)
	assertState(t, b, StateOpen)
	clock.Advance(1 * time.Second)
	assertState(t, b, StateHalfOpen)
}

// A single probe, not a thundering herd. When the open period ends, the whole
// backlog must not be released onto an upstream that has only just come back.
func TestHalfOpenAdmitsExactlyOneProbe(t *testing.T) {
	b, clock := newTestBreaker(t, testConfig())
	tripBreaker(t, b)
	clock.Advance(15 * time.Second)

	permit, err := b.Allow()
	if err != nil {
		t.Fatalf("first caller was refused the probe: %v", err)
	}

	// While that probe is in flight every other caller keeps failing fast.
	for i := 0; i < 50; i++ {
		mustReject(t, b)
	}

	b.Record(permit, Success)
	assertState(t, b, StateClosed)
}

// The concurrent form of the same guarantee, run under -race: exactly one of
// N simultaneous callers may probe.
func TestConcurrentCallersProduceOneProbe(t *testing.T) {
	b, clock := newTestBreaker(t, testConfig())
	tripBreaker(t, b)
	clock.Advance(15 * time.Second)

	const callers = 64
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		granted int
	)

	// Deliberately no Record: the probe stays in flight for the duration of
	// the test. Completing it would close the breaker and the remaining
	// callers would then be admitted legitimately, which measures nothing.
	// What is under test is that while one probe is outstanding, no second
	// caller gets through.
	start := make(chan struct{})
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if _, err := b.Allow(); err != nil {
				return
			}
			mu.Lock()
			granted++
			mu.Unlock()
		}()
	}

	close(start)
	wg.Wait()

	if granted != 1 {
		t.Fatalf("%d callers were admitted during half-open, want exactly 1", granted)
	}
}

// A cancelled probe must release the slot. Without this the breaker would sit
// in half-open forever: no probe in flight, but no probe admissible either,
// and the upstream could never be found healthy again.
func TestCancelledProbeReleasesTheSlot(t *testing.T) {
	b, clock := newTestBreaker(t, testConfig())
	tripBreaker(t, b)
	clock.Advance(15 * time.Second)

	permit, err := b.Allow()
	if err != nil {
		t.Fatalf("probe was refused: %v", err)
	}
	b.Record(permit, Ignored)

	// Still half-open — the cancellation proved nothing either way — but the
	// next caller can now probe.
	assertState(t, b, StateHalfOpen)

	permit, err = b.Allow()
	if err != nil {
		t.Fatalf("slot was not released after a cancelled probe: %v", err)
	}
	b.Record(permit, Success)
	assertState(t, b, StateClosed)
}

// An outcome from before a state change must not be applied afterwards.
// Otherwise a call that started during the outage could close a breaker that
// re-opened while it was in flight.
func TestStaleOutcomesAreDiscarded(t *testing.T) {
	b, _ := newTestBreaker(t, testConfig())

	// A permit taken while closed...
	stale, err := b.Allow()
	if err != nil {
		t.Fatalf("Allow() = %v", err)
	}

	// ...the breaker trips on other evidence...
	tripBreaker(t, b)
	assertState(t, b, StateOpen)

	// ...and the old call finally succeeds. It must not count.
	b.Record(stale, Success)
	assertState(t, b, StateOpen)
}

// ---------------------------------------------------------------------------
// Isolation, config, and reporting
// ---------------------------------------------------------------------------

// Soroban and Horizon fail independently. One upstream's outage must never
// shed the other's traffic — that would turn a partial failure into the total
// one this feature exists to prevent.
func TestBreakersDoNotShareState(t *testing.T) {
	clock := newFakeClock()
	soroban := NewWithClock("soroban_rpc", testConfig(), nil, clock.Now)
	horizon := NewWithClock("horizon", testConfig(), nil, clock.Now)

	tripBreaker(t, soroban)

	assertState(t, soroban, StateOpen)
	assertState(t, horizon, StateClosed)

	// Horizon keeps serving while Soroban sheds.
	for i := 0; i < 10; i++ {
		call(t, horizon, Success)
	}
	assertState(t, horizon, StateClosed)
	mustReject(t, soroban)

	// And the reverse.
	tripBreaker(t, horizon)
	assertState(t, horizon, StateOpen)
}

// Closing resets the window. Carrying the failures that opened the breaker
// into the recovered state would re-trip it on the very first new failure,
// making recovery impossible while any traffic still failed occasionally.
func TestClosingResetsTheWindow(t *testing.T) {
	b, clock := newTestBreaker(t, testConfig())
	tripBreaker(t, b)
	clock.Advance(15 * time.Second)

	permit, _ := b.Allow()
	b.Record(permit, Success)

	snapshot := b.Snapshot()
	if snapshot.Total != 0 || snapshot.Failures != 0 {
		t.Fatalf("window carried %d/%d into the closed state, want 0/0", snapshot.Failures, snapshot.Total)
	}

	// One failure after recovery must not immediately re-open it.
	call(t, b, Failure)
	assertState(t, b, StateClosed)
}

// Snapshot reports the state a call would actually meet. An open breaker whose
// period has elapsed is ready to probe, and reporting it as open would tell an
// operator traffic is still being shed when it is not.
func TestSnapshotReportsHalfOpenOnceThePeriodElapsesWithoutACall(t *testing.T) {
	b, clock := newTestBreaker(t, testConfig())
	tripBreaker(t, b)

	if got := b.Snapshot().State; got != StateOpen {
		t.Fatalf("state = %s, want open", got)
	}

	clock.Advance(15 * time.Second)

	// No Allow() in between — the transition is derived from the clock.
	snapshot := b.Snapshot()
	if snapshot.State != StateHalfOpen {
		t.Fatalf("state = %s, want half_open", snapshot.State)
	}
	if snapshot.RetryIn != 0 {
		t.Fatalf("retry in = %v, want 0 once the period has elapsed", snapshot.RetryIn)
	}
}

func TestTransitionsAreReportedOnce(t *testing.T) {
	clock := newFakeClock()

	var (
		mu     sync.Mutex
		events []string
	)
	record := func(name string, from, to State, _ Snapshot) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, from.String()+"->"+to.String())
	}

	b := NewWithClock("test_upstream", testConfig(), record, clock.Now)

	tripBreaker(t, b)
	clock.Advance(15 * time.Second)
	permit, _ := b.Allow()
	b.Record(permit, Success)

	mu.Lock()
	defer mu.Unlock()

	want := []string{"closed->open", "open->half_open", "half_open->closed"}
	if len(events) != len(want) {
		t.Fatalf("transitions = %v, want %v", events, want)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Fatalf("transitions = %v, want %v", events, want)
		}
	}
}

// Rejections are never reported as transitions: an open breaker rejecting
// thousands of calls must not produce thousands of log lines.
func TestRejectionsAreNotReportedAsTransitions(t *testing.T) {
	clock := newFakeClock()

	var mu sync.Mutex
	count := 0
	record := func(string, State, State, Snapshot) {
		mu.Lock()
		count++
		mu.Unlock()
	}

	b := NewWithClock("test_upstream", testConfig(), record, clock.Now)
	tripBreaker(t, b)

	mu.Lock()
	afterTrip := count
	mu.Unlock()

	for i := 0; i < 500; i++ {
		mustReject(t, b)
	}

	mu.Lock()
	defer mu.Unlock()
	if count != afterTrip {
		t.Fatalf("500 rejections produced %d extra transition callbacks, want 0", count-afterTrip)
	}
}

func TestZeroConfigUsesDefaults(t *testing.T) {
	b := New("test_upstream", Config{}, nil)

	if b.cfg.FailureRatio != DefaultFailureRatio {
		t.Errorf("failure ratio = %v, want %v", b.cfg.FailureRatio, DefaultFailureRatio)
	}
	if b.cfg.MinRequests != DefaultMinRequests {
		t.Errorf("min requests = %d, want %d", b.cfg.MinRequests, DefaultMinRequests)
	}
	if b.cfg.Window != DefaultWindow {
		t.Errorf("window = %v, want %v", b.cfg.Window, DefaultWindow)
	}
	if b.cfg.OpenDuration != DefaultOpenDuration {
		t.Errorf("open duration = %v, want %v", b.cfg.OpenDuration, DefaultOpenDuration)
	}
}

func TestConfigValidate(t *testing.T) {
	valid := testConfig()
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}

	cases := map[string]func(*Config){
		"zero ratio":         func(c *Config) { c.FailureRatio = 0 },
		"negative ratio":     func(c *Config) { c.FailureRatio = -0.1 },
		"ratio above one":    func(c *Config) { c.FailureRatio = 1.5 },
		"zero min requests":  func(c *Config) { c.MinRequests = 0 },
		"zero window":        func(c *Config) { c.Window = 0 },
		"zero open duration": func(c *Config) { c.OpenDuration = 0 },
	}

	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := testConfig()
			mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("Validate() = nil, want an error")
			}
		})
	}
}

// Concurrent callers hammering a breaker through its whole lifecycle. Run
// under -race, this is what proves the state machine is safe to share.
func TestConcurrentUseIsRaceFree(t *testing.T) {
	b := New("test_upstream", testConfig(), func(string, State, State, Snapshot) {})

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				permit, err := b.Allow()
				if err != nil {
					continue
				}
				outcome := Success
				if (id+j)%3 == 0 {
					outcome = Failure
				}
				b.Record(permit, outcome)
			}
		}(i)
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				_ = b.Snapshot()
			}
		}()
	}
	wg.Wait()
}

// tripBreaker drives a closed breaker to open by failing calls until it trips.
//
// The bound is generous rather than exact because the number of failures
// required depends on what is already in the window: a breaker that has just
// served successful calls needs enough failures to drag the ratio over the
// threshold, not merely MinRequests of them.
func tripBreaker(t *testing.T, b *Breaker) {
	t.Helper()

	const maxAttempts = 200
	for i := 0; i < maxAttempts; i++ {
		if b.State() == StateOpen {
			return
		}
		permit, err := b.Allow()
		if err != nil {
			break
		}
		b.Record(permit, Failure)
	}

	if b.State() != StateOpen {
		t.Fatalf("breaker did not open after %d failures", maxAttempts)
	}
}
