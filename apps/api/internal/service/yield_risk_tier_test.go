package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRiskTierForScore(t *testing.T) {
	t.Parallel()

	cases := []struct {
		score float64
		want  string
	}{
		{0.0, RiskTierLow},
		{0.2, RiskTierLow},
		{0.3, RiskTierLow},     // 30
		{0.33, RiskTierLow},    // 33 — upper edge of low
		{0.34, RiskTierMedium}, // 34 — lower edge of medium
		{0.4, RiskTierMedium},
		{0.5, RiskTierMedium},
		{0.6, RiskTierMedium},
		{0.66, RiskTierMedium}, // 66 — upper edge of medium
		{0.67, RiskTierHigh},   // 67 — lower edge of high
		{0.7, RiskTierHigh},
		{0.9, RiskTierHigh},
		{1.0, RiskTierHigh},
	}
	for _, c := range cases {
		if got := RiskTierForScore(c.score); got != c.want {
			t.Errorf("RiskTierForScore(%v) = %q, want %q", c.score, got, c.want)
		}
	}
}

func TestIsValidRiskTier(t *testing.T) {
	t.Parallel()

	for _, s := range []string{RiskTierLow, RiskTierMedium, RiskTierHigh} {
		if !IsValidRiskTier(s) {
			t.Errorf("IsValidRiskTier(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", "LOW", "extreme", "lo", "none"} {
		if IsValidRiskTier(s) {
			t.Errorf("IsValidRiskTier(%q) = true, want false", s)
		}
	}
}

// mixedTierDefiLlama serves three Stellar pools that land in distinct tiers once
// scored: p_low (0.0), p_med (0.5: APY swing + reward dependency), p_high (0.9:
// also low TVL). The min-TVL env override lets the low-TVL high-risk pool past
// the threshold filter so all three tiers are representable.
func mixedTierDefiLlama(t *testing.T) string {
	t.Helper()
	t.Setenv("YIELD_MIN_TVL_USD", "1000")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[
			{"pool":"p_low","project":"blend","symbol":"USDC","apy":6.0,"apyBase":6.0,"apyReward":0.0,"tvlUsd":5000000,"apyPct7d":1.0,"chain":"Stellar"},
			{"pool":"p_med","project":"aqua","symbol":"AQUA","apy":10.0,"apyBase":1.0,"apyReward":9.0,"tvlUsd":5000000,"apyPct7d":25.0,"chain":"Stellar"},
			{"pool":"p_high","project":"risky","symbol":"FOO","apy":10.0,"apyBase":1.0,"apyReward":9.0,"tvlUsd":50000,"apyPct7d":25.0,"chain":"Stellar"}
		]}`))
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func tierOf(t *testing.T, pools []YieldPool, pool string) string {
	t.Helper()
	for _, p := range pools {
		if p.Pool == pool {
			return p.RiskTier
		}
	}
	t.Fatalf("pool %q not found in %+v", pool, pools)
	return ""
}

func TestGetYieldOpportunitiesByTier_EmptyTierReturnsAll(t *testing.T) {
	svc := NewYieldService(mixedTierDefiLlama(t))

	resp, err := svc.GetYieldOpportunitiesByTier(context.Background(), "Stellar", 20, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Pools) != 3 {
		t.Fatalf("empty tier should return all 3 pools, got %d: %+v", len(resp.Pools), resp.Pools)
	}
	// Each opportunity carries its tier classification.
	if got := tierOf(t, resp.Pools, "p_low"); got != RiskTierLow {
		t.Errorf("p_low tier = %q, want low", got)
	}
	if got := tierOf(t, resp.Pools, "p_med"); got != RiskTierMedium {
		t.Errorf("p_med tier = %q, want medium", got)
	}
	if got := tierOf(t, resp.Pools, "p_high"); got != RiskTierHigh {
		t.Errorf("p_high tier = %q, want high", got)
	}
}

func TestGetYieldOpportunitiesByTier_FiltersToRequestedTier(t *testing.T) {
	url := mixedTierDefiLlama(t)

	for _, tc := range []struct {
		tier string
		pool string
	}{
		{RiskTierLow, "p_low"},
		{RiskTierMedium, "p_med"},
		{RiskTierHigh, "p_high"},
	} {
		t.Run(tc.tier, func(t *testing.T) {
			svc := NewYieldService(url)
			resp, err := svc.GetYieldOpportunitiesByTier(context.Background(), "Stellar", 20, tc.tier)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(resp.Pools) != 1 {
				t.Fatalf("tier %q should return exactly 1 pool, got %d: %+v", tc.tier, len(resp.Pools), resp.Pools)
			}
			if resp.Pools[0].Pool != tc.pool {
				t.Fatalf("tier %q returned %q, want %q", tc.tier, resp.Pools[0].Pool, tc.pool)
			}
			if resp.Pools[0].RiskTier != tc.tier {
				t.Fatalf("returned pool tier = %q, want %q", resp.Pools[0].RiskTier, tc.tier)
			}
		})
	}
}

func TestGetYieldOpportunitiesByTier_LimitAppliesAfterFilter(t *testing.T) {
	svc := NewYieldService(mixedTierDefiLlama(t))

	// limit=1 with no tier truncates the full set to 1.
	resp, err := svc.GetYieldOpportunitiesByTier(context.Background(), "Stellar", 1, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Pools) != 1 {
		t.Fatalf("limit=1 should return 1 pool, got %d", len(resp.Pools))
	}

	// limit=1 with tier=low still finds the single low pool even though it is not
	// the top pool by risk-adjusted APY (the filter runs before the limit).
	low, err := svc.GetYieldOpportunitiesByTier(context.Background(), "Stellar", 1, RiskTierLow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(low.Pools) != 1 || low.Pools[0].Pool != "p_low" {
		t.Fatalf("tier=low,limit=1 should return p_low, got %+v", low.Pools)
	}
}
