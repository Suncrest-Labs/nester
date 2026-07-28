package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/intelligence"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/nudge"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
	"github.com/suncrestlabs/nester/apps/api/internal/notifications"
)

// goalCoachingLookaheadDays is passed to ListActiveApproachingDeadline to
// approximate "all active goals": savings goal deadlines are always in the
// future relative to creation, and ~27 years comfortably covers any
// realistic goal horizon without requiring a new repository method.
const goalCoachingLookaheadDays = 10_000

// GoalCoachingRepository is the read the weekly coaching job needs.
type GoalCoachingRepository interface {
	ListActiveApproachingDeadline(ctx context.Context, maxDays int) ([]savingsgoal.SavingsGoal, error)
}

// GoalCoachingClient requests an AI progress assessment for a single goal.
type GoalCoachingClient interface {
	GetGoalCoaching(ctx context.Context, request intelligence.CoachingRequest) (*intelligence.CoachingResponse, error)
}

// GoalCoachingScheduler periodically generates and delivers an AI progress
// coaching notification for every active savings goal (#112 "AI coaching
// message generated weekly per goal").
type GoalCoachingScheduler struct {
	repo        GoalCoachingRepository
	coaching    GoalCoachingClient
	dispatcher  *notifications.Dispatcher
	logger      *slog.Logger
	prefChecker nudge.PreferenceChecker
}

func NewGoalCoachingScheduler(
	repo GoalCoachingRepository,
	coaching GoalCoachingClient,
	dispatcher *notifications.Dispatcher,
	logger *slog.Logger,
	prefChecker nudge.PreferenceChecker,
) *GoalCoachingScheduler {
	if logger == nil {
		logger = slog.Default()
	}
	return &GoalCoachingScheduler{
		repo:        repo,
		coaching:    coaching,
		dispatcher:  dispatcher,
		logger:      logger,
		prefChecker: prefChecker,
	}
}

// Run ticks every `interval` (weekly in production) until ctx is cancelled,
// sending one coaching notification per active goal on each tick. A tick
// also fires once immediately on start so coaching isn't delayed a full
// interval after deploy.
func (s *GoalCoachingScheduler) Run(ctx context.Context, interval time.Duration) {
	s.tick(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *GoalCoachingScheduler) tick(ctx context.Context) {
	if s.repo == nil || s.coaching == nil || s.dispatcher == nil {
		return
	}

	goals, err := s.repo.ListActiveApproachingDeadline(ctx, goalCoachingLookaheadDays)
	if err != nil {
		s.logger.Error("goal coaching: failed to list active goals", "error", err.Error())
		return
	}

	for _, goal := range goals {
		s.sendCoaching(ctx, goal)
	}
}

func (s *GoalCoachingScheduler) sendCoaching(ctx context.Context, goal savingsgoal.SavingsGoal) {
	// Opt-out (#935): some users don't want AI-driven nudges/insights.
	// Checked before any intelligence-service call is made — mirrors
	// nudge_engine_service.go's prefChecker gate for the same preference.
	// Also passed through explicitly on the request below so the
	// intelligence service enforces the same preference independent of
	// this check (defense in depth, not just "trust the caller skipped it").
	enabled := true
	if s.prefChecker != nil {
		var err error
		enabled, err = s.prefChecker.NudgesEnabled(ctx, goal.UserID)
		if err != nil {
			s.logger.Warn("goal coaching: failed to check nudges preference", "goal_id", goal.ID.String(), "error", err.Error())
			return
		}
		if !enabled {
			return
		}
	}

	targetAmount, _ := goal.TargetAmount.Float64()
	currentAmount, _ := goal.CurrentAmount.Float64()

	result, err := s.coaching.GetGoalCoaching(ctx, intelligence.CoachingRequest{
		AIInsightsEnabled: &enabled,
		Goal: intelligence.SavingsGoalContext{
			ID:            goal.ID.String(),
			TargetAmount:  targetAmount,
			Currency:      goal.Currency,
			Deadline:      goal.Deadline.Format(time.RFC3339),
			Description:   goal.Description,
			CurrentAmount: currentAmount,
			ProgressPct:   goal.ProgressPct,
		},
		Portfolio: intelligence.PortfolioContext{
			TotalBalanceUSD: currentAmount,
		},
	})
	if err != nil {
		s.logger.Warn("goal coaching: generation failed", "goal_id", goal.ID.String(), "error", err.Error())
		return
	}

	title := "Your weekly savings check-in"
	_ = s.dispatcher.Send(ctx, goal.UserID, notifications.EventGoalCoaching, title, result.ProgressAssessment, map[string]any{
		"goal_id":  goal.ID.String(),
		"currency": goal.Currency,
		"nudges":   result.Nudges,
	})
}
