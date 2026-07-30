package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/nudge"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsstreak"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/transaction"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/user"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/usersignal"
	"github.com/suncrestlabs/nester/apps/api/internal/notifications"
)

// NudgeCopyGenerator produces nudge copy and reports which source actually
// generated it ("template" or "llm") so effectiveness tracking and the
// dispatch log reflect what was really sent, not just what was requested.
type NudgeCopyGenerator interface {
	Generate(nudgeType nudge.NudgeType, facts nudge.Facts, segment usersignal.Segment) (title, body, source string, err error)
}

type NudgeEngineService struct {
	savingsGoalRepo savingsgoal.Repository
	streakRepo      savingsstreak.Repository
	transactionRepo transaction.Repository
	userRepo        user.UserRepository
	segmentProv     usersignal.SegmentProvider
	engagementProv  usersignal.EngagementProvider
	timingProv      usersignal.TimingProvider
	historyRepo     nudge.HistoryRepository
	outcomeRepo     nudge.OutcomeRepository
	prefChecker     nudge.PreferenceChecker
	copyGen         NudgeCopyGenerator
	notifier        DispatcherNudgeNotifier
	logger          *slog.Logger
}

func NewNudgeEngineService(
	sgRepo savingsgoal.Repository,
	stRepo savingsstreak.Repository,
	trRepo transaction.Repository,
	urRepo user.UserRepository,
	segProv usersignal.SegmentProvider,
	engProv usersignal.EngagementProvider,
	timProv usersignal.TimingProvider,
	histRepo nudge.HistoryRepository,
	outRepo nudge.OutcomeRepository,
	prefChecker nudge.PreferenceChecker,
	copyGen NudgeCopyGenerator,
	notifier DispatcherNudgeNotifier,
) *NudgeEngineService {
	return &NudgeEngineService{
		savingsGoalRepo: sgRepo,
		streakRepo:      stRepo,
		transactionRepo: trRepo,
		userRepo:        urRepo,
		segmentProv:     segProv,
		engagementProv:  engProv,
		timingProv:      timProv,
		historyRepo:     histRepo,
		outcomeRepo:     outRepo,
		prefChecker:     prefChecker,
		copyGen:         copyGen,
		notifier:        notifier,
		logger:          slog.Default(),
	}
}

// EvaluateAndDispatch is the poll-driven entry point: it derives every
// state/time-based candidate itself (deadline, goal-proximity, payday,
// streak-protection) and, if one qualifies and clears anti-fatigue, sends it.
func (s *NudgeEngineService) EvaluateAndDispatch(ctx context.Context, userID uuid.UUID) error {
	return s.evaluate(ctx, userID, nil)
}

// EvaluateWithHint is the event-driven entry point used by the milestone
// and streak adapters below: the caller already knows a specific
// celebration-worthy event just happened (a milestone/streak was hit) and
// hands it in as a candidate to be ranked alongside the poll-based ones,
// rather than re-deriving it from state that may have already been marked
// "notified" by the caller.
func (s *NudgeEngineService) EvaluateWithHint(ctx context.Context, userID uuid.UUID, hint nudge.Candidate) error {
	return s.evaluate(ctx, userID, &hint)
}

func (s *NudgeEngineService) evaluate(ctx context.Context, userID uuid.UUID, hint *nudge.Candidate) error {
	if s.prefChecker != nil {
		enabled, err := s.prefChecker.NudgesEnabled(ctx, userID)
		if err != nil {
			return err
		}
		if !enabled {
			return nil
		}
	}

	segment, err := s.segmentProv.DeriveSegment(ctx, userID)
	if err != nil {
		return err
	}
	_, tier, err := s.engagementProv.ComputeEngagement(ctx, userID)
	if err != nil {
		return err
	}

	now := time.Now()
	candidates := s.gatherCandidates(ctx, userID, now, hint)
	if len(candidates) == 0 {
		return nil
	}

	stats := make(map[nudge.NudgeType]nudge.EffectivenessStats)
	for t := range nudge.Catalog {
		if st, err := s.historyRepo.GetEffectivenessStats(ctx, string(t), string(segment)); err == nil {
			stats[t] = st
		}
	}
	ranked := nudge.Rank(candidates, segment, tier, stats)
	if len(ranked) == 0 {
		return nil
	}
	best := ranked[0]
	def := nudge.Catalog[best.Candidate.Type]

	// Immediate (event-driven celebration) nudges bypass the responsive
	// window: there is no retry queue, so deferring them until the next
	// good hour would silently drop them instead of delaying them.
	if !def.Immediate {
		window, err := s.timingProv.InferResponsiveWindow(ctx, userID)
		if err != nil {
			return err
		}
		if !usersignal.IsWithinWindow(window, now) || usersignal.IsBadTime(now, window.Timezone) {
			return nil
		}
	}

	history, err := s.historyRepo.GetRecentDispatches(ctx, userID, now.Add(-14*24*time.Hour))
	if err != nil {
		return err
	}
	cap := nudge.EffectiveCap(nudge.Cap{MaxPerDay: 1, MaxPerWeek: 3, MinSpacingHours: 20}, tier)
	if allowed, _ := nudge.Allow(history, cap, now); !allowed {
		return nil
	}

	title, body, copySource, err := s.copyGen.Generate(best.Candidate.Type, best.Candidate.Facts, segment)
	if err != nil {
		return err
	}

	// Dedup on the user's local calendar date, not the server's UTC date
	// (#923): a user whose timezone offset places them ahead of or behind
	// UTC (e.g. UTC+14 or UTC-11) can have their local calendar day span a
	// UTC day boundary. Keying on now.UTC()'s date let two scheduler ticks
	// that fall on the same local day but different UTC days both pass the
	// dedup check, double-firing the reminder.
	dedupKey := fmt.Sprintf("%s-%s-%s", best.Candidate.Type, userID, localCalendarDate(now, s.userTimezone(ctx, userID)))
	if err := s.historyRepo.RecordDispatch(ctx, nudge.DispatchRecord{
		ID:         uuid.New(),
		UserID:     userID,
		NudgeType:  string(best.Candidate.Type),
		DedupKey:   dedupKey,
		Channel:    string(notifications.ChannelPush),
		Title:      title,
		Body:       body,
		CopySource: copySource,
		Segment:    string(segment),
		SentAt:     now,
	}); err != nil {
		return err // dedup collision or db error
	}

	return s.notifier.Dispatcher.Send(ctx, userID, notifications.EventSavingsNudge, title, body, map[string]any{
		"nudge_type": best.Candidate.Type,
	})
}

func (s *NudgeEngineService) gatherCandidates(ctx context.Context, userID uuid.UUID, now time.Time, hint *nudge.Candidate) []nudge.Candidate {
	var candidates []nudge.Candidate
	if hint != nil {
		candidates = append(candidates, *hint)
	}

	goals, _ := s.savingsGoalRepo.ListByUser(ctx, userID, "", "")
	lastDeposit := s.lastDepositAt(ctx, userID)

	for _, goal := range goals {
		if qual, facts := nudge.EvaluateDeadlineReminderTrigger(goal, now); qual {
			candidates = append(candidates, nudge.Candidate{Type: nudge.NudgeTypeDeadlineReminder, Facts: facts, UserID: userID})
		}
		if qual, facts := nudge.EvaluateGoalProximityTrigger(goal, now); qual {
			candidates = append(candidates, nudge.Candidate{Type: nudge.NudgeTypeGoalProximity, Facts: facts, UserID: userID})
		}
		if qual, facts := nudge.EvaluatePaydayDepositTrigger(goal, lastDeposit, now); qual {
			candidates = append(candidates, nudge.Candidate{Type: nudge.NudgeTypePaydayDeposit, Facts: facts, UserID: userID})
		}
	}

	if s.streakRepo != nil {
		if streak, err := s.streakRepo.Get(ctx, userID); err == nil && streak != nil {
			if qual, facts := nudge.EvaluateStreakProtectionTrigger(userID, lastDeposit, now, streak.CurrentStreak); qual {
				candidates = append(candidates, nudge.Candidate{Type: nudge.NudgeTypeStreakProtection, Facts: facts, UserID: userID})
			}
		}
	}

	return candidates
}

// userTimezone looks up the user's saved IANA timezone (#078_add_user_timezone)
// for local-date computations. Falls back to "UTC" if the user can't be
// loaded or hasn't set one, matching the default the users table itself uses.
func (s *NudgeEngineService) userTimezone(ctx context.Context, userID uuid.UUID) string {
	if s.userRepo == nil {
		return "UTC"
	}
	u, err := s.userRepo.GetByID(ctx, userID)
	if err != nil || u == nil || u.Timezone == "" {
		return "UTC"
	}
	return u.Timezone
}

// localCalendarDate returns now's calendar date (YYYY-MM-DD) in the given
// IANA timezone. Falls back to UTC if the timezone can't be loaded, so a bad
// or unknown zone degrades to the old (safe, if imprecise) behavior rather
// than erroring the whole dispatch.
func localCalendarDate(now time.Time, timezone string) string {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}
	return now.In(loc).Format("2006-01-02")
}

func (s *NudgeEngineService) lastDepositAt(ctx context.Context, userID uuid.UUID) time.Time {
	txs, _, err := s.transactionRepo.ListUserTransactions(ctx, transaction.ListFilter{
		UserID: userID,
		Type:   string(transaction.TypeDeposit),
		Status: string(transaction.StatusCompleted),
		Limit:  20,
	})
	if err != nil {
		return time.Time{}
	}
	var latest time.Time
	for _, tx := range txs {
		at := tx.CreatedAt
		if tx.ConfirmedAt != nil {
			at = *tx.ConfirmedAt
		}
		if at.After(latest) {
			latest = at
		}
	}
	return latest
}

// --- Adapters unifying the pre-existing milestone/streak notifiers into
// the nudge engine as catalog entries rather than parallel firing paths.
// Both interfaces they satisfy (GoalMilestoneNotifier / StreakMilestoneNotifier)
// are void-returning and called fire-and-forget from a bare goroutine in
// SavingsGoalService, so failures are logged here rather than propagated.

type NudgeEngineGoalMilestoneNotifier struct {
	NudgeEngine *NudgeEngineService
}

func (n NudgeEngineGoalMilestoneNotifier) SendGoalMilestone(ctx context.Context, userID uuid.UUID, goal savingsgoal.SavingsGoal, milestone int) {
	hint := nudge.Candidate{
		Type: nudge.NudgeTypeMilestone,
		Facts: nudge.Facts{
			GoalName:      savingsgoal.GoalDisplayName(goal),
			TargetAmount:  goal.TargetAmount,
			CurrentAmount: goal.CurrentAmount,
			Currency:      goal.Currency,
			Deadline:      goal.Deadline,
		},
		UserID: userID,
	}
	if err := n.NudgeEngine.EvaluateWithHint(ctx, userID, hint); err != nil {
		n.NudgeEngine.logger.Error("nudge engine: goal milestone dispatch failed", "user_id", userID, "milestone", milestone, "error", err)
	}
}

type NudgeEngineStreakMilestoneNotifier struct {
	NudgeEngine *NudgeEngineService
}

func (n NudgeEngineStreakMilestoneNotifier) SendStreakMilestone(ctx context.Context, userID uuid.UUID, weeks int) {
	hint := nudge.Candidate{
		Type:   nudge.NudgeTypeStreakMilestone,
		Facts:  nudge.Facts{StreakWeeks: weeks},
		UserID: userID,
	}
	if err := n.NudgeEngine.EvaluateWithHint(ctx, userID, hint); err != nil {
		n.NudgeEngine.logger.Error("nudge engine: streak milestone dispatch failed", "user_id", userID, "weeks", weeks, "error", err)
	}
}
