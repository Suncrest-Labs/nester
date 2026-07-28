// Package deterioration defines the domain model for predictive
// protocol-health deterioration monitoring (nester#857): leading
// indicators, a calibrated deterioration score, graduated alert levels, and
// the audit record for any automatic protective action taken.
package deterioration

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Indicators are the leading signals computed for one protocol at one point
// in time, each independently interpretable so an alert can cite exactly
// which ones drove it (#857's explainability requirement).
type Indicators struct {
	// TVLOutflowVelocityPct is the percentage TVL decline over the
	// observation window (positive = outflow, negative = inflow). Distinct
	// from ProtocolHealthChecker's existing 24h drop-threshold alert: this
	// is a continuous signal feeding the score, not a fixed trip-point.
	TVLOutflowVelocityPct float64
	// APYAbnormalityZScore is how many standard deviations the latest APY
	// reading is from its own recent trailing mean — a spike (or a
	// collapse) shows up as a large absolute z-score in either direction.
	APYAbnormalityZScore float64
	// ReportedVsDerivedGapPct is the gap between the latest reported APY
	// and a short-window trailing baseline of the same series, as a
	// percentage of the baseline. This is a proxy for "reported vs.
	// independently-derived APY divergence" — a true on-chain-derived
	// accrual figure requires the oracle-aggregation companion issue,
	// which has not landed; this indicator is scoped to what is
	// computable from data already ingested (see docs/protocol-health.md).
	ReportedVsDerivedGapPct float64
	// PriceInstability is the coefficient of variation (stddev/mean) of the
	// protocol's TVL series over the window, as a proxy for underlying/LP
	// token price instability — direct price-feed data is not available
	// per-protocol in this codebase today, so TVL volatility (which any
	// significant price dislocation shows up in) is used instead.
	PriceInstability float64
	// SampleCount is how many snapshots the window actually had. A low
	// count means the other indicators are less reliable — surfaced so
	// scoring can down-weight a thin sample rather than overreact to it.
	SampleCount int
}

// Level is a graduated alert level, each mapping to proportionate action
// (#857's "graduated alerts drive proportionate action" requirement).
type Level string

const (
	LevelNone     Level = "none"
	LevelMild     Level = "mild"
	LevelModerate Level = "moderate"
	LevelSevere   Level = "severe"
)

// Assessment is one scored evaluation of a protocol at a point in time —
// the deterioration probability, its graduated level, and the specific
// indicators that drove it (never just an opaque number).
type Assessment struct {
	ProtocolSlug string
	Indicators   Indicators
	// Probability is the modeled deterioration probability in [0,1] —
	// distinct from the static per-protocol risk score: this measures
	// acute, developing trouble, not inherent riskiness.
	Probability float64
	Level       Level
	// Explanation is a short, human-readable statement of the specific
	// indicators driving the assessment, e.g. "TVL down 42% in the
	// window, APY z-score 3.1, reported-vs-derived gap 18%" — an operator
	// must be able to act on this in seconds (#857).
	Explanation string
	AssessedAt  time.Time
}

// ActionKind is what a severe assessment triggered.
type ActionKind string

const (
	ActionCeilingCut         ActionKind = "ceiling_cut"
	ActionRecommendRebalance ActionKind = "recommend_rebalance"
	ActionAutomaticRebalance ActionKind = "automatic_rebalance"
)

// Action is the audit record for any action — automatic or
// recommendation-only — taken in response to an Assessment. Every automatic
// capital movement is recorded here: #857 is explicit that an AI moving
// user funds must never do so silently.
type Action struct {
	ID           uuid.UUID
	ProtocolSlug string
	Level        Level
	Probability  float64
	Kind         ActionKind
	// VaultID is set only for ActionAutomaticRebalance (the specific vault
	// moved). Ceiling cuts and rebalance recommendations are
	// protocol-wide, not per-vault.
	VaultID *uuid.UUID
	// RebalanceID references the admindomain.VaultRebalanceRecord created
	// by the automatic rebalance, when applicable — the existing
	// slippage-safe, auditable rebalance mechanism this feature bounds
	// itself to rather than moving funds through a separate path.
	RebalanceID *uuid.UUID
	Explanation string
	Error       string
	CreatedAt   time.Time
}

// Repository is the persistence port for deterioration actions (the audit
// trail) and assessment history (for calibration analysis).
type Repository interface {
	RecordAction(ctx context.Context, action *Action) error
	ListActionsByProtocol(ctx context.Context, slug string, limit int) ([]Action, error)
	// RecordAssessment persists a scored assessment for later calibration
	// review (comparing predicted probability against what actually
	// happened to the protocol afterward).
	RecordAssessment(ctx context.Context, a Assessment) error
}
