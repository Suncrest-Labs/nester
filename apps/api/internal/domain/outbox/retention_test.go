package outbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// stubPruner records the cutoffs it was asked for, so a test can assert the
// retention windows rather than only the row counts.
type stubPruner struct {
	dispatchedBefore time.Time
	deadBefore       time.Time
	dispatched       int64
	dead             int64
	err              error
	calls            int
}

func (s *stubPruner) PruneTerminal(_ context.Context, dispatchedBefore, deadBefore time.Time) (int64, int64, error) {
	s.calls++
	s.dispatchedBefore = dispatchedBefore
	s.deadBefore = deadBefore
	return s.dispatched, s.dead, s.err
}

type stubLeader struct{ leader bool }

func (s stubLeader) IsLeader() bool { return s.leader }

func TestRetentionJobUsesConfiguredWindows(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	pruner := &stubPruner{dispatched: 7, dead: 2}
	metrics := NewStdMetrics()

	job := NewRetentionJob(pruner, RetentionConfig{
		DispatchedRetention: 48 * time.Hour,
		DeadRetention:       14 * 24 * time.Hour,
	}, nil, metrics)
	job.SetClock(func() time.Time { return now })

	if n := job.Tick(context.Background()); n != 9 {
		t.Fatalf("Tick pruned %d rows, want 9", n)
	}
	if want := now.Add(-48 * time.Hour); !pruner.dispatchedBefore.Equal(want) {
		t.Fatalf("dispatched cutoff = %s, want %s", pruner.dispatchedBefore, want)
	}
	if want := now.Add(-14 * 24 * time.Hour); !pruner.deadBefore.Equal(want) {
		t.Fatalf("dead cutoff = %s, want %s", pruner.deadBefore, want)
	}

	snap := metrics.Snapshot()
	if snap.Pruned[string(StatusDispatched)] != 7 || snap.Pruned[string(StatusDead)] != 2 {
		t.Fatalf("pruned metrics = %v, want dispatched=7 dead=2", snap.Pruned)
	}
}

// TestRetentionJobDefaultsKeepDeadLongerThanDispatched pins the asymmetry:
// a delivered event is history, a dead one is the evidence someone needs.
func TestRetentionJobDefaultsKeepDeadLongerThanDispatched(t *testing.T) {
	now := time.Now().UTC()
	pruner := &stubPruner{}
	job := NewRetentionJob(pruner, RetentionConfig{}, nil, nil)
	job.SetClock(func() time.Time { return now })
	job.Tick(context.Background())

	if !pruner.deadBefore.Before(pruner.dispatchedBefore) {
		t.Fatalf("dead cutoff %s is not older than dispatched cutoff %s",
			pruner.deadBefore, pruner.dispatchedBefore)
	}
	if want := now.Add(-DefaultDispatchedRetention); !pruner.dispatchedBefore.Equal(want) {
		t.Fatalf("dispatched cutoff = %s, want %s", pruner.dispatchedBefore, want)
	}
	if want := now.Add(-DefaultDeadRetention); !pruner.deadBefore.Equal(want) {
		t.Fatalf("dead cutoff = %s, want %s", pruner.deadBefore, want)
	}
}

func TestRetentionJobSkipsWhenNotLeader(t *testing.T) {
	pruner := &stubPruner{dispatched: 5}
	job := NewRetentionJob(pruner, RetentionConfig{}, nil, nil)
	job.SetLeaderChecker(stubLeader{leader: false})

	if n := job.Tick(context.Background()); n != 0 {
		t.Fatalf("Tick pruned %d rows as a follower, want 0", n)
	}
	if pruner.calls != 0 {
		t.Fatalf("pruner called %d times as a follower, want 0", pruner.calls)
	}
}

func TestRetentionJobSurvivesStoreErrors(t *testing.T) {
	pruner := &stubPruner{err: errors.New("connection reset")}
	job := NewRetentionJob(pruner, RetentionConfig{}, nil, nil)

	if n := job.Tick(context.Background()); n != 0 {
		t.Fatalf("Tick reported %d pruned rows despite an error, want 0", n)
	}
}

// TestRetentionNeverPrunesUndeliveredEvents is the property that actually
// matters: pruning an undelivered row is silently dropping the side effect
// the outbox exists to guarantee. Only terminal rows may ever be removed.
func TestRetentionNeverPrunesUndeliveredEvents(t *testing.T) {
	repo := newMemRepo()
	ctx := context.Background()

	ancient := time.Now().Add(-365 * 24 * time.Hour)
	pending := publish(t, repo, "goal-1", "dedupe-pending", nil)
	inFlight := publish(t, repo, "goal-2", "dedupe-inflight", nil)
	delivered := publish(t, repo, "goal-3", "dedupe-delivered", nil)
	poison := publish(t, repo, "goal-4", "dedupe-poison", nil)

	jobID := uuid.New()
	repo.mu.Lock()
	for _, e := range repo.events {
		e.CreatedAt = ancient
		e.UpdatedAt = ancient
	}
	repo.events[inFlight.ID].Status = StatusDispatching
	repo.events[inFlight.ID].JobID = &jobID
	repo.events[delivered.ID].Status = StatusDispatched
	repo.events[poison.ID].Status = StatusDead
	repo.mu.Unlock()

	job := NewRetentionJob(repo, RetentionConfig{
		DispatchedRetention: time.Hour,
		DeadRetention:       time.Hour,
	}, nil, nil)
	if n := job.Tick(ctx); n != 2 {
		t.Fatalf("pruned %d rows, want 2 (the terminal ones only)", n)
	}

	if _, ok := repo.byDedupeKey("dedupe-pending"); !ok {
		t.Fatal("retention deleted a pending event — that is a lost side effect")
	}
	if _, ok := repo.byDedupeKey("dedupe-inflight"); !ok {
		t.Fatal("retention deleted an in-flight event — that is a lost side effect")
	}
	if _, ok := repo.byDedupeKey("dedupe-delivered"); ok {
		t.Fatal("retention kept an expired dispatched event")
	}
	if _, ok := repo.byDedupeKey("dedupe-poison"); ok {
		t.Fatal("retention kept an expired dead event")
	}
	_ = pending
}

func TestRetentionJobRunStopsOnContextCancel(t *testing.T) {
	job := NewRetentionJob(&stubPruner{}, RetentionConfig{}, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		job.Run(ctx, time.Hour)
		close(done)
	}()
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}
