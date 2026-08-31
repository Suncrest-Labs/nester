package savingsstreak

import (
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

const (
	StreakStatusSafe   = "safe"
	StreakStatusAtRisk = "at-risk"
	StreakStatusBroken = "broken"

	AchievementFirstSave    = "first_save"
	AchievementWeekStreak   = "week_streak"
	AchievementMonthStreak  = "month_streak"
	AchievementHundredSaved = "hundred_saved"
	AchievementGoalFinisher = "goal_finisher"
)

var (
	ErrInvalidTimezone = errors.New("invalid user timezone")
	ErrDuplicateEvent  = errors.New("duplicate gamification event")
)

// QualifyingRule documents the anti-cheat rule used by the engine:
// a day qualifies only when confirmed net saving is at least MinNetDeposit and
// the deposit was not withdrawn inside the churn window.
type QualifyingRule struct {
	MinNetDeposit decimal.Decimal `json:"min_net_deposit"`
	ChurnWindow   time.Duration   `json:"churn_window"`
	GraceDays     int             `json:"grace_days"`
}

func DefaultQualifyingRule() QualifyingRule {
	return QualifyingRule{
		MinNetDeposit: decimal.NewFromInt(5),
		ChurnWindow:   24 * time.Hour,
		GraceDays:     1,
	}
}

type GamificationState struct {
	UserID              uuid.UUID       `json:"user_id"`
	Timezone            string          `json:"timezone"`
	CurrentStreakDays   int             `json:"current_streak_days"`
	LongestStreakDays   int             `json:"longest_streak_days"`
	LastQualifiedDay    string          `json:"last_qualified_day,omitempty"`
	GraceUsedForDay     string          `json:"grace_used_for_day,omitempty"`
	TotalSaved          decimal.Decimal `json:"total_saved"`
	GoalsCompleted      int             `json:"goals_completed"`
	CurrentLevel        int             `json:"current_level"`
	DurableScore        decimal.Decimal `json:"durable_score"`
	AwardedAchievements []string        `json:"awarded_achievements,omitempty"`
}

type SavingEvent struct {
	EventID               string
	UserID                uuid.UUID
	Amount                decimal.Decimal
	NetAmount             decimal.Decimal
	Type                  string
	OccurredAt            time.Time
	UserTimezone          string
	WithdrawnWithinWindow bool
	GoalsCompletedDelta   int
}

type Transition struct {
	Qualified           bool     `json:"qualified"`
	Reason              string   `json:"reason"`
	LocalDay            string   `json:"local_day"`
	PreviousStreakDays  int      `json:"previous_streak_days"`
	CurrentStreakDays   int      `json:"current_streak_days"`
	LongestStreakDays   int      `json:"longest_streak_days"`
	LevelBefore         int      `json:"level_before"`
	LevelAfter          int      `json:"level_after"`
	AwardedAchievements []string `json:"awarded_achievements,omitempty"`
}

type AchievementDefinition struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	Condition string `json:"condition"`
}

type LevelProgress struct {
	Level             int             `json:"level"`
	Score             decimal.Decimal `json:"score"`
	CurrentThreshold  decimal.Decimal `json:"current_threshold"`
	NextThreshold     decimal.Decimal `json:"next_threshold"`
	ProgressToNextPct decimal.Decimal `json:"progress_to_next_pct"`
}

type AchievementProgress struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	Condition string `json:"condition"`
}

type Progress struct {
	CurrentStreakDays   int                  `json:"current_streak_days"`
	LongestStreakDays   int                  `json:"longest_streak_days"`
	Status              string               `json:"status"`
	GraceRemaining      int                  `json:"grace_remaining"`
	Level               LevelProgress        `json:"level"`
	NearestAchievement  *AchievementProgress `json:"nearest_achievement,omitempty"`
	QualifyingRule      QualifyingRule       `json:"qualifying_rule"`
	AwardedAchievements []string             `json:"awarded_achievements"`
}

type Engine struct {
	Rule QualifyingRule
	Now  func() time.Time
}

func NewGamificationEngine(rule QualifyingRule) Engine {
	if rule.MinNetDeposit.IsZero() {
		rule = DefaultQualifyingRule()
	}
	return Engine{Rule: rule, Now: time.Now}
}

func (e Engine) Apply(state GamificationState, event SavingEvent) (GamificationState, Transition, error) {
	if state.UserID == uuid.Nil {
		state.UserID = event.UserID
	}
	if state.Timezone == "" {
		state.Timezone = event.UserTimezone
	}
	if state.Timezone == "" {
		state.Timezone = "UTC"
	}
	if state.CurrentLevel <= 0 {
		state.CurrentLevel = 1
	}

	day, err := localDay(event.OccurredAt, state.Timezone)
	if err != nil {
		return state, Transition{}, err
	}

	transition := Transition{
		LocalDay:           day,
		PreviousStreakDays: state.CurrentStreakDays,
		CurrentStreakDays:  state.CurrentStreakDays,
		LongestStreakDays:  state.LongestStreakDays,
		LevelBefore:        state.CurrentLevel,
		LevelAfter:         state.CurrentLevel,
	}

	if event.Type != "" && event.Type != "deposit_confirmed" {
		transition.Reason = "unsupported event type"
		return state, transition, nil
	}
	if event.Amount.LessThan(e.Rule.MinNetDeposit) || event.NetAmount.LessThan(e.Rule.MinNetDeposit) {
		transition.Reason = "net deposit below minimum qualifying amount"
		return state, transition, nil
	}
	if event.WithdrawnWithinWindow {
		transition.Reason = "deposit withdrawn inside churn window"
		return state, transition, nil
	}
	if state.LastQualifiedDay == day {
		transition.Qualified = true
		transition.Reason = "already qualified for local day"
		return state, transition, nil
	}

	state.CurrentStreakDays, state.GraceUsedForDay = nextStreakLength(state, day, e.Rule.GraceDays)
	if state.CurrentStreakDays > state.LongestStreakDays {
		state.LongestStreakDays = state.CurrentStreakDays
	}
	state.LastQualifiedDay = day
	state.TotalSaved = state.TotalSaved.Add(event.NetAmount)
	state.GoalsCompleted += event.GoalsCompletedDelta
	state.DurableScore = DurableScore(state)
	state.CurrentLevel = LevelForScore(state.DurableScore)

	newAchievements := NewAchievements(state)
	state.AwardedAchievements = mergeAchievements(state.AwardedAchievements, newAchievements)

	transition.Qualified = true
	transition.Reason = "qualified net saving"
	transition.CurrentStreakDays = state.CurrentStreakDays
	transition.LongestStreakDays = state.LongestStreakDays
	transition.LevelAfter = state.CurrentLevel
	transition.AwardedAchievements = newAchievements
	return state, transition, nil
}

func (e Engine) Progress(state GamificationState, now time.Time) (Progress, error) {
	if state.Timezone == "" {
		state.Timezone = "UTC"
	}
	today, err := localDay(now, state.Timezone)
	if err != nil {
		return Progress{}, err
	}
	status, graceRemaining := streakStatus(state, today, e.Rule.GraceDays)
	return Progress{
		CurrentStreakDays:   state.CurrentStreakDays,
		LongestStreakDays:   state.LongestStreakDays,
		Status:              status,
		GraceRemaining:      graceRemaining,
		Level:               LevelProgressForScore(state.DurableScore),
		NearestAchievement:  NearestAchievement(state),
		QualifyingRule:      e.Rule,
		AwardedAchievements: append([]string(nil), state.AwardedAchievements...),
	}, nil
}

func DurableScore(state GamificationState) decimal.Decimal {
	return state.TotalSaved.Add(decimal.NewFromInt(int64(state.LongestStreakDays * 10))).Add(decimal.NewFromInt(int64(state.GoalsCompleted * 100)))
}

func LevelForScore(score decimal.Decimal) int {
	level := 1
	for _, threshold := range []int64{100, 500, 1500, 5000} {
		if score.GreaterThanOrEqual(decimal.NewFromInt(threshold)) {
			level++
		}
	}
	return level
}

func LevelProgressForScore(score decimal.Decimal) LevelProgress {
	thresholds := []decimal.Decimal{
		decimal.Zero,
		decimal.NewFromInt(100),
		decimal.NewFromInt(500),
		decimal.NewFromInt(1500),
		decimal.NewFromInt(5000),
	}
	level := LevelForScore(score)
	current := thresholds[level-1]
	next := thresholds[len(thresholds)-1]
	if level < len(thresholds) {
		next = thresholds[level]
	}
	progress := decimal.NewFromInt(100)
	if next.GreaterThan(current) {
		progress = score.Sub(current).Div(next.Sub(current)).Mul(decimal.NewFromInt(100)).Round(2)
	}
	return LevelProgress{Level: level, Score: score, CurrentThreshold: current, NextThreshold: next, ProgressToNextPct: progress}
}

func AchievementDefinitions() []AchievementDefinition {
	return []AchievementDefinition{
		{Code: AchievementFirstSave, Name: "First Save", Condition: "Make one qualifying net deposit; cannot be farmed because replayed events dedupe by event_id."},
		{Code: AchievementWeekStreak, Name: "7-Day Streak", Condition: "Reach a 7-day local-time streak; one event can qualify only one local day."},
		{Code: AchievementMonthStreak, Name: "30-Day Streak", Condition: "Reach a 30-day local-time streak; dust/churn deposits do not qualify."},
		{Code: AchievementHundredSaved, Name: "100 Saved", Condition: "Reach 100 total net saved; deposit-withdraw churn is excluded."},
		{Code: AchievementGoalFinisher, Name: "Goal Finisher", Condition: "Complete one savings goal; awarded once per user, not per repeated action."},
	}
}

func NewAchievements(state GamificationState) []string {
	awarded := map[string]bool{}
	for _, code := range state.AwardedAchievements {
		awarded[code] = true
	}
	var out []string
	add := func(code string, ok bool) {
		if ok && !awarded[code] {
			out = append(out, code)
			awarded[code] = true
		}
	}
	add(AchievementFirstSave, state.TotalSaved.GreaterThan(decimal.Zero))
	add(AchievementWeekStreak, state.LongestStreakDays >= 7)
	add(AchievementMonthStreak, state.LongestStreakDays >= 30)
	add(AchievementHundredSaved, state.TotalSaved.GreaterThanOrEqual(decimal.NewFromInt(100)))
	add(AchievementGoalFinisher, state.GoalsCompleted >= 1)
	sort.Strings(out)
	return out
}

func NearestAchievement(state GamificationState) *AchievementProgress {
	awarded := map[string]bool{}
	for _, code := range state.AwardedAchievements {
		awarded[code] = true
	}
	for _, def := range AchievementDefinitions() {
		if !awarded[def.Code] {
			return &AchievementProgress{Code: def.Code, Name: def.Name, Condition: def.Condition}
		}
	}
	return nil
}

func localDay(t time.Time, timezone string) (string, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return "", ErrInvalidTimezone
	}
	return t.In(loc).Format("2006-01-02"), nil
}

func nextStreakLength(state GamificationState, day string, graceDays int) (int, string) {
	if state.LastQualifiedDay == "" {
		return 1, state.GraceUsedForDay
	}
	last, err1 := time.Parse("2006-01-02", state.LastQualifiedDay)
	current, err2 := time.Parse("2006-01-02", day)
	if err1 != nil || err2 != nil || !current.After(last) {
		return state.CurrentStreakDays, state.GraceUsedForDay
	}
	gap := int(current.Sub(last).Hours() / 24)
	if gap == 1 {
		return state.CurrentStreakDays + 1, state.GraceUsedForDay
	}
	if gap == 2 && graceDays > 0 && state.GraceUsedForDay != last.AddDate(0, 0, 1).Format("2006-01-02") {
		return state.CurrentStreakDays + 1, last.AddDate(0, 0, 1).Format("2006-01-02")
	}
	return 1, state.GraceUsedForDay
}

func streakStatus(state GamificationState, today string, graceDays int) (string, int) {
	if state.LastQualifiedDay == "" || state.CurrentStreakDays == 0 {
		return StreakStatusBroken, graceDays
	}
	last, err1 := time.Parse("2006-01-02", state.LastQualifiedDay)
	current, err2 := time.Parse("2006-01-02", today)
	if err1 != nil || err2 != nil {
		return StreakStatusBroken, 0
	}
	gap := int(current.Sub(last).Hours() / 24)
	switch {
	case gap <= 1:
		return StreakStatusSafe, graceDays
	case gap == 2 && graceDays > 0:
		return StreakStatusAtRisk, 1
	default:
		return StreakStatusBroken, 0
	}
}

func mergeAchievements(existing, add []string) []string {
	seen := map[string]bool{}
	for _, code := range existing {
		seen[code] = true
	}
	for _, code := range add {
		seen[code] = true
	}
	out := make([]string, 0, len(seen))
	for code := range seen {
		out = append(out, code)
	}
	sort.Strings(out)
	return out
}
