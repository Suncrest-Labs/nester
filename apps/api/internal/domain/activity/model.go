// Package activity provides the unified transaction-history feed
// (GET /api/v1/activity): deposits, withdrawals, rebalances, settlements,
// and yield harvests for a single user, merged into one cursor-paginated,
// filterable, searchable list.
package activity

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// EventType is the kind of activity event, normalized across the several
// source tables the feed is built from.
type EventType string

const (
	EventDeposit     EventType = "deposit"
	EventWithdrawal  EventType = "withdrawal"
	EventRebalance   EventType = "rebalance"
	EventSettlement  EventType = "settlement"
	EventYieldEarned EventType = "yield_earned"
)

// Status is the normalized lifecycle state of an activity event.
type Status string

const (
	StatusPending   Status = "pending"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
)

// Item is one row of the unified activity feed.
type Item struct {
	ID        uuid.UUID
	Type      EventType
	Amount    decimal.Decimal
	Currency  string
	Status    Status
	CreatedAt time.Time
	VaultID   uuid.UUID
	VaultName string
	Ref       string // transaction hash / external reference, may be empty
}

// ListFilter drives the unified activity feed query for a single user.
type ListFilter struct {
	Types   []EventType
	Status  Status
	VaultID string
	From    *time.Time
	To      *time.Time
	Search  string
	Cursor  string
	Backward bool
	Limit   int
}

// Repository is the read side of the unified activity feed.
type Repository interface {
	List(ctx context.Context, userID uuid.UUID, filter ListFilter) (items []Item, nextCursor, prevCursor string, err error)
}
