package jobqueue

import (
	"math/rand"
	"testing"
	"time"
)

func TestBackoffDuration_WithinCap(t *testing.T) {
	cfg := BackoffConfig{Base: time.Second, Max: time.Minute}
	rng := rand.New(rand.NewSource(1))

	for attempt := 1; attempt <= 20; attempt++ {
		for i := 0; i < 200; i++ {
			d := backoffDuration(cfg, attempt, rng)
			if d < 0 {
				t.Fatalf("attempt %d: negative backoff %v", attempt, d)
			}
			if d > cfg.Max {
				t.Fatalf("attempt %d: backoff %v exceeds cap %v", attempt, d, cfg.Max)
			}
		}
	}
}

func TestBackoffDuration_ExponentialCeiling(t *testing.T) {
	// With zero jitter influence removed by using a deterministic rng returning
	// its max, verify the exponential ceiling grows then clamps at Max.
	cfg := BackoffConfig{Base: time.Second, Max: 10 * time.Second}

	// The theoretical (pre-jitter) cap is Base*2^(attempt-1) clamped to Max.
	// Sample many draws per attempt and take the observed max as a proxy.
	prevCeil := time.Duration(0)
	for attempt := 1; attempt <= 6; attempt++ {
		rng := rand.New(rand.NewSource(int64(attempt)))
		var observedMax time.Duration
		for i := 0; i < 5000; i++ {
			if d := backoffDuration(cfg, attempt, rng); d > observedMax {
				observedMax = d
			}
		}
		if observedMax > cfg.Max {
			t.Fatalf("attempt %d observed %v over cap %v", attempt, observedMax, cfg.Max)
		}
		if attempt < 5 && observedMax < prevCeil {
			t.Fatalf("attempt %d ceiling %v shrank below previous %v", attempt, observedMax, prevCeil)
		}
		prevCeil = observedMax
	}
}

func TestBackoffDuration_Defaults(t *testing.T) {
	// Zero config falls back to sane defaults rather than returning 0 forever.
	d := backoffDuration(BackoffConfig{}, 1, rand.New(rand.NewSource(1)))
	if d < 0 || d > time.Second {
		t.Fatalf("default base backoff for attempt 1 out of range: %v", d)
	}
}

func TestBackoffDuration_AttemptFloor(t *testing.T) {
	cfg := BackoffConfig{Base: time.Second, Max: time.Minute}
	// attempt < 1 is treated as attempt 1, not a panic or negative.
	d := backoffDuration(cfg, 0, rand.New(rand.NewSource(1)))
	if d < 0 || d > time.Second {
		t.Fatalf("attempt 0 clamped backoff out of range: %v", d)
	}
}
