package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsstreak"
	"github.com/suncrestlabs/nester/apps/api/internal/notifications"
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
