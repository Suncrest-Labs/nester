// Package analytics defines the types used in analytics API responses.
package analytics

import "github.com/shopspring/decimal"

// AnalyticsResponse represents the analytics data for a user
type AnalyticsResponse struct {
	DailySnapshots     []DailySnapshot     `json:"daily_snapshots"`
	VaultMonthlyYield  []VaultMonthlyYield `json:"vault_monthly_yield"`
	CurrentAllocation  []CurrentAllocation `json:"current_allocation"`
	PerformanceMetrics PerformanceMetrics  `json:"performance_metrics"`
	Vaults             []VaultInfo         `json:"vaults"`
}

// DailySnapshot represents a daily balance snapshot.
//
// Monetary fields are decimal.Decimal so an amount that crosses this layer
// keeps its exact value; rates and percentages remain float64 (nester#1121).
type DailySnapshot struct {
	Date            string          `json:"date"`
	TotalBalanceUSD decimal.Decimal `json:"total_balance_usd"`
	YieldEarnedUSD  decimal.Decimal `json:"yield_earned_usd"`
}

// VaultMonthlyYield represents yield per vault per month
type VaultMonthlyYield struct {
	VaultID   string          `json:"vault_id"`
	VaultName string          `json:"vault_name"`
	Month     string          `json:"month"` // Format: YYYY-MM
	YieldUSD  decimal.Decimal `json:"yield_usd"`
}

// CurrentAllocation represents current portfolio allocation
type CurrentAllocation struct {
	Protocol      string          `json:"protocol"`
	AllocationPCT float64         `json:"allocation_pct"`
	Balance       decimal.Decimal `json:"balance_usd"`
	APY           float64         `json:"apy"`
}

// PerformanceMetrics contains key performance indicators
type PerformanceMetrics struct {
	TotalYieldEarned decimal.Decimal `json:"total_yield_earned"`
	YieldChangePCT   float64         `json:"yield_change_pct"`
	BestVaultName    string          `json:"best_vault_name"`
	BestVaultAPY     float64         `json:"best_vault_apy"`
	AverageAPY       float64         `json:"average_apy"`
	TotalDeposited   decimal.Decimal `json:"total_deposited"`
	TotalWithdrawn   decimal.Decimal `json:"total_withdrawn"`
	NetPosition      decimal.Decimal `json:"net_position"`
}

// VaultInfo represents vault information for comparison table
type VaultInfo struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Balance        decimal.Decimal `json:"balance_usd"`
	APY            float64         `json:"apy"`
	YieldEarned    decimal.Decimal `json:"yield_earned"`
	LockPeriodDays int             `json:"lock_period_days"`
}
