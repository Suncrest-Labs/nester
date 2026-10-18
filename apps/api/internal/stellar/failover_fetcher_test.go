package stellar

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newRetentionAwareMockRPCServer simulates a Soroban RPC endpoint whose event
// store only retains `retentionLedgers` below `latestLedger`. Any getEvents
// request with startLedger > 0 and startLedger < (latestLedger - retentionLedgers)
// returns a JSON-RPC error matching stellar-rpc's out-of-retention response.
func newRetentionAwareMockRPCServer(
	t *testing.T,
	events []mockRPCEvent,
	latestLedger uint64,
	retentionLedgers uint64,
	callCount *atomic.Int64,
) *httptest.Server {
	t.Helper()
	oldestLedger := uint64(1)
	if latestLedger > retentionLedgers {
		oldestLedger = latestLedger - retentionLedgers
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if callCount != nil {
			callCount.Add(1)
		}

		var req struct {
			Method string `json:"method"`
			Params struct {
				StartLedger uint64 `json:"startLedger"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("mock rpc decode: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")

		if req.Method == "getLatestLedger" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      "test",
				"result":  map[string]any{"sequence": latestLedger},
			})
			return
		}

		// startLedger == 0 is used by headLedgerMinusMargin as a cheap head probe.
		if req.Params.StartLedger > 0 && req.Params.StartLedger < oldestLedger {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      "test",
				"error": map[string]any{
					"code":    -32600,
					"message": fmt.Sprintf("startLedger %d is before oldest ledger %d", req.Params.StartLedger, oldestLedger),
				},
			})
			return
		}

		type rpcEvent struct {
			ID         string         `json:"id"`
			ContractID string         `json:"contractId"`
			Ledger     uint64         `json:"ledger"`
			Topic      []string       `json:"topic"`
			Value      map[string]any `json:"value"`
		}
		var out []rpcEvent
		for _, e := range events {
			if e.Ledger < req.Params.StartLedger {
				continue
			}
			out = append(out, rpcEvent{
				ID:         e.ID,
				ContractID: e.ContractID,
				Ledger:     e.Ledger,
				Topic:      []string{e.EventType},
				Value:      e.Data,
			})
			if len(out) == 200 {
				break
			}
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      "test",
			"result": map[string]any{
				"latestLedger": latestLedger,
				"events":       out,
			},
		})
	}))
}

func TestFailoverEventFetcher_Backfills14DayGapViaArchiveProvider(t *testing.T) {
	const (
		startLedger14dAgo = uint64(55_000_000)
		// 14 days = 241,920 ledgers at 5s/ledger.
		networkTip = startLedger14dAgo + FourteenDayRetentionLedgers
		// Within the 7-day hot window (tip - 50,000 ledgers).
		recentLedger = networkTip - 50_000
	)

	fixture := []mockRPCEvent{
		{ID: "ev-day-1", ContractID: "CVAULT1", Ledger: startLedger14dAgo + 100, EventType: "deposit", Data: map[string]any{"amount": "1000"}},
		{ID: "ev-day-7", ContractID: "CVAULT1", Ledger: startLedger14dAgo + 100_000, EventType: "deposit", Data: map[string]any{"amount": "2000"}},
		{ID: "ev-day-12", ContractID: "CVAULT1", Ledger: recentLedger, EventType: "deposit", Data: map[string]any{"amount": "3000"}},
	}

	var primaryCalls, archiveCalls atomic.Int64
	primary7d := newRetentionAwareMockRPCServer(t, fixture, networkTip, DefaultHotRetentionLedgers, &primaryCalls)
	defer primary7d.Close()

	archive30d := newRetentionAwareMockRPCServer(t, fixture, networkTip, ThirtyDayRetentionLedgers, &archiveCalls)
	defer archive30d.Close()

	fetcher := NewFailoverEventFetcherWithOptions(
		&http.Client{Timeout: 5 * time.Second},
		primary7d.URL,
		nil,
		archive30d.URL,
		RPCOptions{},
		nil,
	)

	// 1. Cold-start or freshness check observes the network tip from primary.
	tip, err := fetcher.LatestLedger(context.Background())
	if err != nil {
		t.Fatalf("LatestLedger: %v", err)
	}
	if tip != networkTip {
		t.Fatalf("tip = %d, want %d", tip, networkTip)
	}

	primaryBefore := primaryCalls.Load()

	// 2. Polling from a 14-day-old cursor (241,920 ledgers behind tip) exceeds
	// DefaultHotRetentionLedgers (120,960), so FailoverEventFetcher promotes
	// the 30-day archive provider directly without hitting the 7-day primary.
	events, gotTip, err := fetcher.FetchEvents(context.Background(), []string{"CVAULT1"}, startLedger14dAgo)
	if err != nil {
		t.Fatalf("FetchEvents across 14-day gap failed: %v", err)
	}
	if gotTip != networkTip {
		t.Errorf("gotTip = %d, want %d", gotTip, networkTip)
	}
	if len(events) != 3 {
		t.Fatalf("len(events) = %d, want 3", len(events))
	}
	if primaryCalls.Load() != primaryBefore {
		t.Errorf("expected archive-first routing to skip 7-day primary for 14-day gap, but primary calls increased from %d to %d", primaryBefore, primaryCalls.Load())
	}
	if archiveCalls.Load() == 0 {
		t.Errorf("expected archive provider to serve 14-day gap")
	}

	// 3. Once the cursor advances into the 7-day hot window, requests route to
	// the primary provider first.
	archiveBefore := archiveCalls.Load()
	recentEvents, _, err := fetcher.FetchEvents(context.Background(), []string{"CVAULT1"}, recentLedger)
	if err != nil {
		t.Fatalf("FetchEvents in hot window: %v", err)
	}
	if len(recentEvents) != 1 || recentEvents[0].ID != "ev-day-12" {
		t.Fatalf("unexpected recent events: %+v", recentEvents)
	}
	if archiveCalls.Load() != archiveBefore {
		t.Errorf("expected hot-window poll to be served by primary without calling archive")
	}
}

func TestFailoverEventFetcher_FollowsConfiguredFailoverOrder(t *testing.T) {
	var mu sync.Mutex
	var order []string

	recordProvider := func(name string) {
		mu.Lock()
		defer mu.Unlock()
		if len(order) == 0 || order[len(order)-1] != name {
			order = append(order, name)
		}
	}

	makeFailingServer := func(name string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			recordProvider(name)
			w.WriteHeader(http.StatusBadGateway)
		}))
	}

	primary := makeFailingServer("primary")
	defer primary.Close()
	fallback1 := makeFailingServer("fallback-1")
	defer fallback1.Close()

	fallback2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recordProvider("fallback-2")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      "test",
			"result": map[string]any{
				"latestLedger": 1000,
				"events":       []any{},
			},
		})
	}))
	defer fallback2.Close()

	archive := makeFailingServer("archive")
	defer archive.Close()

	fetcher := NewFailoverEventFetcherWithOptions(
		&http.Client{Timeout: 2 * time.Second},
		primary.URL,
		[]string{fallback1.URL, fallback2.URL},
		archive.URL,
		RPCOptions{},
		nil,
	)

	_, tip, err := fetcher.FetchEvents(context.Background(), []string{"CVAULT1"}, 950)
	if err != nil {
		t.Fatalf("expected failover to succeed on fallback-2, got err: %v", err)
	}
	if tip != 1000 {
		t.Errorf("tip = %d, want 1000", tip)
	}

	wantOrder := []string{"primary", "fallback-1", "fallback-2"}
	if len(order) != len(wantOrder) {
		t.Fatalf("call order = %v, want %v", order, wantOrder)
	}
	for i := range wantOrder {
		if order[i] != wantOrder[i] {
			t.Errorf("order[%d] = %q, want %q", i, order[i], wantOrder[i])
		}
	}
}

func TestBackfillRunner_14DayGapUsesArchiveProvider(t *testing.T) {
	const (
		fromLedger = uint64(55_000_000)
		// 14 days = 241,920 ledgers at 5s/ledger.
		toLedger   = fromLedger + FourteenDayRetentionLedgers - 1
		chainHead  = toLedger + 500 // beyond backfillSafetyMargin (100)
		ledgersDay = uint64(17_280)
	)

	// Seed 14 daily events spanning the full 14-day (241,920-ledger) gap.
	var events []mockRPCEvent
	for day := uint64(0); day < 14; day++ {
		events = append(events, mockRPCEvent{
			ID:         fmt.Sprintf("ev-day-%02d", day+1),
			ContractID: "CVAULT_MAIN",
			Ledger:     fromLedger + day*ledgersDay + 42,
			EventType:  "pnlty_chg",
			Data:       map[string]any{"user": "GUSER", "amount": "100"},
		})
	}

	var primaryCalls, archiveCalls atomic.Int64
	// Primary only retains 7 days (120,960 ledgers) and rejects days 1..7.
	primary7d := newRetentionAwareMockRPCServer(t, events, chainHead, DefaultHotRetentionLedgers, &primaryCalls)
	defer primary7d.Close()

	// Archive retains 30 days (518,400 ledgers) and serves the entire 14-day window.
	archive30d := newRetentionAwareMockRPCServer(t, events, chainHead, ThirtyDayRetentionLedgers, &archiveCalls)
	defer archive30d.Close()

	repo := newFakeBackfillRepo()
	runner := &Runner{
		Repo:          repo,
		Client:        &http.Client{Timeout: 5 * time.Second},
		RPCURL:        primary7d.URL,
		ArchiveRPCURL: archive30d.URL,
		Throttle:      time.Millisecond,
	}

	run, err := runner.Start(context.Background(), StartInput{
		FromLedger:  fromLedger,
		ToLedger:    toLedger,
		ContractIDs: []string{"CVAULT_MAIN"},
		DryRun:      true,
		InitiatedBy: "ops-14d-drill",
	})
	if err != nil {
		t.Fatalf("14-day backfill failed: %v", err)
	}
	if run.EventsProcessed != 14 {
		t.Errorf("EventsProcessed = %d, want 14 (one event per day across 14 days)", run.EventsProcessed)
	}
	if run.LastLedgerDone == nil || *run.LastLedgerDone != toLedger {
		t.Errorf("LastLedgerDone = %v, want %d", run.LastLedgerDone, toLedger)
	}
	if archiveCalls.Load() == 0 {
		t.Errorf("expected archive RPC to be queried for 14-day gap")
	}
}
