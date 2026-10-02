package analytics

import (
	"context"
	"time"
)

// ProtocolYieldPoint is one day's aggregate realized APY for a protocol,
// averaged across every vault allocation to that protocol.
type ProtocolYieldPoint struct {
	Date string  `json:"date"` // YYYY-MM-DD
	APY  float64 `json:"apy"`
}

// ProtocolYieldSeries is one protocol's time series in a comparison response.
type ProtocolYieldSeries struct {
	Protocol string               `json:"protocol"`
	Points   []ProtocolYieldPoint `json:"points"`
}

// ComparisonResponse is the payload for GET /api/v1/analytics/protocols/comparison.
type ComparisonResponse struct {
	Period    string                `json:"period"`
	Protocols []ProtocolYieldSeries `json:"protocols"`
}

// ComparisonRepository aggregates realized APY by protocol over time, so
// callers can answer "which protocol has been most consistent over
// 30/90 days" (#1324) rather than only comparing point-in-time snapshots.
type ComparisonRepository interface {
	// ProtocolYieldHistory returns, for each protocol with at least one
	// allocation, the daily average realized APY (across all vaults allocated
	// to that protocol) since the given time.
	ProtocolYieldHistory(ctx context.Context, since time.Time) ([]ProtocolYieldSeries, error)
}
