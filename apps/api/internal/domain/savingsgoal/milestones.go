package savingsgoal

// ProgressMilestones are the savings goal progress percentages that trigger notifications.
var ProgressMilestones = []int{25, 50, 75, 100}

// ContainsMilestone reports whether milestone is already recorded for the goal.
func ContainsMilestone(notified []int, milestone int) bool {
	for _, m := range notified {
		if m == milestone {
			return true
		}
	}
	return false
}

// DetectNewMilestones returns milestones newly crossed at progressPct that are not yet notified.
func DetectNewMilestones(progressPct float64, notified []int) []int {
	var out []int
	for _, milestone := range ProgressMilestones {
		m := milestone
		if progressPct >= float64(m) && !ContainsMilestone(notified, m) {
			out = append(out, m)
		}
	}
	return out
}

// GoalDisplayName returns a human-readable name for notifications.
func GoalDisplayName(goal SavingsGoal) string {
	if goal.Description != "" {
		return goal.Description
	}
	return "your savings goal"
}
