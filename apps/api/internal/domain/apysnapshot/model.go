package apysnapshot

import (
	"context"
	"errors"
	"fmt"
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

// AnomalyJumpMultiplier is the factor a new APY reading may move (up or down)
// relative to the protocol's prior reading before DetectAnomalousJump flags
// it as an implausible jump (#941). E.g. 3.0 flags anything that more than
// triples or drops to under a third of the prior value.
const AnomalyJumpMultiplier = 3.0

// AnomalyJumpMinPriorAPY is the minimum prior APY (in percent) below which
// jump detection is skipped: small absolute moves off a near-zero base
// produce huge percentage swings that are usually normal, not anomalous.
const AnomalyJumpMinPriorAPY = "0.5"

type APYSnapshot struct {
	ID           uuid.UUID       `json:"id"`
	ProtocolSlug string          `json:"protocol_slug"`
	APY          decimal.Decimal `json:"apy"`
	TVL          decimal.Decimal `json:"tvl"`
	CapturedAt   time.Time       `json:"captured_at"`
	// Flagged marks a snapshot whose APY moved implausibly relative to the
	// protocol's prior reading (#941). Flagged snapshots are still persisted,
	// not rejected, so oracle aggregation/failover (#830) and vault APY
	// history aren't starved of data during a genuine market dislocation —
	// callers that care can filter or surface the flag instead.
	Flagged bool `json:"flagged"`
	// FlagReason explains why Flagged is true. Empty when Flagged is false.
	FlagReason string `json:"flag_reason,omitempty"`
}

// DetectAnomalousJump compares a new snapshot's APY against the protocol's
// previous reading and reports whether the move looks implausible. prev may
// be nil when no prior snapshot exists yet, in which case the result is
// always "not anomalous" — there is nothing to compare against.
func DetectAnomalousJump(prev *APYSnapshot, next APYSnapshot) (bool, string) {
	if prev == nil {
		return false, ""
	}
	if prev.APY.LessThan(decimal.RequireFromString(AnomalyJumpMinPriorAPY)) {
		return false, ""
	}
	if next.APY.LessThanOrEqual(decimal.Zero) {
		return false, ""
	}

	ratio := next.APY.Div(prev.APY)
	threshold := decimal.NewFromFloat(AnomalyJumpMultiplier)

	switch {
	case ratio.GreaterThan(threshold):
		return true, fmt.Sprintf(
			"apy jumped %sx (%s%% -> %s%%) since previous snapshot at %s",
			ratio.StringFixed(2), prev.APY.StringFixed(2), next.APY.StringFixed(2), prev.CapturedAt.Format(time.RFC3339),
		)
	case ratio.LessThan(decimal.NewFromInt(1).Div(threshold)):
		return true, fmt.Sprintf(
			"apy dropped to %sx (%s%% -> %s%%) since previous snapshot at %s",
			ratio.StringFixed(2), prev.APY.StringFixed(2), next.APY.StringFixed(2), prev.CapturedAt.Format(time.RFC3339),
		)
	default:
		return false, ""
	}
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
