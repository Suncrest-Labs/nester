// Package performance owns the snapshot worker, APY calculator, and the
// read-side query layer that backs the /vaults/{id}/performance endpoints.
package performance

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	perfdom "github.com/suncrestlabs/nester/apps/api/internal/domain/performance"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

// VaultLister is the subset of the vault repo we need. Defined locally so
// tests can stub it without dragging the full vault repository surface in.
type VaultLister interface {
	ListActive(ctx context.Context) ([]vault.Vault, error)
}

// Service is the read-side façade used by the HTTP handler.
type Service struct {
	repo  perfdom.SnapshotRepository
	clock func() time.Time
}

func NewService(repo perfdom.SnapshotRepository) *Service {
	return &Service{repo: repo, clock: func() time.Time { return time.Now().UTC() }}
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
	if math.IsNaN(apy) || math.IsInf(apy, 0) {
		return decimal.Zero
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

// Tracker is the snapshot-taking background worker.
type Tracker struct {
	repo     perfdom.SnapshotRepository
	vaults   VaultLister
	chain    BalanceProvider
	interval time.Duration
	clock    func() time.Time
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
	}
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
		// Log-and-continue is the caller's job; we surface the error once and
		// keep looping so a transient failure doesn't kill the worker.
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			_ = t.TakeSnapshots(ctx)
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
