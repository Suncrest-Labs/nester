package oracle_test

import (
	"testing"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/oracle"
)

func TestRateCache_MissOnEmptyCache(t *testing.T) {
	c := oracle.NewRateCache()

	_, ok := c.Get("USDC", "NGN")
	if ok {
		t.Error("expected miss on empty cache")
	}
	if c.IsFresh("USDC", "NGN") {
		t.Error("expected not-fresh on empty cache")
	}
}

func TestRateCache_StoresAndRetrieves(t *testing.T) {
	c := oracle.NewRateCache()
	r := oracle.ExchangeRate{
		Base:      "USDC",
		Quote:     "NGN",
		Rate:      1650.0,
		Source:    "test",
		FetchedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(5 * time.Minute),
	}
	c.Set(r)

	got, ok := c.Get("USDC", "NGN")
	if !ok {
		t.Fatal("expected cache hit after Set")
	}
	if got.Rate != r.Rate {
		t.Errorf("want rate %.2f, got %.2f", r.Rate, got.Rate)
	}
	if got.Source != r.Source {
		t.Errorf("want source %q, got %q", r.Source, got.Source)
	}
}

func TestRateCache_IsFreshBeforeExpiry(t *testing.T) {
	c := oracle.NewRateCache()
	c.Set(oracle.ExchangeRate{
		Base:      "XLM",
		Quote:     "USD",
		Rate:      0.12,
		Source:    "test",
		FetchedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(30 * time.Second),
	})

	if !c.IsFresh("XLM", "USD") {
		t.Error("expected entry to be fresh before expiry")
	}
}

func TestRateCache_NotFreshAfterExpiry(t *testing.T) {
	c := oracle.NewRateCache()
	c.Set(oracle.ExchangeRate{
		Base:      "XLM",
		Quote:     "USD",
		Rate:      0.12,
		Source:    "test",
		FetchedAt: time.Now().UTC().Add(-2 * time.Minute),
		ExpiresAt: time.Now().UTC().Add(-1 * time.Second), // already expired
	})

	if c.IsFresh("XLM", "USD") {
		t.Error("expected expired entry to be not-fresh")
	}

	// Stale entry should still be retrievable for fallback serving.
	_, ok := c.Get("XLM", "USD")
	if !ok {
		t.Error("expected expired entry to still be retrievable via Get")
	}
}

func TestRateCache_OverwritesExistingEntry(t *testing.T) {
	c := oracle.NewRateCache()
	c.Set(oracle.ExchangeRate{Base: "USDC", Quote: "KES", Rate: 130.0, ExpiresAt: time.Now().Add(time.Minute)})
	c.Set(oracle.ExchangeRate{Base: "USDC", Quote: "KES", Rate: 135.5, ExpiresAt: time.Now().Add(time.Minute)})

	got, _ := c.Get("USDC", "KES")
	if got.Rate != 135.5 {
		t.Errorf("want updated rate 135.5, got %f", got.Rate)
	}
}

func TestRateCache_IndependentKeys(t *testing.T) {
	c := oracle.NewRateCache()
	c.Set(oracle.ExchangeRate{Base: "USDC", Quote: "NGN", Rate: 1650.0, ExpiresAt: time.Now().Add(time.Minute)})
	c.Set(oracle.ExchangeRate{Base: "USDC", Quote: "GHS", Rate: 15.5, ExpiresAt: time.Now().Add(time.Minute)})

	ngn, _ := c.Get("USDC", "NGN")
	ghs, _ := c.Get("USDC", "GHS")

	if ngn.Rate == ghs.Rate {
		t.Error("different pairs should store independently")
	}
}
