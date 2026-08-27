package performance

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
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
	resolver  *RebalanceAPYResolver
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

// WithResolver installs the precedence-aware APY resolver. With it, a source
// whose on-chain APY is unknown or stale is omitted from the weighted average
// instead of being counted as zero, and DeFiLlama is never consulted for a
// rebalancing input. See apy_precedence.go for the full rule.
func (r *APYRefresher) WithResolver(resolver *RebalanceAPYResolver) *APYRefresher {
	r.resolver = resolver
	return r
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

	// On-chain adapter APY is authoritative — see apy_precedence.go. When a
	// resolver is wired, a source whose APY is unknown or stale is omitted
	// rather than counted as zero; weightedVaultAPY then weights only the
	// sources with a usable rate.
	protocolAPY := make(map[string]uint32, len(v.Allocations))
	for _, alloc := range v.Allocations {
		if _, ok := protocolAPY[alloc.Protocol]; ok {
			continue
		}
		if r.resolver != nil {
			bps, ok := r.resolver.APYForRebalance(ctx, alloc.Protocol)
			if !ok {
				r.logger.Info("apy refresher: on-chain apy unusable, source omitted",
					"protocol", alloc.Protocol, "vault_id", v.ID)
				continue
			}
			protocolAPY[alloc.Protocol] = bps
			continue
		}
		bps, err := r.registry.SourceAPYBPS(ctx, alloc.Protocol)
		if err != nil {
			return fmt.Errorf("fetch apy for %s: %w", alloc.Protocol, err)
		}
		protocolAPY[alloc.Protocol] = bps
	}

	// In resolver mode an omitted protocol is an expected state (unknown or
	// stale on-chain APY), not a missing-data error, so the weighted average is
	// taken over the usable allocations only.
	weightedBPS, breakdown, err := weightedVaultAPYSkippingUnknown(v, protocolAPY, r.resolver != nil)
	if err != nil {
		return err
	}

	// With no usable rate anywhere, persisting or broadcasting would publish a
	// synthetic 0% APY that is really "we don't know". Hold instead.
	if r.resolver != nil && len(protocolAPY) == 0 {
		r.logger.Info("apy refresher: no usable on-chain apy for vault, skipping snapshot",
			"vault_id", v.ID)
		return nil
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
	return weightedVaultAPYSkippingUnknown(v, protocolAPY, false)
}

// weightedVaultAPYSkippingUnknown computes the allocation-weighted APY.
//
// When skipUnknown is set (resolver mode), an allocation with no entry in
// protocolAPY is excluded from both the weighted average and the breakdown
// rather than treated as an error: the on-chain rate being unknown or stale is
// a normal state, and excluding it is what keeps an unknown from being averaged
// in as a zero. Without it, a missing entry is a data error as before.
func weightedVaultAPYSkippingUnknown(
	v vault.Vault,
	protocolAPY map[string]uint32,
	skipUnknown bool,
) (uint32, []perfdom.AllocationBreakdownEntry, error) {
	var totalWeight decimal.Decimal
	var weightedSum decimal.Decimal
	breakdown := make([]perfdom.AllocationBreakdownEntry, 0, len(v.Allocations))

	for _, alloc := range v.Allocations {
		bps, ok := protocolAPY[alloc.Protocol]
		if !ok {
			if skipUnknown {
				continue
			}
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
	//
	// The intermediate is int64 and is clamped before narrowing to uint32. A
	// negative APY is possible in principle (a yield source can lose value),
	// and converting it directly would wrap into a very large positive bps that
	// the deviation threshold would then read as an enormous yield increase
	// (nester#1035, G115). A value above uint32 max is equally nonsensical and
	// is clamped for the same reason.
	raw := avgAPY.Mul(decimal.NewFromInt(100)).Round(0).IntPart()
	switch {
	case raw < 0:
		raw = 0
	case raw > math.MaxUint32:
		raw = math.MaxUint32
	}
	bps := uint32(raw) // #nosec G115 -- clamped to [0, MaxUint32] immediately above
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
	Reader interface {
		SourceAPYBPS(ctx context.Context, registryAddress, protocolID string) (uint32, error)
	}
	Address string
}

func (r *RegistryReader) SourceAPYBPS(ctx context.Context, protocolID string) (uint32, error) {
	return r.Reader.SourceAPYBPS(ctx, r.Address, protocolID)
}
