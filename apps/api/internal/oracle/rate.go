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
