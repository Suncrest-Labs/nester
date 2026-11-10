package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/savings"
)

type countingSavingsChain struct {
	states   atomic.Int32
	converts atomic.Int32
	state    savings.ChainState
	failing  atomic.Bool
	gate     chan struct{}
}

func (c *countingSavingsChain) State(context.Context) (savings.ChainState, error) {
	c.states.Add(1)
	if c.gate != nil {
		<-c.gate
	}
	if c.failing.Load() {
		return savings.ChainState{}, errors.New("rpc down")
	}
	return c.state, nil
}

func (c *countingSavingsChain) Balance(context.Context, string) (decimal.Decimal, error) {
	return decimal.Zero, nil
}

func (c *countingSavingsChain) ConvertToAssets(_ context.Context, shares decimal.Decimal) (decimal.Decimal, error) {
	c.converts.Add(1)
	return shares, nil
}

func cachedSvc(chain SavingsChain, now *time.Time) *SavingsService {
	svc := NewSavingsService(chain, &fakeSavingsRepo{})
	svc.clock = func() time.Time { return *now }
	return svc
}

func TestSavingsService_Summary_CachesChainStateWithinTTL(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	chain := &countingSavingsChain{state: sampleChainState()}
	svc := cachedSvc(chain, &now)

	for i := 0; i < 2; i++ {
		if _, err := svc.Summary(context.Background()); err != nil {
			t.Fatalf("summary: %v", err)
		}
	}
	if got := chain.states.Load(); got != 1 {
		t.Fatalf("State calls within TTL = %d, want 1", got)
	}
	if got := chain.converts.Load(); got != 1 {
		t.Fatalf("ConvertToAssets calls within TTL = %d, want 1", got)
	}

	now = now.Add(savingsSummaryTTL + time.Second)
	if _, err := svc.Summary(context.Background()); err != nil {
		t.Fatalf("summary: %v", err)
	}
	if got := chain.states.Load(); got != 2 {
		t.Fatalf("State calls after TTL = %d, want 2", got)
	}
}

func TestSavingsService_Summary_ErrorsAreNotCached(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	chain := &countingSavingsChain{state: sampleChainState()}
	chain.failing.Store(true)
	svc := cachedSvc(chain, &now)

	if _, err := svc.Summary(context.Background()); err == nil {
		t.Fatal("want error")
	}
	chain.failing.Store(false)
	if _, err := svc.Summary(context.Background()); err != nil {
		t.Fatalf("second summary should recover: %v", err)
	}
	if got := chain.states.Load(); got != 2 {
		t.Fatalf("State calls = %d, want 2 (error must not be cached)", got)
	}
}

func TestSavingsService_Summary_ConcurrentCallsCoalesce(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	chain := &countingSavingsChain{state: sampleChainState(), gate: make(chan struct{})}
	svc := cachedSvc(chain, &now)

	const n = 8
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := svc.Summary(context.Background()); err != nil {
				t.Errorf("summary: %v", err)
			}
		}()
	}
	deadline := time.After(2 * time.Second)
	for chain.states.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("State never called")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	time.Sleep(50 * time.Millisecond)
	close(chain.gate)
	wg.Wait()
	if got := chain.states.Load(); got != 1 {
		t.Fatalf("State calls for %d concurrent summaries = %d, want 1", n, got)
	}
}
