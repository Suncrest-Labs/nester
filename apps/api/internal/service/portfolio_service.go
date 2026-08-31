package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/portfolio"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

// PortfolioService handles portfolio-level aggregations and queries.
type PortfolioService struct {
	vaultRepository vault.Repository
}

// NewPortfolioService creates a new portfolio service instance.
func NewPortfolioService(vaultRepository vault.Repository) *PortfolioService {
	return &PortfolioService{
		vaultRepository: vaultRepository,
	}
}

// GetUserPortfolioSummary returns an aggregated view of all user positions across their vaults.
// Returns an empty portfolio with zero totals if user has no vaults (not an error).
func (s *PortfolioService) GetUserPortfolioSummary(ctx context.Context, userID uuid.UUID) (portfolio.Summary, error) {
	// Fetch all vaults for the user with minimal pagination.
	//
	// A 10,000-vault ceiling is safe here in a way the pending-deposit and
	// reconciliation ceilings were not (nester#1193): vault count is bounded
	// by how many a person can plausibly create through the product UI, not
	// by transaction volume that grows with usage over time. If vault
	// creation ever becomes bulk/programmatic this reasoning stops holding
	// and this call needs real pagination.
	vaults, _, err := s.vaultRepository.ListUserVaults(ctx, userID, vault.UserListFilter{
		Page:    1,
		PerPage: 10000,
	})
	if err != nil {
		return portfolio.Summary{}, err
	}

	// Initialize summary with zero totals
	summary := portfolio.Summary{
		TotalDepositedUSDC:    decimal.Zero,
		TotalCurrentValueUSDC: decimal.Zero,
		TotalYieldEarnedUSDC:  decimal.Zero,
		Positions:             make([]portfolio.Position, 0),
	}

	// If user has no vaults, return empty portfolio summary
	if len(vaults) == 0 {
		return summary, nil
	}

	// Aggregate vault data into portfolio summary
	for _, v := range vaults {
		// Skip closed vaults
		if v.Status == vault.StatusClosed {
			continue
		}

		// Accumulate totals
		summary.TotalDepositedUSDC = summary.TotalDepositedUSDC.Add(v.TotalDeposited)
		summary.TotalCurrentValueUSDC = summary.TotalCurrentValueUSDC.Add(v.CurrentBalance)
		summary.TotalYieldEarnedUSDC = summary.TotalYieldEarnedUSDC.Add(v.YieldEarned)

		// Calculate APY from allocations using a weighted average by allocation amount.
		apy := decimal.Zero
		if len(v.Allocations) > 0 {
			totalAmount := decimal.Zero
			weightedAPY := decimal.Zero
			for _, alloc := range v.Allocations {
				weightedAPY = weightedAPY.Add(alloc.APY.Mul(alloc.Amount))
				totalAmount = totalAmount.Add(alloc.Amount)
			}
			if totalAmount.GreaterThan(decimal.Zero) {
				apy = weightedAPY.Div(totalAmount)
			}
		}

		// Add vault as a position in the summary
		position := portfolio.Position{
			VaultID:      v.ID,
			VaultName:    v.ContractAddress, // Use contract address as vault name (could extend with metadata)
			Deposited:    v.TotalDeposited,
			CurrentValue: v.CurrentBalance,
			Shares:       decimal.Zero, // Share balance would require additional data from on-chain or DB
			APY7d:        apy,          // 7-day APY from allocations
		}
		summary.Positions = append(summary.Positions, position)
	}

	return summary, nil
}
