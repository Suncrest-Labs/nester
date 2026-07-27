package savingsgoal

// ProgressMilestones are the savings goal progress percentages that trigger notifications.
//
// Order matters: it must match the savings_goal contract's
// MILESTONE_THRESHOLDS_PCT exactly, since OnchainMilestoneBit/
// MilestonesFromBitmask assume ProgressMilestones[i] is attested by bit i of
// the contract's on-chain bitmask (#807). A divergence here would make the
// notifier and the on-chain attestation disagree about which milestone a
// given bit means.
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

// OnchainMilestoneBit returns the bit position the savings_goal contract sets
// in its milestone bitmask when pct is attested, and false if pct is not one
// of ProgressMilestones.
func OnchainMilestoneBit(pct int) (uint32, bool) {
	for i, m := range ProgressMilestones {
		if m == pct {
			return uint32(i), true
		}
	}
	return 0, false
}

// MilestonesFromBitmask translates the contract's on-chain milestone bitmask
// into the []int form used by ContainsMilestone/DetectNewMilestones, so the
// notifier can treat an on-chain attestation as equivalent to an
// already-notified milestone.
func MilestonesFromBitmask(bitmask uint32) []int {
	var out []int
	for i, m := range ProgressMilestones {
		if bitmask&(1<<uint(i)) != 0 {
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
