package savingsstreak

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func qualifyingEvent(userID uuid.UUID, eventID string, at time.Time) SavingEvent {
	return SavingEvent{
		EventID:      eventID,
		UserID:       userID,
		Type:         "deposit_confirmed",
		Amount:       decimal.NewFromInt(10),
		NetAmount:    decimal.NewFromInt(10),
		UserTimezone: "Pacific/Kiritimati",
		OccurredAt:   at,
	}
}

func TestGamificationUsesUserLocalDay(t *testing.T) {
	userID := uuid.New()
	engine := NewGamificationEngine(DefaultQualifyingRule())
	state := GamificationState{UserID: userID, Timezone: "Pacific/Kiritimati", CurrentLevel: 1}

	first := qualifyingEvent(userID, "evt-1", time.Date(2026, 1, 1, 10, 30, 0, 0, time.UTC))
	state, transition, err := engine.Apply(state, first)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if transition.LocalDay != "2026-01-02" {
		t.Fatalf("local day = %s, want 2026-01-02", transition.LocalDay)
	}

	second := qualifyingEvent(userID, "evt-2", time.Date(2026, 1, 2, 10, 30, 0, 0, time.UTC))
	state, transition, err = engine.Apply(state, second)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if state.CurrentStreakDays != 2 || transition.LocalDay != "2026-01-03" {
		t.Fatalf("streak/local day = %d/%s, want 2/2026-01-03", state.CurrentStreakDays, transition.LocalDay)
	}
}

// TestGamificationLocalDayAcrossDateLine proves the streak is bucketed by the
// user's local calendar day, not the UTC day the event happens to arrive in.
// Both date-line directions are checked against a UTC-bucketing control that
// would get the wrong answer, so a regression to naive UTC bucketing fails
// loudly instead of only failing for one hemisphere.
func TestGamificationLocalDayAcrossDateLine(t *testing.T) {
	cases := []struct {
		name         string
		timezone     string
		occurredAt   time.Time
		wantLocalDay string
		utcDay       string
	}{
		{
			// UTC+14: local day rolls over to the next UTC day.
			name:         "far ahead of UTC (Kiritimati, UTC+14)",
			timezone:     "Pacific/Kiritimati",
			occurredAt:   time.Date(2026, 1, 1, 23, 0, 0, 0, time.UTC),
			wantLocalDay: "2026-01-02",
			utcDay:       "2026-01-01",
		},
		{
			// UTC-11: local day is still the previous UTC day.
			name:         "far behind UTC (Niue, UTC-11)",
			timezone:     "Pacific/Niue",
			occurredAt:   time.Date(2026, 1, 2, 5, 0, 0, 0, time.UTC),
			wantLocalDay: "2026-01-01",
			utcDay:       "2026-01-02",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.wantLocalDay == tc.utcDay {
				t.Fatalf("test case is not actually date-line-adjacent: local day equals UTC day")
			}
			userID := uuid.New()
			engine := NewGamificationEngine(DefaultQualifyingRule())
			state := GamificationState{UserID: userID, Timezone: tc.timezone, CurrentLevel: 1}

			event := SavingEvent{
				EventID:      "evt-1",
				UserID:       userID,
				Type:         "deposit_confirmed",
				Amount:       decimal.NewFromInt(10),
				NetAmount:    decimal.NewFromInt(10),
				UserTimezone: tc.timezone,
				OccurredAt:   tc.occurredAt,
			}
			_, transition, err := engine.Apply(state, event)
			if err != nil {
				t.Fatalf("Apply() error = %v", err)
			}
			if transition.LocalDay != tc.wantLocalDay {
				t.Fatalf("local day = %s, want %s (a naive UTC bucketing would give %s)", transition.LocalDay, tc.wantLocalDay, tc.utcDay)
			}
		})
	}
}

// TestGamificationTwoConsecutiveLocalDaysCanSpanTwoUTCDays proves the streak
// counts by local calendar day: two deposits on consecutive local days, whose
// timestamps happen to fall two UTC calendar days apart because of the
// timezone offset, must extend the streak by one day and must not consume
// the grace day.
func TestGamificationTwoConsecutiveLocalDaysCanSpanTwoUTCDays(t *testing.T) {
	userID := uuid.New()
	engine := NewGamificationEngine(DefaultQualifyingRule())
	state := GamificationState{UserID: userID, Timezone: "Pacific/Kiritimati", CurrentLevel: 1}

	// 2026-01-01 23:00 UTC -> local day 2026-01-02 (Kiritimati is UTC+14).
	first := qualifyingEvent(userID, "evt-1", time.Date(2026, 1, 1, 23, 0, 0, 0, time.UTC))
	state, transition, err := engine.Apply(state, first)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if transition.LocalDay != "2026-01-02" || state.CurrentStreakDays != 1 {
		t.Fatalf("first day = %s/%d, want 2026-01-02/1", transition.LocalDay, state.CurrentStreakDays)
	}

	// 2026-01-03 01:00 UTC -> local day 2026-01-03, the very next local day,
	// even though the UTC calendar date moved by two days.
	second := qualifyingEvent(userID, "evt-2", time.Date(2026, 1, 3, 1, 0, 0, 0, time.UTC))
	state, transition, err = engine.Apply(state, second)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if transition.LocalDay != "2026-01-03" {
		t.Fatalf("second local day = %s, want 2026-01-03", transition.LocalDay)
	}
	if state.CurrentStreakDays != 2 {
		t.Fatalf("current streak = %d, want 2 (consecutive local days must not need the grace day)", state.CurrentStreakDays)
	}
	if state.GraceUsedForDay != "" {
		t.Fatalf("grace used for day = %q, want unused: consecutive local days must not consume it", state.GraceUsedForDay)
	}
}

// TestGamificationLateEventDoesNotRegressStreak is a regression test: an
// event for a local day earlier than the streak's LastQualifiedDay (e.g. an
// out-of-order on-chain event, or a split-deposit request that finishes
// processing after a later deposit already qualified) must not move the
// streak's bookmark backwards. Doing so previously caused the *next* real
// deposit to measure its gap from the wrong (earlier) day, incorrectly
// resetting the streak or burning the grace day.
func TestGamificationLateEventDoesNotRegressStreak(t *testing.T) {
	userID := uuid.New()
	engine := NewGamificationEngine(DefaultQualifyingRule())
	state := GamificationState{
		UserID:            userID,
		Timezone:          "UTC",
		CurrentLevel:      1,
		CurrentStreakDays: 3,
		LongestStreakDays: 3,
		LastQualifiedDay:  "2026-01-03",
	}

	// A late event for 2026-01-01, two days before the streak's current
	// bookmark, arrives after the fact (e.g. backfilled or delayed).
	late := qualifyingEvent(userID, "evt-late", time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	late.UserTimezone = "UTC"
	next, transition, err := engine.Apply(state, late)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if !transition.Qualified {
		t.Fatalf("late event should still qualify (the saving is real), reason = %q", transition.Reason)
	}
	if next.LastQualifiedDay != "2026-01-03" {
		t.Fatalf("LastQualifiedDay regressed to %s, want unchanged 2026-01-03", next.LastQualifiedDay)
	}
	if next.CurrentStreakDays != 3 {
		t.Fatalf("current streak = %d, want unchanged 3", next.CurrentStreakDays)
	}
	if !next.TotalSaved.Equal(decimal.NewFromInt(10)) {
		t.Fatalf("total saved = %s, want 10: the late deposit itself must still count", next.TotalSaved.String())
	}

	// The next real deposit, for 2026-01-04, must extend from the correct
	// (unregressed) bookmark rather than from the late event's day.
	realNext := qualifyingEvent(userID, "evt-3", time.Date(2026, 1, 4, 12, 0, 0, 0, time.UTC))
	realNext.UserTimezone = "UTC"
	final, transition, err := engine.Apply(next, realNext)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if final.CurrentStreakDays != 4 {
		t.Fatalf("current streak after real next-day deposit = %d, want 4 (streak should extend normally, not have been corrupted by the late event)", final.CurrentStreakDays)
	}
	if transition.Reason != "qualified net saving" {
		t.Fatalf("reason = %q, want qualified net saving", transition.Reason)
	}
}

func TestGamificationGracePeriodPreservesStreakOnce(t *testing.T) {
	userID := uuid.New()
	engine := NewGamificationEngine(DefaultQualifyingRule())
	state := GamificationState{UserID: userID, Timezone: "UTC", CurrentLevel: 1, CurrentStreakDays: 5, LongestStreakDays: 5, LastQualifiedDay: "2026-01-01"}

	state, _, err := engine.Apply(state, qualifyingEvent(userID, "evt-1", time.Date(2026, 1, 3, 12, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if state.CurrentStreakDays != 6 || state.GraceUsedForDay != "2026-01-02" {
		t.Fatalf("grace state = %d/%s, want 6/2026-01-02", state.CurrentStreakDays, state.GraceUsedForDay)
	}

	state, _, err = engine.Apply(state, qualifyingEvent(userID, "evt-2", time.Date(2026, 1, 6, 12, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if state.CurrentStreakDays != 1 {
		t.Fatalf("current streak = %d, want reset to 1 after second missed window", state.CurrentStreakDays)
	}
}

func TestGamificationRejectsDustAndChurn(t *testing.T) {
	userID := uuid.New()
	engine := NewGamificationEngine(DefaultQualifyingRule())
	state := GamificationState{UserID: userID, Timezone: "UTC", CurrentLevel: 1}

	dust := qualifyingEvent(userID, "dust", time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	dust.Amount = decimal.NewFromInt(1)
	dust.NetAmount = decimal.NewFromInt(1)
	state, transition, err := engine.Apply(state, dust)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if transition.Qualified || state.CurrentStreakDays != 0 {
		t.Fatalf("dust qualified=%v streak=%d, want false/0", transition.Qualified, state.CurrentStreakDays)
	}

	churn := qualifyingEvent(userID, "churn", time.Date(2026, 1, 1, 13, 0, 0, 0, time.UTC))
	churn.WithdrawnWithinWindow = true
	state, transition, err = engine.Apply(state, churn)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if transition.Qualified || state.CurrentStreakDays != 0 {
		t.Fatalf("churn qualified=%v streak=%d, want false/0", transition.Qualified, state.CurrentStreakDays)
	}
}

func TestGamificationAwardsDurableAchievementsOnce(t *testing.T) {
	userID := uuid.New()
	engine := NewGamificationEngine(DefaultQualifyingRule())
	state := GamificationState{UserID: userID, Timezone: "UTC", CurrentLevel: 1}

	event := qualifyingEvent(userID, "save-1", time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	event.NetAmount = decimal.NewFromInt(120)
	event.Amount = decimal.NewFromInt(120)
	state, transition, err := engine.Apply(state, event)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if state.CurrentLevel < 2 {
		t.Fatalf("level = %d, want at least 2", state.CurrentLevel)
	}
	if len(transition.AwardedAchievements) != 2 {
		t.Fatalf("awarded = %v, want first_save and hundred_saved", transition.AwardedAchievements)
	}

	replayedState, replay, err := engine.Apply(state, event)
	if err != nil {
		t.Fatalf("Apply replay error = %v", err)
	}
	if len(replay.AwardedAchievements) != 0 || !replayedState.TotalSaved.Equal(state.TotalSaved) {
		t.Fatalf("replay awards/state = %v/%s, want no awards/no double count", replay.AwardedAchievements, replayedState.TotalSaved)
	}
}

func TestGamificationProgressShowsAtRiskAndNearestAchievement(t *testing.T) {
	engine := NewGamificationEngine(DefaultQualifyingRule())
	state := GamificationState{
		UserID:            uuid.New(),
		Timezone:          "UTC",
		CurrentLevel:      1,
		CurrentStreakDays: 3,
		LongestStreakDays: 3,
		LastQualifiedDay:  "2026-01-01",
	}

	progress, err := engine.Progress(state, time.Date(2026, 1, 3, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Progress() error = %v", err)
	}
	if progress.Status != StreakStatusAtRisk || progress.GraceRemaining != 1 {
		t.Fatalf("status/grace = %s/%d, want at-risk/1", progress.Status, progress.GraceRemaining)
	}
	if progress.NearestAchievement == nil || progress.NearestAchievement.Code != AchievementFirstSave {
		t.Fatalf("nearest achievement = %+v, want first_save", progress.NearestAchievement)
	}
}
