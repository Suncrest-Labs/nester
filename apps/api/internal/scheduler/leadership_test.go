package scheduler

import (
	"log/slog"
	"sync"
	"testing"
	"time"
)

// discardLogger builds a Leadership with a non-nil logger for white-box
// tests that construct the struct directly (bypassing NewLeadership, which
// normally installs this default).
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError}))
}

// TestLeadership_IsLeader_DefaultsFalse checks the zero-value Leadership
// (before Run has ever ticked) reports "not leader" rather than panicking or
// defaulting to true — a job loop wired to a Leadership that hasn't yet won
// an election must not treat that as a green light.
func TestLeadership_IsLeader_DefaultsFalse(t *testing.T) {
	l := &Leadership{}
	if l.IsLeader() {
		t.Fatal("a freshly-constructed Leadership must not report leader before winning an election")
	}
	if !l.Since().IsZero() {
		t.Fatal("Since() must be zero when not leader")
	}
}

// TestLeadership_SplitBrainGuard demonstrates, without a real database, why
// a singleton job MUST call IsLeader() again immediately before its
// money-moving step acts rather than trusting a leadership check taken at
// the top of a (possibly slow) tick.
//
// Sequence: this instance is leader when a tick begins. While the tick is
// "doing work" (simulated by a sleep), the lock is lost — in production
// this happens because the pinned connection died or another instance's
// heartbeat discovered ours is gone; here we force it directly via demote()
// to simulate exactly that outcome without needing Postgres. A job that
// only checked leadership once at the top of the tick would proceed to act
// anyway (recordedActions == 1, double-processing risk with a second
// instance now also leader). A job that rechecks immediately before acting
// correctly skips (recordedActions == 0).
func TestLeadership_SplitBrainGuard(t *testing.T) {
	l := &Leadership{isLeader: true, since: time.Now(), logger: discardLogger()}

	var mu sync.Mutex
	var staleCheckActions, freshCheckActions int

	// checkedAtTickStart simulates a naive job that caches the leadership
	// verdict once and never rechecks it.
	checkedAtTickStart := l.IsLeader()

	// Leadership is lost mid-tick (simulating: connection died, or another
	// instance's heartbeat found the lock already gone).
	l.demote()

	// The naive job, still trusting its stale read, would act:
	if checkedAtTickStart {
		mu.Lock()
		staleCheckActions++
		mu.Unlock()
	}

	// The correct job rechecks immediately before acting:
	if l.IsLeader() {
		mu.Lock()
		freshCheckActions++
		mu.Unlock()
	}

	if staleCheckActions != 1 {
		t.Fatalf("expected the naive stale-check path to have acted once (demonstrating the risk), got %d", staleCheckActions)
	}
	if freshCheckActions != 0 {
		t.Fatalf("execution-time recheck must prevent acting after leadership was lost mid-tick, got %d actions", freshCheckActions)
	}
}

// TestLeadership_ConcurrentIsLeaderReadsSafe exercises IsLeader() under
// concurrent access alongside demote()/acquire()-style mutation, as a
// regression guard for the RWMutex usage (job loops call IsLeader() from
// their own goroutine while Run's ticker goroutine mutates state).
func TestLeadership_ConcurrentIsLeaderReadsSafe(t *testing.T) {
	l := &Leadership{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 1000; i++ {
			_ = l.IsLeader()
		}
	}()

	for i := 0; i < 1000; i++ {
		l.mu.Lock()
		l.isLeader = !l.isLeader
		l.mu.Unlock()
	}
	<-done
}
