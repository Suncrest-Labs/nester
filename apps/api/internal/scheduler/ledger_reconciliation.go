package scheduler

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/ledger"
)

// defaultEscalationThresholdUSD is used when RECONCILIATION_ESCALATION_THRESHOLD_USD is unset.
const defaultEscalationThresholdUSD = 100.0

// LedgerReconciliationConfigFromEnv builds a ledger.ReconciliationConfig, reading the
// mainnet dollar-threshold for immediate on-call paging from
// RECONCILIATION_ESCALATION_THRESHOLD_USD (default defaultEscalationThresholdUSD), and
// determining IsMainnet from the same environment variables isMainnetEnvironment checks.
func LedgerReconciliationConfigFromEnv(enabled bool, interval time.Duration, toleranceStroops int64) ledger.ReconciliationConfig {
	threshold := defaultEscalationThresholdUSD
	if v := os.Getenv("RECONCILIATION_ESCALATION_THRESHOLD_USD"); v != "" {
		if parsed, err := strconv.ParseFloat(v, 64); err == nil && parsed > 0 {
			threshold = parsed
		}
	}
	return ledger.ReconciliationConfig{
		Enabled:                enabled,
		Interval:               interval,
		ToleranceStroops:       toleranceStroops,
		MainnetDollarThreshold: threshold,
		IsMainnet:              isMainnetEnvironment(),
	}
}

// LedgerReconciliationDeps holds dependencies for the reconciliation job.
type LedgerReconciliationDeps struct {
	LedgerRepo  ledger.Repository
	VaultLister ReconciliationVaultLister // reuses existing vault lister
	ChainReader ledger.ChainReader
	Logger      *slog.Logger
	Config      ledger.ReconciliationConfig
}

// ReconciliationVaultLister lists active vaults with contract addresses for reconciliation.
type ReconciliationVaultLister interface {
	ListActiveForReconciliation(ctx context.Context) ([]ReconcileVaultInfo, error)
}

// ReconcileVaultInfo is minimal vault info needed for reconciliation.
type ReconcileVaultInfo struct {
	ID              uuid.UUID
	ContractAddress string
	Currency        string
}

// LedgerReconciliationJob periodically compares ledger vault-pool balances against
// on-chain state and sum of user-position accounts against total_shares * share_price.
// Any drift beyond tolerance raises an alert and writes a reconciliation record without auto-correcting.
type LedgerReconciliationJob struct {
	cfg         ledger.ReconciliationConfig
	ledgerRepo  ledger.Repository
	vaults      ReconciliationVaultLister
	chainReader ledger.ChainReader
	logger      *slog.Logger
	leader      LeaderChecker
}

func NewLedgerReconciliationJob(deps LedgerReconciliationDeps) *LedgerReconciliationJob {
	logger := deps.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	return &LedgerReconciliationJob{
		cfg:         deps.Config,
		ledgerRepo:  deps.LedgerRepo,
		vaults:      deps.VaultLister,
		chainReader: deps.ChainReader,
		logger:      logger,
	}
}

func (j *LedgerReconciliationJob) SetLeaderChecker(l LeaderChecker) { j.leader = l }
func (j *LedgerReconciliationJob) isLeader() bool                   { return j.leader == nil || j.leader.IsLeader() }

func (j *LedgerReconciliationJob) Run(ctx context.Context) {
	if !j.cfg.Enabled {
		j.logger.Info("ledger reconciliation disabled")
		return
	}
	interval := j.cfg.Interval
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	j.logger.Info("ledger reconciliation starting", "interval", interval, "tolerance_stroops", j.cfg.ToleranceStroops)
	j.Tick(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			j.logger.Info("ledger reconciliation stopping")
			return
		case <-ticker.C:
			j.Tick(ctx)
		}
	}
}

// Tick runs a single reconciliation pass.
func (j *LedgerReconciliationJob) Tick(ctx context.Context) {
	if !j.isLeader() {
		j.logger.Debug("ledger reconciliation: skipping, not leader")
		return
	}
	if j.ledgerRepo == nil || j.vaults == nil || j.chainReader == nil {
		j.logger.Error("ledger reconciliation: missing dependencies")
		return
	}
	vaultInfos, err := j.vaults.ListActiveForReconciliation(ctx)
	if err != nil {
		j.logger.Error("ledger reconciliation: list vaults failed", "error", err)
		return
	}
	for _, v := range vaultInfos {
		j.reconcileVault(ctx, v)
	}
}

func (j *LedgerReconciliationJob) reconcileVault(ctx context.Context, v ReconcileVaultInfo) {
	ledgerPoolBal, err := j.ledgerRepo.GetVaultPoolBalance(ctx, v.ID)
	if err != nil {
		j.logger.Error("ledger reconciliation: get vault pool balance failed", "vault_id", v.ID, "error", err)
		return
	}
	// On-chain balance in stroops
	onChainBal, err := j.chainReader.ReadVaultBalance(ctx, v.ContractAddress)
	if err != nil {
		j.logger.Error("ledger reconciliation: read on-chain balance failed", "vault_id", v.ID, "contract", v.ContractAddress, "error", err)
		// Still record as error status without on-chain value? We skip for now
		return
	}

	diff := ledgerPoolBal - onChainBal
	tolerance := j.cfg.ToleranceStroops
	if tolerance <= 0 {
		tolerance = 1_000_000 // default 0.1 USDC = 1e6 stroops
	}
	status := "ok"
	if diff < 0 {
		diff = -diff
	}
	// absolute difference
	absDiff := ledgerPoolBal - onChainBal
	if absDiff < 0 {
		absDiff = -absDiff
	}
	if absDiff > tolerance {
		status = "drift"
	}

	// Also check sum user positions vs total_shares * share_price
	sumUser, err := j.ledgerRepo.SumUserPositionBalances(ctx, v.ID)
	if err != nil {
		j.logger.Error("ledger reconciliation: sum user positions failed", "vault_id", v.ID, "error", err)
	} else {
		// Try to read total_shares * share_price
		totalSharesPrice, err := j.chainReader.ReadTotalSharesTimesPrice(ctx, v.ContractAddress)
		if err == nil && totalSharesPrice != 0 {
			userDiff := sumUser - totalSharesPrice
			if userDiff < 0 {
				userDiff = -userDiff
			}
			if userDiff > tolerance && status == "ok" {
				status = "drift"
			}
			// If drift, enrich details
			if status == "drift" {
				j.logger.Warn("ledger reconciliation: drift detected",
					"vault_id", v.ID,
					"ledger_pool", ledgerPoolBal,
					"on_chain", onChainBal,
					"pool_diff", absDiff,
					"sum_user", sumUser,
					"total_shares_price", totalSharesPrice,
					"user_diff", userDiff,
				)
			}
		}
	}

	// Write reconciliation record (no auto-correct)
	detailsMap := map[string]any{
		"ledger_pool_balance": ledgerPoolBal,
		"on_chain_balance":    onChainBal,
		"sum_user_positions":  sumUser,
		"contract_address":    v.ContractAddress,
	}
	detailsJSON, _ := json.Marshal(detailsMap)

	rec := ledger.ReconciliationRecord{
		VaultID:                v.ID,
		LedgerVaultPoolBalance: ledgerPoolBal,
		OnChainBalance:         onChainBal,
		Difference:             absDiff,
		Tolerance:              tolerance,
		Status:                 status,
		Details:                string(detailsJSON),
	}

	if err := j.ledgerRepo.CreateReconciliationRecord(ctx, rec); err != nil {
		j.logger.Error("ledger reconciliation: failed to write record", "vault_id", v.ID, "error", err)
		return
	}

	if status == "drift" {
		// Check if drift exceeds mainnet dollar threshold and we are on mainnet
		// 1 USDC = 10^7 stroops. Dollar threshold converted to stroops.
		thresholdStroops := int64(0)
		if j.cfg.MainnetDollarThreshold > 0 {
			thresholdStroops = parseDecimalToStroops(decimal.NewFromFloat(j.cfg.MainnetDollarThreshold))
		}
		isMainnet := j.cfg.IsMainnet

		if isMainnet && thresholdStroops > 0 && absDiff > thresholdStroops {
			// Page on-call immediately with a critical alert log that triggers pagers
			j.logger.Error("PAGER ALERT: ledger-vs-chain drift exceeded mainnet dollar threshold!",
				"vault_id", v.ID,
				"contract_address", v.ContractAddress,
				"ledger", ledgerPoolBal,
				"on_chain", onChainBal,
				"difference", absDiff,
				"dollar_threshold", j.cfg.MainnetDollarThreshold,
				"environment", "mainnet",
			)
		} else {
			// Raise alert — log as error
			j.logger.Error("ledger reconciliation drift beyond tolerance — alerting, not auto-correcting",
				"vault_id", v.ID,
				"ledger", ledgerPoolBal,
				"on_chain", onChainBal,
				"difference", absDiff,
				"tolerance", tolerance,
			)
		}
	} else {
		j.logger.Debug("ledger reconciliation ok", "vault_id", v.ID, "ledger", ledgerPoolBal, "on_chain", onChainBal)
	}
}

// Helper for decimal conversion used elsewhere
func parseDecimalToStroops(d decimal.Decimal) int64 {
	return d.Mul(decimal.NewFromInt(10_000_000)).Round(0).IntPart()
}

func isMainnetEnvironment() bool {
	// Check standard environment variables to determine if running on mainnet
	return os.Getenv("STELLAR_NETWORK") == "mainnet" || os.Getenv("NESTER_ENV") == "mainnet" || os.Getenv("ENVIRONMENT") == "mainnet"
}
