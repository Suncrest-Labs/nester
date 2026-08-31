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
