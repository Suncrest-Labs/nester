package valuation

import (
	"context"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/portfolio"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/transaction"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

// VaultLister is the subset of the vault repository the position source needs.
type VaultLister interface {
	ListUserVaults(ctx context.Context, userID uuid.UUID, filter vault.UserListFilter) ([]vault.Vault, int, error)
}

// VaultPositionSource adapts the vault repository to PositionSource. Balances are
// denominated in each vault's own currency; the aggregator prices them to USDC.
// Locked is reported as zero — the ledger does not currently distinguish a locked
// tranche, so the full current value is treated as flexible.
type VaultPositionSource struct {
	vaults VaultLister
}

// NewVaultPositionSource constructs a VaultPositionSource.
func NewVaultPositionSource(vaults VaultLister) *VaultPositionSource {
	return &VaultPositionSource{vaults: vaults}
}

// Positions implements PositionSource.
func (s *VaultPositionSource) Positions(ctx context.Context, userID uuid.UUID) ([]Position, error) {
	vaults, _, err := s.vaults.ListUserVaults(ctx, userID, vault.UserListFilter{Page: 1, PerPage: 10000})
	if err != nil {
		return nil, err
	}
	out := make([]Position, 0, len(vaults))
	for _, v := range vaults {
		if v.Status == vault.StatusClosed {
			continue
		}
		out = append(out, Position{
			VaultID:   v.ID,
			Asset:     v.Currency,
			Principal: v.TotalDeposited,
			Yield:     v.YieldEarned,
			Locked:    decimal.Zero,
		})
	}
	return out, nil
}

// TxLister is the subset of the transaction repository the pending source needs.
type TxLister interface {
	ListUserTransactions(ctx context.Context, filter transaction.ListFilter) ([]transaction.Transaction, int, error)
}

// TxPendingSource adapts the transaction repository to PendingSource, reporting
// deposits still awaiting on-chain settlement. These are surfaced separately and
// are NOT counted in settled net worth.
type TxPendingSource struct {
	txs TxLister
}

// NewTxPendingSource constructs a TxPendingSource.
func NewTxPendingSource(txs TxLister) *TxPendingSource {
	return &TxPendingSource{txs: txs}
}

// PendingDeposits implements PendingSource.
func (s *TxPendingSource) PendingDeposits(ctx context.Context, userID uuid.UUID) ([]AssetAmount, error) {
	txs, _, err := s.txs.ListUserTransactions(ctx, transaction.ListFilter{
		UserID: userID,
		Type:   string(transaction.TypeDeposit),
		Status: string(transaction.StatusPending),
		Limit:  10000,
	})
	if err != nil {
		return nil, err
	}
	out := make([]AssetAmount, 0, len(txs))
	for _, t := range txs {
		out = append(out, AssetAmount{Asset: t.Currency, Amount: t.Amount})
	}
	return out, nil
}

// GoalLister is the subset of the savings-goal repository the goal source needs.
type GoalLister interface {
	ListByUser(ctx context.Context, userID uuid.UUID, category, search string) ([]savingsgoal.SavingsGoal, error)
}

// GoalAllocationSource adapts the savings-goal repository to GoalSource.
type GoalAllocationSource struct {
	goals GoalLister
}

// NewGoalAllocationSource constructs a GoalAllocationSource.
func NewGoalAllocationSource(goals GoalLister) *GoalAllocationSource {
	return &GoalAllocationSource{goals: goals}
}

// Goals implements GoalSource.
func (s *GoalAllocationSource) Goals(ctx context.Context, userID uuid.UUID) ([]GoalInput, error) {
	goals, err := s.goals.ListByUser(ctx, userID, "", "")
	if err != nil {
		return nil, err
	}
	out := make([]GoalInput, 0, len(goals))
	for _, g := range goals {
		out = append(out, GoalInput{
			GoalID:    g.ID,
			Name:      g.Name,
			Asset:     g.Currency,
			Allocated: g.CurrentAmount,
			Target:    g.TargetAmount,
		})
	}
	return out, nil
}

// WSNotifier pushes valuations to a user's WebSocket channel.
type WSNotifier struct {
	hub UserPusher
}

// UserPusher is the WebSocket hub method used to push to a single user.
type UserPusher interface {
	PushToUser(ctx context.Context, userID uuid.UUID, eventName string, payload any) error
}

// NewWSNotifier constructs a WSNotifier.
func NewWSNotifier(hub UserPusher) *WSNotifier { return &WSNotifier{hub: hub} }

// PushValuation implements Notifier by pushing a portfolio_valuation event.
func (n *WSNotifier) PushValuation(userID uuid.UUID, val portfolio.Valuation) {
	_ = n.hub.PushToUser(context.Background(), userID, "portfolio_valuation", val)
}

var _ Notifier = (*WSNotifier)(nil)
