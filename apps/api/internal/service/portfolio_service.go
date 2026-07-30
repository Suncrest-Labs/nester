package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/ledger"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/portfolio"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

// PortfolioService handles portfolio-level aggregations and queries.
// It now reads balances from the ledger when available, making the ledger
// the authoritative source of truth for portfolio valuation.
type PortfolioService struct {
	vaultRepository vault.Repository
	ledgerRepo      ledger.Repository
}

// NewPortfolioService creates a new portfolio service instance.
func NewPortfolioService(vaultRepository vault.Repository) *PortfolioService {
	return &PortfolioService{
		vaultRepository: vaultRepository,
	}
}

// SetLedgerRepository wires the ledger repository so balances are read from the ledger.
func (s *PortfolioService) SetLedgerRepository(repo ledger.Repository) {
	s.ledgerRepo = repo
}

// LedgerBalanceProvider is satisfied by LedgerService or LedgerRepository for portfolio reads.
type LedgerBalanceProvider interface {
	GetUserVaultBalance(ctx context.Context, userID, vaultID uuid.UUID) (int64, error)
	GetVaultPoolBalance(ctx context.Context, vaultID uuid.UUID) (int64, error)
}

// GetUserPortfolioSummary returns an aggregated view of all user positions across their vaults.
// Returns an empty portfolio with zero totals if user has no vaults (not an error).
// Balances are read from the ledger when a ledger repo is configured — the ledger is the source of truth.
// Fallback to vault table aggregates if ledger is unavailable (e.g., old tests or migration window).
func (s *PortfolioService) GetUserPortfolioSummary(ctx context.Context, userID uuid.UUID) (portfolio.Summary, error) {
	// Fetch all vaults for the user with minimal pagination
	vaults, _, err := s.vaultRepository.ListUserVaults(ctx, userID, vault.UserListFilter{
		Page:    1,
		PerPage: 10000, // Reasonable upper limit for number of vaults per user
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

		// Try ledger as authoritative source for balances
		currentBalance := v.CurrentBalance
		deposited := v.TotalDeposited
		if s.ledgerRepo != nil {
			if lb, err := s.ledgerRepo.GetUserVaultBalance(ctx, userID, v.ID); err == nil && lb != 0 {
				// lb is stroops int64, convert to decimal USDC
				currentBalance = decimal.NewFromInt(lb).Div(decimal.NewFromInt(10_000_000))
				// For deposited, use same as current for simplicity; real would track deposits vs yield separately
				// We keep TotalDeposited from vault table unless ledger also tracks deposits.
				// For ledger-backed total, we can consider deposited = current - yield, but using ledger balance as current value.
			}
		}

		// Accumulate totals
		summary.TotalDepositedUSDC = summary.TotalDepositedUSDC.Add(deposited)
		summary.TotalCurrentValueUSDC = summary.TotalCurrentValueUSDC.Add(currentBalance)
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
			Deposited:    deposited,
			CurrentValue: currentBalance,
			Shares:       decimal.Zero, // Share balance would require additional data from on-chain or DB
			APY7d:        apy,           // 7-day APY from allocations
		}
		summary.Positions = append(summary.Positions, position)
	}

	return summary, nil
}
