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

// RateService resolves exchange rates using a priority-ordered set of providers
// and an in-memory TTL cache. On source failure it serves stale cached data
// rather than propagating an error.
type RateService struct {
	cache       *RateCache
	xlmFetchers []Provider // tried in order: first success wins
	fiatFetcher Provider
}

// NewRateService constructs a RateService with Horizon (primary) and DeFiLlama
// (fallback) for crypto rates and the open.er-api.com feed for fiat rates.
func NewRateService(horizonURL string) *RateService {
	return NewRateServiceWithFetchers(
		[]Provider{NewStellarProvider(horizonURL), NewDefiLlamaProvider()},
		NewFiatProvider(),
	)
}

// NewRateServiceWithFetchers allows injecting custom providers for testing.
func NewRateServiceWithFetchers(xlmFetchers []Provider, fiatFetcher Provider) *RateService {
	return &RateService{
		cache:       NewRateCache(),
		xlmFetchers: xlmFetchers,
		fiatFetcher: fiatFetcher,
	}
}

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

func (s *RateService) fetchXLM(ctx context.Context) (ExchangeRate, error) {
	var lastErr error
	for _, p := range s.xlmFetchers {
		rate, err := p.Fetch(ctx, "XLM", "USD")
		if err == nil && rate > 0 {
			now := time.Now().UTC()
			return ExchangeRate{
				Base: "XLM", Quote: "USD", Rate: rate,
				Source: p.Name(), FetchedAt: now, ExpiresAt: now.Add(cryptoTTL),
			}, nil
		}
		if err != nil {
			lastErr = err
		}
	}
	if lastErr == nil {
		lastErr = ErrNoSource
	}
	return ExchangeRate{}, fmt.Errorf("xlm: all sources failed: %w", lastErr)
}

func (s *RateService) fetchFiat(ctx context.Context, quote string) (float64, string, error) {
	rate, err := s.fiatFetcher.Fetch(ctx, "USD", quote)
	if err != nil {
		return 0, "", err
	}
	return rate, s.fiatFetcher.Name(), nil
}
