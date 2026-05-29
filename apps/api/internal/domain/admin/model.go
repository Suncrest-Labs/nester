package admin

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/offramp"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/user"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

type VaultListFilter struct {
	Page    int
	PerPage int
	Status  string
	Sort    string
	Order   string
	Search  string
}

type SettlementListFilter struct {
	Page     int
	PerPage  int
	Status   string
	Sort     string
	Order    string
	Search   string
	DateFrom *time.Time
	DateTo   *time.Time
}

type UserListFilter struct {
	Page    int
	PerPage int
	Sort    string
	Order   string
	Search  string
}

type DashboardSettlementMetrics struct {
	Total        int64           `json:"total"`
	Pending      int64           `json:"pending"`
	Completed24h int64           `json:"completed_24h"`
	Failed24h    int64           `json:"failed_24h"`
	Volume24h    decimal.Decimal `json:"volume_24h"`
}

type VaultAlert struct {
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

type SystemAlert struct {
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

type VaultHealthEntry struct {
	ID                  uuid.UUID    `json:"id"`
	Name                string       `json:"name"`
	TVLUSDC             string       `json:"tvl_usdc"`
	APY7d               string       `json:"apy_7d"`
	Depositors          int64        `json:"depositors"`
	PendingTransactions int64        `json:"pending_transactions"`
	LastRebalanceAt     *string      `json:"last_rebalance_at,omitempty"`
	Status              string       `json:"status"`
	Alerts              []VaultAlert `json:"alerts"`
}

type VaultHealthDashboard struct {
	TotalTVLUSDC    string             `json:"total_tvl_usdc"`
	TotalDepositors int64              `json:"total_depositors"`
	Vaults          []VaultHealthEntry `json:"vaults"`
	SystemAlerts    []SystemAlert      `json:"system_alerts"`
}

type VaultHealthRow struct {
	ID                  uuid.UUID
	Name                string
	TVL                 decimal.Decimal
	APY7d               *decimal.Decimal
	APY7d24hAgo         *decimal.Decimal
	Depositors          int64
	PendingTransactions int64
	LastRebalanceAt     *time.Time
	Status              vault.VaultStatus
}

type VaultHealthDashboardData struct {
	TotalTVL        decimal.Decimal
	TotalDepositors int64
	Vaults          []VaultHealthRow
}

type VaultSummary struct {
	ID              uuid.UUID       `json:"id"`
	UserID          uuid.UUID       `json:"user_id"`
	WalletAddress   string          `json:"wallet_address"`
	ContractAddress string          `json:"contract_address"`
	TotalDeposited  decimal.Decimal `json:"total_deposited"`
	CurrentBalance  decimal.Decimal `json:"current_balance"`
	Currency        string          `json:"currency"`
	Status          vault.VaultStatus `json:"status"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

type VaultDetail struct {
	VaultSummary
	Allocations []vault.Allocation `json:"allocations"`
}

type SettlementSummary struct {
	offramp.Settlement
	WalletAddress string `json:"wallet_address"`
}

type UserSummary struct {
	ID             uuid.UUID       `json:"id"`
	WalletAddress  string          `json:"wallet_address"`
	DisplayName    string          `json:"display_name"`
	KYCStatus      user.KYCStatus  `json:"kyc_status"`
	VaultCount     int64           `json:"vault_count"`
	TotalDeposited decimal.Decimal `json:"total_deposited"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type HealthStatus struct {
	Status         string     `json:"status"`
	LatencyMS      int64      `json:"latency_ms,omitempty"`
	Message        string     `json:"message,omitempty"`
	LastCheckedAt  time.Time  `json:"last_checked_at"`
	LastEventAt    *time.Time `json:"last_event_at,omitempty"`
	LagSeconds     int64      `json:"lag_seconds,omitempty"`
}

type DetailedHealth struct {
	Database           HealthStatus `json:"database"`
	StellarRPC         HealthStatus `json:"stellar_rpc"`
	SettlementProvider HealthStatus `json:"settlement_provider"`
	EventIndexer       HealthStatus `json:"event_indexer"`
	DiskUsage          string       `json:"disk_usage"`
	Uptime             string       `json:"uptime"`
}

type DashboardSystemHealth struct {
	Database           string  `json:"database"`
	StellarRPC         string  `json:"stellar_rpc"`
	SettlementProvider string  `json:"settlement_provider"`
	LastEventIndexed   string  `json:"last_event_indexed"`
}

// Repository is the persistence/read model contract required by admin APIs.
type Repository interface {
	GetVaultHealthDashboard(ctx context.Context) (VaultHealthDashboardData, error)
	ListVaults(ctx context.Context, filter VaultListFilter) ([]VaultSummary, int, error)
	GetVaultDetail(ctx context.Context, id uuid.UUID) (VaultDetail, error)
	UpdateVaultStatus(ctx context.Context, id uuid.UUID, status vault.VaultStatus) (VaultDetail, error)
	ListSettlements(ctx context.Context, filter SettlementListFilter) ([]SettlementSummary, int, error)
	ListUsers(ctx context.Context, filter UserListFilter) ([]UserSummary, int, error)
	GetLastEventIndexedAt(ctx context.Context) (*time.Time, error)
	DatabaseHealth(ctx context.Context) (int64, error)
}
