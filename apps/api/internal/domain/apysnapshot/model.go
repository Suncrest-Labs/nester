package apysnapshot

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

var (
	ErrProtocolNotFound  = errors.New("protocol not found")
	ErrDuplicateSnapshot = errors.New("snapshot already exists for this protocol and timestamp")
	ErrInvalidSnapshot   = errors.New("invalid snapshot input")
)

type APYSnapshot struct {
	ID           uuid.UUID       `json:"id"`
	ProtocolSlug string          `json:"protocol_slug"`
	APY          decimal.Decimal `json:"apy"`
	TVL          decimal.Decimal `json:"tvl"`
	CapturedAt   time.Time       `json:"captured_at"`
}

// Validate checks that a snapshot has the minimum required data before it is
// persisted: a protocol slug, non-negative APY and TVL, and a non-zero
// capture timestamp.
func (s APYSnapshot) Validate() error {
	if strings.TrimSpace(s.ProtocolSlug) == "" {
		return ErrInvalidSnapshot
	}
	if s.APY.LessThan(decimal.Zero) {
		return ErrInvalidSnapshot
	}
	if s.TVL.LessThan(decimal.Zero) {
		return ErrInvalidSnapshot
	}
	if s.CapturedAt.IsZero() {
		return ErrInvalidSnapshot
	}
	return nil
}

// ByCapturedAt sorts a slice of snapshots in ascending order of CapturedAt,
// matching the chronological ordering ListByProtocol callers expect.
type ByCapturedAt []APYSnapshot

func (s ByCapturedAt) Len() int           { return len(s) }
func (s ByCapturedAt) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s ByCapturedAt) Less(i, j int) bool { return s[i].CapturedAt.Before(s[j].CapturedAt) }

// DuplicateTimestamps returns the CapturedAt values that occur more than once
// within snaps, which the unique (protocol_slug, captured_at) constraint
// should otherwise prevent from ever reaching storage.
func DuplicateTimestamps(snaps []APYSnapshot) []time.Time {
	seen := make(map[int64]int, len(snaps))
	for _, s := range snaps {
		seen[s.CapturedAt.UnixNano()]++
	}
	var dupes []time.Time
	for _, s := range snaps {
		if seen[s.CapturedAt.UnixNano()] > 1 {
			dupes = append(dupes, s.CapturedAt)
			seen[s.CapturedAt.UnixNano()] = 1 // report each duplicated timestamp once
		}
	}
	return dupes
}

type Repository interface {
	Upsert(ctx context.Context, snap APYSnapshot) error
	ListByProtocol(ctx context.Context, slug string, since time.Time) ([]APYSnapshot, error)
	PruneOlderThan(ctx context.Context, age time.Duration) error
}
