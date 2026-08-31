package retry

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeClock records the delays the loop asked for instead of waiting them out.
// Every backoff assertion below is about a schedule measured in seconds;
// sleeping through it would make the suite slow and flaky, and would not let a
// test see the delays at all.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	delays []time.Duration
	// cancelAfter, when positive, makes the Nth sleep report the context as
	// expired, standing in for the budget running out mid-backoff.
	cancelAfter int
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Sleep(_ context.Context, d time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.delays = append(c.delays, d)
	c.now = c.now.Add(d)

	if c.cancelAfter > 0 && len(c.delays) >= c.cancelAfter {
		return context.DeadlineExceeded
	}
	return nil
}

func (c *fakeClock) Delays() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.delays...)
}

func testPolicy() Policy {
	return Policy{
		MaxAttempts: 3,
		BaseDelay:   100 * time.Millisecond,
		MaxDelay:    2 * time.Second,
		Budget:      12 * time.Second,
	}
}

// newTestRunner returns a runner with no jitter, so the backoff schedule is
// exactly the cap and can be asserted.
func newTestRunner(clock *fakeClock) *Runner {
	return NewWithClock(clock.Now, clock.Sleep, nil)
}

var errTransient = errors.New("connection reset")

func alwaysRetryable(error) bool { return true }

// ---------------------------------------------------------------------------
// The happy paths
// ---------------------------------------------------------------------------

func TestSuccessOnFirstAttemptDoesNotSleep(t *testing.T) {
	clock := newFakeClock()

	calls := 0
	result, err := newTestRunner(clock).Do(context.Background(), testPolicy(), alwaysRetryable,
		func(context.Context) error {
			calls++
			return nil
		})

	if err != nil {
		t.Fatalf("Do() = %v, want nil", err)
	}
	if calls != 1 {
		t.Fatalf("fn called %d times, want 1", calls)
	}
	if result.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", result.Attempts)
	}
	if got := clock.Delays(); len(got) != 0 {
		t.Fatalf("slept %v on a first-attempt success, want no sleeps", got)
	}
}

// The reason the feature exists: a transient failure is absorbed instead of
// reaching the user.
func TestTransientFailureIsAbsorbed(t *testing.T) {
	clock := newFakeClock()

	calls := 0
	result, err := newTestRunner(clock).Do(context.Background(), testPolicy(), alwaysRetryable,
		func(context.Context) error {
			calls++
			if calls < 3 {
				return errTransient
			}
			return nil
		})

	if err != nil {
		t.Fatalf("Do() = %v, want nil after recovery", err)
	}
	if calls != 3 {
		t.Fatalf("fn called %d times, want 3", calls)
	}
	if result.Attempts != 3 {
		t.Fatalf("attempts = %d, want 3: the metric must show it took three tries", result.Attempts)
	}
}

// ---------------------------------------------------------------------------
// Backoff schedule
// ---------------------------------------------------------------------------

func TestBackoffIsExponentialAndCapped(t *testing.T) {
	clock := newFakeClock()
	policy := Policy{MaxAttempts: 6, BaseDelay: 100 * time.Millisecond, MaxDelay: 500 * time.Millisecond, Budget: time.Minute}

	_, err := newTestRunner(clock).Do(context.Background(), policy, alwaysRetryable,
		func(context.Context) error { return errTransient })
	if !errors.Is(err, ErrExhausted) {
		t.Fatalf("Do() = %v, want an exhaustion error", err)
	}

	// Five retries after six attempts: 100, 200, 400, then clamped at 500.
	want := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		400 * time.Millisecond,
		500 * time.Millisecond,
		500 * time.Millisecond,
	}

	got := clock.Delays()
	if len(got) != len(want) {
		t.Fatalf("delays = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("delays = %v, want %v", got, want)
		}
	}
}

// Full jitter must draw from the whole interval below the cap, never above it.
// Retrying on a fixed schedule synchronises every client that failed together,
// and the resulting storm is what knocks a recovering endpoint back over.
func TestFullJitterStaysWithinTheCap(t *testing.T) {
	for _, cap := range []time.Duration{time.Millisecond, 100 * time.Millisecond, 2 * time.Second} {
		belowCap := false
		for i := 0; i < 200; i++ {
			got := fullJitter(cap)
			if got < 0 || got >= cap {
				t.Fatalf("fullJitter(%v) = %v, want within [0, %v)", cap, got, cap)
			}
			if got < cap/2 {
				belowCap = true
			}
		}
		// Not a distribution test — just proof it is not returning the cap
		// every time, which would be no jitter at all.
		if !belowCap {
			t.Errorf("fullJitter(%v) never returned a value below half the cap", cap)
		}
	}
}

func TestFullJitterHandlesZeroCap(t *testing.T) {
	if got := fullJitter(0); got != 0 {
		t.Fatalf("fullJitter(0) = %v, want 0", got)
	}
	if got := fullJitter(-time.Second); got != 0 {
		t.Fatalf("fullJitter(negative) = %v, want 0", got)
	}
}

// A pathological attempt count must not shift the backoff into a wrap.
func TestBackoffCapDoesNotOverflow(t *testing.T) {
	policy := Policy{BaseDelay: time.Second, MaxDelay: 30 * time.Second}.withDefaults()

	for _, attempt := range []int{0, 1, 64, 1000, 1 << 30} {
		got := backoffCap(policy, attempt)
		if got <= 0 {
			t.Fatalf("backoffCap(attempt=%d) = %v, want a positive delay", attempt, got)
		}
		if got > policy.MaxDelay {
			t.Fatalf("backoffCap(attempt=%d) = %v, want at most %v", attempt, got, policy.MaxDelay)
		}
	}
}

// ---------------------------------------------------------------------------
// Bounds
// ---------------------------------------------------------------------------

func TestMaxAttemptsIsRespected(t *testing.T) {
	for _, maxAttempts := range []int{1, 2, 5} {
		t.Run("", func(t *testing.T) {
			clock := newFakeClock()
			policy := testPolicy()
			policy.MaxAttempts = maxAttempts

			calls := 0
			result, err := newTestRunner(clock).Do(context.Background(), policy, alwaysRetryable,
				func(context.Context) error {
					calls++
					return errTransient
				})

			if calls != maxAttempts {
				t.Fatalf("fn called %d times, want %d", calls, maxAttempts)
			}
			if result.Attempts != maxAttempts {
				t.Fatalf("attempts = %d, want %d", result.Attempts, maxAttempts)
			}
			if !errors.Is(err, ErrExhausted) {
				t.Fatalf("Do() = %v, want an exhaustion error", err)
			}
			if got := len(clock.Delays()); got != maxAttempts-1 {
				t.Fatalf("slept %d times for %d attempts, want %d", got, maxAttempts, maxAttempts-1)
			}
		})
	}
}

// MaxAttempts of 1 turns retrying off without turning the helper off, so a
// deployment can disable retries without losing the metrics or the typed
// error.
func TestSingleAttemptPolicyNeverRetries(t *testing.T) {
	clock := newFakeClock()
	policy := testPolicy()
	policy.MaxAttempts = 1

	calls := 0
	_, err := newTestRunner(clock).Do(context.Background(), policy, alwaysRetryable,
		func(context.Context) error {
			calls++
			return errTransient
		})

	if calls != 1 {
		t.Fatalf("fn called %d times, want 1", calls)
	}
	if len(clock.Delays()) != 0 {
		t.Fatalf("slept %v with retries disabled", clock.Delays())
	}
	if !errors.Is(err, ErrExhausted) {
		t.Fatalf("Do() = %v, want an exhaustion error", err)
	}
}

// The budget running out mid-backoff stops the loop, and the reported error is
// the last real failure rather than the timeout that ended the wait.
func TestBudgetExhaustionStopsTheLoop(t *testing.T) {
	clock := newFakeClock()
	clock.cancelAfter = 1

	calls := 0
	result, err := newTestRunner(clock).Do(context.Background(), testPolicy(), alwaysRetryable,
		func(context.Context) error {
			calls++
			return errTransient
		})

	if calls != 1 {
		t.Fatalf("fn called %d times, want 1: the budget expired during the first backoff", calls)
	}
	if !errors.Is(err, ErrExhausted) {
		t.Fatalf("Do() = %v, want an exhaustion error", err)
	}
	if !errors.Is(err, errTransient) {
		t.Fatalf("Do() = %v, want it to carry the underlying failure", err)
	}
	if result.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", result.Attempts)
	}
}

// A slow attempt must be cut off by the budget rather than running to its own
// client timeout. Without this the budget would bound only the backoff, which
// is the cheap part.
func TestBudgetBoundsASlowAttempt(t *testing.T) {
	policy := testPolicy()
	policy.Budget = 50 * time.Millisecond

	started := time.Now()
	_, err := New().Do(context.Background(), policy, alwaysRetryable,
		func(ctx context.Context) error {
			// Stands in for a hung upstream with a far longer client timeout.
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
				return nil
			}
		})
	elapsed := time.Since(started)

	if !errors.Is(err, ErrExhausted) {
		t.Fatalf("Do() = %v, want an exhaustion error", err)
	}
	if elapsed > time.Second {
		t.Fatalf("a hung attempt ran for %v against a 50ms budget", elapsed)
	}
}

// A cancelled caller stops the loop immediately; it does not pay for another
// round trip or another backoff.
func TestContextCancellationStopsImmediately(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	clock := newFakeClock()
	calls := 0
	_, err := newTestRunner(clock).Do(ctx, testPolicy(), alwaysRetryable,
		func(context.Context) error {
			calls++
			return errTransient
		})

	if calls != 0 {
		t.Fatalf("fn called %d times with an already-cancelled context, want 0", calls)
	}
	if !errors.Is(err, ErrExhausted) {
		t.Fatalf("Do() = %v, want an exhaustion error", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Do() = %v, want it to carry the cancellation", err)
	}
}

// ---------------------------------------------------------------------------
// What is and is not retried
// ---------------------------------------------------------------------------

// An error the caller declines to retry is returned unchanged. Wrapping it
// would make every deterministic failure look like an upstream outage, and the
// handler layer maps those to different HTTP statuses.
func TestNonRetryableErrorIsReturnedUnwrapped(t *testing.T) {
	clock := newFakeClock()
	sentinel := errors.New("contract rejected the call")

	calls := 0
	result, err := newTestRunner(clock).Do(context.Background(), testPolicy(),
		func(error) bool { return false },
		func(context.Context) error {
			calls++
			return sentinel
		})

	if calls != 1 {
		t.Fatalf("fn called %d times, want 1", calls)
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("Do() = %v, want the original error", err)
	}
	if errors.Is(err, ErrExhausted) {
		t.Fatal("a declined error was reported as retry exhaustion")
	}
	if result.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", result.Attempts)
	}
}

// The safety default. A caller that supplies no predicate gets one attempt,
// not unlimited retries — because the operation this package is most likely to
// be pointed at without thought is a write.
func TestNilRetryablePredicateRetriesNothing(t *testing.T) {
	clock := newFakeClock()

	calls := 0
	_, err := newTestRunner(clock).Do(context.Background(), testPolicy(), nil,
		func(context.Context) error {
			calls++
			return errTransient
		})

	if calls != 1 {
		t.Fatalf("fn called %d times with a nil predicate, want 1", calls)
	}
	if !errors.Is(err, errTransient) {
		t.Fatalf("Do() = %v, want the original error", err)
	}
}

// Only the errors the predicate accepts are retried, and the loop stops the
// moment it sees one it does not.
func TestLoopStopsOnTheFirstNonRetryableError(t *testing.T) {
	clock := newFakeClock()
	permanent := errors.New("bad request")

	calls := 0
	_, err := newTestRunner(clock).Do(context.Background(), testPolicy(),
		func(err error) bool { return errors.Is(err, errTransient) },
		func(context.Context) error {
			calls++
			if calls == 1 {
				return errTransient
			}
			return permanent
		})

	if calls != 2 {
		t.Fatalf("fn called %d times, want 2", calls)
	}
	if !errors.Is(err, permanent) {
		t.Fatalf("Do() = %v, want the permanent error", err)
	}
}

// ---------------------------------------------------------------------------
// The typed error
// ---------------------------------------------------------------------------

// Exhaustion must be recognisable as exhaustion *and* keep its cause, so the
// handler layer can answer 503 while the log line still names what broke.
func TestExhaustionErrorIsTypedAndKeepsItsCause(t *testing.T) {
	clock := newFakeClock()

	_, err := newTestRunner(clock).Do(context.Background(), testPolicy(), alwaysRetryable,
		func(context.Context) error { return errTransient })

	if !errors.Is(err, ErrExhausted) {
		t.Fatalf("errors.Is(%v, ErrExhausted) = false", err)
	}
	if !errors.Is(err, errTransient) {
		t.Fatalf("errors.Is(%v, errTransient) = false: the cause was lost", err)
	}

	var retryErr *Error
	if !errors.As(err, &retryErr) {
		t.Fatalf("errors.As(%v, *Error) = false", err)
	}
	if retryErr.Attempts != 3 {
		t.Fatalf("Attempts = %d, want 3", retryErr.Attempts)
	}
	if retryErr.Err == nil {
		t.Fatal("Err is nil; the underlying failure was dropped")
	}
	if got := retryErr.Error(); got == "" {
		t.Fatal("Error() is empty")
	}
}

// ---------------------------------------------------------------------------
// Policy
// ---------------------------------------------------------------------------

func TestZeroPolicyUsesDefaults(t *testing.T) {
	got := Policy{}.withDefaults()

	if got.MaxAttempts != DefaultMaxAttempts {
		t.Errorf("max attempts = %d, want %d", got.MaxAttempts, DefaultMaxAttempts)
	}
	if got.BaseDelay != DefaultBaseDelay {
		t.Errorf("base delay = %v, want %v", got.BaseDelay, DefaultBaseDelay)
	}
	if got.MaxDelay != DefaultMaxDelay {
		t.Errorf("max delay = %v, want %v", got.MaxDelay, DefaultMaxDelay)
	}
	if got.Budget != DefaultBudget {
		t.Errorf("budget = %v, want %v", got.Budget, DefaultBudget)
	}
}

func TestPolicyValidate(t *testing.T) {
	if err := testPolicy().Validate(); err != nil {
		t.Fatalf("valid policy rejected: %v", err)
	}

	cases := map[string]func(*Policy){
		"zero attempts":     func(p *Policy) { p.MaxAttempts = 0 },
		"negative attempts": func(p *Policy) { p.MaxAttempts = -1 },
		"zero base delay":   func(p *Policy) { p.BaseDelay = 0 },
		"max below base":    func(p *Policy) { p.MaxDelay = p.BaseDelay - time.Millisecond },
		"zero budget":       func(p *Policy) { p.Budget = 0 },
		"negative budget":   func(p *Policy) { p.Budget = -time.Second },
	}

	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			policy := testPolicy()
			mutate(&policy)
			if err := policy.Validate(); err == nil {
				t.Fatal("Validate() = nil, want an error")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Concurrency
// ---------------------------------------------------------------------------

// One Runner is shared by every call site. Run under -race, this is what
// proves that is safe — including the PRNG behind the jitter.
func TestRunnerIsSafeForConcurrentUse(t *testing.T) {
	runner := New()
	policy := Policy{MaxAttempts: 3, BaseDelay: time.Microsecond, MaxDelay: time.Microsecond, Budget: time.Minute}

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_, _ = runner.Do(context.Background(), policy, alwaysRetryable,
					func(context.Context) error {
						if (id+j)%2 == 0 {
							return errTransient
						}
						return nil
					})
			}
		}(i)
	}
	wg.Wait()
}

// sleepCtx must release a cancelled caller immediately rather than after the
// full backoff, and must not leak its timer either way.
func TestSleepCtxReleasesOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	started := time.Now()
	err := sleepCtx(ctx, 5*time.Second)
	elapsed := time.Since(started)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("sleepCtx() = %v, want context.Canceled", err)
	}
	if elapsed > time.Second {
		t.Fatalf("sleepCtx waited %v after cancellation", elapsed)
	}
}

func TestSleepCtxWaits(t *testing.T) {
	started := time.Now()
	if err := sleepCtx(context.Background(), 20*time.Millisecond); err != nil {
		t.Fatalf("sleepCtx() = %v, want nil", err)
	}
	if elapsed := time.Since(started); elapsed < 15*time.Millisecond {
		t.Fatalf("sleepCtx returned after %v, want at least ~20ms", elapsed)
	}
}
