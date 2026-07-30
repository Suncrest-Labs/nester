package jobqueue

import (
	"math"
	"math/rand"
	"time"
)

// BackoffConfig parameterizes exponential backoff with full jitter.
type BackoffConfig struct {
	// Base is the delay before the first retry (attempt 1).
	Base time.Duration
	// Max caps the delay regardless of attempt count.
	Max time.Duration
}

// backoffDuration returns the delay before the next retry of a job that has
// already failed `attempt` times (attempt >= 1). It is exponential in attempt
// (Base * 2^(attempt-1)), clamped to Max, with "full jitter": the returned
// value is uniformly random in [0, cap] where cap is the clamped exponential.
//
// Full jitter (rather than fixed backoff) spreads retries so a burst of jobs
// that fail together — e.g. during a downstream outage — do not synchronize
// their retries into a thundering herd when the dependency recovers.
//
// rng may be nil, in which case the shared default source is used.
func backoffDuration(cfg BackoffConfig, attempt int, rng *rand.Rand) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	base := cfg.Base
	if base <= 0 {
		base = time.Second
	}
	max := cfg.Max
	if max <= 0 {
		max = 5 * time.Minute
	}

	// Compute base * 2^(attempt-1) in float to avoid integer overflow on large
	// attempt counts, then clamp to max.
	exp := float64(base) * math.Pow(2, float64(attempt-1))
	capNanos := float64(max)
	if exp < capNanos {
		capNanos = exp
	}
	if capNanos < 0 {
		capNanos = float64(max)
	}

	var jittered float64
	if rng != nil {
		jittered = rng.Float64() * capNanos
	} else {
		jittered = rand.Float64() * capNanos //nolint:gosec // jitter, not security-sensitive
	}
	return time.Duration(jittered)
}
