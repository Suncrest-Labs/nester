package ledger

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

// Reconciler executes periodic reconciliation between double-entry ledger balances and on-chain state.
type Reconciler struct {
	repo        Repository
	chain       ChainReader
	cfg         ReconciliationConfig
	logger      *slog.Logger
	metrics     ReconciliationMetrics
}

// ReconciliationMetrics defines metrics published by the reconciler.
type ReconciliationMetrics interface {
	IncReconciliationRun(status string)
	IncReconciliationDrift()
	ObserveDriftAmount(vaultID string, diff int64)
}

// NoopReconciliationMetrics discards metrics.
type NoopReconciliationMetrics struct{}

func (NoopReconciliationMetrics) IncReconciliationRun(string) {}
func (NoopReconciliationMetrics) IncReconciliationDrift() {}
func (NoopReconciliationMetrics) ObserveDriftAmount(string, int64) {}

// NewReconciler creates a new ledger reconciler.
func NewReconciler(repo Repository, chain ChainReader, cfg ReconciliationConfig, logger *slog.Logger, metrics ReconciliationMetrics) *Reconciler {
	if metrics == nil {
		metrics = NoopReconciliationMetrics{}
	}
	return &Reconciler{
		repo:    repo,
		chain:   chain,
		cfg:     cfg,
		logger:  logger,
		metrics: metrics,
	}
}

// ReconcileVaults fetches active vaults and reconciles ledger pool balances against on-chain state.
func (r *Reconciler) ReconcileVaults(ctx context.Context, vaultIDs []uuid.UUID, contractAddresses map[uuid.UUID]string) error {
	if !r.cfg.Enabled {
		return nil
	}

	for _, vaultID := range vaultIDs {
		contractAddr, ok := contractAddresses[vaultID]
		if !ok || contractAddr == "" {
			continue
		}

		ledgerBal, err := r.repo.GetVaultPoolBalance(ctx, vaultID)
		if err != nil {
			r.metrics.IncReconciliationRun("error")
			r.logger.Error("failed to read ledger vault pool balance for reconciliation", "vault_id", vaultID, "error", err)
			continue
		}

		onChainBal, err := r.chain.ReadVaultBalance(ctx, contractAddr)
		if err != nil {
			r.metrics.IncReconciliationRun("error")
			r.logger.Error("failed to read on-chain vault balance for reconciliation", "vault_id", vaultID, "contract", contractAddr, "error", err)
			continue
		}

		diff := ledgerBal - onChainBal
		absDiff := diff
		if absDiff < 0 {
			absDiff = -absDiff
		}

		status := "ok"
		var details string

		if absDiff > r.cfg.ToleranceStroops {
			status = "drift"
			r.metrics.IncReconciliationDrift()
			r.metrics.ObserveDriftAmount(vaultID.String(), absDiff)
			r.logger.Error("ledger reconciliation drift detected exceeding tolerance",
				"vault_id", vaultID,
				"contract_address", contractAddr,
				"ledger_balance", ledgerBal,
				"on_chain_balance", onChainBal,
				"difference", diff,
				"tolerance", r.cfg.ToleranceStroops,
			)
			details = fmt.Sprintf("drift of %d stroops exceeds tolerance %d", diff, r.cfg.ToleranceStroops)
		} else {
			r.metrics.IncReconciliationRun("ok")
			r.logger.Debug("ledger reconciliation passed", "vault_id", vaultID, "ledger_balance", ledgerBal, "on_chain_balance", onChainBal)
		}

		rec := ReconciliationRecord{
			ID:                     uuid.New(),
			VaultID:                vaultID,
			LedgerVaultPoolBalance: ledgerBal,
			OnChainBalance:         onChainBal,
			Difference:             diff,
			Tolerance:              r.cfg.ToleranceStroops,
			Status:                 status,
			Details:                details,
			CreatedAt:              time.Now(),
		}

		if err := r.repo.CreateReconciliationRecord(ctx, rec); err != nil {
			r.logger.Error("failed to persist reconciliation record", "vault_id", vaultID, "error", err)
		}
	}

	return nil
}
