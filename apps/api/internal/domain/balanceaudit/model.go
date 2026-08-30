// Package balanceaudit is the append-only ledger of every balance-changing
// vault operation (nester#1124): deposits, withdrawals, harvests, and
// rebalance legs. Kept dependency-free, like domain/moneypath and
// domain/caps, so the service layer and the postgres repository can both
// depend on it without a repository -> service import cycle.
//
// This is deliberately lighter than a full double-entry ledger: one row per
// operation with an explicit before/after balance, not a set of debit/credit
// postings across accounts. That is enough to answer the two questions a
// launch needs answered — "what happened to this user's balance, and does it
// reconcile to what we show them today" — without building ledger machinery
// the team doesn't yet need.
//
// Retention: rows are kept indefinitely; see the comment on the
// balance_audit_log table (migration 110) for the growth-rate rationale.
package balanceaudit

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Operation names the kind of balance change. Kept as a plain string (not a
// closed enum) so a new operation never requires a migration — the same
// choice already made for vault.TransactionRecord's transaction type.
type Operation string

const (
	OperationDeposit           Operation = "deposit"
	OperationWithdrawal        Operation = "withdrawal"
	OperationHarvest           Operation = "harvest"
	OperationRebalanceWithdraw Operation = "rebalance_withdraw"
	OperationRebalanceDeposit  Operation = "rebalance_deposit"
	OperationEmergencyWithdraw Operation = "emergency_withdraw"
	// OperationOpeningBalance marks the one immutable entry migration 110
	// inserts per pre-existing vault, recording whatever balance it already
	// held before the audit trail started (before=0, after=current balance
	// at migration time). Without it, Reconcile (which sums from zero) would
	// omit that pre-existing balance for every vault created before this
	// ledger existed.
	OperationOpeningBalance Operation = "opening_balance"
)

// SystemActor prefixes a non-user actor, e.g. "system:harvest".
func SystemActor(source string) string { return "system:" + source }

// Entry is a single append-only row in balance_audit_log.
type Entry struct {
	ID uuid.UUID
	// VaultID identifies the vault whose balance changed.
	VaultID uuid.UUID
	// UserID is the vault owner, i.e. whose balance this is — distinct from
	// Actor, which is who caused the change (a user action vs. a background
	// job acting on their behalf, e.g. an auto-harvest).
	UserID uuid.UUID
	// Actor is the user id (as text) or a SystemActor label.
	Actor string
	// Operation is what kind of change this was.
	Operation Operation
	// Amount is the magnitude of the change (always positive; the sign is
	// implied by Operation and by BalanceAfter - BalanceBefore).
	Amount decimal.Decimal
	// BalanceBefore / BalanceAfter are the vault's current_balance
	// immediately before and after this operation was applied.
	BalanceBefore decimal.Decimal
	BalanceAfter  decimal.Decimal
	// ChainReference is the on-chain transaction hash this change
	// corresponds to, when one exists.
	ChainReference string
	// Metadata is optional free-form context (e.g. share price at time,
	// protocol names for a rebalance leg). Never PII or secrets.
	Metadata  map[string]any
	CreatedAt time.Time
}

// ErrNotFound is returned when no entries exist for a lookup.
var ErrNotFound = errors.New("balance audit: no entries found")

// ErrReconciliationGap is returned by Reconcile when the entry chain for a
// vault is broken: some entry's BalanceBefore does not match the prior
// entry's BalanceAfter, meaning a balance-changing operation happened that
// was never recorded (or was recorded out of order). Summing such a ledger
// anyway can silently produce a total that happens to match the live
// balance despite the chain being wrong, which is exactly the failure mode
// this check exists to catch.
var ErrReconciliationGap = errors.New("balance audit: reconciliation gap in entry chain")

// DefaultListLimit is the page size ListByVault/ListByUser callers get when
// they don't yet need to page (mirrors reconciliation.vaultReconcilePageSize's
// role: a generous default rather than an unbounded SELECT against an
// append-only table that only grows).
const DefaultListLimit = 500

// Repository is the persistence port. Deliberately exposes no Update or
// Delete method — see the package doc. Append is the only write.
type Repository interface {
	// Append inserts a new entry. The row is immutable once written.
	Append(ctx context.Context, entry Entry) (Entry, error)
	// ListByVault returns up to limit entries for a vault starting at offset,
	// oldest first — the order needed to replay the ledger and reconstruct
	// balance history — plus the total entry count so a caller can keep
	// paging (the same Limit/Offset/total shape reconciliation.Reconcile
	// uses to page through vault.ListVaults). Returns ErrNotFound when the
	// vault has no entries.
	ListByVault(ctx context.Context, vaultID uuid.UUID, limit, offset int) ([]Entry, int, error)
	// ListByUser returns up to limit entries across all of a user's vaults
	// starting at offset, oldest first, plus the total entry count. Returns
	// ErrNotFound when the user has no entries.
	ListByUser(ctx context.Context, userID uuid.UUID, limit, offset int) ([]Entry, int, error)
}

// Reconcile replays entries (which must already be ordered oldest-first, as
// ListByVault/ListByUser return them) and returns the balance implied by
// summing every recorded change from zero. Comparing the result to the
// vault's live current_balance is the reconciliation check (nester#1124):
// equal means the audit trail fully accounts for the current balance.
//
// Before summing, Reconcile walks the entries and validates chain
// continuity: every entry's BalanceBefore must equal the previous entry's
// BalanceAfter (the first entry's BalanceBefore is the opening balance and
// is accepted as-is). Without this check, a gap — a balance change that
// happened but was never recorded, or entries applied out of order — can
// still sum to a total that coincidentally matches the live balance,
// silently hiding a broken chain. ErrReconciliationGap is returned the
// moment such a mismatch is found, per vault, so callers know the trail
// cannot be trusted rather than trusting a possibly-wrong total.
//
// This is only correct because migration 110 inserts an OperationOpeningBalance
// entry (before=0, after=current balance at migration time) for every vault
// that already existed when the ledger table was created — otherwise summing
// from zero would omit whatever balance a pre-existing vault already held.
// Every vault, including ones created after migration 110, therefore has an
// unbroken chain of entries back to a true balance-before-history of zero.
func Reconcile(entries []Entry) (decimal.Decimal, error) {
	total := decimal.Zero
	prevByVault := make(map[uuid.UUID]decimal.Decimal, 1)
	seen := make(map[uuid.UUID]bool, 1)
	for _, e := range entries {
		if seen[e.VaultID] {
			if !e.BalanceBefore.Equal(prevByVault[e.VaultID]) {
				return decimal.Zero, fmt.Errorf("%w: vault %s entry balance_before %s does not match prior balance_after %s",
					ErrReconciliationGap, e.VaultID, e.BalanceBefore, prevByVault[e.VaultID])
			}
		} else {
			seen[e.VaultID] = true
		}
		prevByVault[e.VaultID] = e.BalanceAfter
		total = total.Add(e.BalanceAfter.Sub(e.BalanceBefore))
	}
	return total, nil
}
