package portfolio

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// USDCScale is the stroop scale for USDC valuations. Stellar amounts carry 7
// decimal places, so all USDC figures are rounded to 7 dp to match the ledger to
// the stroop.
const USDCScale int32 = 7

// Confidence is a valuation-quality signal propagated from the price oracle. A
// value derived from a stale or low-confidence price is flagged so the client
// can surface it rather than presenting an unreliable number as exact.
type Confidence string

const (
	ConfidenceHigh   Confidence = "high"
	ConfidenceMedium Confidence = "medium"
	ConfidenceLow    Confidence = "low"
	ConfidenceStale  Confidence = "stale"
)

// confidenceRank orders confidence from best (high) to worst (stale). Lower rank
// is better; the overall valuation confidence is the worst contributing rank.
var confidenceRank = map[Confidence]int{
	ConfidenceHigh:   0,
	ConfidenceMedium: 1,
	ConfidenceLow:    2,
	ConfidenceStale:  3,
}

// WorseConfidence returns the lower-quality of two confidences.
func WorseConfidence(a, b Confidence) Confidence {
	if confidenceRank[b] > confidenceRank[a] {
		return b
	}
	return a
}

// VaultValuation is the valued breakdown of a single vault position.
type VaultValuation struct {
	VaultID          uuid.UUID       `json:"vault_id"`
	Asset            string          `json:"asset"`
	PrincipalUSDC    decimal.Decimal `json:"principal_usdc"`
	YieldUSDC        decimal.Decimal `json:"yield_usdc"`
	FlexibleUSDC     decimal.Decimal `json:"flexible_usdc"`
	LockedUSDC       decimal.Decimal `json:"locked_usdc"`
	CurrentValueUSDC decimal.Decimal `json:"current_value_usdc"`
	Confidence       Confidence      `json:"confidence"`
}

// GoalValuation is the valued progress of a single savings goal.
type GoalValuation struct {
	GoalID        uuid.UUID       `json:"goal_id"`
	Name          string          `json:"name,omitempty"`
	AllocatedUSDC decimal.Decimal `json:"allocated_usdc"`
	TargetUSDC    decimal.Decimal `json:"target_usdc"`
	ProgressBps   int             `json:"progress_bps"`
}

// Valuation is the full real-time portfolio valuation for one user: a structured
// breakdown per vault and per goal, split principal vs yield, locked vs flexible,
// and settled vs pending, plus claimable rewards and an overall confidence.
//
// Pending deposits are reported separately and deliberately NOT included in
// TotalValueUSDC — an unsettled deposit is not yet part of settled net worth.
type Valuation struct {
	UserID      uuid.UUID `json:"user_id"`
	GeneratedAt time.Time `json:"generated_at"`

	TotalValueUSDC       decimal.Decimal `json:"total_value_usdc"`
	PrincipalUSDC        decimal.Decimal `json:"principal_usdc"`
	YieldUSDC            decimal.Decimal `json:"yield_usdc"`
	FlexibleUSDC         decimal.Decimal `json:"flexible_usdc"`
	LockedUSDC           decimal.Decimal `json:"locked_usdc"`
	SettledUSDC          decimal.Decimal `json:"settled_usdc"`
	PendingDepositsUSDC  decimal.Decimal `json:"pending_deposits_usdc"`
	ClaimableRewardsUSDC decimal.Decimal `json:"claimable_rewards_usdc"`

	Confidence Confidence       `json:"confidence"`
	Vaults     []VaultValuation `json:"vaults"`
	Goals      []GoalValuation  `json:"goals"`
}
