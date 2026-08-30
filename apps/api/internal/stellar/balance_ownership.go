package stellar

import (
	"context"
	"database/sql"
	"strings"

	"github.com/shopspring/decimal"
)

// Balance ownership model (nester#1147).
//
// Two independent writers can credit the same vault balance columns:
//
//  1. The API write path. A user records a deposit through the API; the vault
//     repository increments vaults.total_deposited / vaults.current_balance
//     keyed on the vault id, and inserts a vault_transactions row.
//
//  2. This indexer. The corresponding on-chain event arrives and
//     applyEventMutation increments the same two columns keyed on the vault's
//     contract_address.
//
// Both describe the SAME movement of money. Before this file the two had no
// shared dedupe key — the indexer deduped only on processed_events.event_id,
// which is unique per *event*, not per deposit, and says nothing about whether
// the API already credited it. Every deposit made through the API was
// therefore counted twice: once at record time and again when its event was
// indexed.
//
// The model chosen is SHARED IDEMPOTENCY KEY, not single-writer.
//
// The key is vault_transactions.transaction_hash, which already carries a
// unique index (migration 023, column renamed in 033) and which the API
// confirmation path already claims in exactly this way
// (VaultRepository.applyConfirmedBalanceChange). Both writers now insert that
// row with ON CONFLICT (transaction_hash) DO NOTHING *before* touching a
// balance, and whichever writer loses the race applies nothing. Whoever
// observes the transaction first credits it; the other becomes a no-op.
//
// Single-writer was rejected: making the indexer the only writer would leave a
// deposit invisible until its event is indexed (seconds to minutes behind, and
// unbounded when the RPC is degraded), and making the API the only writer would
// drop every deposit made directly on-chain, which the indexer exists to catch.
// The shared key keeps both entry points live and still credits exactly once.
//
// Events that carry no transaction hash cannot participate in the shared key.
// See claimBalanceTxHash for how they are handled.

// claimBalanceTxHash claims an event's transaction hash in vault_transactions
// so that only one writer credits a given on-chain movement.
//
// It reports whether the caller should proceed with the balance mutation:
// true when this call won the claim, false when the hash was already recorded
// (by the API write path or by an earlier delivery of the event), in which
// case the balance has already moved and the caller must do nothing.
//
// An event with no transaction hash is allowed through. Such an event cannot
// have come from the API path — that path always records a hash — so it can
// only be a direct on-chain movement that nothing else will credit. Blocking
// it would silently drop real deposits, which is the worse failure; it remains
// deduped by processed_events.event_id against repeat delivery of the same
// event.
//
// The insert runs inside the caller's transaction, so a later failure rolls the
// claim back with the mutation and the event can be retried.
func claimBalanceTxHash(ctx context.Context, tx *sql.Tx, event indexedEvent, txType string, amount decimal.Decimal) (bool, error) {
	txHash := strings.TrimSpace(event.TxHash)
	if txHash == "" {
		return true, nil
	}

	result, err := tx.ExecContext(
		ctx,
		`INSERT INTO vault_transactions (vault_id, type, amount, transaction_hash)
		 SELECT id, $2, $3::numeric, $4 FROM vaults
		 WHERE contract_address = $1 AND deleted_at IS NULL
		 ON CONFLICT (transaction_hash) DO NOTHING`,
		event.ContractID,
		txType,
		amount.String(),
		txHash,
	)
	if err != nil {
		return false, err
	}

	inserted, err := result.RowsAffected()
	if err != nil {
		return false, err
	}

	// Zero rows means either the hash was already claimed (the API path
	// credited it, or this event was delivered before) or no live vault
	// matches the contract address. Neither is a case where this indexer
	// should move a balance.
	return inserted > 0, nil
}
