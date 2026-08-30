package nudge

import (
	"testing"

	"github.com/google/uuid"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/usersignal"
)

func TestRank_PicksHighestImpactForSegment(t *testing.T) {
	userID := uuid.New()
	candidates := []Candidate{
		{Type: NudgeTypeDeadlineReminder, UserID: userID}, // BaseImpact 0.8
		{Type: NudgeTypeReEngagement, UserID: userID},     // BaseImpact 0.5, boosted for dormant users
	}

	ranked := Rank(candidates, usersignal.SegmentDormant, usersignal.TierDormant, nil)

	if len(ranked) != 2 {
		t.Fatalf("expected 2 ranked candidates, got %d", len(ranked))
	}
	if ranked[0].Candidate.Type != NudgeTypeReEngagement {
		t.Fatalf("expected re_engagement to win for a dormant segment, got %s (score %.2f) ahead of %s (score %.2f)",
			ranked[0].Candidate.Type, ranked[0].Score, ranked[1].Candidate.Type, ranked[1].Score)
	}
	if ranked[0].Score <= ranked[1].Score {
		t.Fatalf("expected ranked[0].Score > ranked[1].Score, got %.2f vs %.2f", ranked[0].Score, ranked[1].Score)
	}
}

func TestRank_NonDormantSegmentKeepsBaseImpactOrder(t *testing.T) {
	userID := uuid.New()
	candidates := []Candidate{
		{Type: NudgeTypeReEngagement, UserID: userID},
		{Type: NudgeTypeDeadlineReminder, UserID: userID},
	}

	ranked := Rank(candidates, usersignal.SegmentActiveSaver, usersignal.TierHighlyEngaged, nil)

	if ranked[0].Candidate.Type != NudgeTypeDeadlineReminder {
		t.Fatalf("expected deadline_reminder (higher base impact) to win absent a dormant boost, got %s", ranked[0].Candidate.Type)
	}
}

func TestRank_EffectivenessBreaksTieBetweenEqualBaseImpact(t *testing.T) {
	userID := uuid.New()
	candidates := []Candidate{
		{Type: NudgeTypeMilestone, UserID: userID},
		{Type: NudgeTypeStreakMilestone, UserID: userID},
	}
	// Both types share BaseImpact 0.7 in the catalog; a higher historical
	// conversion rate for one should be what breaks the tie.
	stats := map[NudgeType]EffectivenessStats{
		NudgeTypeMilestone:       {ConversionRate: 0.1, HasData: true},
		NudgeTypeStreakMilestone: {ConversionRate: 0.9, HasData: true},
	}

	ranked := Rank(candidates, usersignal.SegmentActiveSaver, usersignal.TierEngaged, stats)

	if ranked[0].Candidate.Type != NudgeTypeStreakMilestone {
		t.Fatalf("expected the higher-converting nudge type to rank first, got %s", ranked[0].Candidate.Type)
	}
}

func TestRank_ColdStartEntryDoesNotOutrankRealData(t *testing.T) {
	userID := uuid.New()
	candidates := []Candidate{
		{Type: NudgeTypeMilestone, UserID: userID},
		{Type: NudgeTypeStreakMilestone, UserID: userID},
	}
	// Both types share BaseImpact 0.7. A cold-start entry (HasData: false,
	// the zero-value ConversionRate) must not be treated as a perfect 1.0
	// conversion rate: with the old behavior a cold-start entry would score
	// 0.7*(0.5+1.0)=1.05, beating even this strong 0.7 measured conversion
	// rate (0.7*(0.5+0.7)=0.84). With the fix, cold start gets no boost at
	// all and stays at the unscaled BaseImpact of 0.7 — so the type with
	// real, strong measured performance must win (nester#1196).
	stats := map[NudgeType]EffectivenessStats{
		NudgeTypeMilestone:       {ConversionRate: 0.7, HasData: true},
		NudgeTypeStreakMilestone: {HasData: false}, // cold start: zero-value ConversionRate
	}

	ranked := Rank(candidates, usersignal.SegmentActiveSaver, usersignal.TierEngaged, stats)

	if ranked[0].Candidate.Type != NudgeTypeMilestone {
		t.Fatalf("expected the nudge type with real measured data to rank first over a cold-start entry, got %s", ranked[0].Candidate.Type)
	}
}
