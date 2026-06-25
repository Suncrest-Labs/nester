package scheduler

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsschedule"
)

type fakeScheduleStore struct {
	due      []savingsschedule.SavingsSchedule
	updates  []scheduleUpdate
	deactivated []uuid.UUID
}

type scheduleUpdate struct {
	id         uuid.UUID
	lastRunAt  time.Time
	nextRunAt  time.Time
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

type recordingDepositRecorder struct {
	mu    sync.Mutex
	calls []depositCall
}

type depositCall struct {
	userID, vaultID, scheduleID uuid.UUID
	amount                    decimal.Decimal
}

func (r *recordingDepositRecorder) RecordScheduledDeposit(_ context.Context, userID, vaultID uuid.UUID, amount decimal.Decimal, scheduleID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, depositCall{userID, vaultID, scheduleID, amount})
	return nil
}

type fakeGoalChecker struct {
	completed bool
	name      string
}

func (f fakeGoalChecker) IsGoalCompleted(context.Context, uuid.UUID, uuid.UUID) (bool, string, error) {
	return f.completed, f.name, nil
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

func TestRecurringDepositJob_Tick_RecordsDepositAndUpdatesSchedule(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	scheduleID := uuid.New()
	userID := uuid.New()
	vaultID := uuid.New()
	goalID := uuid.New()

	store := &fakeScheduleStore{
		due: []savingsschedule.SavingsSchedule{{
			ID:        scheduleID,
			UserID:    userID,
			GoalID:    goalID,
			VaultID:   vaultID,
			Amount:    decimal.RequireFromString("50"),
			Currency:  "USDC",
			Frequency: savingsschedule.FrequencyWeekly,
			NextRunAt: now.Add(-time.Hour),
			IsActive:  true,
		}},
	}
	deposits := &recordingDepositRecorder{}
	notifier := &recordingDepositNotifier{}

	job := NewRecurringDepositJob(
		RecurringDepositConfig{Enabled: true, Interval: time.Hour},
		store,
		deposits,
		fakeGoalChecker{name: "Emergency Fund"},
		notifier,
		nil,
	)
	job.SetClock(func() time.Time { return now })

	job.Tick(context.Background())

	if len(deposits.calls) != 1 {
		t.Fatalf("expected 1 deposit, got %d", len(deposits.calls))
	}
	if !deposits.calls[0].amount.Equal(decimal.RequireFromString("50")) {
		t.Fatalf("deposit amount = %s", deposits.calls[0].amount)
	}
	if len(store.updates) != 1 {
		t.Fatalf("expected 1 schedule update, got %d", len(store.updates))
	}
	wantNext := now.AddDate(0, 0, 7)
	if !store.updates[0].nextRunAt.Equal(wantNext) {
		t.Fatalf("next_run_at = %v, want %v", store.updates[0].nextRunAt, wantNext)
	}
	if len(notifier.calls) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(notifier.calls))
	}
	if notifier.calls[0].goalName != "Emergency Fund" {
		t.Fatalf("goal name = %q", notifier.calls[0].goalName)
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
	deposits := &recordingDepositRecorder{}

	job := NewRecurringDepositJob(
		RecurringDepositConfig{Enabled: true},
		store,
		deposits,
		fakeGoalChecker{completed: true},
		nil,
		nil,
	)
	job.SetClock(func() time.Time { return now })
	job.Tick(context.Background())

	if len(store.deactivated) != 1 || store.deactivated[0] != scheduleID {
		t.Fatalf("expected schedule deactivated, got %v", store.deactivated)
	}
	if len(deposits.calls) != 0 {
		t.Fatal("expected no deposit for completed goal")
	}
}

func TestRecurringDepositJob_Run_NoOpsWhenDisabled(t *testing.T) {
	store := &fakeScheduleStore{
		due: []savingsschedule.SavingsSchedule{{ID: uuid.New()}},
	}
	deposits := &recordingDepositRecorder{}
	job := NewRecurringDepositJob(
		RecurringDepositConfig{Enabled: false},
		store,
		deposits,
		fakeGoalChecker{},
		nil,
		nil,
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	job.Run(ctx)

	if len(deposits.calls) != 0 {
		t.Fatal("disabled job should not process deposits")
	}
}
