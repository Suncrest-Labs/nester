package service

import (
    "context"
    "fmt"
    "net/http"
    "net/http/httptest"
    "strings"
    "sync/atomic"
    "testing"
)

// newMockDeFiLlamaServer writes a raw JSON string directly to the response
func newMockDeFiLlamaServer(t *testing.T, statusCode int, rawJSON string, hitCounter *int32) *httptest.Server {
    return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if hitCounter != nil {
            atomic.AddInt32(hitCounter, 1)
        }
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(statusCode)
        if rawJSON != "" {
            w.Write([]byte(rawJSON))
        }
    }))
}

func TestYieldService_CacheHit(t *testing.T) {
    var hitCount int32
    // Added TVL to bypass the liquidity filter
    payload := `{"status":"success","data":[{"pool":"1","chain":"Stellar","apy":5.0,"tvlUsd":100000}]}`
    ts := newMockDeFiLlamaServer(t, http.StatusOK, payload, &hitCount)
    defer ts.Close()

    svc := NewYieldService(ts.URL)
    ctx := context.Background()

    _, err := svc.GetYieldOpportunities(ctx, "Stellar", 10)
    if err != nil {
        t.Fatalf("expected no error, got %v", err)
    }
    _, err = svc.GetYieldOpportunities(ctx, "Stellar", 10)
    if err != nil {
        t.Fatalf("expected no error, got %v", err)
    }

    if atomic.LoadInt32(&hitCount) != 1 {
        t.Errorf("expected exactly 1 HTTP call, but got %d", hitCount)
    }
}

func TestYieldService_ChainFilter(t *testing.T) {
    // Added TVL to bypass the liquidity filter
    payload := `{"status":"success","data":[
        {"pool":"1","chain":"Ethereum","apy":5.0,"tvlUsd":100000},
        {"pool":"2","chain":"Arbitrum","apy":5.0,"tvlUsd":100000},
        {"pool":"3","chain":"Stellar","apy":5.0,"tvlUsd":100000}
    ]}`
    ts := newMockDeFiLlamaServer(t, http.StatusOK, payload, nil)
    defer ts.Close()

    svc := NewYieldService(ts.URL)
    ctx := context.Background()

    resp, err := svc.GetYieldOpportunities(ctx, "Stellar", 10)
    if err != nil {
        t.Fatalf("expected no error, got %v", err)
    }

    if len(resp.Pools) != 1 {
        t.Fatalf("expected 1 pool, got %d. Dump: %+v", len(resp.Pools), resp.Pools)
    }
    if resp.Pools[0].Chain != "Stellar" {
        t.Errorf("expected Stellar pool, got %s", resp.Pools[0].Chain)
    }
}

func TestYieldService_Limits(t *testing.T) {
    var buf strings.Builder
    buf.WriteString(`{"status":"success","data":[`)
    for i := 0; i < 150; i++ {
        // Added TVL to bypass the liquidity filter
        buf.WriteString(fmt.Sprintf(`{"pool":"%d","chain":"Stellar","apy":5.0,"tvlUsd":100000}`, i))
        if i < 149 {
            buf.WriteString(",")
        }
    }
    buf.WriteString(`]}`)

    ts := newMockDeFiLlamaServer(t, http.StatusOK, buf.String(), nil)
    defer ts.Close()

    svc := NewYieldService(ts.URL)
    ctx := context.Background()

    tests := []struct {
        name           string
        requestedLimit int
        expectedCount  int
    }{
        {"Test 3: Limit is respected", 5, 5},
        {"Test 4: Limit is clamped to 100", 500, 100},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            resp, err := svc.GetYieldOpportunities(ctx, "Stellar", tt.requestedLimit)
            if err != nil {
                t.Fatalf("expected no error, got %v", err)
            }
            if len(resp.Pools) != tt.expectedCount {
                t.Errorf("expected %d pools, got %d. Dump: %+v", tt.expectedCount, len(resp.Pools), resp.Pools)
            }
        })
    }
}

func TestYieldService_UpstreamError(t *testing.T) {
    ts := newMockDeFiLlamaServer(t, http.StatusInternalServerError, "", nil)
    defer ts.Close()

    svc := NewYieldService(ts.URL)
    ctx := context.Background()

    _, err := svc.GetYieldOpportunities(ctx, "Stellar", 10)
    if err == nil {
        t.Error("expected an error when upstream returns 500, but got nil")
    }
}

func TestYieldService_EmptyList(t *testing.T) {
    payload := `{"status":"success","data":[]}`
    ts := newMockDeFiLlamaServer(t, http.StatusOK, payload, nil)
    defer ts.Close()

    svc := NewYieldService(ts.URL)
    ctx := context.Background()

    resp, err := svc.GetYieldOpportunities(ctx, "Stellar", 10)
    if err != nil {
        t.Fatalf("expected no error, got %v", err)
    }
    if resp == nil {
        t.Fatal("expected response object, got nil")
    }
    if len(resp.Pools) != 0 {
        t.Errorf("expected 0 pools, got %d", len(resp.Pools))
    }
}

func TestYieldService_TVLFilter(t *testing.T) {
    t.Skip("Skipping until #669 (TVL filter) is merged")
}

func TestYieldService_RiskScore(t *testing.T) {
    t.Skip("Skipping until #662 (Risk score) is merged")
}

func TestYieldService_WarmCachePopulatesCache(t *testing.T) {
    var hitCount int32
    payload := `{"status":"success","data":[{"pool":"1","chain":"Stellar","apy":5.0,"tvlUsd":100000},{"pool":"2","chain":"Stellar","apy":3.0,"tvlUsd":200000}]}`
    ts := newMockDeFiLlamaServer(t, http.StatusOK, payload, &hitCount)
    defer ts.Close()

    svc := NewYieldService(ts.URL)
    ctx := context.Background()

    pools, err := svc.WarmCache(ctx)
    if err != nil {
        t.Fatalf("WarmCache() error = %v", err)
    }
    if pools != 2 {
        t.Errorf("WarmCache() pools = %d, want 2", pools)
    }

    // The warmed cache serves the same chain/limit key without another upstream call.
    if _, err := svc.GetYieldOpportunities(ctx, "Stellar", 100); err != nil {
        t.Fatalf("GetYieldOpportunities() after warm error = %v", err)
    }
    if atomic.LoadInt32(&hitCount) != 1 {
        t.Errorf("expected exactly 1 HTTP call after warm, got %d", hitCount)
    }
}

func TestYieldService_WarmCacheFailureIsNonFatal(t *testing.T) {
    ts := newMockDeFiLlamaServer(t, http.StatusInternalServerError, "", nil)
    defer ts.Close()

    svc := NewYieldService(ts.URL)
    pools, err := svc.WarmCache(context.Background())
    if err == nil {
        t.Fatal("WarmCache() error = nil, want upstream error")
    }
    if pools != 0 {
        t.Errorf("WarmCache() pools = %d, want 0 on failure", pools)
    }

    // A failed warm leaves no cache entry: the lazy path still surfaces the
    // upstream error rather than serving stale/empty data.
    if _, err := svc.GetYieldOpportunities(context.Background(), "Stellar", 100); err == nil {
        t.Error("GetYieldOpportunities() after failed warm = nil error, want upstream error")
    }
}
