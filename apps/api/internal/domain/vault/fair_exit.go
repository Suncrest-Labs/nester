package vault

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// EmergencyQueueEntry is a fair-ordering emergency withdrawal queue position
// (issue #814), reconstructed off-chain from the `emrg_reqd`/`emrg_fill`/
// `emrg_canc` event stream so a user can see their position without an RPC
// round-trip on every page load.
type EmergencyQueueEntry struct {
	ID              uuid.UUID       `json:"id"`
	UserAddress     string          `json:"user_address"`
	Seq             int64           `json:"seq"`
	SharesRequested decimal.Decimal `json:"shares_requested"`
	SharesFilled    decimal.Decimal `json:"shares_filled"`
	Status          string          `json:"status"`
	EnqueuedAt      time.Time       `json:"enqueued_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

// PenaltyEvent is a single early-exit penalty charge (issue #805).
type PenaltyEvent struct {
	ID           uuid.UUID       `json:"id"`
	UserAddress  string          `json:"user_address"`
	Amount       decimal.Decimal `json:"amount"`
	SharesBurned decimal.Decimal `json:"shares_burned"`
	Reason       string          `json:"reason"`
	OccurredAt   time.Time       `json:"occurred_at"`
}

// PenaltyDistribution is a single `distribute_penalties` sweep of the
// escrow, split between remaining depositors and the treasury.
type PenaltyDistribution struct {
	ID              uuid.UUID       `json:"id"`
	DepositorAmount decimal.Decimal `json:"depositor_amount"`
	TreasuryAmount  decimal.Decimal `json:"treasury_amount"`
	RetainedDust    decimal.Decimal `json:"retained_dust"`
	OccurredAt      time.Time       `json:"occurred_at"`
}

// RebalanceLeg is one executed leg of a slippage-safe multi-hop rebalance
// (issue #810).
type RebalanceLeg struct {
	ID         uuid.UUID       `json:"id"`
	SourceID   string          `json:"source_id"`
	Delta      decimal.Decimal `json:"delta"`
	AmountOut  decimal.Decimal `json:"amount_out"`
	MinOut     decimal.Decimal `json:"min_out"`
	OccurredAt time.Time       `json:"occurred_at"`
}

// RebalanceCompletion is the once-per-call summary emitted at the end of
// `execute_rebalance`.
type RebalanceCompletion struct {
	ID                  uuid.UUID       `json:"id"`
	PlanHash            string          `json:"plan_hash"`
	TotalValueMoved     decimal.Decimal `json:"total_value_moved"`
	RealizedSlippageBps int32           `json:"realized_slippage_bps"`
	OccurredAt          time.Time       `json:"occurred_at"`
}
