package oracle_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/oracle"
)

// ── stub Provider ────────────────────────────────────────────────────────────

type stubProvider struct {
	name      string
	rate      float64
	err       error
	callCount int
}

func (p *stubProvider) Name() string { return p.name }

func (p *stubProvider) Fetch(_ context.Context, _, _ string) (float64, error) {
	p.callCount++
	return p.rate, p.err
}

// ── helpers ──────────────────────────────────────────────────────────────────

func expiredRate(base, quote string, rate float64) oracle.ExchangeRate {
	return oracle.ExchangeRate{
		Base:      base,
		Quote:     quote,
		Rate:      rate,
		Source:    "stale-source",
		FetchedAt: time.Now().UTC().Add(-10 * time.Minute),
		ExpiresAt: time.Now().UTC().Add(-1 * time.Second),
	}
}

// ── tests ────────────────────────────────────────────────────────────────────

func TestRateService_USDCToUSDIsFixed(t *testing.T) {
	svc := oracle.NewRateServiceWithFetchers(nil, &stubProvider{name: "fiat", rate: 1.0})

	r, err := svc.GetRate(context.Background(), "USDC", "USD")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Rate != 1.0 {
		t.Errorf("USDC→USD: want 1.0, got %f", r.Rate)
	}
	if r.Source != "fixed" {
		t.Errorf("want source 'fixed', got %q", r.Source)
	}
}

func TestRateService_UnsupportedPairReturnsError(t *testing.T) {
	svc := oracle.NewRateServiceWithFetchers(nil, &stubProvider{})

	_, err := svc.GetRate(context.Background(), "BTC", "USD")
	if !errors.Is(err, oracle.ErrUnsupportedPair) {
		t.Errorf("expected ErrUnsupportedPair, got %v", err)
	}
}

func TestRateService_XLMUsesFirstSucceedingProvider(t *testing.T) {
	failing := &stubProvider{name: "p1", err: errors.New("timeout")}
	succeeding := &stubProvider{name: "p2", rate: 0.15}

	svc := oracle.NewRateServiceWithFetchers(
		[]oracle.Provider{failing, succeeding},
		&stubProvider{name: "fiat"},
	)

	r, err := svc.GetRate(context.Background(), "XLM", "USD")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Rate != 0.15 {
		t.Errorf("want rate 0.15 from second provider, got %f", r.Rate)
	}
	if r.Source != "p2" {
		t.Errorf("want source 'p2', got %q", r.Source)
	}
	if failing.callCount != 1 {
		t.Errorf("first provider should be called once, got %d", failing.callCount)
	}
	if succeeding.callCount != 1 {
		t.Errorf("second provider should be called once, got %d", succeeding.callCount)
	}
}

func TestRateService_XLMAllProvidersFail_ReturnsStaleCached(t *testing.T) {
	svc := oracle.NewRateServiceWithFetchers(
		[]oracle.Provider{
			&stubProvider{name: "p1", err: errors.New("network error")},
			&stubProvider{name: "p2", err: errors.New("network error")},
		},
		&stubProvider{name: "fiat"},
	)

	// Pre-fill with an expired (stale) entry.
	svc.Cache().Set(expiredRate("XLM", "USD", 0.12))

	r, err := svc.GetRate(context.Background(), "XLM", "USD")
	if err != nil {
		t.Fatalf("expected stale fallback, got error: %v", err)
	}
	if !r.Stale {
		t.Error("expected Stale=true when served from expired cache")
	}
	if r.Rate != 0.12 {
		t.Errorf("expected stale rate 0.12, got %f", r.Rate)
	}
}

func TestRateService_XLMAllProvidersFail_NoCacheReturnsError(t *testing.T) {
	svc := oracle.NewRateServiceWithFetchers(
		[]oracle.Provider{
			&stubProvider{name: "p1", err: errors.New("unreachable")},
		},
		&stubProvider{name: "fiat"},
	)

	_, err := svc.GetRate(context.Background(), "XLM", "USD")
	if err == nil {
		t.Fatal("expected an error when all providers fail and cache is empty")
	}
}

func TestRateService_USDCToFiatUsesFiatProvider(t *testing.T) {
	fiat := &stubProvider{name: "forex", rate: 1650.0}
	svc := oracle.NewRateServiceWithFetchers(nil, fiat)

	r, err := svc.GetRate(context.Background(), "USDC", "NGN")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Rate != 1650.0 {
		t.Errorf("want rate 1650.0, got %f", r.Rate)
	}
	if r.Source != "forex" {
		t.Errorf("want source 'forex', got %q", r.Source)
	}
	if fiat.callCount != 1 {
		t.Errorf("fiat provider should be called once, got %d", fiat.callCount)
	}
}

func TestRateService_USDCToFiatFails_ReturnsStaleCached(t *testing.T) {
	svc := oracle.NewRateServiceWithFetchers(
		nil,
		&stubProvider{name: "forex", err: errors.New("forex unavailable")},
	)
	svc.Cache().Set(expiredRate("USDC", "NGN", 1600.0))

	r, err := svc.GetRate(context.Background(), "USDC", "NGN")
	if err != nil {
		t.Fatalf("expected stale fallback, got error: %v", err)
	}
	if !r.Stale {
		t.Error("expected Stale=true when served from expired cache")
	}
	if r.Rate != 1600.0 {
		t.Errorf("expected stale rate 1600.0, got %f", r.Rate)
	}
}

func TestRateService_CacheHitSkipsProviders(t *testing.T) {
	p := &stubProvider{name: "xlm", rate: 0.20}
	svc := oracle.NewRateServiceWithFetchers([]oracle.Provider{p}, &stubProvider{name: "fiat"})

	// Prime a fresh cache entry.
	svc.Cache().Set(oracle.ExchangeRate{
		Base:      "XLM",
		Quote:     "USD",
		Rate:      0.18,
		Source:    "cached",
		FetchedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(30 * time.Second),
	})

	r, err := svc.GetRate(context.Background(), "XLM", "USD")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Rate != 0.18 {
		t.Errorf("want cached rate 0.18, got %f", r.Rate)
	}
	if p.callCount != 0 {
		t.Errorf("provider should not be called on cache hit, got %d calls", p.callCount)
	}
}
