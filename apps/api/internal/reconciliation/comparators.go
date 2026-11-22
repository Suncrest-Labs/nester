package reconciliation

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/transaction"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

type VaultBalanceReader interface {
	TotalAssets(ctx context.Context, contractAddress string) (decimal.Decimal, error)
}

// USDCBalanceReader reads the USDC balance of an address from the USDC SAC contract.
type USDCBalanceReader interface {
	USDCBalance(ctx context.Context, address string) (decimal.Decimal, error)
}

// VaultAdapterPositionReader reads deployed adapter positions from a vault contract.
type VaultAdapterPositionReader interface {
	AdapterPositions(ctx context.Context, vaultAddress string) (map[string]decimal.Decimal, error)
}

type VaultLister interface {
	ListVaults(ctx context.Context, filter vault.ListFilter) ([]vault.Vault, int, error)
}

type BalanceComparator struct {
	Vaults     VaultLister
	Chain      VaultBalanceReader
	Classifier Classifier
	Clock      func() time.Time
	// Logger records per-vault chain-read failures (nil falls back to
	// slog.Default). Those are logged and skipped rather than failing the
	// run — see Reconcile — so the log line is the only trace of a vault
	// that could not be compared.
	Logger *slog.Logger
}

func (c BalanceComparator) Name() string { return "vault_balance" }
func (c BalanceComparator) Level() Level { return LevelBalance }

// vaultReconcilePageSize is the page size Reconcile fetches at a time. It
// bounds single-query memory/row cost, not how many active vaults are
// checked — Reconcile pages through every vault ListVaults reports via its
// total count. Reconciliation exists specifically to catch balances that
// have drifted from on-chain state; checking only the first page silently
// shrinks the fraction of the book actually verified as the vault count
// grows past it, while every run still reports success (nester#1192).
const vaultReconcilePageSize = 500

func (c BalanceComparator) Reconcile(ctx context.Context, scope Scope) (ComparisonResult, error) {
	now := clock(c.Clock)
	logger := c.Logger
	if logger == nil {
		logger = slog.Default()
	}

	var findings []Finding
	classifier := NewClassifier(c.Classifier)
	checked := 0
	offset := 0
	// Per-vault chain-read failures are logged and skipped rather than
	// failing the run (nester#1082): contract addresses are caller-supplied
	// at vault creation, so a single unreadable contract — uninitialized,
	// malformed, or holding an i128 balance past the reader's int64 range —
	// must not become a standing denial of the whole safety net, dropping
	// every finding already collected each pass. The false-clean trap is
	// guarded below: if every attempted read failed the run still fails,
	// so an RPC outage or open circuit breaker cannot read as "all clear".
	readFailures := 0
	firstReadErr := error(nil)
	attemptedReads := 0
	for {
		filter := vault.ListFilter{Status: string(vault.StatusActive), Limit: vaultReconcilePageSize, Offset: offset}
		vaults, total, err := c.Vaults.ListVaults(ctx, filter)
		if err != nil {
			// A page-fetch failure mid-run fails the whole run rather than
			// silently returning only what was checked so far as if it were
			// complete (nester#1192's fourth acceptance criterion).
			return ComparisonResult{Checked: checked}, err
		}

		for _, v := range vaults {
			if scope.VaultID != uuid.Nil && v.ID != scope.VaultID {
				continue
			}
			if v.ContractAddress == "" {
				checked++
				continue
			}
			attemptedReads++
			onChain, err := c.Chain.TotalAssets(ctx, v.ContractAddress)
			if err != nil {
				// Shutdown is not a per-vault condition: propagate so the
				// engine records the aborted run instead of a sweep that
				// skips every remaining vault and completes.
				if ctx.Err() != nil {
					return ComparisonResult{Checked: checked}, err
				}
				readFailures++
				if firstReadErr == nil {
					firstReadErr = err
				}
				logger.Warn("reconciliation: chain read failed; vault skipped this pass",
					"vault_id", v.ID.String(),
					"contract_address", v.ContractAddress,
					"error", err,
				)
				continue
			}
			checked++
			if !v.CurrentBalance.Equal(onChain) {
				recorded := v.CurrentBalance
				chain := onChain
				findings = append(findings, classifier.Classify(FindingInput{
					Level:         LevelBalance,
					Type:          TypeMismatch,
					EntityType:    "vault",
					EntityID:      v.ID.String(),
					RecordedValue: &recorded,
					OnChainValue:  &chain,
					Details: map[string]string{
						"contract_address": v.ContractAddress,
						"currency":         v.Currency,
					},
				}, now))
			}
		}

		offset += len(vaults)
		// len(vaults) == 0 guards against an implementation whose total
		// count disagrees with what it actually returns, so a mismatch can
		// never spin this loop forever.
		if offset >= total || len(vaults) == 0 {
			break
		}
	}

	// Every attempted read failing is not a set of per-vault problems — it is
	// the chain-read path being down (RPC outage, open breaker, bad config).
	// Completing the run would report zero divergences because nothing was
	// compared, the exact false-clean reading this system must never produce.
	if attemptedReads > 0 && readFailures == attemptedReads {
		return ComparisonResult{Checked: checked},
			fmt.Errorf("all %d chain reads failed; first error: %w", readFailures, firstReadErr)
	}
	if readFailures > 0 {
		// checked_count on the run row reflects only vaults actually
		// compared (or empty-address rows with nothing to compare), so a
		// partial sweep is visible as checked < active vault count.
		logger.Warn("reconciliation: pass completed with partial coverage",
			"checked", checked,
			"read_failures", readFailures,
		)
	}

	return ComparisonResult{Checked: checked, Findings: findings}, nil
}

// VaultAssetCompositionComparator verifies that a vault's total_assets equals
// the sum of its USDC balance and deployed adapter positions (nester#1421).
//
// This is the independent check that catches exploits and accounting bugs: the
// vault reports one total, but when we sum what it actually holds (USDC SAC
// balance + adapter positions), the numbers must agree within a tolerance
// measured in basis points.
//
// Unlike BalanceComparator (which compares DB vs chain), this compares chain
// against chain — no database involved. Both values are authoritative on-chain
// reads, so disagreement means either:
//   - The vault's accounting is broken (bug in total_assets logic)
//   - An exploit has drained funds without updating total_assets
//   - An adapter is reporting incorrect positions
type VaultAssetCompositionComparator struct {
	Vaults           VaultLister
	Chain            VaultBalanceReader
	USDCReader       USDCBalanceReader
	AdapterReader    VaultAdapterPositionReader
	Classifier       Classifier
	Clock            func() time.Time
	Logger           *slog.Logger
	// DivergenceThresholdBPS is the maximum acceptable difference between
	// total_assets and (USDC + adapters), in basis points (1 bp = 0.01%).
	// A vault reporting 1000 USDC with a 10bp threshold can diverge by up to
	// 1 USDC (1000 * 0.001 = 1) before alerting.
	DivergenceThresholdBPS int64
}

func (c VaultAssetCompositionComparator) Name() string { return "vault_asset_composition" }
func (c VaultAssetCompositionComparator) Level() Level { return LevelBalance }

func (c VaultAssetCompositionComparator) Reconcile(ctx context.Context, scope Scope) (ComparisonResult, error) {
	now := clock(c.Clock)
	logger := c.Logger
	if logger == nil {
		logger = slog.Default()
	}

	thresholdBPS := c.DivergenceThresholdBPS
	if thresholdBPS <= 0 {
		thresholdBPS = 10 // Default 10 basis points (0.1%)
	}

	var findings []Finding
	classifier := NewClassifier(c.Classifier)
	checked := 0
	offset := 0
	readFailures := 0
	firstReadErr := error(nil)
	attemptedReads := 0

	for {
		filter := vault.ListFilter{Status: string(vault.StatusActive), Limit: vaultReconcilePageSize, Offset: offset}
		vaults, total, err := c.Vaults.ListVaults(ctx, filter)
		if err != nil {
			return ComparisonResult{Checked: checked}, err
		}

		for _, v := range vaults {
			if scope.VaultID != uuid.Nil && v.ID != scope.VaultID {
				continue
			}
			if v.ContractAddress == "" {
				checked++
				continue
			}
			attemptedReads++

			// Read vault's reported total_assets (raw stroops)
			totalAssets, err := c.Chain.TotalAssets(ctx, v.ContractAddress)
			if err != nil {
				if ctx.Err() != nil {
					return ComparisonResult{Checked: checked}, err
				}
				readFailures++
				if firstReadErr == nil {
					firstReadErr = err
				}
				logger.Warn("vault asset composition: total_assets read failed; vault skipped",
					"vault_id", v.ID.String(),
					"contract_address", v.ContractAddress,
					"error", err,
				)
				continue
			}

			// Read USDC balance of the vault address from USDC SAC
			usdcBalance, err := c.USDCReader.USDCBalance(ctx, v.ContractAddress)
			if err != nil {
				if ctx.Err() != nil {
					return ComparisonResult{Checked: checked}, err
				}
				readFailures++
				if firstReadErr == nil {
					firstReadErr = err
				}
				logger.Warn("vault asset composition: USDC balance read failed; vault skipped",
					"vault_id", v.ID.String(),
					"contract_address", v.ContractAddress,
					"error", err,
				)
				continue
			}

			// Read deployed adapter positions
			adapterPositions, err := c.AdapterReader.AdapterPositions(ctx, v.ContractAddress)
			if err != nil {
				if ctx.Err() != nil {
					return ComparisonResult{Checked: checked}, err
				}
				readFailures++
				if firstReadErr == nil {
					firstReadErr = err
				}
				logger.Warn("vault asset composition: adapter positions read failed; vault skipped",
					"vault_id", v.ID.String(),
					"contract_address", v.ContractAddress,
					"error", err,
				)
				continue
			}

			// Sum adapter positions
			adapterTotal := decimal.Zero
			for _, amount := range adapterPositions {
				adapterTotal = adapterTotal.Add(amount)
			}

			// Calculate actual composition: USDC + adapters
			actualComposition := usdcBalance.Add(adapterTotal)

			// Calculate divergence in basis points
			// delta / totalAssets * 10000 = basis points
			delta := totalAssets.Sub(actualComposition).Abs()
			var divergenceBPS decimal.Decimal
			if !totalAssets.IsZero() {
				divergenceBPS = delta.Div(totalAssets).Mul(decimal.NewFromInt(10000))
			} else if !actualComposition.IsZero() {
				// If total_assets is zero but actual composition isn't, that's 100% divergence
				divergenceBPS = decimal.NewFromInt(10000)
			}

			checked++

			// Check if divergence exceeds threshold
			if divergenceBPS.Cmp(decimal.NewFromInt(thresholdBPS)) > 0 {
				reported := totalAssets
				actual := actualComposition
				findings = append(findings, classifier.Classify(FindingInput{
					Level:         LevelBalance,
					Type:          TypeMismatch,
					EntityType:    "vault_composition",
					EntityID:      v.ID.String(),
					RecordedValue: &reported, // total_assets (what vault reports)
					OnChainValue:  &actual,   // USDC + adapters (what it actually holds)
					Details: map[string]string{
						"contract_address":   v.ContractAddress,
						"currency":           v.Currency,
						"total_assets":       totalAssets.String(),
						"usdc_balance":       usdcBalance.String(),
						"adapter_total":      adapterTotal.String(),
						"actual_composition": actualComposition.String(),
						"divergence_bps":     divergenceBPS.StringFixed(2),
						"threshold_bps":      decimal.NewFromInt(thresholdBPS).String(),
					},
				}, now))

				logger.Warn("vault asset composition divergence detected",
					"vault_id", v.ID.String(),
					"contract_address", v.ContractAddress,
					"total_assets", totalAssets.String(),
					"usdc_balance", usdcBalance.String(),
					"adapter_total", adapterTotal.String(),
					"divergence_bps", divergenceBPS.StringFixed(2),
				)
			}
		}

		offset += len(vaults)
		if offset >= total || len(vaults) == 0 {
			break
		}
	}

	// All reads failing is infrastructure down, not per-vault problems
	if attemptedReads > 0 && readFailures == attemptedReads {
		return ComparisonResult{Checked: checked},
			fmt.Errorf("all %d vault composition reads failed; first error: %w", readFailures, firstReadErr)
	}
	if readFailures > 0 {
		logger.Warn("vault asset composition: pass completed with partial coverage",
			"checked", checked,
			"read_failures", readFailures,
		)
	}

	return ComparisonResult{Checked: checked, Findings: findings}, nil
}

type TransactionStatusReader interface {
	Status(ctx context.Context, txHash string) (transaction.TransactionStatus, bool, error)
}

type PendingTransactionLister interface {
	ListPendingOlderThan(ctx context.Context, cutoff time.Time) ([]transaction.Transaction, error)
}

type TransactionComparator struct {
	Transactions PendingTransactionLister
	Chain        TransactionStatusReader
	Classifier   Classifier
	PendingAfter  time.Duration
	Clock        func() time.Time
}

func (c TransactionComparator) Name() string { return "transaction_status" }
func (c TransactionComparator) Level() Level { return LevelTransaction }

func (c TransactionComparator) Reconcile(ctx context.Context, scope Scope) (ComparisonResult, error) {
	pendingAfter := c.PendingAfter
	if pendingAfter <= 0 {
		pendingAfter = 30 * time.Minute
	}
	now := clock(c.Clock)
	pending, err := c.Transactions.ListPendingOlderThan(ctx, now.Add(-pendingAfter))
	if err != nil {
		return ComparisonResult{}, err
	}

	classifier := NewClassifier(c.Classifier)
	var findings []Finding
	for _, tx := range pending {
		status, found, err := c.Chain.Status(ctx, tx.TxHash)
		if err != nil {
			return ComparisonResult{}, err
		}
		if found && status != transaction.StatusPending {
			findings = append(findings, classifier.Classify(FindingInput{
				Level:      LevelTransaction,
				Type:       TypeStuck,
				EntityType: "transaction",
				EntityID:   tx.TxHash,
				Age:        now.Sub(tx.CreatedAt),
				Details: map[string]string{
					"recorded_status": string(tx.Status),
					"on_chain_status": string(status),
				},
			}, now))
		}
	}
	return ComparisonResult{Checked: len(pending), Findings: findings}, nil
}

type InvariantCheck func(ctx context.Context, scope Scope, classifier Classifier, observedAt time.Time) (ComparisonResult, error)

type InvariantComparator struct {
	Check      InvariantCheck
	Classifier Classifier
	Clock      func() time.Time
}

func (c InvariantComparator) Name() string { return "protocol_invariant" }
func (c InvariantComparator) Level() Level { return LevelInvariant }

func (c InvariantComparator) Reconcile(ctx context.Context, scope Scope) (ComparisonResult, error) {
	if c.Check == nil {
		return ComparisonResult{}, nil
	}
	return c.Check(ctx, scope, NewClassifier(c.Classifier), clock(c.Clock))
}

func clock(now func() time.Time) time.Time {
	if now == nil {
		return time.Now().UTC()
	}
	return now().UTC()
}
