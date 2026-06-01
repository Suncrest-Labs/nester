package portfolio

import (
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Position represents a user's position in a single vault
type Position struct {
	VaultID      uuid.UUID       `json:"vault_id"`
	VaultName    string          `json:"vault_name"`
	Deposited    decimal.Decimal `json:"deposited"`
	CurrentValue decimal.Decimal `json:"current_value"`
	Shares       decimal.Decimal `json:"shares"`
	APY7d        decimal.Decimal `json:"apy_7d"`
}

// Summary represents the aggregated portfolio data across all user vaults
type Summary struct {
	TotalDepositedUSDC  decimal.Decimal `json:"total_deposited_usdc"`
	TotalCurrentValueUSDC decimal.Decimal `json:"total_current_value_usdc"`
	TotalYieldEarnedUSDC decimal.Decimal `json:"total_yield_earned_usdc"`
	Positions           []Position      `json:"positions"`
}
