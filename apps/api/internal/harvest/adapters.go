package harvest

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/user"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
)

// VaultLister is the subset of the vault repository the harvest source needs.
type VaultLister interface {
	ListActive(ctx context.Context) ([]vault.Vault, error)
	GetVault(ctx context.Context, id uuid.UUID) (vault.Vault, error)
}

// RepoSource adapts the vault repository to VaultSource. Owner wallet is left
// unresolved here (resolved lazily at execution) to avoid an N-user lookup on
// every evaluation tick.
type RepoSource struct {
	vaults VaultLister
}

// NewRepoSource constructs a RepoSource.
func NewRepoSource(vaults VaultLister) *RepoSource { return &RepoSource{vaults: vaults} }

// ListHarvestable returns active vaults with positive accrued yield.
func (s *RepoSource) ListHarvestable(ctx context.Context) ([]VaultYield, error) {
	vs, err := s.vaults.ListActive(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]VaultYield, 0, len(vs))
	for _, v := range vs {
		y := toVaultYield(v)
		if y.AccruedYield.GreaterThan(decimal.Zero) {
			out = append(out, y)
		}
	}
	return out, nil
}

// GetVaultYield returns the yield view for one vault.
func (s *RepoSource) GetVaultYield(ctx context.Context, id uuid.UUID) (VaultYield, error) {
	v, err := s.vaults.GetVault(ctx, id)
	if err != nil {
		return VaultYield{}, err
	}
	return toVaultYield(v), nil
}

// toVaultYield derives harvestable yield the same way the vault service does:
// prefer the explicit YieldEarned, otherwise the positive balance-over-principal
// delta.
func toVaultYield(v vault.Vault) VaultYield {
	accrued := decimal.Zero
	switch {
	case v.YieldEarned.GreaterThan(decimal.Zero):
		accrued = v.YieldEarned
	case v.CurrentBalance.Sub(v.TotalDeposited).GreaterThan(decimal.Zero):
		accrued = v.CurrentBalance.Sub(v.TotalDeposited)
	}
	return VaultYield{
		VaultID:          v.ID,
		UserID:           v.UserID,
		ContractAddress:  v.ContractAddress,
		Currency:         v.Currency,
		AccruedYield:     accrued,
		HarvestFrequency: v.HarvestFrequency,
		LastHarvestedAt:  v.LastHarvestedAt,
	}
}

// UserLookup resolves a vault owner's wallet address.
type UserLookup interface {
	GetUser(ctx context.Context, id uuid.UUID) (*user.User, error)
}

// VaultHarvester performs the actual on-chain harvest. *service.VaultService
// satisfies it.
type VaultHarvester interface {
	HarvestVault(ctx context.Context, in service.HarvestVaultInput) (service.HarvestResult, error)
}

// ServiceExecutor adapts the vault service to the harvest Executor. It resolves
// the owner wallet (when the job payload omits it) and delegates to
// VaultService.HarvestVault, which is naturally idempotent — a re-run finds no
// unclaimed yield and no-ops.
type ServiceExecutor struct {
	harvester VaultHarvester
	users     UserLookup
}

// NewServiceExecutor constructs a ServiceExecutor.
func NewServiceExecutor(harvester VaultHarvester, users UserLookup) *ServiceExecutor {
	return &ServiceExecutor{harvester: harvester, users: users}
}

// Harvest executes one vault harvest.
func (x *ServiceExecutor) Harvest(ctx context.Context, cmd HarvestCommand) (HarvestOutcome, error) {
	p := cmd.Payload
	wallet := p.WalletAddress
	if wallet == "" {
		u, err := x.users.GetUser(ctx, p.UserID)
		if err != nil {
			return HarvestOutcome{}, fmt.Errorf("resolve owner wallet: %w", err)
		}
		if u == nil || u.WalletAddress == "" {
			// No wallet on file: retrying will not help — dead-letter it.
			return HarvestOutcome{}, fmt.Errorf("vault owner %s has no wallet address", p.UserID)
		}
		wallet = u.WalletAddress
	}
	compound := p.Compound
	res, err := x.harvester.HarvestVault(ctx, service.HarvestVaultInput{
		VaultID:       p.VaultID,
		UserID:        p.UserID,
		WalletAddress: wallet,
		Compound:      &compound,
	})
	if err != nil {
		return HarvestOutcome{}, err
	}
	return HarvestOutcome{NetYield: res.NetYieldUSDC, TxHash: res.TxHash}, nil
}
