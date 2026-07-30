package protocoltvl

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Snapshot is a single point-in-time TVL recording for a DeFiLlama protocol.
type Snapshot struct {
	ID            uuid.UUID
	ProtocolSlug  string
	TVLUSD        float64
	SnapshottedAt time.Time
}

// AlertCooldown is 12 hours between repeated alerts for the same protocol.
const AlertCooldown = 12 * time.Hour

// TVLDropThreshold is the minimum percentage drop in 24h that triggers an alert.
const TVLDropThreshold = 20.0

// Repository persists protocol TVL snapshots and alert cooldown records.
type Repository interface {
	// InsertSnapshot stores a new TVL reading for the protocol.
	InsertSnapshot(ctx context.Context, slug string, tvlUSD float64) error
	// SnapshotAt returns the most recent snapshot for slug at or before the given time.
	// Returns (nil, nil) when no snapshot exists.
	SnapshotAt(ctx context.Context, slug string, at time.Time) (*Snapshot, error)
	// LatestSnapshot returns the most recent snapshot for slug, or (nil, nil).
	LatestSnapshot(ctx context.Context, slug string) (*Snapshot, error)
	// CanAlert returns true when no alert has been sent for slug within AlertCooldown.
	CanAlert(ctx context.Context, slug string) (bool, error)
	// RecordAlert marks that an alert was just sent for slug.
	RecordAlert(ctx context.Context, slug string) error
}



