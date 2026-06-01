package performance

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	perfdom "github.com/suncrestlabs/nester/apps/api/internal/domain/performance"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

// YieldRegistryReader fetches on-chain APY (basis points) for a protocol.
type YieldRegistryReader interface {
	SourceAPYBPS(ctx context.Context, protocolID string) (uint32, error)
}

// APYBroadcaster fires when vault APY moves beyond the configured threshold.
type APYBroadcaster func(vaultID uuid.UUID, previousBPS, currentBPS uint32)

// APYRefresherConfig controls the yield_registry polling loop.
type APYRefresherConfig struct {
	Interval              time.Duration
	BroadcastThresholdBPS int
	RegistryAddress       string
}

// APYRefresher polls yield_registry, writes performance snapshots, and
// broadcasts when APY change exceeds the threshold.
type APYRefresher struct {
	cfg       APYRefresherConfig
	repo      perfdom.SnapshotRepository
	vaults    VaultLister
	registry  YieldRegistryReader
	broadcast APYBroadcaster
	logger    *slog.Logger
	clock     func() time.Time

	mu         sync.RWMutex
	cachedAPYB map[uuid.UUID]uint32
}

func NewAPYRefresher(
	cfg APYRefresherConfig,
	repo perfdom.SnapshotRepository,
	vaults VaultLister,
	registry YieldRegistryReader,
	broadcast APYBroadcaster,
) *APYRefresher {
	if broadcast == nil {
		broadcast = func(uuid.UUID, uint32, uint32) {}
	}
	return &APYRefresher{
		cfg:        cfg,
		repo:       repo,
		vaults:     vaults,
		registry:   registry,
		broadcast:  broadcast,
		logger:     slog.Default(),
		clock:      func() time.Time { return time.Now().UTC() },
		cachedAPYB: make(map[uuid.UUID]uint32),
	}
}

func (r *APYRefresher) WithLogger(logger *slog.Logger) *APYRefresher {
	r.logger = logger
	return r
}

func (r *APYRefresher) SetClock(clock func() time.Time) {
	r.clock = clock
}

func (r *APYRefresher) CachedAPYBPS(vaultID uuid.UUID) (uint32, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.cachedAPYB[vaultID]
	return v, ok
}

// Run blocks until ctx is cancelled.
func (r *APYRefresher) Run(ctx context.Context) error {
	if r.cfg.Interval <= 0 {
		return errors.New("apy refresher: interval must be positive")
	}
	if r.cfg.RegistryAddress == "" || r.registry == nil {
		r.logger.Info("apy refresher disabled: yield registry not configured")
		return nil
	}

	ticker := time.NewTicker(r.cfg.Interval)
	defer ticker.Stop()

	if err := r.RefreshOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
		r.logger.Error("apy refresher: initial tick failed", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := r.RefreshOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
				r.logger.Error("apy refresher: tick failed", "error", err)
			}
		}
	}
}

// RefreshOnce fetches on-chain APYs, writes snapshots, and broadcasts changes.
func (r *APYRefresher) RefreshOnce(ctx context.Context) error {
	vaults, err := r.vaults.ListActive(ctx)
	if err != nil {
		return fmt.Errorf("list active vaults: %w", err)
	}

	now := r.clock()
	var firstErr error
	for _, v := range vaults {
		if err := r.refreshVault(ctx, v, now); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			r.logger.Error("apy refresher: vault failed", "vault_id", v.ID, "error", err)
		}
	}
	return firstErr
}

func (r *APYRefresher) refreshVault(ctx context.Context, v vault.Vault, now time.Time) error {
	if len(v.Allocations) == 0 {
		return nil
	}

	protocolAPY := make(map[string]uint32, len(v.Allocations))
	for _, alloc := range v.Allocations {
		if _, ok := protocolAPY[alloc.Protocol]; ok {
			continue
		}
		bps, err := r.registry.SourceAPYBPS(ctx, alloc.Protocol)
		if err != nil {
			return fmt.Errorf("fetch apy for %s: %w", alloc.Protocol, err)
		}
		protocolAPY[alloc.Protocol] = bps
	}

	weightedBPS, breakdown, err := weightedVaultAPY(v, protocolAPY)
	if err != nil {
		return err
	}

	balance := v.CurrentBalance
	deposited := v.TotalDeposited
	yieldEarned := balance.Sub(deposited)
	sharePrice := decimal.NewFromInt(1)
	if !deposited.IsZero() && deposited.Sign() > 0 {
		sharePrice = balance.Div(deposited).Round(8)
	}

	if _, err := r.repo.Insert(ctx, perfdom.Snapshot{
		VaultID:             v.ID,
		TotalBalance:        balance,
		TotalDeposited:      deposited,
		TotalYieldEarned:    yieldEarned,
		SharePrice:          sharePrice,
		SnapshotAt:          now,
		AllocationBreakdown: breakdown,
	}); err != nil {
		return fmt.Errorf("insert snapshot: %w", err)
	}

	r.mu.RLock()
	prev, hadPrev := r.cachedAPYB[v.ID]
	r.mu.RUnlock()

	r.mu.Lock()
	r.cachedAPYB[v.ID] = weightedBPS
	r.mu.Unlock()

	if hadPrev && int(absDiffBPS(prev, weightedBPS)) >= r.cfg.BroadcastThresholdBPS {
		r.broadcast(v.ID, prev, weightedBPS)
	}

	// Persist a 7d APY row derived from the weighted on-chain rate.
	apyPct := decimal.NewFromInt(int64(weightedBPS)).Div(decimal.NewFromInt(100))
	return r.repo.UpsertAPY(ctx, perfdom.APYRecord{
		VaultID:      v.ID,
		Period:       perfdom.Period7d,
		RealizedAPY:  apyPct,
		CalculatedAt: now,
	})
}

func weightedVaultAPY(v vault.Vault, protocolAPY map[string]uint32) (uint32, []perfdom.AllocationBreakdownEntry, error) {
	var totalWeight decimal.Decimal
	var weightedSum decimal.Decimal
	breakdown := make([]perfdom.AllocationBreakdownEntry, 0, len(v.Allocations))

	for _, alloc := range v.Allocations {
		bps, ok := protocolAPY[alloc.Protocol]
		if !ok {
			return 0, nil, fmt.Errorf("missing apy for protocol %s", alloc.Protocol)
		}
		apyDec := decimal.NewFromInt(int64(bps)).Div(decimal.NewFromInt(100))
		breakdown = append(breakdown, perfdom.AllocationBreakdownEntry{
			Source: alloc.Protocol,
			Amount: alloc.Amount,
			APY:    apyDec,
		})
		totalWeight = totalWeight.Add(alloc.Amount)
		weightedSum = weightedSum.Add(alloc.Amount.Mul(apyDec))
	}

	if totalWeight.IsZero() {
		return 0, breakdown, nil
	}
	avgAPY := weightedSum.Div(totalWeight)
	// Convert percent back to bps for threshold comparison.
	bps := uint32(avgAPY.Mul(decimal.NewFromInt(100)).Round(0).IntPart())
	return bps, breakdown, nil
}

func absDiffBPS(a, b uint32) uint32 {
	if a >= b {
		return a - b
	}
	return b - a
}

// RegistryReader adapts stellar.ContractReader to YieldRegistryReader.
type RegistryReader struct {
	Reader  interface {
		SourceAPYBPS(ctx context.Context, registryAddress, protocolID string) (uint32, error)
	}
	Address string
}

func (r *RegistryReader) SourceAPYBPS(ctx context.Context, protocolID string) (uint32, error) {
	return r.Reader.SourceAPYBPS(ctx, r.Address, protocolID)
}
