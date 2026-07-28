package apysnapshot

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

var (
	ErrProtocolNotFound    = errors.New("protocol not found")
	ErrDuplicateSnapshot   = errors.New("snapshot already exists for this protocol and timestamp")
)

type APYSnapshot struct {
	ID           uuid.UUID       `json:"id"`
	ProtocolSlug string          `json:"protocol_slug"`
	APY          decimal.Decimal `json:"apy"`
	TVL          decimal.Decimal `json:"tvl"`
	CapturedAt   time.Time       `json:"captured_at"`
}

type Repository interface {
	Upsert(ctx context.Context, snap APYSnapshot) error
	ListByProtocol(ctx context.Context, slug string, since time.Time) ([]APYSnapshot, error)
	PruneOlderThan(ctx context.Context, age time.Duration) error
}
