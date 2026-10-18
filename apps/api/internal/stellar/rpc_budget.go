package stellar

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrRPCBudgetExceeded is returned when an outbound RPC call is shed because
// the provider's token bucket has reached the priority class's reserve ceiling (#1560).
var ErrRPCBudgetExceeded = errors.New("soroban rpc provider rate-limit budget exceeded for priority class")

// RPCPriority classifies outbound Soroban RPC traffic so critical background
// loops (event indexer, keeper/reconciler) are never starved by user-triggered
// transaction simulations (#1560).
type RPCPriority int

const (
	// PriorityUnspecified falls back to the client's default priority or
	// method-based classification.
	PriorityUnspecified RPCPriority = iota
	// PriorityUserSimulation is the lowest priority class (user-triggered
	// simulateTransaction and ad-hoc preview reads). Shed first when the
	// provider bucket nears exhaustion.
	PriorityUserSimulation
	// PriorityKeeper is the medium priority class (keeper, harvest, rebalancer,
	// and transaction submission reconciliation).
	PriorityKeeper
	// PriorityIndexer is the highest priority class (event indexer and ledger
	// freshness polling). Reserved headroom ensures it keeps running even when
	// user simulations saturate the provider budget.
	PriorityIndexer
)

func (p RPCPriority) String() string {
	switch p {
	case PriorityUserSimulation:
		return "user_simulation"
	case PriorityKeeper:
		return "keeper"
	case PriorityIndexer:
		return "indexer"
	default:
		return "user_simulation"
	}
}

type rpcPriorityCtxKey struct{}

// WithRPCPriority attaches an explicit RPCPriority to ctx.
func WithRPCPriority(ctx context.Context, p RPCPriority) context.Context {
	return context.WithValue(ctx, rpcPriorityCtxKey{}, p)
}

// RPCPriorityFromContext extracts the RPCPriority from ctx, or returns
// PriorityUnspecified if none was set.
func RPCPriorityFromContext(ctx context.Context) RPCPriority {
	if p, ok := ctx.Value(rpcPriorityCtxKey{}).(RPCPriority); ok {
		return p
	}
	return PriorityUnspecified
}

// classifyRPCMethodPriority chooses a sensible default priority when neither
// the context nor the client specifies one explicitly.
func classifyRPCMethodPriority(method string) RPCPriority {
	switch method {
	case "getEvents", "getLatestLedger":
		return PriorityIndexer
	case "sendTransaction", "getTransaction":
		return PriorityKeeper
	default:
		return PriorityUserSimulation
	}
}

// ProviderBudgetConfig configures the token-bucket rate limiter and priority
// reserve thresholds for a single RPC provider (#1560).
type ProviderBudgetConfig struct {
	Provider string
	// RatePerSecond is the token refill rate per second.
	RatePerSecond float64
	// Burst is the maximum token capacity of the bucket.
	Burst int
	// KeeperReserveRatio is the fraction of Burst (e.g. 0.25) reserved for
	// PriorityKeeper and PriorityIndexer. When available tokens after a call
	// would drop below Burst*KeeperReserveRatio, PriorityUserSimulation calls
	// are shed first.
	KeeperReserveRatio float64
	// IndexerReserveRatio is the fraction of Burst (e.g. 0.10) reserved
	// exclusively for PriorityIndexer. When available tokens after a call
	// would drop below Burst*IndexerReserveRatio, PriorityKeeper calls are
	// also shed so the event indexer never misses ledgers.
	IndexerReserveRatio float64
	// Now is an optional clock for deterministic tests.
	Now func() time.Time
}

// DefaultProviderBudgetConfig returns a production-safe default token-bucket
// budget for a Soroban RPC provider.
func DefaultProviderBudgetConfig(provider string) ProviderBudgetConfig {
	return ProviderBudgetConfig{
		Provider:            provider,
		RatePerSecond:       50,
		Burst:               100,
		KeeperReserveRatio:  0.20,
		IndexerReserveRatio: 0.10,
	}
}

// ProviderBudgetSnapshot captures the instantaneous token-bucket state and
// cumulative counters for Prometheus exposition (#1560).
type ProviderBudgetSnapshot struct {
	Provider         string
	UtilizationRatio float64
	AvailableTokens  float64
	Burst            int
	AllowedTotal     uint64
	ShedByPriority   map[RPCPriority]uint64
}

// ProviderBudgetLimiter is a thread-safe, priority-aware token-bucket rate
// limiter for a single Soroban RPC provider (#1560).
type ProviderBudgetLimiter struct {
	mu                  sync.Mutex
	provider            string
	ratePerSec          float64
	burst               float64
	keeperReserveRatio  float64
	indexerReserveRatio float64
	now                 func() time.Time

	tokens       float64
	lastRefillAt time.Time
	allowedTotal uint64
	shed         map[RPCPriority]uint64
}

// NewProviderBudgetLimiter constructs a priority-aware token-bucket limiter.
func NewProviderBudgetLimiter(cfg ProviderBudgetConfig) *ProviderBudgetLimiter {
	if cfg.RatePerSecond <= 0 {
		cfg.RatePerSecond = 50
	}
	if cfg.Burst <= 0 {
		cfg.Burst = 100
	}
	if cfg.KeeperReserveRatio <= 0 || cfg.KeeperReserveRatio >= 1 {
		cfg.KeeperReserveRatio = 0.20
	}
	if cfg.IndexerReserveRatio <= 0 || cfg.IndexerReserveRatio >= cfg.KeeperReserveRatio {
		cfg.IndexerReserveRatio = cfg.KeeperReserveRatio / 2
	}
	nowFn := cfg.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	now := nowFn()
	return &ProviderBudgetLimiter{
		provider:            cfg.Provider,
		ratePerSec:          cfg.RatePerSecond,
		burst:               float64(cfg.Burst),
		keeperReserveRatio:  cfg.KeeperReserveRatio,
		indexerReserveRatio: cfg.IndexerReserveRatio,
		now:                 nowFn,
		tokens:              float64(cfg.Burst),
		lastRefillAt:        now,
		shed: map[RPCPriority]uint64{
			PriorityUserSimulation: 0,
			PriorityKeeper:         0,
			PriorityIndexer:        0,
		},
	}
}

// Allow consumes 1 token if the bucket has sufficient capacity above the
// reserve floor for priority, or sheds the call with ErrRPCBudgetExceeded.
func (l *ProviderBudgetLimiter) Allow(priority RPCPriority) error {
	if l == nil {
		return nil
	}
	if priority == PriorityUnspecified {
		priority = PriorityUserSimulation
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	l.refillLocked(l.now())

	minRemaining := l.reserveFloorLocked(priority)
	if l.tokens-1.0 < minRemaining {
		l.shed[priority]++
		return ErrRPCBudgetExceeded
	}

	l.tokens -= 1.0
	l.allowedTotal++
	return nil
}

func (l *ProviderBudgetLimiter) reserveFloorLocked(priority RPCPriority) float64 {
	switch priority {
	case PriorityIndexer:
		return 0
	case PriorityKeeper:
		return l.burst * l.indexerReserveRatio
	default:
		return l.burst * l.keeperReserveRatio
	}
}

func (l *ProviderBudgetLimiter) refillLocked(now time.Time) {
	if elapsed := now.Sub(l.lastRefillAt); elapsed > 0 {
		l.tokens += elapsed.Seconds() * l.ratePerSec
		if l.tokens > l.burst {
			l.tokens = l.burst
		}
		l.lastRefillAt = now
	}
}

// Snapshot returns the current budget utilization and counters.
func (l *ProviderBudgetLimiter) Snapshot() ProviderBudgetSnapshot {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.refillLocked(l.now())

	utilization := 0.0
	if l.burst > 0 {
		utilization = 1.0 - (l.tokens / l.burst)
		if utilization < 0 {
			utilization = 0
		}
		if utilization > 1 {
			utilization = 1
		}
	}

	shedCopy := make(map[RPCPriority]uint64, len(l.shed))
	for k, v := range l.shed {
		shedCopy[k] = v
	}

	return ProviderBudgetSnapshot{
		Provider:         l.provider,
		UtilizationRatio: utilization,
		AvailableTokens:  l.tokens,
		Burst:            int(l.burst),
		AllowedTotal:     l.allowedTotal,
		ShedByPriority:   shedCopy,
	}
}
