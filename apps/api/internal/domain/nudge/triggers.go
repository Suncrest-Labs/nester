package nudge

import (
	"time"

	"github.com/google/uuid"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
)

func EvaluateDeadlineReminderTrigger(goal savingsgoal.SavingsGoal, now time.Time) (bool, Facts) {
	if goal.Status == savingsgoal.GoalStatusCompleted || goal.Status == savingsgoal.GoalStatusArchived || goal.Status == savingsgoal.GoalStatusPaused {
		return false, Facts{}
	}

	daysUntil := int(goal.Deadline.Sub(now).Hours() / 24)
	if daysUntil == 7 || daysUntil == 3 || daysUntil == 1 {
		return true, goalFacts(goal)
	}
	return false, Facts{}
}

// EvaluateGoalProximityTrigger fires the "almost there" nudge once a goal is
// close to its target but hasn't crossed the final 100% milestone yet.
func EvaluateGoalProximityTrigger(goal savingsgoal.SavingsGoal, now time.Time) (bool, Facts) {
	if goal.Status != savingsgoal.GoalStatusActive && goal.Status != "" {
		return false, Facts{}
	}
	if goal.ProgressPct >= 90 && goal.ProgressPct < 100 {
		return true, goalFacts(goal)
	}
	return false, Facts{}
}

func goalFacts(goal savingsgoal.SavingsGoal) Facts {
	return Facts{
		GoalName:      savingsgoal.GoalDisplayName(goal),
		TargetAmount:  goal.TargetAmount,
		CurrentAmount: goal.CurrentAmount,
		Currency:      goal.Currency,
		Deadline:      goal.Deadline,
	}
}

// EvaluateStreakProtectionTrigger fires a proactive "don't lose your streak"
// nudge when a user with an active streak hasn't deposited in a while this
// week. This is distinct from EvaluateStreakMilestoneCandidate below, which
// celebrates a streak week that was just hit.
func EvaluateStreakProtectionTrigger(userID uuid.UUID, lastDeposit time.Time, now time.Time, streakWeeks int) (bool, Facts) {
	daysSinceDeposit := now.Sub(lastDeposit).Hours() / 24
	if daysSinceDeposit >= 6 && streakWeeks > 0 {
		return true, Facts{StreakWeeks: streakWeeks}
	}
	return false, Facts{}
}

// EvaluatePaydayDepositTrigger is a lightweight heuristic: if it's been
// roughly a month (28-35 days, covering both weekly-paid and monthly-paid
// cadences) since the user's last deposit and their goal isn't complete,
// they may have just been paid and be worth a nudge. This is intentionally
// simple until real payday/income-timing signals exist.
func EvaluatePaydayDepositTrigger(goal savingsgoal.SavingsGoal, lastDepositAt time.Time, now time.Time) (bool, Facts) {
	if goal.Status != savingsgoal.GoalStatusActive && goal.Status != "" {
		return false, Facts{}
	}
	if lastDepositAt.IsZero() {
		return false, Facts{}
	}
	daysSince := now.Sub(lastDepositAt).Hours() / 24
	if daysSince >= 28 && daysSince <= 35 {
		return true, goalFacts(goal)
	}
	return false, Facts{}
}
