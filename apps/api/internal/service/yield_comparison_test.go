package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetYieldComparison(t *testing.T) {
	t.Parallel()

	defiLlama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[
			{"pool":"p1","project":"blend","symbol":"USDC","apy":8.0,"apyBase":8.0,"apyReward":0.0,"tvlUsd":1000000,"apyPct7d":1.0,"chain":"Stellar"},
			{"pool":"p2","project":"aqua","symbol":"AQUA","apy":12.0,"apyBase":2.0,"apyReward":10.0,"tvlUsd":500000,"apyPct7d":5.0,"chain":"Stellar"}
		]}`))
	}))
	t.Cleanup(defiLlama.Close)

	svc := NewYieldService(defiLlama.URL)
	comp, err := svc.GetYieldComparison(context.Background(), "Stellar", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(comp.Protocols) != 2 {
		t.Fatalf("got %d protocols, want 2", len(comp.Protocols))
	}

	// Sorted descending by APY: Aqua (12%) then Blend (8%)
	if comp.Protocols[0].Protocol != "aqua" {
		t.Errorf("first protocol = %q, want aqua", comp.Protocols[0].Protocol)
	}
	if comp.Protocols[0].CurrentAPY != 12.0 {
		t.Errorf("aqua APY = %v, want 12.0", comp.Protocols[0].CurrentAPY)
	}
	if comp.Protocols[0].TVLUSD != 500000 {
		t.Errorf("aqua TVL = %v, want 500000", comp.Protocols[0].TVLUSD)
	}
	if comp.Protocols[0].RiskTier == "" {
		t.Error("expected risk tier to be populated")
	}
}
