// Package performance owns the snapshot worker, APY calculator, and the
// read-side query layer that backs the /vaults/{id}/performance endpoints.
package performance

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	perfdom "github.com/suncrestlabs/nester/apps/api/internal/domain/performance"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/analytics"
)

// VaultLister is the subset of the vault repo we need. Defined locally so
// tests can stub it without dragging the full vault repository surface in.
type VaultLister interface {
	ListActive(ctx context.Context) ([]vault.Vault, error)
}

// Service is the read-side façade used by the HTTP handler.
type Service struct {
	repo      perfdom.SnapshotRepository
	vaultRepo vault.Repository
	clock     func() time.Time
}

func NewService(repo perfdom.SnapshotRepository, vaultRepo vault.Repository) *Service {
	return &Service{repo: repo, vaultRepo: vaultRepo, clock: func() time.Time { return time.Now().UTC() }}
}

// SetClock lets tests inject deterministic time. Production stays on UTC.
func (s *Service) SetClock(clock func() time.Time) {
	s.clock = clock
}

// Summary returns the headline performance view for a vault. Falls back to
// zero values (and an empty APY map) when no snapshots exist yet — never
// errors on a missing snapshot since brand-new vaults are normal.
func (s *Service) Summary(ctx context.Context, vaultID uuid.UUID) (perfdom.PerformanceSummary, error) {
	latest, err := s.repo.LatestForVault(ctx, vaultID)
	if err != nil && !errors.Is(err, perfdom.ErrSnapshotNotFound) {
		return perfdom.PerformanceSummary{}, err
	}

	apyRecords, err := s.repo.ListAPY(ctx, vaultID)
	if err != nil {
		return perfdom.PerformanceSummary{}, err
	}

	apyMap := make(map[perfdom.Period]float64, len(perfdom.AllAPYPeriods))
	for _, rec := range apyRecords {
		v, _ := rec.RealizedAPY.Float64()
		apyMap[rec.Period] = v
	}

	out := perfdom.PerformanceSummary{
		VaultID: vaultID,
		APY:     apyMap,
	}

	if errors.Is(err, perfdom.ErrSnapshotNotFound) || latest.ID == uuid.Nil {
		// No snapshot yet — return shape with zero values.
		out.CurrentBalance = decimal.Zero
		out.TotalDeposited = decimal.Zero
		out.TotalYieldEarned = decimal.Zero
		out.SharePrice = decimal.NewFromInt(1)
		return out, nil
	}

	out.CurrentBalance = latest.TotalBalance
	out.TotalDeposited = latest.TotalDeposited
	out.TotalYieldEarned = latest.TotalYieldEarned
	out.SharePrice = latest.SharePrice
	t := latest.SnapshotAt
	out.LastSnapshotAt = &t
	return out, nil
}

// History returns all snapshots inside the requested window for charting.
// `since` is computed by the handler from the `period` query param.
func (s *Service) History(ctx context.Context, vaultID uuid.UUID, since time.Time) ([]perfdom.Snapshot, error) {
	return s.repo.HistoryForVault(ctx, vaultID, since)
}

// APY returns the latest realized APY for every tracked window.
func (s *Service) APY(ctx context.Context, vaultID uuid.UUID) (map[perfdom.Period]float64, error) {
	records, err := s.repo.ListAPY(ctx, vaultID)
	if err != nil {
		return nil, err
	}
	out := make(map[perfdom.Period]float64, len(perfdom.AllAPYPeriods))
	for _, rec := range records {
		v, _ := rec.RealizedAPY.Float64()
		out[rec.Period] = v
	}
	return out, nil
}

// GetAPYHistory returns bucketed APY data points plus summary stats for the
// requested period and interval.
func (s *Service) GetAPYHistory(
	ctx context.Context,
	vaultID uuid.UUID,
	period string, // "7d" | "30d" | "90d" | "1y"
	interval string, // "daily" | "weekly"
) (perfdom.APYHistoryResponse, error) {
	periodDays := map[string]int{
		"7d":  7,
		"30d": 30,
		"90d": 90,
		"1y":  365,
	}

	days, ok := periodDays[period]
	if !ok {
		days = 30
	}

	since := s.clock().Add(-time.Duration(days) * 24 * time.Hour)

	dataPoints, err := s.repo.APYHistoryForVault(ctx, vaultID, since, interval)
	if err != nil {
		return perfdom.APYHistoryResponse{}, err
	}

	minAPY, maxAPY, avgAPY, err := s.repo.APYStatsForVault(ctx, vaultID, since)
	if err != nil {
		return perfdom.APYHistoryResponse{}, err
	}

	// Current APY comes from the latest snapshot-derived value
	latest, err := s.repo.LatestForVault(ctx, vaultID)
	var currentAPY string
	if err == nil && !latest.TotalDeposited.IsZero() {
		pct := latest.TotalBalance.Sub(latest.TotalDeposited).
			Div(latest.TotalDeposited).
			Mul(decimal.NewFromInt(100))
		currentAPY = pct.StringFixed(2)
	} else {
		currentAPY = "0.00"
	}

	// dataComplete is true when we have at least as many points as expected
	expectedPoints := days
	if interval == "weekly" {
		expectedPoints = (days + 6) / 7
	}
	dataComplete := len(dataPoints) >= expectedPoints

	if dataPoints == nil {
		dataPoints = []perfdom.APYDataPoint{}
	}

	return perfdom.APYHistoryResponse{
		VaultID:      vaultID.String(),
		Period:       period,
		Interval:     interval,
		CurrentAPY:   currentAPY,
		AvgAPY:       avgAPY.StringFixed(2),
		MinAPY:       minAPY.StringFixed(2),
		MaxAPY:       maxAPY.StringFixed(2),
		DataComplete: dataComplete,
		DataPoints:   dataPoints,
	}, nil
}


// CalculateRealizedAPY annualizes the return between two snapshots.
//
//	APY = ((current_balance / total_deposited) ^ (365 / days_elapsed) - 1) * 100
//
// Returns 0 when there isn't enough data to annualize (zero deposit, zero
// elapsed time, or non-positive ratio). Bounded to [-1000, 10000] to defend
// against runaway values from tiny denominators.
func CalculateRealizedAPY(currentBalance, totalDeposited decimal.Decimal, daysElapsed float64) decimal.Decimal {
	if daysElapsed <= 0 || totalDeposited.IsZero() || totalDeposited.Sign() <= 0 {
		return decimal.Zero
	}
	current, _ := currentBalance.Float64()
	deposited, _ := totalDeposited.Float64()
	if deposited <= 0 {
		return decimal.Zero
	}
	ratio := current / deposited
	if ratio <= 0 {
		return decimal.Zero
	}
	apy := (math.Pow(ratio, 365.0/daysElapsed) - 1) * 100
	if math.IsNaN(apy) {
		return decimal.Zero
	}
	if math.IsInf(apy, 1) {
		return decimal.NewFromFloat(10000)
	}
	if math.IsInf(apy, -1) {
		return decimal.NewFromFloat(-1000)
	}
	if apy > 10000 {
		apy = 10000
	}
	if apy < -1000 {
		apy = -1000
	}
	return decimal.NewFromFloat(apy).Round(4)
}

// BalanceProvider abstracts the source of an on-chain balance read. The
// production implementation hits Stellar via internal/stellar; tests stub it.
type BalanceProvider interface {
	VaultBalance(ctx context.Context, contractAddress string) (decimal.Decimal, error)
}

// GetUserAnalytics returns aggregated analytics data for a user's vaults
func (s *Service) GetUserAnalytics(ctx context.Context, userID uuid.UUID, fromTime, toTime time.Time) (*analytics.AnalyticsResponse, error) {
	// Get user's vaults
	userVaults, _, err := s.vaultRepo.ListUserVaults(ctx, userID, vault.UserListFilter{Page: 1, PerPage: 1000})
	if err != nil {
		return &analytics.AnalyticsResponse{}, fmt.Errorf("failed to get user vaults: %w", err)
	}

	if len(userVaults) == 0 {
	// Return empty response if user has no vaults
	return &analytics.AnalyticsResponse{
		DailySnapshots:      []analytics.DailySnapshot{},
		VaultMonthlyYield:   []analytics.VaultMonthlyYield{},
		CurrentAllocation:   []analytics.CurrentAllocation{},
		PerformanceMetrics:  analytics.PerformanceMetrics{},
		Vaults:              []analytics.VaultInfo{},
	}, nil
	}

	// Get daily snapshots for the user (aggregated across all vaults)
	// For now, we'll use the latest snapshot as a placeholder
	// In a real implementation, this would query a user-level analytics table or aggregate vault snapshots
	var dailySnapshots []analytics.DailySnapshot
	if len(userVaults) > 0 {
		// For simplicity, we're generating a simple time series based on the first vault's history
		// In production, this should come from a dedicated analytics table or service
		firstVaultID := userVaults[0].ID
		snapshots, err := s.repo.HistoryForVault(ctx, firstVaultID, fromTime)
		if err != nil && !errors.Is(err, perfdom.ErrSnapshotNotFound) {
			return &analytics.AnalyticsResponse{}, fmt.Errorf("failed to get vault history: %w", err)
		}

		for _, snap := range snapshots {
			dailySnapshots = append(dailySnapshots, analytics.DailySnapshot{
				Date:          snap.SnapshotAt.Format("2006-01-02"),
				TotalBalanceUSD: snap.TotalBalance.InexactFloat64(),
				YieldEarnedUSD:  snap.TotalYieldEarned.InexactFloat64(),
			})
		}
	}

	// Get vault monthly yield (aggregated yield per vault per month)
	var vaultMonthlyYield []analytics.VaultMonthlyYield
	// This would typically come from a separate aggregation table
	// For now, we'll leave it empty and let the frontend handle empty state

	// Get current allocation across all vaults
	var currentAllocation []analytics.CurrentAllocation
	protocolAllocations := make(map[string]decimal.Decimal)
	protocolAPYSums := make(map[string]decimal.Decimal)
	var totalBalance decimal.Decimal

	for _, vault := range userVaults {
		totalBalance = totalBalance.Add(vault.CurrentBalance)
		for _, alloc := range vault.Allocations {
			protocolAllocations[alloc.Protocol] = protocolAllocations[alloc.Protocol].Add(alloc.Amount)
			// Weighted sum of APY * amount for averaging later
			protocolAPYSums[alloc.Protocol] = protocolAPYSums[alloc.Protocol].Add(alloc.Amount.Mul(alloc.APY))
		}
	}

	for protocol, amount := range protocolAllocations {
		if !totalBalance.IsZero() {
			allocationPCT := amount.Div(totalBalance).Mul(decimal.NewFromInt(100)).InexactFloat64()
			var avgAPY float64
			if !amount.IsZero() {
				avgAPY = protocolAPYSums[protocol].Div(amount).InexactFloat64()
			}
			currentAllocation = append(currentAllocation, analytics.CurrentAllocation{
				Protocol:      protocol,
				AllocationPCT: allocationPCT,
				BalanceUSD:    amount.InexactFloat64(),
				APY:           avgAPY,
			})
		}
	}

	// Calculate performance metrics
	var totalYieldEarned decimal.Decimal
	var totalDeposited decimal.Decimal
	var totalWithdrawn decimal.Decimal // This would come from transaction history

	for _, vault := range userVaults {
		totalYieldEarned = totalYieldEarned.Add(vault.YieldEarned)
		totalDeposited = totalDeposited.Add(vault.TotalDeposited)
		// totalWithdrawn would need to be calculated from withdrawal transactions
	}

	netPosition := totalBalance.Sub(totalDeposited)

	// Find best vault (highest APY)
	var bestVaultName string
	var bestVaultAPY float64
	for _, vault := range userVaults {
		// Calculate vault's average APY from allocations
		var vaultTotalAPY decimal.Decimal
		var vaultTotalAmount decimal.Decimal
		for _, alloc := range vault.Allocations {
			vaultTotalAPY = vaultTotalAPY.Add(alloc.Amount.Mul(alloc.APY))
			vaultTotalAmount = vaultTotalAmount.Add(alloc.Amount)
		}
		var vaultAPY float64
		if !vaultTotalAmount.IsZero() {
			vaultAPY = vaultTotalAPY.Div(vaultTotalAmount).InexactFloat64()
		}
		if vaultAPY > bestVaultAPY {
			bestVaultAPY = vaultAPY
			bestVaultName = vault.ContractAddress // Using contract address as name for now
			if bestVaultName == "" {
				bestVaultName = vault.ID.String()
			}
		}
	}

	// Calculate average APY across all vaults
	var averageAPY float64
	if !totalBalance.IsZero() {
		var weightedAPYSum decimal.Decimal
		for _, vault := range userVaults {
			for _, alloc := range vault.Allocations {
				weightedAPYSum = weightedAPYSum.Add(alloc.Amount.Mul(alloc.APY))
			}
		}
		averageAPY = weightedAPYSum.Div(totalBalance).InexactFloat64()
	}

	// Calculate yield change % (placeholder - would compare to previous period)
	var yieldChangePCT float64 = 0.0

	// Build vaults info for comparison table
	var vaultsInfo []analytics.VaultInfo
	for _, vault := range userVaults {
		var lockPeriodDays int
		// Determine lock period from vault metadata or default to 0
		// This would come from vault configuration in a real implementation
		lockPeriodDays = 0 // placeholder

		vaultsInfo = append(vaultsInfo, analytics.VaultInfo{
			ID:            vault.ID.String(),
			Name:          vault.ContractAddress,
			BalanceUSD:    vault.CurrentBalance.InexactFloat64(),
			APY:           0, // placeholder - would calculate from allocations
			YieldEarned:   vault.YieldEarned.InexactFloat64(),
			LockPeriodDays: lockPeriodDays,
		})
	}

	return &analytics.AnalyticsResponse{
		DailySnapshots:      dailySnapshots,
		VaultMonthlyYield:   vaultMonthlyYield,
		CurrentAllocation:   currentAllocation,
		PerformanceMetrics: analytics.PerformanceMetrics{
			TotalYieldEarned: totalYieldEarned.InexactFloat64(),
			YieldChangePCT:   yieldChangePCT,
			BestVaultName:    bestVaultName,
			BestVaultAPY:     bestVaultAPY,
			AverageAPY:       averageAPY,
			TotalDeposited:   totalDeposited.InexactFloat64(),
			TotalWithdrawn:   totalWithdrawn.InexactFloat64(),
			NetPosition:      netPosition.InexactFloat64(),
		},
		Vaults: vaultsInfo,
	}, nil
}

// Tracker is the snapshot-taking background worker.
type Tracker struct {
	repo     perfdom.SnapshotRepository
	vaults   VaultLister
	chain    BalanceProvider
	interval time.Duration
	clock    func() time.Time
	logger   *slog.Logger
}

func NewTracker(
	repo perfdom.SnapshotRepository,
	vaults VaultLister,
	chain BalanceProvider,
	interval time.Duration,
) *Tracker {
	return &Tracker{
		repo:     repo,
		vaults:   vaults,
		chain:    chain,
		interval: interval,
		clock:    func() time.Time { return time.Now().UTC() },
		logger:   slog.Default(),
	}
}

// WithLogger sets a custom logger on the tracker (for dependency injection / tests).
func (t *Tracker) WithLogger(logger *slog.Logger) *Tracker {
	t.logger = logger
	return t
}

// SetClock is for tests.
func (t *Tracker) SetClock(clock func() time.Time) {
	t.clock = clock
}

// Run blocks until ctx is cancelled, taking a snapshot every `interval`.
func (t *Tracker) Run(ctx context.Context) error {
	if t.interval <= 0 {
		return errors.New("performance tracker: interval must be positive")
	}

	ticker := time.NewTicker(t.interval)
	defer ticker.Stop()

	// Snapshot immediately so the API has data without waiting one full tick.
	if err := t.TakeSnapshots(ctx); err != nil && !errors.Is(err, context.Canceled) {
		t.logger.Error("performance: initial snapshot failed", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := t.TakeSnapshots(ctx); err != nil && !errors.Is(err, context.Canceled) {
				t.logger.Error("performance: snapshot tick failed", "error", err)
			}
		}
	}
}

// TakeSnapshots iterates every active vault, reads its on-chain balance, and
// persists a snapshot + recomputed APY history. Failures on individual vaults
// are isolated: one bad vault doesn't block the others.
func (t *Tracker) TakeSnapshots(ctx context.Context) error {
	vaults, err := t.vaults.ListActive(ctx)
	if err != nil {
		return fmt.Errorf("list active vaults: %w", err)
	}

	now := t.clock()
	var firstErr error

	for _, v := range vaults {
		if err := t.snapshotVault(ctx, v, now); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
	}

	return firstErr
}

func (t *Tracker) snapshotVault(ctx context.Context, v vault.Vault, now time.Time) error {
	balance := v.CurrentBalance
	if t.chain != nil && v.ContractAddress != "" {
		if onchain, err := t.chain.VaultBalance(ctx, v.ContractAddress); err == nil {
			balance = onchain
		}
		// On error fall back to the DB value rather than skip the snapshot;
		// continuity matters more than freshness for one missed read.
	}

	deposited := v.TotalDeposited
	yieldEarned := balance.Sub(deposited)

	sharePrice := decimal.NewFromInt(1)
	if !deposited.IsZero() && deposited.Sign() > 0 {
		sharePrice = balance.Div(deposited).Round(8)
	}

	breakdown := make([]perfdom.AllocationBreakdownEntry, 0, len(v.Allocations))
	for _, a := range v.Allocations {
		breakdown = append(breakdown, perfdom.AllocationBreakdownEntry{
			Source: a.Protocol,
			Amount: a.Amount,
			APY:    a.APY,
		})
	}

	snapshot := perfdom.Snapshot{
		VaultID:             v.ID,
		TotalBalance:        balance,
		TotalDeposited:      deposited,
		TotalYieldEarned:    yieldEarned,
		SharePrice:          sharePrice,
		SnapshotAt:          now,
		AllocationBreakdown: breakdown,
	}

	if _, err := t.repo.Insert(ctx, snapshot); err != nil {
		return fmt.Errorf("insert snapshot for vault %s: %w", v.ID, err)
	}

	return t.recalculateAPY(ctx, v, balance, deposited, now)
}

func (t *Tracker) recalculateAPY(ctx context.Context, v vault.Vault, currentBalance, totalDeposited decimal.Decimal, now time.Time) error {
	for _, period := range perfdom.AllAPYPeriods {
		var since time.Time
		var elapsedDays float64

		if period == perfdom.PeriodAll {
			since = v.CreatedAt
			elapsedDays = now.Sub(v.CreatedAt).Hours() / 24
		} else {
			days := period.Days()
			since = now.Add(-time.Duration(days) * 24 * time.Hour)
		}

		earliest, err := t.repo.FirstAtOrAfter(ctx, v.ID, since)
		if err != nil && !errors.Is(err, perfdom.ErrSnapshotNotFound) {
			return err
		}
		if errors.Is(err, perfdom.ErrSnapshotNotFound) {
			// Not enough history for this window yet — skip without writing
			// a noisy zero row.
			continue
		}

		// For non-PeriodAll, anchor elapsed at the earliest snapshot inside
		// the window so we don't over-annualize when the vault is younger
		// than the window.
		if period != perfdom.PeriodAll {
			elapsedDays = now.Sub(earliest.SnapshotAt).Hours() / 24
		}
		if elapsedDays <= 0 {
			continue
		}

		// Use the deposited amount captured at the start of the window when
		// available, falling back to the current value.
		baseDeposit := totalDeposited
		if !earliest.TotalDeposited.IsZero() {
			baseDeposit = earliest.TotalDeposited
		}

		apy := CalculateRealizedAPY(currentBalance, baseDeposit, elapsedDays)
		if err := t.repo.UpsertAPY(ctx, perfdom.APYRecord{
			VaultID:      v.ID,
			Period:       period,
			RealizedAPY:  apy,
			CalculatedAt: now,
		}); err != nil {
			return err
		}
	}
	return nil
}
