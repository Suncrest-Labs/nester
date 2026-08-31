package middleware

import (
	"context"
	"testing"
	"time"
)

func backoffConfig() AuthLockoutConfig {
	return AuthLockoutConfig{
		Threshold: 3,
		Window:    time.Minute,
		Base:      30 * time.Second,
		Max:       10 * time.Minute,
	}
}

// The backoff is zero up to the threshold, then doubles per extra failure,
// capped at Max (nester#1104).
func TestAuthLockoutConfig_ProgressiveBackoff(t *testing.T) {
	cfg := backoffConfig()

	cases := []struct {
		failures int
		want     time.Duration
	}{
		{0, 0},
		{1, 0},
		{3, 0}, // at the threshold, still tolerated
		{4, 30 * time.Second},
		{5, 1 * time.Minute},
		{6, 2 * time.Minute},
		{7, 4 * time.Minute},
		{8, 8 * time.Minute},
		{9, 10 * time.Minute},  // capped
		{50, 10 * time.Minute}, // still capped
	}

	for _, tc := range cases {
		if got := cfg.lockoutFor(tc.failures); got != tc.want {
			t.Errorf("lockoutFor(%d) = %s, want %s", tc.failures, got, tc.want)
		}
	}
}

// A very large failure count must stay capped rather than overflowing the
// duration into a negative (which would read as "not locked").
func TestAuthLockoutConfig_LargeFailureCountDoesNotOverflow(t *testing.T) {
	cfg := backoffConfig()
	for _, failures := range []int{100, 1_000, 100_000} {
		got := cfg.lockoutFor(failures)
		if got != cfg.Max {
			t.Errorf("lockoutFor(%d) = %s, want the %s cap", failures, got, cfg.Max)
		}
		if got <= 0 {
			t.Fatalf("lockoutFor(%d) overflowed to %s", failures, got)
		}
	}
}

func TestMemoryAuthLockout_LocksAfterThresholdAndResets(t *testing.T) {
	ctx := context.Background()
	l := NewAuthLockout(nil, "test", backoffConfig())

	const key = "GWALLET"

	if locked, _ := l.Locked(ctx, key); locked {
		t.Fatal("a fresh key must not be locked")
	}

	// Up to the threshold, no lockout.
	for i := 0; i < 3; i++ {
		if lockout, justLocked := l.RecordFailure(ctx, key); lockout != 0 || justLocked {
			t.Fatalf("failure %d produced lockout=%s justLocked=%v, want none", i+1, lockout, justLocked)
		}
	}

	// The next one locks, and reports the transition exactly once.
	lockout, justLocked := l.RecordFailure(ctx, key)
	if lockout != 30*time.Second || !justLocked {
		t.Fatalf("threshold+1 gave lockout=%s justLocked=%v, want 30s/true", lockout, justLocked)
	}
	if locked, wait := l.Locked(ctx, key); !locked || wait <= 0 {
		t.Fatalf("key should be locked with a positive wait, got locked=%v wait=%s", locked, wait)
	}

	// A further failure extends the lock but is not a fresh transition, so a
	// lockout metric is not double-counted.
	if _, justLocked := l.RecordFailure(ctx, key); justLocked {
		t.Error("an already-locked key reported a second lock transition")
	}

	l.Reset(ctx, key)
	if locked, _ := l.Locked(ctx, key); locked {
		t.Error("Reset should clear an active lockout")
	}
}

// Distinct keys must not interfere: locking one wallet must not lock another.
func TestMemoryAuthLockout_KeysAreIndependent(t *testing.T) {
	ctx := context.Background()
	l := NewAuthLockout(nil, "test", backoffConfig())

	for i := 0; i < 6; i++ {
		l.RecordFailure(ctx, "GVICTIM")
	}
	if locked, _ := l.Locked(ctx, "GVICTIM"); !locked {
		t.Fatal("the hammered key should be locked")
	}
	if locked, _ := l.Locked(ctx, "GBYSTANDER"); locked {
		t.Error("an unrelated key was locked out")
	}
}

// Entries must be evicted once their window and lockout have both passed, so a
// flood of distinct keys cannot grow the map without bound.
func TestMemoryAuthLockout_EvictsExpiredEntries(t *testing.T) {
	l := newMemoryAuthLockout(AuthLockoutConfig{
		Threshold: 3,
		Window:    time.Millisecond,
		Base:      time.Millisecond,
		Max:       time.Millisecond,
	})
	ctx := context.Background()

	for i := 0; i < 100; i++ {
		l.RecordFailure(ctx, string(rune('a'+i%26))+string(rune('a'+i/26)))
	}

	// Let every window and lockout lapse, then record once more to trigger the
	// eviction sweep.
	time.Sleep(20 * time.Millisecond)
	l.RecordFailure(ctx, "trigger")

	l.mu.Lock()
	size := len(l.m)
	l.mu.Unlock()

	if size > 1 {
		t.Errorf("map holds %d entries after expiry, want the single fresh one", size)
	}
}
