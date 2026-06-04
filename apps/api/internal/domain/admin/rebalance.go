package admin

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const (
	RebalanceStrategyAuto = "auto"

	RebalanceStatusPending   = "pending"
	RebalanceStatusSubmitted = "submitted"
	RebalanceStatusCompleted = "completed"
	RebalanceStatusFailed    = "failed"
	RebalanceStatusDryRun    = "dry_run"
)

// AllocationDeltaProjection is a single source-level change previewed by dry_run.
type AllocationDeltaProjection struct {
	SourceID string `json:"source_id"`
	Delta    string `json:"delta"`
	Current  string `json:"current,omitempty"`
	Target   string `json:"target,omitempty"`
}

// TargetAllocation is a user-confirmed protocol weight (audit / preview; on-chain still uses auto strategy).
type TargetAllocation struct {
	Protocol   string  `json:"protocol"`
	Percentage float64 `json:"percentage"`
}

// RebalanceRequest is the JSON body for POST /api/v1/admin/vaults/{id}/rebalance.
type RebalanceRequest struct {
	Strategy          string             `json:"strategy"`
	DryRun            bool               `json:"dry_run"`
	TargetAllocations []TargetAllocation `json:"target_allocations,omitempty"`
}

// RebalanceResponse is returned for both dry_run and live submissions.
type RebalanceResponse struct {
	Status                  string                      `json:"status"`
	TxHash                  string                      `json:"tx_hash,omitempty"`
	RebalanceID             uuid.UUID                   `json:"rebalance_id"`
	EstimatedCompletionMS   int64                       `json:"estimated_completion_ms,omitempty"`
	ProjectedDeltas         []AllocationDeltaProjection `json:"projected_deltas,omitempty"`
}

// VaultRebalanceRecord is the persisted audit row for a rebalance attempt.
type VaultRebalanceRecord struct {
	ID              uuid.UUID       `json:"id"`
	VaultID         uuid.UUID       `json:"vault_id"`
	Strategy        string          `json:"strategy"`
	DryRun          bool            `json:"dry_run"`
	Status          string          `json:"status"`
	TxHash          *string         `json:"tx_hash,omitempty"`
	ProjectedDeltas json.RawMessage  `json:"projected_deltas,omitempty"`
	ErrorMessage    *string         `json:"error_message,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}
