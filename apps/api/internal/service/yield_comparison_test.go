package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestGetYieldComparison_AggregatesMultiPoolProtocolsByTVLWeight pins the
// core contract (nester#950): a protocol with more than one active pool comes
// back as one entry, with APY and risk TVL-weighted across its pools and TVL
// summed. blend has two pools here; aqua has one, so aqua's entry is just its
// own pool's numbers unweighted.
func TestGetYieldComparison_AggregatesMultiPoolProtocolsByTVLWeight(t *testing.T) {
	payload := `{"status":"success","data":[
		{"pool":"b1","project":"blend","symbol":"USDC","apy":6.0,"tvlUsd":2000000,"chain":"Stellar"},
		{"pool":"b2","project":"blend","symbol":"XLM","apy":8.23,"tvlUsd":2200000,"chain":"Stellar"},
		{"pool":"a1","project":"aqua","symbol":"AQUA","apy":11.0,"tvlUsd":890000,"chain":"Stellar"}
	]}`
	ts := newMockDeFiLlamaServer(t, 200, payload, nil)
	defer ts.Close()

	svc := NewYieldService(ts.URL)
	got, err := svc.GetYieldComparison(context.Background(), "Stellar", 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Protocols) != 2 {
		t.Fatalf("got %d protocols, want 2: %+v", len(got.Protocols), got.Protocols)
	}

	var blend, aqua *YieldComparisonEntry
	for i := range got.Protocols {
		switch got.Protocols[i].Protocol {
		case "blend":
			blend = &got.Protocols[i]
		case "aqua":
			aqua = &got.Protocols[i]
		}
	}
	if blend == nil || aqua == nil {
		t.Fatalf("expected both blend and aqua present, got %+v", got.Protocols)
	}

	wantBlendTVL := 2_000_000.0 + 2_200_000.0
	if blend.TVLUSD != wantBlendTVL {
		t.Errorf("blend tvl_usd = %v, want %v", blend.TVLUSD, wantBlendTVL)
	}
	wantBlendAPY := (6.0*2_000_000.0 + 8.23*2_200_000.0) / wantBlendTVL
	if diff := blend.CurrentAPY - wantBlendAPY; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("blend current_apy = %v, want %v", blend.CurrentAPY, wantBlendAPY)
	}

	if aqua.TVLUSD != 890_000 || aqua.CurrentAPY != 11.0 {
		t.Errorf("aqua entry wrong: %+v", aqua)
	}
}

// TestGetYieldComparison_SortsDescendingByCurrentAPY pins the documented
// ordering: highest APY first, so a comparison view can render top-to-bottom
// without its own sort.
func TestGetYieldComparison_SortsDescendingByCurrentAPY(t *testing.T) {
	payload := `{"status":"success","data":[
		{"pool":"p1","project":"low-apy","symbol":"A","apy":2.0,"tvlUsd":500000,"chain":"Stellar"},
		{"pool":"p2","project":"high-apy","symbol":"B","apy":15.0,"tvlUsd":500000,"chain":"Stellar"},
		{"pool":"p3","project":"mid-apy","symbol":"C","apy":8.0,"tvlUsd":500000,"chain":"Stellar"}
	]}`
	ts := newMockDeFiLlamaServer(t, 200, payload, nil)
	defer ts.Close()

	svc := NewYieldService(ts.URL)
	got, err := svc.GetYieldComparison(context.Background(), "Stellar", 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Protocols) != 3 {
		t.Fatalf("got %d protocols, want 3: %+v", len(got.Protocols), got.Protocols)
	}
	wantOrder := []string{"high-apy", "mid-apy", "low-apy"}
	for i, want := range wantOrder {
		if got.Protocols[i].Protocol != want {
			t.Errorf("position %d = %q, want %q (full order: %+v)", i, got.Protocols[i].Protocol, want, got.Protocols)
		}
	}
}

// TestGetYieldComparison_LimitTruncatesAfterSorting proves the limit is
// applied to the sorted, aggregated result — the highest-APY protocols
// survive truncation, not an arbitrary prefix of the unsorted pool list.
func TestGetYieldComparison_LimitTruncatesAfterSorting(t *testing.T) {
	payload := `{"status":"success","data":[
		{"pool":"p1","project":"low-apy","symbol":"A","apy":2.0,"tvlUsd":500000,"chain":"Stellar"},
		{"pool":"p2","project":"high-apy","symbol":"B","apy":15.0,"tvlUsd":500000,"chain":"Stellar"},
		{"pool":"p3","project":"mid-apy","symbol":"C","apy":8.0,"tvlUsd":500000,"chain":"Stellar"}
	]}`
	ts := newMockDeFiLlamaServer(t, 200, payload, nil)
	defer ts.Close()

	svc := NewYieldService(ts.URL)
	got, err := svc.GetYieldComparison(context.Background(), "Stellar", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Protocols) != 1 {
		t.Fatalf("got %d protocols, want 1", len(got.Protocols))
	}
	if got.Protocols[0].Protocol != "high-apy" {
		t.Errorf("kept protocol = %q, want the highest-APY one (high-apy)", got.Protocols[0].Protocol)
	}
}

// TestGetYieldComparison_LimitClampsToValidRange proves the service defends
// its own bounds rather than trusting the caller: <=0 defaults to 100, and
// anything above 100 is clamped down to it. The handler also validates this
// range before calling in, but the service must not silently misbehave if
// called directly or from a future caller that skips that check.
func TestGetYieldComparison_LimitClampsToValidRange(t *testing.T) {
	payload := `{"status":"success","data":[{"pool":"p1","project":"solo","symbol":"A","apy":5.0,"tvlUsd":500000,"chain":"Stellar"}]}`

	for _, limit := range []int{0, -5, 500} {
		ts := newMockDeFiLlamaServer(t, 200, payload, nil)
		svc := NewYieldService(ts.URL)
		got, err := svc.GetYieldComparison(context.Background(), "Stellar", limit)
		ts.Close()
		if err != nil {
			t.Fatalf("limit=%d: unexpected error: %v", limit, err)
		}
		if len(got.Protocols) != 1 {
			t.Fatalf("limit=%d: got %d protocols, want the single solo entry to survive clamping", limit, len(got.Protocols))
		}
	}
}

// TestGetYieldComparison_UpstreamErrorPropagates proves a DeFiLlama failure
// surfaces as an error rather than an empty, misleadingly-successful
// comparison.
func TestGetYieldComparison_UpstreamErrorPropagates(t *testing.T) {
	ts := newMockDeFiLlamaServer(t, 500, "", nil)
	defer ts.Close()

	svc := NewYieldService(ts.URL)
	_, err := svc.GetYieldComparison(context.Background(), "Stellar", 100)
	if err == nil {
		t.Fatal("expected an error when the upstream fails, got nil")
	}
}

// TestGetYieldComparison_RiskTierIsDerivedFromScore proves each entry carries
// a risk_tier consistent with RiskTierForScore(risk_score), not a raw or
// stale value.
func TestGetYieldComparison_RiskTierIsDerivedFromScore(t *testing.T) {
	// tvlUsd below $1M and apyPct7d volatility push risk_score into the high
	// tier under the service's own scoring (mirrors mixedTierDefiLlama's
	// p_high fixture in yield_risk_tier_test.go).
	t.Setenv("YIELD_MIN_TVL_USD", "1000")
	payload := `{"status":"success","data":[
		{"pool":"p_high","project":"risky","symbol":"FOO","apy":10.0,"apyBase":1.0,"apyReward":9.0,"tvlUsd":50000,"apyPct7d":25.0,"chain":"Stellar"}
	]}`
	ts := newMockDeFiLlamaServer(t, 200, payload, nil)
	defer ts.Close()

	svc := NewYieldService(ts.URL)
	got, err := svc.GetYieldComparison(context.Background(), "Stellar", 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Protocols) != 1 {
		t.Fatalf("got %d protocols, want 1: %+v", len(got.Protocols), got.Protocols)
	}
	entry := got.Protocols[0]
	if want := RiskTierForScore(entry.RiskScore); entry.RiskTier != want {
		t.Errorf("risk_tier = %q, want %q (derived from risk_score %v)", entry.RiskTier, want, entry.RiskScore)
	}
	if entry.RiskTier != RiskTierHigh {
		t.Errorf("risk_tier = %q, want high for this fixture", entry.RiskTier)
	}
}

// TestGetYieldComparison_AggregatesFullPoolSetBeforeLimit pins the fix for a
// bug where the requested comparison `limit` was passed straight through to
// pool retrieval, truncating the *raw* pool list (sorted by risk-adjusted
// APY, not by protocol) before aggregation. A multi-pool protocol whose
// individual pools straddled that raw cutoff had some of its pools silently
// dropped, understating its aggregated TVL and APY.
//
// Here "blend" has 3 pools that rank 5th-7th individually by APY, behind 4
// single-pool protocols. With limit=5, the old pool-level truncation kept
// only blend's top pool (dropping the other two), while the fix fetches
// every eligible pool, aggregates first, and applies the limit to the
// resulting protocol list.
func TestGetYieldComparison_AggregatesFullPoolSetBeforeLimit(t *testing.T) {
	payload := `{"status":"success","data":[
		{"pool":"p1","project":"p1","symbol":"P1","apy":50.0,"tvlUsd":100000,"chain":"Stellar"},
		{"pool":"p2","project":"p2","symbol":"P2","apy":45.0,"tvlUsd":100000,"chain":"Stellar"},
		{"pool":"p3","project":"p3","symbol":"P3","apy":40.0,"tvlUsd":100000,"chain":"Stellar"},
		{"pool":"p4","project":"p4","symbol":"P4","apy":35.0,"tvlUsd":100000,"chain":"Stellar"},
		{"pool":"blend1","project":"blend","symbol":"USDC","apy":30.0,"tvlUsd":1000000,"chain":"Stellar"},
		{"pool":"blend2","project":"blend","symbol":"XLM","apy":28.0,"tvlUsd":1000000,"chain":"Stellar"},
		{"pool":"blend3","project":"blend","symbol":"AQUA","apy":25.0,"tvlUsd":1000000,"chain":"Stellar"}
	]}`
	ts := newMockDeFiLlamaServer(t, http.StatusOK, payload, nil)
	defer ts.Close()

	svc := NewYieldService(ts.URL)
	got, err := svc.GetYieldComparison(context.Background(), "Stellar", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Protocols) != 5 {
		t.Fatalf("got %d protocols, want 5 (p1-p4 plus blend): %+v", len(got.Protocols), got.Protocols)
	}

	var blend *YieldComparisonEntry
	for i := range got.Protocols {
		if got.Protocols[i].Protocol == "blend" {
			blend = &got.Protocols[i]
		}
	}
	if blend == nil {
		t.Fatalf("expected blend to survive aggregation, got %+v", got.Protocols)
	}

	wantBlendTVL := 3_000_000.0
	if blend.TVLUSD != wantBlendTVL {
		t.Errorf("blend tvl_usd = %v, want %v (all 3 pools, not just the top-ranked one)", blend.TVLUSD, wantBlendTVL)
	}
	wantBlendAPY := (30.0 + 28.0 + 25.0) / 3.0
	if diff := blend.CurrentAPY - wantBlendAPY; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("blend current_apy = %v, want %v (weighted across all 3 pools)", blend.CurrentAPY, wantBlendAPY)
	}
}

// TestGetYieldComparison_PropagatesFreshnessMetadata pins the fix carrying
// the opportunity service's freshness metadata through to the comparison
// response, so a caller can distinguish a comparison served from stale cache
// (after an upstream failure) from an ordinary successful one.
func TestGetYieldComparison_PropagatesFreshnessMetadata(t *testing.T) {
	payload := `{"status":"success","data":[{"pool":"p1","project":"blend","symbol":"USDC","apy":6.0,"tvlUsd":500000,"chain":"Stellar"}]}`

	var upstreamHealthy atomic.Bool
	upstreamHealthy.Store(true)
	var hits int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		if !upstreamHealthy.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}))
	defer ts.Close()

	svc := NewYieldService(ts.URL)
	// Force the fresh-cache entry to be immediately expired (but still within
	// the stale-cache window) so the second call is forced to hit upstream
	// and fall back to stale data, without waiting on a real clock.
	svc.cacheTTL = time.Nanosecond

	fresh, err := svc.GetYieldComparison(context.Background(), "Stellar", 100)
	if err != nil {
		t.Fatalf("unexpected error priming the cache: %v", err)
	}
	if fresh.Meta.Stale {
		t.Errorf("expected a fresh comparison to report meta.stale=false, got %+v", fresh.Meta)
	}

	upstreamHealthy.Store(false)
	stale, err := svc.GetYieldComparison(context.Background(), "Stellar", 100)
	if err != nil {
		t.Fatalf("expected the stale-cache fallback to succeed, got error: %v", err)
	}
	if !stale.Meta.Stale {
		t.Errorf("expected meta.stale=true once upstream fails and the fallback serves cached pools, got %+v", stale.Meta)
	}
	if stale.Meta.FetchedAt == "" {
		t.Errorf("expected meta.fetched_at to be set on a stale response, got %+v", stale.Meta)
	}
	if len(stale.Protocols) != 1 || stale.Protocols[0].Protocol != "blend" {
		t.Errorf("expected the stale cached protocol data to survive the fallback, got %+v", stale.Protocols)
	}
}
