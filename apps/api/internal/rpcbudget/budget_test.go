package rpcbudget

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLimiterShedsLowestPriorityFirst(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	lim := NewWithClock("soroban_rpc", Config{
		RefillRate:   10,
		Burst:        20,
		HighReserve:  0.25, // 5 tokens reserved for PriorityHigh
		LowThreshold: 0.50, // 10 tokens threshold for PriorityLow
	}, func() time.Time { return now })

	// Consume 11 low-priority tokens: 20 -> 19 -> ... -> 10 (admitted), then 9 < 10 (shed).
	for i := 0; i < 11; i++ {
		if err := lim.Allow(PriorityLow); err != nil {
			t.Fatalf("low call %d unexpectedly shed: %v", i, err)
		}
	}
	// Now tokens = 9, which is below LowThreshold (10).
	if err := lim.Allow(PriorityLow); !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("expected PriorityLow to be shed at 9 tokens, got %v", err)
	}

	// Normal priority still has tokens >= 5 (HighReserve), so 5 normal calls succeed (9 -> 4).
	for i := 0; i < 5; i++ {
		if err := lim.Allow(PriorityNormal); err != nil {
			t.Fatalf("normal call %d unexpectedly shed: %v", i, err)
		}
	}
	// Now tokens = 4, below HighReserve (5): normal is shed.
	if err := lim.Allow(PriorityNormal); !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("expected PriorityNormal to be shed at 4 tokens, got %v", err)
	}

	// High priority can still consume the remaining 4 reserved tokens (4 -> 0).
	for i := 0; i < 4; i++ {
		if err := lim.Allow(PriorityHigh); err != nil {
			t.Fatalf("high call %d unexpectedly shed: %v", i, err)
		}
	}
	// Bucket is now completely empty (0 tokens): even high is shed.
	if err := lim.Allow(PriorityHigh); !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("expected PriorityHigh to be shed at 0 tokens, got %v", err)
	}

	snap := lim.Snapshot()
	if snap.Utilization != 1.0 {
		t.Fatalf("utilization = %v, want 1.0", snap.Utilization)
	}
	if snap.Admitted[PriorityLow] != 11 || snap.Shed[PriorityLow] != 1 {
		t.Fatalf("low counts = admitted:%d shed:%d, want 11/1", snap.Admitted[PriorityLow], snap.Shed[PriorityLow])
	}
	if snap.Admitted[PriorityNormal] != 5 || snap.Shed[PriorityNormal] != 1 {
		t.Fatalf("normal counts = admitted:%d shed:%d, want 5/1", snap.Admitted[PriorityNormal], snap.Shed[PriorityNormal])
	}
	if snap.Admitted[PriorityHigh] != 4 || snap.Shed[PriorityHigh] != 1 {
		t.Fatalf("high counts = admitted:%d shed:%d, want 4/1", snap.Admitted[PriorityHigh], snap.Shed[PriorityHigh])
	}

	// Advance clock by 0.5s -> refills 5 tokens. High and Normal should be admitted again, Low still shed.
	now = now.Add(500 * time.Millisecond)
	if err := lim.Allow(PriorityLow); !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("expected PriorityLow still shed at 5 tokens, got %v", err)
	}
	if err := lim.Allow(PriorityNormal); err != nil {
		t.Fatalf("expected PriorityNormal admitted at 5 tokens, got %v", err)
	}
	if err := lim.Allow(PriorityHigh); err != nil {
		t.Fatalf("expected PriorityHigh admitted at 4 tokens, got %v", err)
	}
}

func TestPriorityContextHelpers(t *testing.T) {
	if got := PriorityFromContext(context.Background()); got != PriorityNormal {
		t.Fatalf("default priority = %v, want PriorityNormal", got)
	}

	ctx := WithDefaultPriority(context.Background(), PriorityLow)
	if got := PriorityFromContext(ctx); got != PriorityLow {
		t.Fatalf("WithDefaultPriority = %v, want PriorityLow", got)
	}

	// WithDefaultPriority must not override an explicit priority.
	ctxHigh := WithPriority(context.Background(), PriorityHigh)
	ctxHigh = WithDefaultPriority(ctxHigh, PriorityLow)
	if got := PriorityFromContext(ctxHigh); got != PriorityHigh {
		t.Fatalf("WithDefaultPriority overwrote PriorityHigh: got %v", got)
	}
}

func TestRouterLongestPrefixAndIsolation(t *testing.T) {
	soroban := New("soroban_rpc", DefaultConfig())
	horizon := New("horizon", DefaultConfig())

	r := NewRouter()
	if err := r.Register("https://rpc.example.com/soroban", soroban); err != nil {
		t.Fatalf("register soroban: %v", err)
	}
	if err := r.Register("https://rpc.example.com/horizon", horizon); err != nil {
		t.Fatalf("register horizon: %v", err)
	}

	reqSoroban, _ := http.NewRequest(http.MethodPost, "https://rpc.example.com/soroban/v1", nil)
	reqHorizon, _ := http.NewRequest(http.MethodGet, "https://rpc.example.com/horizon/accounts/G1", nil)
	reqOther, _ := http.NewRequest(http.MethodGet, "https://other.example.com/ping", nil)

	if got := r.For(reqSoroban.URL); got != soroban {
		t.Fatalf("soroban route = %v, want soroban", got)
	}
	if got := r.For(reqHorizon.URL); got != horizon {
		t.Fatalf("horizon route = %v, want horizon", got)
	}
	if got := r.For(reqOther.URL); got != nil {
		t.Fatalf("unregistered route = %v, want nil", got)
	}
}

// TestLoadIndexerAdvancesWhileQuotesSaturated verifies the acceptance
// criterion: when the quote endpoint (PriorityLow) saturates the shared
// provider's RPC quota under heavy concurrent load, low-priority quote calls
// are shed while the indexer (PriorityHigh) continues advancing ledgers
// without stalling.
func TestLoadIndexerAdvancesWhileQuotesSaturated(t *testing.T) {
	var providerCalls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalls.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"sequence":1}}`))
	}))
	defer srv.Close()

	// Provider quota: 40 req/s, burst 20, 25% reserved for high, 50% threshold for low.
	lim := New("soroban_rpc", Config{
		RefillRate:   40,
		Burst:        20,
		HighReserve:  0.25,
		LowThreshold: 0.50,
	})

	router := NewRouter()
	if err := router.Register(srv.URL, lim); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	client := &http.Client{
		Transport: router.Transport(srv.Client().Transport),
		Timeout:   2 * time.Second,
	}

	stop := make(chan struct{})
	var quoteAdmitted atomic.Int64
	var quoteShed atomic.Int64

	// Saturate the quote endpoint with 16 concurrent workers firing PriorityLow
	// RPC calls continuously.
	var quoteWG sync.WaitGroup
	for i := 0; i < 16; i++ {
		quoteWG.Add(1)
		go func() {
			defer quoteWG.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				ctx := WithPriority(context.Background(), PriorityLow)
				req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL, nil)
				if err != nil {
					continue
				}
				resp, err := client.Do(req)
				if err != nil {
					if errors.Is(err, ErrBudgetExhausted) {
						quoteShed.Add(1)
					}
					continue
				}
				_ = resp.Body.Close()
				quoteAdmitted.Add(1)
			}
		}()
	}

	// Give quote workers a moment to saturate the bucket down to LowThreshold.
	time.Sleep(40 * time.Millisecond)

	// Run the indexer loop (PriorityHigh): advance 15 ledgers at 10ms intervals
	// while quotes remain completely saturated.
	const targetLedgers = 15
	var ledgersAdvanced int
	var indexerErrors int
	for i := 0; i < targetLedgers; i++ {
		ctx := WithPriority(context.Background(), PriorityHigh)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL, nil)
		if err != nil {
			t.Fatalf("build indexer request: %v", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			indexerErrors++
		} else {
			_ = resp.Body.Close()
			ledgersAdvanced++
		}
		time.Sleep(10 * time.Millisecond)
	}

	close(stop)
	quoteWG.Wait()

	if quoteShed.Load() == 0 {
		t.Fatalf("expected saturated quote endpoint to shed low-priority calls, got 0 shed (admitted=%d)", quoteAdmitted.Load())
	}
	if ledgersAdvanced != targetLedgers || indexerErrors != 0 {
		t.Fatalf("indexer stalled under quote saturation: advanced %d/%d ledgers (errors=%d, quotes_shed=%d, quotes_admitted=%d)",
			ledgersAdvanced, targetLedgers, indexerErrors, quoteShed.Load(), quoteAdmitted.Load())
	}

	snap := lim.Snapshot()
	if snap.Shed[PriorityLow] == 0 {
		t.Fatalf("snapshot shed[low] = 0, want > 0")
	}
	if snap.Admitted[PriorityHigh] != targetLedgers {
		t.Fatalf("snapshot admitted[high] = %d, want %d", snap.Admitted[PriorityHigh], targetLedgers)
	}
	if snap.Shed[PriorityHigh] != 0 {
		t.Fatalf("snapshot shed[high] = %d, want 0", snap.Shed[PriorityHigh])
	}
}
