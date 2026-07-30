package oracle

import (
	"context"
	"fmt"
	"time"
)

const (
	cryptoTTL = 30 * time.Second
	fiatTTL   = 5 * time.Minute
)

// xlmAggregationOptions governs XLM/USD consensus (#830). MaxDeviationBPS
// of 300 (3%) is wide enough to tolerate normal cross-venue spread while
// still catching a genuinely bad or manipulated print; MinAgreeingSources
// is 1 rather than len(xlmFetchers) because this price's two sources
// (Horizon, DeFiLlama) were originally registered purely as primary/
// fallback for *availability* — requiring both to agree would regress
// uptime whenever either source is briefly down, which is exactly the
// failure mode having two sources was meant to protect against. Confidence
// still correctly reflects "only one of two responded" as lower than "both
// agreed" (see confidenceFor), so a caller that genuinely needs full
// two-source consensus can gate on Confidence instead.
var xlmAggregationOptions = AggregationOptions{
	MaxDeviationBPS:    300,
	MinAgreeingSources: 1,
	PerSourceTimeout:   3 * time.Second,
}

// RateService resolves exchange rates using a multi-source consensus
// aggregator (#830) for crypto rates and an in-memory TTL cache. On source
// failure it serves stale cached data rather than propagating an error.
type RateService struct {
	cache       *RateCache
	xlmFetchers []Provider // reconciled via Aggregate, not tried in priority order
	fiatFetcher Provider
	health      *HealthTracker
}

// NewRateService constructs a RateService with Horizon and DeFiLlama as
// independent XLM/USD sources (reconciled via consensus, not priority
// failover — see xlmAggregationOptions) and the open.er-api.com feed for
// fiat rates.
func NewRateService(horizonURL, usdcIssuer string) *RateService {
	return NewRateServiceWithFetchers(
		[]Provider{NewStellarProvider(horizonURL, usdcIssuer), NewDefiLlamaProvider()},
		NewFiatProvider(),
	)
}

// NewRateServiceWithFetchers allows injecting custom providers for testing.
func NewRateServiceWithFetchers(xlmFetchers []Provider, fiatFetcher Provider) *RateService {
	return &RateService{
		cache:       NewRateCache(),
		xlmFetchers: xlmFetchers,
		fiatFetcher: fiatFetcher,
		health:      NewHealthTracker(),
	}
}

// Health returns the source-health tracker backing XLM/USD aggregation, for
// a metrics endpoint to scrape (#830).
func (s *RateService) Health() *HealthTracker { return s.health }

// Cache returns the underlying cache so tests can pre-fill or inspect entries.
func (s *RateService) Cache() *RateCache { return s.cache }

// GetRate returns the exchange rate for base→quote. It serves a cached value
// when fresh, fetches a new one otherwise, and falls back to stale cache data
// (marked with Stale: true) when all live sources fail.
func (s *RateService) GetRate(ctx context.Context, base, quote string) (ExchangeRate, error) {
	if !IsSupported(base, quote) {
		return ExchangeRate{}, ErrUnsupportedPair
	}

	if s.cache.IsFresh(base, quote) {
		r, _ := s.cache.Get(base, quote)
		return r, nil
	}

	fresh, err := s.fetch(ctx, base, quote)
	if err != nil {
		if stale, ok := s.cache.Get(base, quote); ok {
			stale.Stale = true
			return stale, nil
		}
		return ExchangeRate{}, err
	}

	s.cache.Set(fresh)
	return fresh, nil
}

func (s *RateService) fetch(ctx context.Context, base, quote string) (ExchangeRate, error) {
	now := time.Now().UTC()

	switch {
	case base == "USDC" && quote == "USD":
		return ExchangeRate{
			Base: "USDC", Quote: "USD", Rate: 1.0,
			Source: "fixed", FetchedAt: now, ExpiresAt: now.Add(cryptoTTL),
		}, nil

	case base == "USDC":
		// USDC is pegged 1:1 to USD; obtain the USD→quote forex rate.
		rate, source, err := s.fetchFiat(ctx, quote)
		if err != nil {
			return ExchangeRate{}, err
		}
		return ExchangeRate{
			Base: "USDC", Quote: quote, Rate: rate,
			Source: source, FetchedAt: now, ExpiresAt: now.Add(fiatTTL),
		}, nil

	case base == "XLM" && quote == "USD":
		return s.fetchXLM(ctx)

	default:
		return ExchangeRate{}, ErrUnsupportedPair
	}
}

// Sanity bounds for XLM/USD price. These are intentionally wide — they exist
// to catch feed corruption (e.g. a 1000× spike), not to predict the market.
const (
	xlmMinUSD = 0.001 // XLM has never traded this low
	xlmMaxUSD = 100.0 // XLM at $100 would be an unprecedented 200× from its ATH
)

func (s *RateService) fetchXLM(ctx context.Context) (ExchangeRate, error) {
	agg, err := Aggregate(ctx, "XLM", "USD", s.xlmFetchers, s.health, xlmAggregationOptions)
	if err != nil {
		return ExchangeRate{}, fmt.Errorf("xlm: %w", err)
	}
	// The sanity bounds remain as a second, independent line of defense
	// beyond deviation-band outlier rejection: they catch the case where
	// every registered source is simultaneously corrupted or manipulated in
	// the same direction (deviation-band rejection alone can't catch that,
	// since nothing would look like an outlier relative to the others).
	if agg.Value < xlmMinUSD || agg.Value > xlmMaxUSD {
		return ExchangeRate{}, fmt.Errorf("xlm: consensus rate %v outside sanity bounds [%v, %v]", agg.Value, xlmMinUSD, xlmMaxUSD)
	}

	now := time.Now().UTC()
	return ExchangeRate{
		Base: "XLM", Quote: "USD", Rate: agg.Value,
		Source: agg.SourceName(), FetchedAt: now, ExpiresAt: now.Add(cryptoTTL),
		Confidence: agg.Confidence, SourcesUsed: agg.SourcesUsed,
	}, nil
}

func (s *RateService) fetchFiat(ctx context.Context, quote string) (float64, string, error) {
	rate, err := s.fiatFetcher.Fetch(ctx, "USD", quote)
	if err != nil {
		return 0, "", err
	}
	return rate, s.fiatFetcher.Name(), nil
}
