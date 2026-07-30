package reconciliation

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/transaction"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

type VaultBalanceReader interface {
	TotalAssets(ctx context.Context, contractAddress string) (decimal.Decimal, error)
}

type VaultLister interface {
	ListVaults(ctx context.Context, filter vault.ListFilter) ([]vault.Vault, int, error)
}

type BalanceComparator struct {
	Vaults     VaultLister
	Chain      VaultBalanceReader
	Classifier Classifier
	Clock      func() time.Time
}

func (c BalanceComparator) Name() string { return "vault_balance" }
func (c BalanceComparator) Level() Level { return LevelBalance }

func (c BalanceComparator) Reconcile(ctx context.Context, scope Scope) (ComparisonResult, error) {
	now := clock(c.Clock)
	filter := vault.ListFilter{Status: string(vault.StatusActive), Limit: 500, Offset: 0}
	vaults, _, err := c.Vaults.ListVaults(ctx, filter)
	if err != nil {
		return ComparisonResult{}, err
	}

	var findings []Finding
	classifier := NewClassifier(c.Classifier)
	checked := 0
	for _, v := range vaults {
		if scope.VaultID != uuid.Nil && v.ID != scope.VaultID {
			continue
		}
		checked++
		if v.ContractAddress == "" {
			continue
		}
		onChain, err := c.Chain.TotalAssets(ctx, v.ContractAddress)
		if err != nil {
			return ComparisonResult{}, err
		}
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
