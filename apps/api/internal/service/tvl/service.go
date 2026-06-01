package tvl

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	tvldom "github.com/suncrestlabs/nester/apps/api/internal/domain/tvl"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

// VaultAssetsReader fetches on-chain total_assets for a vault contract.
type VaultAssetsReader interface {
	TotalAssets(ctx context.Context, contractAddress string) (decimal.Decimal, error)
}

// VaultLister lists active vaults for the background tracker.
type VaultLister interface {
	ListActive(ctx context.Context) ([]vault.Vault, error)
	GetVault(ctx context.Context, id uuid.UUID) (vault.Vault, error)
}

// Service is the read-side façade for TVL endpoints.
type Service struct {
	repo      tvldom.Repository
	vaultRepo VaultLister
	clock     func() time.Time
}

func NewService(repo tvldom.Repository, vaultRepo VaultLister) *Service {
	return &Service{
		repo:      repo,
		vaultRepo: vaultRepo,
		clock:     func() time.Time { return time.Now().UTC() },
	}
}

func (s *Service) SetClock(clock func() time.Time) {
	s.clock = clock
}

func (s *Service) GetVaultTVL(ctx context.Context, vaultID uuid.UUID) (tvldom.VaultTVL, error) {
	if _, err := s.vaultRepo.GetVault(ctx, vaultID); err != nil {
		return tvldom.VaultTVL{}, err
	}

	latest, err := s.repo.LatestForVault(ctx, vaultID)
	if err != nil {
		if errors.Is(err, tvldom.ErrSnapshotNotFound) {
			return tvldom.VaultTVL{
				VaultID:         vaultID,
				TVLUSDC:         "0.000000",
				TVLUSD:          "0.00",
				TotalDepositors: 0,
				LastUpdated:     s.clock(),
				Change24hPct:    "0.00",
			}, nil
		}
		return tvldom.VaultTVL{}, err
	}

	change, err := s.change24h(ctx, vaultID, latest.TVLUSDC)
	if err != nil && !errors.Is(err, tvldom.ErrSnapshotNotFound) {
		return tvldom.VaultTVL{}, err
	}

	return tvldom.VaultTVL{
		VaultID:         vaultID,
		TVLUSDC:         latest.TVLUSDC.StringFixed(6),
		TVLUSD:          latest.TVLUSDC.StringFixed(2),
		TotalDepositors: latest.TotalDepositors,
		LastUpdated:     latest.SnapshotAt,
		Change24hPct:    change,
	}, nil
}

func (s *Service) GetAggregateTVL(ctx context.Context) (tvldom.AggregateTVL, error) {
	snapshots, err := s.repo.LatestPerActiveVault(ctx)
	if err != nil {
		return tvldom.AggregateTVL{}, err
	}

	var total decimal.Decimal
	var depositors int
	var latestAt time.Time
	var priorTotal decimal.Decimal
	hasPrior := false

	for _, snap := range snapshots {
		total = total.Add(snap.TVLUSDC)
		depositors += snap.TotalDepositors
		if snap.SnapshotAt.After(latestAt) {
			latestAt = snap.SnapshotAt
		}

		prior, err := s.repo.LatestAtOrBefore(ctx, snap.VaultID, s.clock().Add(-24*time.Hour))
		if err == nil {
			priorTotal = priorTotal.Add(prior.TVLUSDC)
			hasPrior = true
		}
	}

	change := "0.00"
	if hasPrior && !priorTotal.IsZero() {
		pct := total.Sub(priorTotal).Div(priorTotal).Mul(decimal.NewFromInt(100))
		change = formatChangePct(pct)
	}

	if latestAt.IsZero() {
		latestAt = s.clock()
	}

	return tvldom.AggregateTVL{
		TVLUSDC:         total.StringFixed(6),
		TVLUSD:          total.StringFixed(2),
		TotalDepositors: depositors,
		VaultCount:      len(snapshots),
		LastUpdated:     latestAt,
		Change24hPct:    change,
	}, nil
}

func (s *Service) change24h(ctx context.Context, vaultID uuid.UUID, current decimal.Decimal) (string, error) {
	prior, err := s.repo.LatestAtOrBefore(ctx, vaultID, s.clock().Add(-24*time.Hour))
	if err != nil {
		return "0.00", err
	}
	if prior.TVLUSDC.IsZero() {
		return "0.00", nil
	}
	pct := current.Sub(prior.TVLUSDC).Div(prior.TVLUSDC).Mul(decimal.NewFromInt(100))
	return formatChangePct(pct), nil
}

func formatChangePct(pct decimal.Decimal) string {
	sign := "+"
	if pct.IsNegative() {
		sign = ""
	}
	return sign + pct.Abs().StringFixed(2)
}

// Tracker refreshes on-chain TVL snapshots on a configurable interval.
type Tracker struct {
	repo     tvldom.Repository
	vaults   VaultLister
	chain    VaultAssetsReader
	interval time.Duration
	clock    func() time.Time
	logger   *slog.Logger
}

func NewTracker(repo tvldom.Repository, vaults VaultLister, chain VaultAssetsReader, interval time.Duration) *Tracker {
	return &Tracker{
		repo:     repo,
		vaults:   vaults,
		chain:    chain,
		interval: interval,
		clock:    func() time.Time { return time.Now().UTC() },
		logger:   slog.Default(),
	}
}

func (t *Tracker) WithLogger(logger *slog.Logger) *Tracker {
	t.logger = logger
	return t
}

func (t *Tracker) SetClock(clock func() time.Time) {
	t.clock = clock
}

func (t *Tracker) Run(ctx context.Context) error {
	if t.interval <= 0 {
		return errors.New("tvl tracker: interval must be positive")
	}

	ticker := time.NewTicker(t.interval)
	defer ticker.Stop()

	if err := t.RefreshAll(ctx); err != nil && !errors.Is(err, context.Canceled) {
		t.logger.Error("tvl: initial refresh failed", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := t.RefreshAll(ctx); err != nil && !errors.Is(err, context.Canceled) {
				t.logger.Error("tvl: refresh tick failed", "error", err)
			}
		}
	}
}

// RefreshAll queries total_assets for every active vault and persists snapshots.
func (t *Tracker) RefreshAll(ctx context.Context) error {
	vaults, err := t.vaults.ListActive(ctx)
	if err != nil {
		return fmt.Errorf("list active vaults: %w", err)
	}

	now := t.clock()
	var firstErr error
	for _, v := range vaults {
		if err := t.refreshVault(ctx, v, now); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			t.logger.Error("tvl: vault refresh failed", "vault_id", v.ID, "error", err)
		}
	}
	return firstErr
}

func (t *Tracker) refreshVault(ctx context.Context, v vault.Vault, now time.Time) error {
	tvl := v.CurrentBalance
	if t.chain != nil && v.ContractAddress != "" {
		if onchain, err := t.chain.TotalAssets(ctx, v.ContractAddress); err == nil {
			tvl = onchain
		} else {
			// Fall back to last cached snapshot rather than skipping.
			if cached, cacheErr := t.repo.LatestForVault(ctx, v.ID); cacheErr == nil {
				tvl = cached.TVLUSDC
			}
			t.logger.Warn("tvl: on-chain query failed, using fallback",
				"vault_id", v.ID, "error", err)
		}
	}

	depositors, err := t.repo.CountDepositors(ctx, v.ID)
	if err != nil {
		return err
	}

	_, err = t.repo.Insert(ctx, tvldom.Snapshot{
		VaultID:         v.ID,
		TVLUSDC:         tvl,
		TotalDepositors: depositors,
		SnapshotAt:      now,
	})
	return err
}
