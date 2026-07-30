package yieldharvest

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

var ErrNotFound = errors.New("yield harvest not found")

// YieldHarvest represents a single recorded harvest event.
type YieldHarvest struct {
	ID          uuid.UUID       `json:"id"`
	UserID      uuid.UUID       `json:"user_id"`
	VaultID     uuid.UUID       `json:"vault_id"`
	Amount      decimal.Decimal `json:"amount"`
	Currency    string          `json:"currency"`
	Protocol    string          `json:"protocol,omitempty"`
	HarvestedAt time.Time       `json:"harvested_at"`
	TxHash      string          `json:"tx_hash,omitempty"`
}

// CreateInput carries the fields needed to persist a new harvest record.
type CreateInput struct {
	UserID      uuid.UUID
	VaultID     uuid.UUID
	Amount      decimal.Decimal
	Currency    string
	HarvestedAt time.Time
	TxHash      string
}

// ListFilter carries pagination and optional filtering parameters.
type ListFilter struct {
	UserID uuid.UUID
	// CursorTime and CursorID together form the keyset cursor (harvested_at DESC, id DESC).
	CursorTime *time.Time
	CursorID   *uuid.UUID
	Limit      int
	// Since restricts results to harvests at or after this time (inclusive).
	// Used by the digest fact-assembly source (#859) to sum yield harvested
	// within a specific period without paging through full history.
	Since *time.Time
	// Until restricts results to harvests strictly before this time.
	Until *time.Time
}

// Repository is the persistence contract for yield harvest records.
type Repository interface {
	Create(ctx context.Context, input CreateInput) (YieldHarvest, error)
	ListForUser(ctx context.Context, filter ListFilter) ([]YieldHarvest, error)
}
