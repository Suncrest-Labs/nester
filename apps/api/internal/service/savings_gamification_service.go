package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsstreak"
	"github.com/suncrestlabs/nester/apps/api/internal/notifications"
	"github.com/suncrestlabs/nester/apps/api/internal/stellar"
)

type SavingsGamificationRepository interface {
	GetState(ctx context.Context, userID uuid.UUID) (savingsstreak.GamificationState, error)
	RecordEvent(ctx context.Context, event savingsstreak.SavingEvent, transition savingsstreak.Transition) (bool, error)
	UpsertState(ctx context.Context, state savingsstreak.GamificationState) error
	AwardAchievement(ctx context.Context, userID uuid.UUID, code string) (bool, error)
}

type GamificationNotifier interface {
	SendGamificationEvent(ctx context.Context, userID uuid.UUID, title, body string, payload map[string]any)
}

type noopGamificationNotifier struct{}

func (noopGamificationNotifier) SendGamificationEvent(context.Context, uuid.UUID, string, string, map[string]any) {
}

type DispatcherGamificationNotifier struct {
	Dispatcher *notifications.Dispatcher
}

func (n DispatcherGamificationNotifier) SendGamificationEvent(ctx context.Context, userID uuid.UUID, title, body string, payload map[string]any) {
	if n.Dispatcher == nil {
		return
	}
	_ = n.Dispatcher.Send(ctx, userID, notifications.EventSavingsStreak, title, body, payload)
}

type SavingsGamificationService struct {
	repo     SavingsGamificationRepository
	engine   savingsstreak.Engine
	notifier GamificationNotifier
	now      func() time.Time
}

func NewSavingsGamificationService(repo SavingsGamificationRepository, notifier GamificationNotifier) *SavingsGamificationService {
	if notifier == nil {
		notifier = noopGamificationNotifier{}
	}
	engine := savingsstreak.NewGamificationEngine(savingsstreak.DefaultQualifyingRule())
	return &SavingsGamificationService{
		repo:     repo,
		engine:   engine,
		notifier: notifier,
		now:      time.Now,
	}
}

func (s *SavingsGamificationService) ProcessConfirmedDeposit(ctx context.Context, event savingsstreak.SavingEvent) (savingsstreak.Progress, error) {
	if event.EventID == "" {
		event.EventID = fmt.Sprintf("%s:%s:%s", event.UserID, event.Type, event.OccurredAt.UTC().Format(time.RFC3339Nano))
	}

	state, err := s.repo.GetState(ctx, event.UserID)
	if err != nil {
		return savingsstreak.Progress{}, err
	}
	next, transition, err := s.engine.Apply(state, event)
	if err != nil {
		return savingsstreak.Progress{}, err
	}

	inserted, err := s.repo.RecordEvent(ctx, event, transition)
	if err != nil {
		return savingsstreak.Progress{}, err
	}
	if !inserted {
		return s.Progress(ctx, event.UserID)
	}
	if !transition.Qualified {
		return s.engine.Progress(next, s.now())
	}

	for _, code := range transition.AwardedAchievements {
		awarded, err := s.repo.AwardAchievement(ctx, event.UserID, code)
		if err != nil {
			return savingsstreak.Progress{}, err
		}
		if awarded {
			s.notifier.SendGamificationEvent(ctx, event.UserID, "Achievement unlocked", fmt.Sprintf("You earned %s.", code), map[string]any{
				"achievement": code,
			})
		}
	}
	if transition.LevelAfter > transition.LevelBefore {
		s.notifier.SendGamificationEvent(ctx, event.UserID, "Savings level up", fmt.Sprintf("You reached level %d.", transition.LevelAfter), map[string]any{
			"level": transition.LevelAfter,
		})
	}

	if err := s.repo.UpsertState(ctx, next); err != nil {
		return savingsstreak.Progress{}, err
	}
	return s.engine.Progress(next, s.now())
}

func (s *SavingsGamificationService) Progress(ctx context.Context, userID uuid.UUID) (savingsstreak.Progress, error) {
	state, err := s.repo.GetState(ctx, userID)
	if err != nil {
		return savingsstreak.Progress{}, err
	}
	return s.engine.Progress(state, s.now())
}

// OnConfirmedDeposit implements stellar.DepositObserver, wiring general vault
// deposits observed by the chain indexer into the same streak engine that
// goal deposits already use. Previously only deposits that went through
// SavingsGoalService's goal-contribution path reached the engine at all — a
// deposit into a vault not tied to a specific goal never updated a streak.
//
// The event id is namespaced ("vault-deposit:") so it cannot collide with a
// goal-deposit event id for the same underlying deposit, and RecordEvent's
// dedup on event_id makes a redelivered indexer event a no-op.
func (s *SavingsGamificationService) OnConfirmedDeposit(ctx context.Context, deposit stellar.ConfirmedDeposit) {
	if deposit.VaultUserID == "" || deposit.EventID == "" {
		return
	}
	userID, err := uuid.Parse(deposit.VaultUserID)
	if err != nil {
		slog.Default().Error("gamification: invalid vault owner id from indexer", "raw", deposit.VaultUserID, "error", err)
		return
	}
	amount, err := decimal.NewFromString(deposit.AmountUnits)
	if err != nil {
		slog.Default().Error("gamification: invalid deposit amount from indexer", "raw", deposit.AmountUnits, "error", err)
		return
	}

	if _, err := s.ProcessConfirmedDeposit(ctx, savingsstreak.SavingEvent{
		EventID:    "vault-deposit:" + deposit.EventID,
		UserID:     userID,
		Type:       "deposit_confirmed",
		Amount:     amount,
		NetAmount:  amount,
		OccurredAt: deposit.OccurredAt,
	}); err != nil {
		slog.Default().Error("gamification: failed to process indexed deposit", "event_id", deposit.EventID, "error", err)
	}
}
