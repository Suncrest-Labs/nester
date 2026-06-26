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
	NewYieldHandler(service.NewYieldService(defiLlama.URL)).Register(mux)
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
