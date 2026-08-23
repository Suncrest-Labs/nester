package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

// IntelligenceVault is the vault shape expected by the intelligence service.
type IntelligenceVault struct {
	ID              string                     `json:"id"`
	Name            string                     `json:"name"`
	ContractAddress string                     `json:"contract_address"`
	TotalBalanceUSD float64                    `json:"total_balance_usd"`
	YieldEarnedUSD  float64                    `json:"yield_earned_usd"`
	AverageAPY      float64                    `json:"average_apy"`
	LockPeriodDays  int                        `json:"lock_period_days"`
	Allocations     []IntelligenceAllocation   `json:"allocations"`
}

type IntelligenceAllocation struct {
	Protocol  string  `json:"protocol"`
	AmountUSD float64 `json:"amount_usd"`
	APY       float64 `json:"apy"`
}

type UserVaultsResponse struct {
	Vaults []IntelligenceVault `json:"vaults"`
}

type UserVaultsService struct {
	vaultRepo vault.Repository
}

func NewUserVaultsService(vaultRepo vault.Repository) *UserVaultsService {
	return &UserVaultsService{vaultRepo: vaultRepo}
}

func (s *UserVaultsService) ListForIntelligence(ctx context.Context, userID uuid.UUID) (UserVaultsResponse, error) {
	vaults, _, err := s.vaultRepo.ListUserVaults(ctx, userID, vault.UserListFilter{
		Page:    1,
		PerPage: 500,
		Status:  string(vault.StatusActive),
	})
	if err != nil {
		return UserVaultsResponse{}, err
	}

	out := make([]IntelligenceVault, 0, len(vaults))
	for _, v := range vaults {
		balance, _ := v.CurrentBalance.Float64()
		yield, _ := v.YieldEarned.Float64()
		avgAPY := weightedAPY(v.Allocations)

		allocs := make([]IntelligenceAllocation, 0, len(v.Allocations))
		for _, a := range v.Allocations {
			amt, _ := a.Amount.Float64()
			apy, _ := a.APY.Float64()
			allocs = append(allocs, IntelligenceAllocation{
				Protocol:  a.Protocol,
				AmountUSD: amt,
				APY:       apy,
			})
		}

		out = append(out, IntelligenceVault{
			ID:              v.ID.String(),
			Name:            v.ContractAddress,
			ContractAddress: v.ContractAddress,
			TotalBalanceUSD: balance,
			YieldEarnedUSD:  yield,
			AverageAPY:      avgAPY,
			LockPeriodDays:  0,
			Allocations:     allocs,
		})
	}
	return UserVaultsResponse{Vaults: out}, nil
}

func weightedAPY(allocs []vault.Allocation) float64 {
	if len(allocs) == 0 {
		return 0
	}
	total := decimal.Zero
	weighted := decimal.Zero
	for _, a := range allocs {
		total = total.Add(a.Amount)
		weighted = weighted.Add(a.Amount.Mul(a.APY))
	}
	if !total.IsPositive() {
		return 0
	}
	apy, _ := weighted.Div(total).Float64()
	return apy
}
