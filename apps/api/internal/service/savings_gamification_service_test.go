package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsstreak"
)

type fakeGamificationRepo struct {
	state  savingsstreak.GamificationState
	events map[string]bool
	awards map[string]bool
}

func newFakeGamificationRepo(userID uuid.UUID) *fakeGamificationRepo {
	return &fakeGamificationRepo{
		state:  savingsstreak.GamificationState{UserID: userID, Timezone: "UTC", CurrentLevel: 1},
		events: map[string]bool{},
		awards: map[string]bool{},
	}
}

func (r *fakeGamificationRepo) GetState(context.Context, uuid.UUID) (savingsstreak.GamificationState, error) {
	return r.state, nil
}

func (r *fakeGamificationRepo) RecordEvent(_ context.Context, event savingsstreak.SavingEvent, _ savingsstreak.Transition) (bool, error) {
	if r.events[event.EventID] {
		return false, nil
	}
	r.events[event.EventID] = true
	return true, nil
}

func (r *fakeGamificationRepo) UpsertState(_ context.Context, state savingsstreak.GamificationState) error {
	r.state = state
	return nil
}

func (r *fakeGamificationRepo) AwardAchievement(_ context.Context, _ uuid.UUID, code string) (bool, error) {
	if r.awards[code] {
		return false, nil
	}
	r.awards[code] = true
	return true, nil
}

type recordingGamificationNotifier struct {
	count int
}

func (n *recordingGamificationNotifier) SendGamificationEvent(context.Context, uuid.UUID, string, string, map[string]any) {
	n.count++
}

func TestSavingsGamificationServiceReplayDoesNotDoubleCount(t *testing.T) {
	userID := uuid.New()
	repo := newFakeGamificationRepo(userID)
	notifier := &recordingGamificationNotifier{}
	svc := NewSavingsGamificationService(repo, notifier)
	svc.now = func() time.Time { return time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC) }

	event := savingsstreak.SavingEvent{
		EventID:      "ledger:123:0",
		UserID:       userID,
		Type:         "deposit_confirmed",
		Amount:       decimal.NewFromInt(120),
		NetAmount:    decimal.NewFromInt(120),
		OccurredAt:   time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC),
		UserTimezone: "UTC",
	}

	if _, err := svc.ProcessConfirmedDeposit(context.Background(), event); err != nil {
		t.Fatalf("first ProcessConfirmedDeposit() error = %v", err)
	}
	firstState := repo.state
	firstNotifications := notifier.count

	if _, err := svc.ProcessConfirmedDeposit(context.Background(), event); err != nil {
		t.Fatalf("replay ProcessConfirmedDeposit() error = %v", err)
	}
	if !repo.state.TotalSaved.Equal(firstState.TotalSaved) || repo.state.CurrentStreakDays != firstState.CurrentStreakDays {
		t.Fatalf("state drifted on replay: before=%+v after=%+v", firstState, repo.state)
	}
	if notifier.count != firstNotifications {
		t.Fatalf("notifications = %d, want %d after replay", notifier.count, firstNotifications)
	}
}
