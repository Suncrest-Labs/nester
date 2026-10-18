package stellar

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestProviderBudgetLimiter_ShedsLowestPriorityFirst(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	limiter := NewProviderBudgetLimiter(ProviderBudgetConfig{
		Provider:            "soroban_rpc",
		RatePerSecond:       10,
		Burst:               10,
		KeeperReserveRatio:  0.30, // reserve 3 tokens for keeper + indexer
		IndexerReserveRatio: 0.10, // reserve 1 token exclusively for indexer
		Now:                 func() time.Time { return now },
	})

	// 1. First 7 user simulations succeed (tokens drop from 10 -> 3, utilization = 0.70).
	for i := 0; i < 7; i++ {
		if err := limiter.Allow(PriorityUserSimulation); err != nil {
			t.Fatalf("simulation %d unexpectedly rejected: %v", i, err)
		}
	}

	// 2. 8th user simulation is shed because remaining tokens (2) would drop below KeeperReserve (3).
	if err := limiter.Allow(PriorityUserSimulation); !errors.Is(err, ErrRPCBudgetExceeded) {
		t.Fatalf("expected PriorityUserSimulation to be shed at 70%% utilization, got %v", err)
	}

	// 3. PriorityKeeper is still admitted (consumes tokens 3 -> 2 -> 1).
	for i := 0; i < 2; i++ {
		if err := limiter.Allow(PriorityKeeper); err != nil {
			t.Fatalf("keeper call %d should succeed while above IndexerReserve: %v", i, err)
		}
	}

	// 4. Next PriorityKeeper call is shed because remaining tokens (0) would drop below IndexerReserve (1).
	if err := limiter.Allow(PriorityKeeper); !errors.Is(err, ErrRPCBudgetExceeded) {
		t.Fatalf("expected PriorityKeeper to be shed at 90%% utilization, got %v", err)
	}

	// 5. PriorityIndexer is still admitted using the final reserved token (1 -> 0).
	if err := limiter.Allow(PriorityIndexer); err != nil {
		t.Fatalf("indexer call should succeed using reserved token: %v", err)
	}

	snap := limiter.Snapshot()
	if snap.UtilizationRatio != 1.0 {
		t.Fatalf("expected utilization ratio 1.0, got %v", snap.UtilizationRatio)
	}
	if snap.ShedByPriority[PriorityUserSimulation] != 1 {
		t.Fatalf("expected 1 shed simulation, got %d", snap.ShedByPriority[PriorityUserSimulation])
	}
	if snap.ShedByPriority[PriorityKeeper] != 1 {
		t.Fatalf("expected 1 shed keeper call, got %d", snap.ShedByPriority[PriorityKeeper])
	}
}

func TestProviderBudgetLimiter_SimulationSaturationDoesNotStarveIndexer(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	var clockMu sync.Mutex
	advanceClock := func(d time.Duration) {
		clockMu.Lock()
		now = now.Add(d)
		clockMu.Unlock()
	}
	readClock := func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		return now
	}

	limiter := NewProviderBudgetLimiter(ProviderBudgetConfig{
		Provider:            "soroban_rpc_primary",
		RatePerSecond:       20,
		Burst:               20,
		KeeperReserveRatio:  0.25, // 5 tokens reserved for keeper+indexer
		IndexerReserveRatio: 0.15, // 3 tokens reserved exclusively for indexer
		Now:                 readClock,
	})

	server := newRPCServer(t, func(method string, count int, w http.ResponseWriter) {
		switch method {
		case "getEvents":
			writeRPCResult(w, map[string]any{
				"latestLedger": 1000 + count,
				"events":       []map[string]any{{"id": count}},
			})
		case "simulateTransaction":
			writeRPCResult(w, map[string]any{"cost": "100"})
		default:
			writeRPCResult(w, map[string]any{"ok": true})
		}
	})

	simOpts := noSleepOptions(nil)
	simOpts.Budget = limiter
	simOpts.DefaultPriority = PriorityUserSimulation
	simClient := newRPCClient(server.server.URL, server.server.Client(), simOpts, false)

	idxOpts := noSleepOptions(nil)
	idxOpts.Budget = limiter
	idxOpts.DefaultPriority = PriorityIndexer
	idxClient := newRPCClient(server.server.URL, server.server.Client(), idxOpts, false)

	const rounds = 25
	var shedSimulations atomic.Int64
	var succeededIndexerPolls int

	for r := 0; r < rounds; r++ {
		// Saturate the provider with 40 concurrent user simulations per tick.
		var wg sync.WaitGroup
		wg.Add(40)
		for i := 0; i < 40; i++ {
			go func() {
				defer wg.Done()
				var out map[string]any
				err := simClient.call(context.Background(), "simulateTransaction", map[string]any{"tx": "AAAA"}, &out)
				if errors.Is(err, ErrRPCBudgetExceeded) {
					shedSimulations.Add(1)
				}
			}()
		}
		wg.Wait()

		// Despite complete saturation by user simulations, the indexer must
		// succeed on every single poll without missing any events.
		var evOut struct {
			Result struct {
				LatestLedger int              `json:"latestLedger"`
				Events       []map[string]any `json:"events"`
			} `json:"result"`
		}
		if err := idxClient.call(context.Background(), "getEvents", map[string]any{"startLedger": 1000 + r}, &evOut); err != nil {
			t.Fatalf("round %d: indexer getEvents failed under simulation saturation: %v", r, err)
		}
		if len(evOut.Result.Events) != 1 {
			t.Fatalf("round %d: indexer missed events: %+v", r, evOut)
		}
		succeededIndexerPolls++

		// Advance clock by 200ms (refills 4 tokens per tick).
		advanceClock(200 * time.Millisecond)
	}

	if succeededIndexerPolls != rounds {
		t.Fatalf("indexer completed %d/%d polls", succeededIndexerPolls, rounds)
	}
	if shedSimulations.Load() == 0 {
		t.Fatal("expected user simulations to be shed during saturation")
	}
}
