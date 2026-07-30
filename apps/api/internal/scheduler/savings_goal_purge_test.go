package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
)

// fakeSavingsGoalPurgeRepo is an in-memory stand-in for the repository
// methods SavingsGoalPurgeJob needs, so its tick logic can be exercised
// without a database (#924).
type fakeSavingsGoalPurgeRepo struct {
	goals            []savingsgoal.SavingsGoal
	hardDeleted      []uuid.UUID
	listCutoffs      []time.Time
	hardDeleteErrFor map[uuid.UUID]error
}

func (f *fakeSavingsGoalPurgeRepo) ListDeletedOlderThan(_ context.Context, cutoff time.Time) ([]savingsgoal.SavingsGoal, error) {
	f.listCutoffs = append(f.listCutoffs, cutoff)
	var out []savingsgoal.SavingsGoal
	for _, g := range f.goals {
		if g.DeletedAt != nil && g.DeletedAt.Before(cutoff) {
			out = append(out, g)
		}
	}
	return out, nil
}

func (f *fakeSavingsGoalPurgeRepo) HardDelete(_ context.Context, id uuid.UUID) error {
	if err, ok := f.hardDeleteErrFor[id]; ok {
		return err
	}
	f.hardDeleted = append(f.hardDeleted, id)
	return nil
}

func TestSavingsGoalPurgeJob_HardDeletesExpiredSoftDeletes(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

	expiredID := uuid.New()
	expiredDeletedAt := now.Add(-31 * 24 * time.Hour) // past the 30-day window
	withinWindowID := uuid.New()
	withinWindowDeletedAt := now.Add(-5 * 24 * time.Hour) // still recoverable
	notDeletedID := uuid.New()

	repo := &fakeSavingsGoalPurgeRepo{
		goals: []savingsgoal.SavingsGoal{
			{ID: expiredID, DeletedAt: &expiredDeletedAt},
			{ID: withinWindowID, DeletedAt: &withinWindowDeletedAt},
			{ID: notDeletedID},
		},
	}

	job := NewSavingsGoalPurgeJob(repo, nil)
	job.SetClock(func() time.Time { return now })

	job.Tick(context.Background())

	if len(repo.hardDeleted) != 1 || repo.hardDeleted[0] != expiredID {
		t.Fatalf("expected only the expired goal (%s) to be hard-deleted, got %v", expiredID, repo.hardDeleted)
	}
}

func TestSavingsGoalPurgeJob_UsesRecoveryWindowAsCutoff(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	repo := &fakeSavingsGoalPurgeRepo{}

	job := NewSavingsGoalPurgeJob(repo, nil)
	job.SetClock(func() time.Time { return now })

	job.Tick(context.Background())

	if len(repo.listCutoffs) != 1 {
		t.Fatalf("expected exactly one ListDeletedOlderThan call, got %d", len(repo.listCutoffs))
	}
	wantCutoff := now.Add(-savingsgoal.SavingsGoalRecoveryWindow)
	if !repo.listCutoffs[0].Equal(wantCutoff) {
		t.Fatalf("cutoff = %v, want %v", repo.listCutoffs[0], wantCutoff)
	}
}

// fakePurgeLeaderChecker lets the "not leader" skip path be exercised
// deterministically, mirroring recurring_deposit_test.go's leadership tests.
type fakePurgeLeaderChecker struct {
	leader bool
}

func (f fakePurgeLeaderChecker) IsLeader() bool { return f.leader }

func TestSavingsGoalPurgeJob_SkipsTickWhenNotLeader(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	expiredID := uuid.New()
	expiredDeletedAt := now.Add(-31 * 24 * time.Hour)

	repo := &fakeSavingsGoalPurgeRepo{
		goals: []savingsgoal.SavingsGoal{{ID: expiredID, DeletedAt: &expiredDeletedAt}},
	}

	job := NewSavingsGoalPurgeJob(repo, nil)
	job.SetClock(func() time.Time { return now })
	job.SetLeaderChecker(fakePurgeLeaderChecker{leader: false})

	job.Tick(context.Background())

	if len(repo.hardDeleted) != 0 {
		t.Fatalf("expected no hard deletes while not leader, got %v", repo.hardDeleted)
	}
}
