package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/suncrestlabs/nester/apps/api/internal/service"
)

// newYieldServerMixedTiers wires the handler to a real YieldService whose mock
// DeFiLlama returns Stellar pools spanning all three risk tiers. The min-TVL
// override lets the low-TVL high-risk pool past the threshold filter.
func newYieldServerMixedTiers(t *testing.T) *httptest.Server {
	t.Helper()
	t.Setenv("YIELD_MIN_TVL_USD", "1000")
	defiLlama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[
			{"pool":"p_low","project":"blend","symbol":"USDC","apy":6.0,"apyBase":6.0,"apyReward":0.0,"tvlUsd":5000000,"apyPct7d":1.0,"chain":"Stellar"},
			{"pool":"p_med","project":"aqua","symbol":"AQUA","apy":10.0,"apyBase":1.0,"apyReward":9.0,"tvlUsd":5000000,"apyPct7d":25.0,"chain":"Stellar"},
			{"pool":"p_high","project":"risky","symbol":"FOO","apy":10.0,"apyBase":1.0,"apyReward":9.0,"tvlUsd":50000,"apyPct7d":25.0,"chain":"Stellar"}
		]}`))
	}))
	t.Cleanup(defiLlama.Close)

	mux := http.NewServeMux()
	NewYieldHandler(service.NewYieldService(defiLlama.URL), nil).Register(mux)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

type yieldListBody struct {
	Success bool `json:"success"`
	Data    struct {
		Data []service.YieldPool `json:"data"`
	} `json:"data"`
}

func fetchYields(t *testing.T, server *httptest.Server, query string) (*http.Response, []service.YieldPool) {
	t.Helper()
	resp, err := http.Get(server.URL + "/api/v1/yield-opportunities" + query)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		return resp, nil
	}
	var body yieldListBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp, body.Data.Data
}

func TestYieldList_NoRiskParamReturnsAllTiers(t *testing.T) {
	server := newYieldServerMixedTiers(t)
	resp, pools := fetchYields(t, server, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if len(pools) != 3 {
		t.Fatalf("expected all 3 tiers, got %d: %+v", len(pools), pools)
	}
}

func TestYieldList_RiskParamFiltersByTier(t *testing.T) {
	server := newYieldServerMixedTiers(t)

	for _, tc := range []struct {
		tier string
		pool string
	}{
		{"low", "p_low"},
		{"medium", "p_med"},
		{"high", "p_high"},
	} {
		t.Run(tc.tier, func(t *testing.T) {
			resp, pools := fetchYields(t, server, "?risk="+tc.tier)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
			if len(pools) != 1 {
				t.Fatalf("risk=%s should return 1 pool, got %d: %+v", tc.tier, len(pools), pools)
			}
			if pools[0].Pool != tc.pool {
				t.Fatalf("risk=%s returned %q, want %q", tc.tier, pools[0].Pool, tc.pool)
			}
			if pools[0].RiskTier != tc.tier {
				t.Fatalf("risk_tier = %q, want %q", pools[0].RiskTier, tc.tier)
			}
		})
	}
}

func TestYieldList_ResponseIncludesRiskFields(t *testing.T) {
	server := newYieldServerMixedTiers(t)

	// Decode into a raw map to assert the exact JSON keys are present per item.
	httpResp, err := http.Get(server.URL + "/api/v1/yield-opportunities")
	if err != nil {
		t.Fatal(err)
	}
	defer httpResp.Body.Close()
	var raw struct {
		Data struct {
			Data []map[string]json.RawMessage `json:"data"`
		} `json:"data"`
	}
	if err := json.NewDecoder(httpResp.Body).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	if len(raw.Data.Data) == 0 {
		t.Fatal("expected at least one opportunity")
	}
	for i, item := range raw.Data.Data {
		if _, ok := item["risk_score"]; !ok {
			t.Errorf("opportunity %d missing risk_score field", i)
		}
		if _, ok := item["risk_tier"]; !ok {
			t.Errorf("opportunity %d missing risk_tier field", i)
		}
	}
}

func TestYieldList_InvalidRiskReturns400(t *testing.T) {
	server := newYieldServerMixedTiers(t)
	resp, _ := fetchYields(t, server, "?risk=extreme")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}
