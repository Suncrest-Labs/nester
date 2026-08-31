package service

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/jobqueue"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/outbox"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
)

// outboxMemorySavingsGoalRepo is memorySavingsGoalRepo plus the
// MilestoneOutboxRecorder extension, standing in for the Postgres repository
// so the service's atomic path can be exercised without a database. It
// records the events it was handed and whether they arrived together with
// the milestone update.
type outboxMemorySavingsGoalRepo struct {
	*memorySavingsGoalRepo

	mu     sync.Mutex
	events []outbox.Event
	// failWrite makes the combined write fail, standing in for a rolled-back
	// transaction.
	failWrite error
}

func newOutboxMemorySavingsGoalRepo() *outboxMemorySavingsGoalRepo {
	return &outboxMemorySavingsGoalRepo{memorySavingsGoalRepo: newMemorySavingsGoalRepo()}
}

func (r *outboxMemorySavingsGoalRepo) UpdateMilestonesWithOutbox(
	ctx context.Context,
	goalID uuid.UUID,
	milestones []int,
	events []outbox.Event,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failWrite != nil {
		// Both writes fail together, as they would in one transaction.
		return r.failWrite
	}
	if err := r.memorySavingsGoalRepo.UpdateMilestones(ctx, goalID, milestones); err != nil {
		return err
	}
	r.events = append(r.events, events...)
	return nil
}

func (r *outboxMemorySavingsGoalRepo) recordedEvents() []outbox.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]outbox.Event(nil), r.events...)
}

func testGoalForMilestone(t *testing.T) savingsgoal.SavingsGoal {
	t.Helper()
	return savingsgoal.SavingsGoal{
		ID:            uuid.New(),
		UserID:        uuid.New(),
		Description:   "Vacation",
		Currency:      "USDC",
		TargetAmount:  decimal.NewFromInt(100),
		CurrentAmount: decimal.NewFromInt(50),
		Deadline:      time.Now().Add(90 * 24 * time.Hour),
	}
}

// TestMilestoneUsesOutboxWhenRepositorySupportsIt is the point of the whole
// change: the milestone flag and the intent to notify are handed to the
// repository together, so the process cannot die between them.
func TestMilestoneUsesOutboxWhenRepositorySupportsIt(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()

	repo := newOutboxMemorySavingsGoalRepo()
	repo.setBalance(userID, "USDC", decimal.NewFromInt(50))
	notifier := &recordingGoalMilestoneNotifier{}
	svc := NewSavingsGoalService(repo, nil, notifier)

	goal, err := svc.Create(ctx, userID, CreateSavingsGoalInput{
		TargetAmount: decimal.NewFromInt(100),
		Currency:     "USDC",
		Deadline:     testDeadline(),
		Description:  "Vacation",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	events := repo.recordedEvents()
	if len(events) != 2 {
		t.Fatalf("recorded %d outbox events, want 2 (25%% and 50%%)", len(events))
	}

	// Nothing was notified inline: delivery is now the relay's job, which is
	// exactly what makes it survive a restart.
	time.Sleep(50 * time.Millisecond)
	if n := notifier.count(); n != 0 {
		t.Fatalf("notifier called %d times inline, want 0 — the outbox owns delivery now", n)
	}

	for i, e := range events {
		if e.EventType != OutboxEventGoalMilestone {
			t.Fatalf("event[%d] type = %q, want %q", i, e.EventType, OutboxEventGoalMilestone)
		}
		if e.AggregateType != "savings_goal" || e.AggregateID != goal.ID.String() {
			t.Fatalf("event[%d] aggregate = %s/%s, want savings_goal/%s",
				i, e.AggregateType, e.AggregateID, goal.ID)
		}
		if e.Status != outbox.StatusPending {
			t.Fatalf("event[%d] status = %q, want pending", i, e.Status)
		}
	}
	// Both milestones share an aggregate, so they are delivered in order.
	if events[0].DedupeKey != MilestoneDedupeKey(goal.ID, 25) {
		t.Fatalf("first dedupe key = %q, want the 25%% key", events[0].DedupeKey)
	}
	if events[1].DedupeKey != MilestoneDedupeKey(goal.ID, 50) {
		t.Fatalf("second dedupe key = %q, want the 50%% key", events[1].DedupeKey)
	}
}

// TestMilestoneWriteFailureLeavesGoalUnnotified: if the combined write
// fails, the goal must NOT be left marked as notified — that is the
// half-committed state the outbox exists to prevent.
func TestMilestoneWriteFailureLeavesGoalUnnotified(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()

	repo := newOutboxMemorySavingsGoalRepo()
	repo.setBalance(userID, "USDC", decimal.NewFromInt(50))
	svc := NewSavingsGoalService(repo, nil, &recordingGoalMilestoneNotifier{})

	goal, err := svc.Create(ctx, userID, CreateSavingsGoalInput{
		TargetAmount: decimal.NewFromInt(100),
		Currency:     "USDC",
		Deadline:     testDeadline(),
		Description:  "Vacation",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Reset the goal to unnotified, then make the combined write fail.
	if err := repo.UpdateMilestones(ctx, goal.ID, nil); err != nil {
		t.Fatalf("reset milestones: %v", err)
	}
	repo.failWrite = context.DeadlineExceeded

	if _, err := svc.Get(ctx, userID, goal.ID); err == nil {
		t.Fatal("Get succeeded despite the milestone write failing")
	}

	stored, err := repo.GetByID(ctx, goal.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if len(stored.NotifiedMilestones) != 0 {
		t.Fatalf("notified milestones = %v after a failed write, want none — the goal would never be announced",
			stored.NotifiedMilestones)
	}
}

// TestMilestoneFallsBackWhenRepositoryCannotTransact keeps non-database
// repositories working, at the previous (lossy) guarantee.
func TestMilestoneFallsBackWhenRepositoryCannotTransact(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()

	repo := newMemorySavingsGoalRepo()
	repo.setBalance(userID, "USDC", decimal.NewFromInt(25))
	notifier := &recordingGoalMilestoneNotifier{}
	svc := NewSavingsGoalService(repo, nil, notifier)

	goal, err := svc.Create(ctx, userID, CreateSavingsGoalInput{
		TargetAmount: decimal.NewFromInt(100),
		Currency:     "USDC",
		Deadline:     testDeadline(),
		Description:  "Vacation",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	waitForMilestoneNotifications(t, notifier, 1)

	// Even the fallback path now carries the dedupe key, so a caller that
	// somehow drives it twice is recognisable downstream.
	if got, want := notifier.calls[0].DedupeKey, MilestoneDedupeKey(goal.ID, 25); got != want {
		t.Fatalf("dedupe key = %q, want %q", got, want)
	}
}

// --- job handler ---

func milestoneJob(t *testing.T, p GoalMilestonePayload) jobqueue.Job {
	t.Helper()
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return jobqueue.Job{ID: uuid.New(), Type: GoalMilestoneJobType, Payload: raw}
}

func TestGoalMilestoneJobHandler_InvokesNotifierChain(t *testing.T) {
	notifier := &recordingGoalMilestoneNotifier{}
	h := NewGoalMilestoneJobHandler(notifier, nil)
	goal := testGoalForMilestone(t)

	err := h.Handle(context.Background(), milestoneJob(t, GoalMilestonePayload{
		UserID:    goal.UserID,
		Milestone: 50,
		Goal:      goal,
		DedupeKey: MilestoneDedupeKey(goal.ID, 50),
	}))
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if notifier.count() != 1 {
		t.Fatalf("notifier called %d times, want 1", notifier.count())
	}
	call := notifier.calls[0]
	if call.GoalID != goal.ID || call.Milestone != 50 {
		t.Fatalf("notified %+v, want goal %s milestone 50", call, goal.ID)
	}
	if call.DedupeKey != MilestoneDedupeKey(goal.ID, 50) {
		t.Fatalf("dedupe key = %q, want %q", call.DedupeKey, MilestoneDedupeKey(goal.ID, 50))
	}
}

// TestGoalMilestoneJobHandler_SnapshotIsDeliveredNotReRead: the notification
// describes the goal as it was when the milestone happened, not as it is
// whenever the queue gets around to it.
func TestGoalMilestoneJobHandler_SnapshotIsDeliveredNotReRead(t *testing.T) {
	notifier := &recordingGoalMilestoneNotifier{}
	h := NewGoalMilestoneJobHandler(notifier, nil)

	goal := testGoalForMilestone(t)
	event, err := NewGoalMilestoneEvent(goal.UserID, goal, 50)
	if err != nil {
		t.Fatalf("NewGoalMilestoneEvent: %v", err)
	}

	var p GoalMilestonePayload
	if err := json.Unmarshal(event.Payload, &p); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if !p.Goal.CurrentAmount.Equal(goal.CurrentAmount) {
		t.Fatalf("snapshot current amount = %s, want %s", p.Goal.CurrentAmount, goal.CurrentAmount)
	}
	if !p.Goal.TargetAmount.Equal(goal.TargetAmount) {
		t.Fatalf("snapshot target amount = %s, want %s", p.Goal.TargetAmount, goal.TargetAmount)
	}

	if err := h.Handle(context.Background(), jobqueue.Job{Payload: event.Payload}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if notifier.count() != 1 {
		t.Fatalf("notifier called %d times, want 1", notifier.count())
	}
}

func TestGoalMilestoneJobHandler_MalformedPayloadIsPermanent(t *testing.T) {
	h := NewGoalMilestoneJobHandler(&recordingGoalMilestoneNotifier{}, nil)
	err := h.Handle(context.Background(), jobqueue.Job{Payload: json.RawMessage(`{oops`)})
	if !jobqueue.IsPermanent(err) {
		t.Fatalf("error = %v, want permanent", err)
	}
}

func TestGoalMilestoneJobHandler_MissingDedupeKeyIsPermanent(t *testing.T) {
	h := NewGoalMilestoneJobHandler(&recordingGoalMilestoneNotifier{}, nil)
	err := h.Handle(context.Background(), milestoneJob(t, GoalMilestonePayload{
		UserID: uuid.New(), Milestone: 50, Goal: testGoalForMilestone(t),
	}))
	if !jobqueue.IsPermanent(err) {
		t.Fatalf("error = %v, want permanent", err)
	}
}

func TestMilestoneDedupeKeyIsStableAndPerMilestone(t *testing.T) {
	goalID := uuid.New()
	if MilestoneDedupeKey(goalID, 25) != MilestoneDedupeKey(goalID, 25) {
		t.Fatal("MilestoneDedupeKey is not deterministic")
	}
	if MilestoneDedupeKey(goalID, 25) == MilestoneDedupeKey(goalID, 50) {
		t.Fatal("different milestones share a dedupe key; one would suppress the other")
	}
	if MilestoneDedupeKey(goalID, 25) == MilestoneDedupeKey(uuid.New(), 25) {
		t.Fatal("different goals share a dedupe key")
	}
}
