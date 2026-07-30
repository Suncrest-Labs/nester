package oracle

import (
	"context"
	"errors"
	"time"
)

var (
	ErrUnsupportedPair = errors.New("unsupported currency pair")
	ErrNoSource        = errors.New("no source available for pair")
)

// ExchangeRate holds the result of a single rate lookup.
type ExchangeRate struct {
	Base      string
	Quote     string
	Rate      float64
	Source    string
	FetchedAt time.Time
	ExpiresAt time.Time
	Stale     bool

	// Confidence is a 0..1 signal derived from source agreement (see
	// Aggregate/AggregatedValue) and, once served stale, freshness (see
	// DecayConfidenceForStaleness). Zero-valued for rates that never went
	// through the aggregator (the fixed USDC/USD peg, and single-source
	// fiat lookups that don't have a second source to reconcile against).
	Confidence float64
	// SourcesUsed lists every source that contributed to Rate via
	// consensus. Empty for rates not produced by Aggregate.
	SourcesUsed []string
}

// MeetsConfidenceThreshold reports whether r's confidence is at least min.
// Intended for downstream consumers (risk engine, allocation strategy,
// on-chain attestation signer) that must refuse to act on a low-confidence
// value rather than treating "a value came back" as sufficient (#830).
func (r ExchangeRate) MeetsConfidenceThreshold(threshold float64) bool {
	return r.Confidence >= threshold
}

// Provider fetches a numeric rate for a given base/quote pair.
type Provider interface {
	Name() string
	Fetch(ctx context.Context, base, quote string) (float64, error)
}

// supportedPairs lists every base→quote combination this oracle handles.
var supportedPairs = map[string]map[string]bool{
	"USDC": {"NGN": true, "GHS": true, "KES": true, "USD": true},
	"XLM":  {"USD": true},
}

// IsSupported reports whether base→quote is a known pair.
func IsSupported(base, quote string) bool {
	quotes, ok := supportedPairs[base]
	if !ok {
		return false
	}
	return quotes[quote]
}
