package valuation

import (
	"context"
	"fmt"
	"maps"

	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/portfolio"
)

// Oracle prices assets in USDC with a confidence signal. Multi-asset portfolios
// resolve every referenced asset through this port.
type Oracle interface {
	// Prices returns a table covering every requested asset. It errors if any
	// asset cannot be priced, so a partial valuation is never produced.
	Prices(ctx context.Context, assets []string) (PriceTable, error)
}

// StaticOracle prices assets from a fixed rate table. USDC always resolves to
// 1.0 at high confidence unless explicitly overridden. It is the correct oracle
// for USDC-denominated portfolios and the base for multi-asset setups — add
// asset rates (or swap in a live-feed Oracle) without touching the aggregator.
type StaticOracle struct {
	rates map[string]Price
}

// NewStaticOracle builds a StaticOracle. The provided rates are copied; USDC is
// seeded to 1.0/high when absent.
func NewStaticOracle(rates map[string]Price) *StaticOracle {
	m := make(map[string]Price, len(rates)+1)
	maps.Copy(m, rates)
	if _, ok := m["USDC"]; !ok {
		m["USDC"] = Price{Rate: decimal.NewFromInt(1), Confidence: portfolio.ConfidenceHigh}
	}
	return &StaticOracle{rates: m}
}

// Prices implements Oracle.
func (o *StaticOracle) Prices(_ context.Context, assets []string) (PriceTable, error) {
	out := make(PriceTable, len(assets))
	for _, a := range assets {
		p, ok := o.rates[a]
		if !ok {
			return nil, fmt.Errorf("valuation oracle: no rate configured for asset %q", a)
		}
		out[a] = p
	}
	return out, nil
}
