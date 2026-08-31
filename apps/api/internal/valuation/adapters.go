package valuation

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/portfolio"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/transaction"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

// wsPushTimeout bounds the WebSocket push in WSNotifier.PushValuation. It is
// called with a background context (the request that triggered the
// recompute has typically already returned), so without a bound a stalled
// hub could hang the call indefinitely (nester#1198).
const wsPushTimeout = 10 * time.Second

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
//
// The 10,000-vault ceiling is safe here for the same reason as
// portfolio_service.go's identical one (nester#1193): it's bounded by vault
// count, which a user can only grow through the product UI, not by
// transaction volume that grows with usage over time — unlike
// TxPendingSource.PendingDeposits below, which paginates through every row
// for exactly that reason.
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

// pendingDepositsPageSize is the page size PendingDeposits fetches at a time.
// It exists to bound single-query memory/row cost, not to cap how many
// pending deposits a user can have — PendingDeposits pages through every
// row ListUserTransactions reports via its total count (nester#1193: this
// feeds a user's valuation, so silently dropping rows past a fixed limit
// would undercount real money, not just paginate badly).
const pendingDepositsPageSize = 500

// PendingDeposits implements PendingSource. It sums every pending deposit
// for the user — see pendingDepositsPageSize's comment for why this must
// page through all of them rather than truncating at a fixed row count.
func (s *TxPendingSource) PendingDeposits(ctx context.Context, userID uuid.UUID) ([]AssetAmount, error) {
	var out []AssetAmount
	offset := 0
	for {
		txs, total, err := s.txs.ListUserTransactions(ctx, transaction.ListFilter{
			UserID: userID,
			Type:   string(transaction.TypeDeposit),
			Status: string(transaction.StatusPending),
			Limit:  pendingDepositsPageSize,
			Offset: offset,
		})
		if err != nil {
			return nil, err
		}
		if out == nil {
			out = make([]AssetAmount, 0, total)
		}
		for _, t := range txs {
			out = append(out, AssetAmount{Asset: t.Currency, Amount: t.Amount})
		}
		offset += len(txs)
		// len(txs) == 0 guards against an implementation whose total count
		// disagrees with what it actually returns, so a mismatch can never
		// spin this loop forever.
		if offset >= total || len(txs) == 0 {
			break
		}
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
	hub    UserPusher
	logger *slog.Logger
}

// UserPusher is the WebSocket hub method used to push to a single user.
type UserPusher interface {
	PushToUser(ctx context.Context, userID uuid.UUID, eventName string, payload any) error
}

// NewWSNotifier constructs a WSNotifier. A nil logger discards push failures
// (matching Service's own nil-Logger fallback in NewService).
func NewWSNotifier(hub UserPusher, logger *slog.Logger) *WSNotifier {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	return &WSNotifier{hub: hub, logger: logger}
}

// PushValuation implements Notifier by pushing a portfolio_valuation event.
func (n *WSNotifier) PushValuation(userID uuid.UUID, val portfolio.Valuation) {
	ctx, cancel := context.WithTimeout(context.Background(), wsPushTimeout)
	defer cancel()
	if err := n.hub.PushToUser(ctx, userID, "portfolio_valuation", val); err != nil {
		n.logger.Error("valuation: websocket push failed", "user_id", userID, "error", err)
	}
}

var _ Notifier = (*WSNotifier)(nil)
