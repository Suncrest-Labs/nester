package nudge

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/usersignal"
)

type Candidate struct {
	Type   NudgeType
	Facts  Facts
	UserID uuid.UUID
}

type RankedCandidate struct {
	Candidate Candidate
	Score     float64
}

type DispatchRecord struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	NudgeType  string
	DedupKey   string
	Channel    string
	Title      string
	Body       string
	CopySource string
	Segment    string
	SentAt     time.Time
}

type Outcome struct {
	ID                 uuid.UUID
	DispatchID         uuid.UUID
	OutcomeType        string
	OccurredAt         time.Time
	HoursAfterDispatch float64
}

type EffectivenessStats struct {
	ConversionRate float64 // 0 to 1
}

type HistoryRepository interface {
	GetRecentDispatches(ctx context.Context, userID uuid.UUID, since time.Time) ([]DispatchRecord, error)
	RecordDispatch(ctx context.Context, record DispatchRecord) error
	GetEffectivenessStats(ctx context.Context, nudgeType string, segment string) (EffectivenessStats, error)
	GetLatestDispatchByType(ctx context.Context, userID uuid.UUID, nudgeType string) (*DispatchRecord, error)
}

type OutcomeRepository interface {
	RecordOutcome(ctx context.Context, userID uuid.UUID, outcomeType string, occurredAt time.Time) error
}

// PreferenceChecker is the opt-out seam: EvaluateAndDispatch must check
// this before doing any ranking/copy/send work, and respect a false result
// immediately and permanently.
type PreferenceChecker interface {
	NudgesEnabled(ctx context.Context, userID uuid.UUID) (bool, error)
}

func Rank(candidates []Candidate, segment usersignal.Segment, engagement usersignal.EngagementTier, effectiveness map[NudgeType]EffectivenessStats) []RankedCandidate {
	var ranked []RankedCandidate
	for _, c := range candidates {
		def := Catalog[c.Type]
		score := def.BaseImpact

		stats, ok := effectiveness[c.Type]
		if ok {
			score = score * (0.5 + stats.ConversionRate) // just a heuristic for the test
		}

		// If dormant, re-engagement gets a boost
		if segment == usersignal.SegmentDormant && c.Type == NudgeTypeReEngagement {
			score += 0.5
		}

		ranked = append(ranked, RankedCandidate{
			Candidate: c,
			Score:     score,
		})
	}

	// Sort by score descending
	for i := 0; i < len(ranked)-1; i++ {
		for j := i + 1; j < len(ranked); j++ {
			if ranked[j].Score > ranked[i].Score {
				ranked[i], ranked[j] = ranked[j], ranked[i]
			}
		}
	}
	return ranked
}
