package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/suncrestlabs/nester/apps/api/internal/service"
)

// newYieldServerWithRealSvc wires the handler to a real YieldService backed by a
// mock DeFiLlama server, so the comparison is exercised end-to-end. (The
// service package's own *_test.go does not compile on main, so these
// integration-style tests live in the handler package.)
func newYieldServerWithRealSvc(t *testing.T) *httptest.Server {
	t.Helper()
	defiLlama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"data": [
				{"pool":"b1","project":"blend","symbol":"USDC","apy":6.0,"apyBase":6.0,"apyReward":0.0,"tvlUsd":2000000,"chain":"Stellar"},
				{"pool":"b2","project":"blend","symbol":"XLM","apy":8.23,"apyBase":7.0,"apyReward":1.23,"tvlUsd":2200000,"chain":"Stellar"},
				{"pool":"a1","project":"aqua","symbol":"AQUA","apy":11.0,"apyBase":2.0,"apyReward":9.0,"tvlUsd":890000,"chain":"Stellar"}
			]
		}`))
	}))
	t.Cleanup(defiLlama.Close)

	mux := http.NewServeMux()
	NewYieldHandler(service.NewYieldService(defiLlama.URL), nil).Register(mux)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func fetchCompare(t *testing.T, server *httptest.Server, query string) (*http.Response, []service.ProtocolSummary) {
	t.Helper()
	resp, err := http.Get(server.URL + "/api/v1/yield-opportunities/compare?protocols=" + query)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		return resp, nil
	}
	var body struct {
		Data struct {
			Data []service.ProtocolSummary `json:"data"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp, body.Data.Data
}

func findSummary(summaries []service.ProtocolSummary, name string) (service.ProtocolSummary, bool) {
	for _, s := range summaries {
		if s.Protocol == name {
			return s, true
		}
	}
	return service.ProtocolSummary{}, false
}

func TestYieldCompare_TwoKnownOneUnknown(t *testing.T) {
	server := newYieldServerWithRealSvc(t)

	// Case-insensitive + whitespace tolerant query.
	resp, summaries := fetchCompare(t, server, "Blend,%20AQUA,unknown")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if len(summaries) != 3 {
		t.Fatalf("got %d summaries, want 3: %+v", len(summaries), summaries)
	}

	blend, ok := findSummary(summaries, "Blend")
	if !ok || !blend.Found {
		t.Fatalf("blend missing/!found: %+v", summaries)
	}
	if blend.BestAPY != 8.23 || blend.TVLUsd != 4_200_000 || blend.PoolCount != 2 || blend.TopPool != "XLM" {
		t.Fatalf("blend aggregation wrong: %+v", blend)
	}

	aqua, ok := findSummary(summaries, "AQUA")
	if !ok || !aqua.Found || aqua.BestAPY != 11.0 || aqua.PoolCount != 1 {
		t.Fatalf("aqua aggregation wrong: %+v", aqua)
	}

	unknown, ok := findSummary(summaries, "unknown")
	if !ok || unknown.Found {
		t.Fatalf("unknown should be present with found=false: %+v", unknown)
	}
}

func TestYieldCompare_TooFewReturns400(t *testing.T) {
	server := newYieldServerWithRealSvc(t)
	resp, _ := fetchCompare(t, server, "blend")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestYieldCompare_TooManyReturns400(t *testing.T) {
	server := newYieldServerWithRealSvc(t)
	resp, _ := fetchCompare(t, server, "a,b,c,d,e,f")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func fetchCompareAll(t *testing.T, server *httptest.Server, query string) (*http.Response, []service.YieldComparisonEntry) {
	t.Helper()
	url := server.URL + "/api/v1/yield-opportunities/compare-all"
	if query != "" {
		url += "?" + query
	}
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		return resp, nil
	}
	var body struct {
		Data struct {
			Data []service.YieldComparisonEntry `json:"data"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp, body.Data.Data
}

func findComparisonEntry(entries []service.YieldComparisonEntry, protocol string) (service.YieldComparisonEntry, bool) {
	for _, e := range entries {
		if e.Protocol == protocol {
			return e, true
		}
	}
	return service.YieldComparisonEntry{}, false
}

// TestYieldCompareAll_ReturnsEveryActiveProtocolUnselected pins the
// requirement compare (above) does not satisfy: a caller gets every active
// protocol back without naming any of them (nester#950). The fixture has two
// projects across three pools — blend (2 pools) and aqua (1 pool) — so this
// also exercises the TVL-weighted aggregation across blend's pools.
func TestYieldCompareAll_ReturnsEveryActiveProtocolUnselected(t *testing.T) {
	server := newYieldServerWithRealSvc(t)

	resp, entries := fetchCompareAll(t, server, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2 (blend, aqua): %+v", len(entries), entries)
	}

	blend, ok := findComparisonEntry(entries, "blend")
	if !ok {
		t.Fatalf("blend missing: %+v", entries)
	}
	wantBlendTVL := 2_000_000.0 + 2_200_000.0
	if blend.TVLUSD != wantBlendTVL {
		t.Errorf("blend tvl_usd = %v, want %v (summed across both pools)", blend.TVLUSD, wantBlendTVL)
	}
	wantBlendAPY := (6.0*2_000_000.0 + 8.23*2_200_000.0) / wantBlendTVL
	if diff := blend.CurrentAPY - wantBlendAPY; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("blend current_apy = %v, want %v (TVL-weighted)", blend.CurrentAPY, wantBlendAPY)
	}

	aqua, ok := findComparisonEntry(entries, "aqua")
	if !ok || aqua.TVLUSD != 890_000 || aqua.CurrentAPY != 11.0 {
		t.Fatalf("aqua entry wrong: %+v", aqua)
	}
}

func TestYieldCompareAll_InvalidLimitReturns400(t *testing.T) {
	server := newYieldServerWithRealSvc(t)

	for _, limit := range []string{"0", "-1", "101", "not-a-number"} {
		resp, _ := fetchCompareAll(t, server, "limit="+limit)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("limit=%s: status = %d, want 400", limit, resp.StatusCode)
		}
	}
}

func TestYieldCompareAll_LimitCapsResultCount(t *testing.T) {
	server := newYieldServerWithRealSvc(t)

	resp, entries := fetchCompareAll(t, server, "limit=1")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1 (limit=1): %+v", len(entries), entries)
	}
}
