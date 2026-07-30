package nudge

type NudgeType string

const (
	NudgeTypeDeadlineReminder NudgeType = "deadline_reminder"
	NudgeTypeGoalProximity    NudgeType = "goal_proximity"
	NudgeTypeStreakProtection NudgeType = "streak_protection"
	NudgeTypeStreakMilestone  NudgeType = "streak_milestone"
	NudgeTypeMilestone        NudgeType = "milestone_celebration"
	NudgeTypePaydayDeposit    NudgeType = "payday_deposit"
	NudgeTypeYieldOpportunity NudgeType = "yield_opportunity"
	NudgeTypeReEngagement     NudgeType = "re_engagement"
)

type NudgeDefinition struct {
	Type        NudgeType
	BaseImpact  float64 // Used for ranking
	UsesLLMCopy bool
	// Immediate marks event-driven celebration nudges (a milestone/streak
	// was just hit) that should bypass the learned responsive-window gate:
	// there is no retry queue, so holding them until the next window would
	// silently lose the notification instead of delaying it.
	Immediate bool
}

var Catalog = map[NudgeType]NudgeDefinition{
	NudgeTypeDeadlineReminder: {Type: NudgeTypeDeadlineReminder, BaseImpact: 0.8, UsesLLMCopy: false},
	NudgeTypeGoalProximity:    {Type: NudgeTypeGoalProximity, BaseImpact: 0.75, UsesLLMCopy: false},
	NudgeTypeStreakProtection: {Type: NudgeTypeStreakProtection, BaseImpact: 0.9, UsesLLMCopy: false},
	NudgeTypeStreakMilestone:  {Type: NudgeTypeStreakMilestone, BaseImpact: 0.7, UsesLLMCopy: false, Immediate: true},
	NudgeTypeMilestone:        {Type: NudgeTypeMilestone, BaseImpact: 0.7, UsesLLMCopy: false, Immediate: true},
	NudgeTypePaydayDeposit:    {Type: NudgeTypePaydayDeposit, BaseImpact: 0.65, UsesLLMCopy: false},
	NudgeTypeYieldOpportunity: {Type: NudgeTypeYieldOpportunity, BaseImpact: 0.6, UsesLLMCopy: true},
	NudgeTypeReEngagement:     {Type: NudgeTypeReEngagement, BaseImpact: 0.5, UsesLLMCopy: true},
}
