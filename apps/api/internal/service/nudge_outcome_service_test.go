package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeOutcomeRepository struct {
	recorded []recordedOutcome
}

type recordedOutcome struct {
	userID      uuid.UUID
	outcomeType string
	occurredAt  time.Time
}

func (f *fakeOutcomeRepository) RecordOutcome(_ context.Context, userID uuid.UUID, outcomeType string, occurredAt time.Time) error {
	f.recorded = append(f.recorded, recordedOutcome{userID: userID, outcomeType: outcomeType, occurredAt: occurredAt})
	return nil
}

// TestNudgeOutcomeService_RecordsEachOutcomeType is the effectiveness-tracking
// requirement: a deposit, a goal completion, and a return visit occurring
// after a nudge must each be recorded so selection/timing can later be
// weighted by what actually converts.
func TestNudgeOutcomeService_RecordsEachOutcomeType(t *testing.T) {
	repo := &fakeOutcomeRepository{}
	svc := NewNudgeOutcomeService(repo)
	userID := uuid.New()
	now := time.Now()

	if err := svc.RecordDeposit(context.Background(), userID, now); err != nil {
		t.Fatalf("RecordDeposit: unexpected error: %v", err)
	}
	if err := svc.RecordGoalCompletion(context.Background(), userID, now); err != nil {
		t.Fatalf("RecordGoalCompletion: unexpected error: %v", err)
	}
	if err := svc.RecordReturnVisit(context.Background(), userID, now); err != nil {
		t.Fatalf("RecordReturnVisit: unexpected error: %v", err)
	}

	if len(repo.recorded) != 3 {
		t.Fatalf("expected 3 recorded outcomes, got %d", len(repo.recorded))
	}

	wantTypes := map[string]bool{"deposit": false, "goal_completed": false, "return_visit": false}
	for _, r := range repo.recorded {
		if r.userID != userID {
			t.Fatalf("expected outcome recorded for %s, got %s", userID, r.userID)
		}
		if _, ok := wantTypes[r.outcomeType]; !ok {
			t.Fatalf("unexpected outcome type %q", r.outcomeType)
		}
		wantTypes[r.outcomeType] = true
	}
	for outcomeType, seen := range wantTypes {
		if !seen {
			t.Fatalf("expected outcome type %q to be recorded", outcomeType)
		}
	}
}
