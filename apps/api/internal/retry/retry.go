// Package retry provides the bounded, jittered retry loop that every Soroban
// RPC call site shares (nester#1086).
//
// Before this, transient RPC failures surfaced directly as user-facing errors
// and each call site decided for itself whether to try again — which in
// practice meant none of them did. A single connection reset during a balance
// read became an error on the user's screen.
//
// The loop is deliberately bounded in three independent ways, because any one
// of them alone fails somewhere:
//
//	MaxAttempts  caps the work done for one logical call.
//	Budget       caps the wall time, so three attempts against an endpoint
//	             that hangs cannot outlive the caller's own deadline.
//	ctx          still wins over both; a cancelled caller stops immediately.
//
// # Jitter
//
// Backoff is exponential with **full jitter**: the delay is drawn uniformly
// from [0, cap) rather than being cap exactly. Retrying on a fixed schedule
// synchronises every client that failed at the same moment — the retry storm
// arrives together and knocks the recovering endpoint back over. Spreading the
// attempts is the entire reason jitter exists here, and drawing from the whole
// interval spreads them better than jittering around a fixed point.
//
// # What this package does NOT decide
//
// Whether a given error is worth retrying, and whether a given operation is
// safe to retry at all. Both are the caller's business: this package retries
// exactly what it is told to. See internal/stellar for the Soroban rules —
// notably that writes are never retried here, because a resubmitted
// transaction is a double spend, and they go through the submission record
// instead.
package retry

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"time"
)

// Defaults sized for Soroban RPC on a user request path.
//
// Three attempts, not more: these calls sit inside HTTP handlers with their
// own write timeout, and a fourth attempt buys little once two have failed
// while costing every waiting user another backoff. The 12s budget is
// comfortably above any healthy chain read and below the API's 15s write
// timeout, so the retry loop can never be the thing that holds a request open
// longest.
const (
	DefaultMaxAttempts = 3
	DefaultBaseDelay   = 100 * time.Millisecond
	DefaultMaxDelay    = 2 * time.Second
	DefaultBudget      = 12 * time.Second
)

// Policy bounds one retry loop. The zero value is usable and takes the
// defaults above.
type Policy struct {
	// MaxAttempts is the total number of calls, including the first. 1
	// disables retrying without disabling the helper.
	MaxAttempts int

	// BaseDelay is the backoff cap for the first retry; it doubles per
	// attempt up to MaxDelay.
	BaseDelay time.Duration

	// MaxDelay caps the exponential growth.
	MaxDelay time.Duration

	// Budget is the total wall-clock allowance for the whole loop, including
	// backoff. It is applied as a context deadline, so a single hung attempt
	// cannot outlive it.
	Budget time.Duration
}

func (p Policy) withDefaults() Policy {
	if p.MaxAttempts <= 0 {
		p.MaxAttempts = DefaultMaxAttempts
	}
	if p.BaseDelay <= 0 {
		p.BaseDelay = DefaultBaseDelay
	}
	if p.MaxDelay <= 0 {
		p.MaxDelay = DefaultMaxDelay
	}
	if p.Budget <= 0 {
		p.Budget = DefaultBudget
	}
	return p
}

// Validate reports a policy that cannot behave sensibly, so startup can refuse
// it rather than letting it become an incident.
func (p Policy) Validate() error {
	if p.MaxAttempts < 1 {
		return fmt.Errorf("max attempts must be at least 1, got %d", p.MaxAttempts)
	}
	if p.BaseDelay <= 0 {
		return fmt.Errorf("base delay must be greater than 0, got %v", p.BaseDelay)
	}
	if p.MaxDelay < p.BaseDelay {
		return fmt.Errorf("max delay %v must be at least the base delay %v", p.MaxDelay, p.BaseDelay)
	}
	if p.Budget <= 0 {
		return fmt.Errorf("budget must be greater than 0, got %v", p.Budget)
	}
	return nil
}

// ErrExhausted marks a call that used up its attempts or its budget. Callers
// match it with errors.Is to answer "the upstream never gave us an answer",
// which is a different condition from "the upstream said no".
var ErrExhausted = errors.New("retry attempts exhausted")

// Error is the typed failure returned when a retried call never succeeded.
//
// It carries the last error so the underlying cause is not lost, and the shape
// of the attempt so an operator reading a log line knows whether the loop ran
// out of attempts or ran out of time — which point at different problems.
type Error struct {
	Attempts int
	Elapsed  time.Duration
	Err      error
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s after %d attempt(s) in %s: %v",
		ErrExhausted.Error(), e.Attempts, e.Elapsed.Round(time.Millisecond), e.Err)
}

// Unwrap returns both sentinels so errors.Is matches ErrExhausted *and*
// anything the underlying error wraps. Callers that care about the cause
// (a status code, a transport failure) keep working unchanged.
func (e *Error) Unwrap() []error { return []error{ErrExhausted, e.Err} }

// Result describes how a completed loop ran, for metrics. It is returned on
// success and on failure, because "succeeded, but only on the third attempt"
// is exactly the signal that an upstream is degrading before it breaks.
type Result struct {
	Attempts int
	Elapsed  time.Duration
}

// Runner executes retry loops. One is shared by every call site; it holds no
// per-call state.
type Runner struct {
	now   func() time.Time
	sleep func(context.Context, time.Duration) error
	// jitter maps a backoff cap to the delay actually waited.
	jitter func(time.Duration) time.Duration
}

// New returns a Runner using the real clock and a concurrency-safe PRNG.
func New() *Runner {
	return &Runner{now: time.Now, sleep: sleepCtx, jitter: fullJitter}
}

// NewWithClock is New with the clock, sleep, and jitter injected, so tests can
// assert the exact backoff schedule without waiting for it.
//
// A nil jitter means "wait the full cap", which makes the schedule
// deterministic; that is the right default for a test and the wrong one for
// production, which is why it is only reachable through this constructor.
func NewWithClock(
	now func() time.Time,
	sleep func(context.Context, time.Duration) error,
	jitter func(time.Duration) time.Duration,
) *Runner {
	if now == nil {
		now = time.Now
	}
	if sleep == nil {
		sleep = sleepCtx
	}
	if jitter == nil {
		jitter = func(d time.Duration) time.Duration { return d }
	}
	return &Runner{now: now, sleep: sleep, jitter: jitter}
}

// Do runs fn until it succeeds, returns an error the caller does not want
// retried, or the loop runs out of attempts, budget, or context.
//
// retryable decides which errors are worth another attempt. A nil retryable
// means nothing is retried, so a caller that forgets to supply one gets the
// old single-attempt behaviour rather than silently retrying a write.
//
// The returned error is a *Error (matching ErrExhausted) only when attempts or
// budget ran out. An error the caller declined to retry is returned unchanged,
// because wrapping it would make every non-transient failure look like an
// upstream outage.
func (r *Runner) Do(
	ctx context.Context,
	policy Policy,
	retryable func(error) bool,
	fn func(context.Context) error,
) (Result, error) {
	policy = policy.withDefaults()

	// The budget is a context deadline rather than a check between attempts,
	// so a single attempt that hangs is cut off by it too. Without this, one
	// slow call against an unresponsive endpoint would ignore the budget
	// entirely and hold the caller for its client timeout instead.
	ctx, cancel := context.WithTimeout(ctx, policy.Budget)
	defer cancel()

	startedAt := r.now()
	var lastErr error

	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		// Checked before each attempt so a cancelled caller stops immediately
		// rather than paying for one more round trip.
		if err := ctx.Err(); err != nil {
			return r.exhausted(attempt-1, startedAt, lastErr, err)
		}

		err := fn(ctx)
		if err == nil {
			return Result{Attempts: attempt, Elapsed: r.since(startedAt)}, nil
		}
		lastErr = err

		if retryable == nil || !retryable(err) {
			return Result{Attempts: attempt, Elapsed: r.since(startedAt)}, err
		}
		if attempt == policy.MaxAttempts {
			break
		}

		delay := r.jitter(backoffCap(policy, attempt))
		if sleepErr := r.sleep(ctx, delay); sleepErr != nil {
			// The budget or the caller expired while backing off. The last
			// real error is the useful one to report, not the timeout.
			return r.exhausted(attempt, startedAt, lastErr, sleepErr)
		}
	}

	return r.exhausted(policy.MaxAttempts, startedAt, lastErr, nil)
}

func (r *Runner) exhausted(attempts int, startedAt time.Time, lastErr, ctxErr error) (Result, error) {
	if lastErr == nil {
		lastErr = ctxErr
	}
	result := Result{Attempts: attempts, Elapsed: r.since(startedAt)}
	return result, &Error{Attempts: attempts, Elapsed: result.Elapsed, Err: lastErr}
}

func (r *Runner) since(startedAt time.Time) time.Duration {
	elapsed := r.now().Sub(startedAt)
	if elapsed < 0 {
		// A clock stepping backwards must not report a negative latency into
		// a histogram.
		return 0
	}
	return elapsed
}

// backoffCap is the upper bound on the delay before the given attempt's retry:
// BaseDelay doubled once per elapsed attempt, clamped to MaxDelay.
func backoffCap(policy Policy, attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}

	// Shifting is bounded before it is applied: 2^62 nanoseconds already
	// exceeds any sane MaxDelay, and shifting past 63 would wrap to zero and
	// silently turn the backoff off.
	const maxShift = 62
	shift := attempt - 1
	if shift > maxShift {
		return policy.MaxDelay
	}

	scaled := float64(policy.BaseDelay) * math.Pow(2, float64(shift))
	if scaled >= float64(policy.MaxDelay) {
		return policy.MaxDelay
	}
	return time.Duration(scaled)
}

// fullJitter draws uniformly from [0, cap). See the package comment for why
// the whole interval is used rather than a band around cap.
func fullJitter(cap time.Duration) time.Duration {
	if cap <= 0 {
		return 0
	}
	// Backoff jitter only has to be unpredictable enough to keep retrying
	// clients from synchronising; it guards no secret, and drawing from
	// crypto/rand on every retry would add a syscall to the failure path.
	// #nosec G404 -- non-cryptographic use.
	return time.Duration(rand.Int64N(int64(cap)))
}

// sleepCtx waits for d, or returns early with the context's error.
//
// It uses a timer rather than time.Sleep so a cancelled caller is released
// immediately instead of after the full backoff, and the timer is always
// stopped so a cancelled wait leaks nothing.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
