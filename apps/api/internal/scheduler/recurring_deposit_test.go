package scheduler

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/jobqueue"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsschedule"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

type fakeScheduleStore struct {
	due         []savingsschedule.SavingsSchedule
	updates     []scheduleUpdate
	deactivated []uuid.UUID
}

type scheduleUpdate struct {
	id        uuid.UUID
	lastRunAt time.Time
	nextRunAt time.Time
}

func (f *fakeScheduleStore) ListDue(_ context.Context, _ time.Time) ([]savingsschedule.SavingsSchedule, error) {
	return f.due, nil
}

func (f *fakeScheduleStore) UpdateAfterRun(_ context.Context, id uuid.UUID, lastRunAt, nextRunAt time.Time) error {
	f.updates = append(f.updates, scheduleUpdate{id, lastRunAt, nextRunAt})
	return nil
}

func (f *fakeScheduleStore) Deactivate(_ context.Context, id uuid.UUID) error {
	f.deactivated = append(f.deactivated, id)
	return nil
}

// fakeEnqueuer records jobqueue.EnqueueInput calls in place of a real
// jobqueue.Client, so RecurringDepositJob.Tick's routing can be asserted
// without a database.
type fakeEnqueuer struct {
	mu    sync.Mutex
	calls []jobqueue.EnqueueInput
	err   error
}

func (f *fakeEnqueuer) Enqueue(_ context.Context, in jobqueue.EnqueueInput) (jobqueue.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return jobqueue.Job{}, f.err
	}
	f.calls = append(f.calls, in)
	return jobqueue.Job{ID: uuid.New(), Type: in.Type, Payload: in.Payload}, nil
}

type recordingDepositRecorder struct {
	mu    sync.Mutex
	calls []depositCall
	err   error
}

type depositCall struct {
	userID, vaultID, scheduleID uuid.UUID
	amount                      decimal.Decimal
	occurrenceAt                time.Time
}

func (r *recordingDepositRecorder) RecordScheduledDeposit(_ context.Context, userID, vaultID uuid.UUID, amount decimal.Decimal, scheduleID uuid.UUID, occurrenceAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	r.calls = append(r.calls, depositCall{userID, vaultID, scheduleID, amount, occurrenceAt})
	return nil
}

type fakeGoalChecker struct {
	completed bool
	name      string
	paused    bool
}

func (f fakeGoalChecker) IsGoalCompleted(context.Context, uuid.UUID, uuid.UUID) (bool, string, error) {
	return f.completed, f.name, nil
}

func (f fakeGoalChecker) IsGoalPausedOrArchived(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return f.paused, nil
}

type recordingDepositNotifier struct {
	mu    sync.Mutex
	calls []notifyCall
}

type notifyCall struct {
	userID   uuid.UUID
	amount   decimal.Decimal
	currency string
	goalName string
}

func (r *recordingDepositNotifier) NotifyScheduledDeposit(_ context.Context, userID uuid.UUID, amount decimal.Decimal, currency, goalName string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, notifyCall{userID, amount, currency, goalName})
	return nil
}

// fakeLeaderChecker lets tests force IsLeader() to a fixed value.
type fakeLeaderChecker struct{ leader bool }

func (f fakeLeaderChecker) IsLeader() bool { return f.leader }

func TestRecurringDepositJob_Tick_EnqueuesDueOccurrence(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	scheduleID := uuid.New()
	userID := uuid.New()
	vaultID := uuid.New()
	goalID := uuid.New()
	nextRunAt := now.Add(-time.Hour)

	store := &fakeScheduleStore{
		due: []savingsschedule.SavingsSchedule{{
			ID:        scheduleID,
			UserID:    userID,
			GoalID:    goalID,
			VaultID:   vaultID,
			Amount:    decimal.RequireFromString("50"),
			Currency:  "USDC",
			Frequency: savingsschedule.FrequencyWeekly,
			NextRunAt: nextRunAt,
			IsActive:  true,
		}},
	}
	queue := &fakeEnqueuer{}

	job := NewRecurringDepositJob(
		RecurringDepositConfig{Enabled: true, Interval: time.Hour},
		store,
		queue,
		fakeGoalChecker{name: "Emergency Fund"},
		nil,
	)
	job.SetClock(func() time.Time { return now })

	job.Tick(context.Background())

	if len(queue.calls) != 1 {
		t.Fatalf("expected 1 enqueue, got %d", len(queue.calls))
	}
	call := queue.calls[0]
	if call.Type != RecurringDepositJobType {
		t.Fatalf("job type = %q, want %q", call.Type, RecurringDepositJobType)
	}
	wantKey := recurringDepositIdempotencyKey(scheduleID, nextRunAt, time.Hour)
	if call.IdempotencyKey != wantKey {
		t.Fatalf("idempotency key = %q, want %q", call.IdempotencyKey, wantKey)
	}

	var payload RecurringDepositJobPayload
	if err := json.Unmarshal(call.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if !payload.Amount.Equal(decimal.RequireFromString("50")) {
		t.Fatalf("payload amount = %s", payload.Amount)
	}
	if payload.GoalName != "Emergency Fund" {
		t.Fatalf("payload goal name = %q", payload.GoalName)
	}
	if !payload.OccurrenceAt.Equal(nextRunAt) {
		t.Fatalf("payload occurrence_at = %v, want %v", payload.OccurrenceAt, nextRunAt)
	}

	// The schedule itself is NOT advanced by the sweep loop — that only
	// happens once the async handler confirms the deposit succeeded.
	if len(store.updates) != 0 {
		t.Fatalf("sweep loop must not advance the schedule directly, got %d updates", len(store.updates))
	}
}

func TestRecurringDepositJob_Tick_DeactivatesCompletedGoal(t *testing.T) {
	now := time.Now().UTC()
	scheduleID := uuid.New()
	store := &fakeScheduleStore{
		due: []savingsschedule.SavingsSchedule{{
			ID:       scheduleID,
			Amount:   decimal.RequireFromString("25"),
			IsActive: true,
		}},
	}
	queue := &fakeEnqueuer{}

	job := NewRecurringDepositJob(
		RecurringDepositConfig{Enabled: true},
		store,
		queue,
		fakeGoalChecker{completed: true},
		nil,
	)
	job.SetClock(func() time.Time { return now })
	job.Tick(context.Background())

	if len(store.deactivated) != 1 || store.deactivated[0] != scheduleID {
		t.Fatalf("expected schedule deactivated, got %v", store.deactivated)
	}
	if len(queue.calls) != 0 {
		t.Fatal("expected no enqueue for completed goal")
	}
}

func TestRecurringDepositJob_Tick_SkipsPausedGoal(t *testing.T) {
	now := time.Now().UTC()
	store := &fakeScheduleStore{
		due: []savingsschedule.SavingsSchedule{{ID: uuid.New(), IsActive: true}},
	}
	queue := &fakeEnqueuer{}

	job := NewRecurringDepositJob(RecurringDepositConfig{Enabled: true}, store, queue, fakeGoalChecker{paused: true}, nil)
	job.SetClock(func() time.Time { return now })
	job.Tick(context.Background())

	if len(queue.calls) != 0 {
		t.Fatal("expected no enqueue for paused goal")
	}
}

func TestRecurringDepositJob_Run_NoOpsWhenDisabled(t *testing.T) {
	store := &fakeScheduleStore{
		due: []savingsschedule.SavingsSchedule{{ID: uuid.New()}},
	}
	queue := &fakeEnqueuer{}
	job := NewRecurringDepositJob(
		RecurringDepositConfig{Enabled: false},
		store,
		queue,
		fakeGoalChecker{},
		nil,
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	job.Run(ctx)

	if len(queue.calls) != 0 {
		t.Fatal("disabled job should not enqueue anything")
	}
}

// TestRecurringDepositJob_Tick_SkipsWhenNotLeader is the singleton-job
// leader-gating contract (#846): a non-leader instance must not enqueue
// money-moving work at all.
func TestRecurringDepositJob_Tick_SkipsWhenNotLeader(t *testing.T) {
	store := &fakeScheduleStore{
		due: []savingsschedule.SavingsSchedule{{ID: uuid.New(), IsActive: true}},
	}
	queue := &fakeEnqueuer{}
	job := NewRecurringDepositJob(RecurringDepositConfig{Enabled: true}, store, queue, fakeGoalChecker{}, nil)
	job.SetLeaderChecker(fakeLeaderChecker{leader: false})

	job.Tick(context.Background())

	if len(queue.calls) != 0 {
		t.Fatal("non-leader instance must not enqueue recurring-deposit work")
	}
}

// TestRecurringDepositIdempotencyKey_DistinctPerOccurrence verifies the
// fixed per-occurrence key: the SAME occurrence (schedule + NextRunAt)
// always collapses to one key regardless of which bucket-aligned instant it
// is measured from, but two DIFFERENT occurrences of the same schedule
// (its NextRunAt having actually advanced) get different keys, so the
// schedule can legitimately fire again next week/month.
func TestRecurringDepositIdempotencyKey_DistinctPerOccurrence(t *testing.T) {
	scheduleID := uuid.New()
	occurrence1 := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	occurrence2 := occurrence1.AddDate(0, 0, 7) // next week's occurrence

	keyA := recurringDepositIdempotencyKey(scheduleID, occurrence1, time.Hour)
	keyARepeat := recurringDepositIdempotencyKey(scheduleID, occurrence1, time.Hour)
	keyB := recurringDepositIdempotencyKey(scheduleID, occurrence2, time.Hour)

	if keyA != keyARepeat {
		t.Fatalf("same occurrence must produce the same idempotency key: %q != %q", keyA, keyARepeat)
	}
	if keyA == keyB {
		t.Fatalf("distinct occurrences must produce distinct idempotency keys, both were %q", keyA)
	}

	otherSchedule := uuid.New()
	keyOther := recurringDepositIdempotencyKey(otherSchedule, occurrence1, time.Hour)
	if keyOther == keyA {
		t.Fatal("different schedules must not collide on the same key")
	}
}

// --- RecurringDepositJobHandler tests ---

func TestRecurringDepositJobHandler_Handle_RecordsDepositAndAdvancesSchedule(t *testing.T) {
	scheduleID := uuid.New()
	userID := uuid.New()
	vaultID := uuid.New()
	goalID := uuid.New()
	occurrenceAt := time.Date(2026, 6, 1, 11, 0, 0, 0, time.UTC)

	deposits := &recordingDepositRecorder{}
	schedules := &fakeScheduleStore{}
	notifier := &recordingDepositNotifier{}

	handler := NewRecurringDepositJobHandler(deposits, schedules, notifier, nil)

	payload, _ := json.Marshal(RecurringDepositJobPayload{
		ScheduleID:   scheduleID,
		UserID:       userID,
		VaultID:      vaultID,
		GoalID:       goalID,
		Amount:       decimal.RequireFromString("50"),
		Currency:     "USDC",
		Frequency:    string(savingsschedule.FrequencyWeekly),
		GoalName:     "Emergency Fund",
		OccurrenceAt: occurrenceAt,
	})

	err := handler.Handle(context.Background(), jobqueue.Job{Type: RecurringDepositJobType, Payload: payload})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	if len(deposits.calls) != 1 {
		t.Fatalf("expected 1 deposit call, got %d", len(deposits.calls))
	}
	if !deposits.calls[0].occurrenceAt.Equal(occurrenceAt) {
		t.Fatalf("deposit occurrenceAt = %v, want %v", deposits.calls[0].occurrenceAt, occurrenceAt)
	}

	if len(schedules.updates) != 1 {
		t.Fatalf("expected 1 schedule update, got %d", len(schedules.updates))
	}
	wantNext := occurrenceAt.AddDate(0, 0, 7)
	if !schedules.updates[0].nextRunAt.Equal(wantNext) {
		t.Fatalf("next_run_at = %v, want %v", schedules.updates[0].nextRunAt, wantNext)
	}

	if len(notifier.calls) != 1 || notifier.calls[0].goalName != "Emergency Fund" {
		t.Fatalf("expected 1 notification with goal name, got %+v", notifier.calls)
	}
}

func TestRecurringDepositJobHandler_Handle_MalformedPayloadIsPermanent(t *testing.T) {
	handler := NewRecurringDepositJobHandler(&recordingDepositRecorder{}, &fakeScheduleStore{}, nil, nil)

	err := handler.Handle(context.Background(), jobqueue.Job{Payload: []byte("not json")})
	if err == nil {
		t.Fatal("expected an error for malformed payload")
	}
	if !jobqueue.IsPermanent(err) {
		t.Fatal("malformed payload must be a permanent failure (dead-letter, not retry)")
	}
}

func TestRecurringDepositJobHandler_Handle_TransientDepositErrorRetries(t *testing.T) {
	deposits := &recordingDepositRecorder{err: context.DeadlineExceeded}
	handler := NewRecurringDepositJobHandler(deposits, &fakeScheduleStore{}, nil, nil)

	payload, _ := json.Marshal(RecurringDepositJobPayload{ScheduleID: uuid.New(), Amount: decimal.RequireFromString("10")})
	err := handler.Handle(context.Background(), jobqueue.Job{Payload: payload})
	if err == nil {
		t.Fatal("expected a transient error to be returned for retry")
	}
	if jobqueue.IsPermanent(err) {
		t.Fatal("a transient deposit failure must not be classified permanent")
	}
}

// TestRecurringDepositJobHandler_Handle_DuplicateTransactionIsIdempotent
// covers the #846 belt-and-suspenders case: if RecordScheduledDeposit
// reports the transaction hash already exists (a retried delivery of a job
// whose deposit already landed), the handler must treat it as success and
// still advance the schedule / notify, rather than fail forever.
func TestRecurringDepositJobHandler_Handle_DuplicateTransactionIsIdempotent(t *testing.T) {
	deposits := &recordingDepositRecorder{err: vault.ErrDuplicateTransaction}
	schedules := &fakeScheduleStore{}
	handler := NewRecurringDepositJobHandler(deposits, schedules, nil, nil)

	payload, _ := json.Marshal(RecurringDepositJobPayload{
		ScheduleID:   uuid.New(),
		Amount:       decimal.RequireFromString("10"),
		Frequency:    string(savingsschedule.FrequencyWeekly),
		OccurrenceAt: time.Now().UTC(),
	})
	err := handler.Handle(context.Background(), jobqueue.Job{Payload: payload})
	if err != nil {
		t.Fatalf("expected duplicate-transaction to be treated as success, got %v", err)
	}
	if len(schedules.updates) != 1 {
		t.Fatal("schedule must still advance after a duplicate-transaction no-op")
	}
}
