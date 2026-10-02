package stellar

import (
	"context"
	"database/sql"
	"time"
)

// ConfirmedDeposit is a vault deposit the indexer has durably applied, passed
// to a DepositObserver after the event's transaction has committed.
type ConfirmedDeposit struct {
	// VaultUserID is the owner of the vault the deposit landed in
	// (vaults.user_id), not necessarily the Stellar address that signed the
	// deposit.
	VaultUserID string
	// EventID is the indexer's event id. It is stable and unique per event,
	// so callers can derive their own idempotency key from it.
	EventID string
	// AmountUnits is the deposit amount in asset units (already converted
	// from stroops).
	AmountUnits string
	// OccurredAt is when the deposit actually happened on-chain, taken from
	// the RPC's reported ledger close time. It falls back to the time the
	// indexer observed the event when the RPC does not report one.
	OccurredAt time.Time
}

// DepositObserver is notified of vault deposits the indexer has just applied
// for the first time.
//
// It is called after the event's transaction commits, never inside it: a slow
// or failing observer must not be able to roll back a balance write or block
// the poller. It is deliberately not wired into backfill/replay (Runner in
// backfill.go) — replaying historical events out of order would need a
// streak recomputed from full history, not a naive per-event notification.
type DepositObserver interface {
	OnConfirmedDeposit(ctx context.Context, deposit ConfirmedDeposit)
}

// notifyDepositObserver looks up the vault's owner and calls the observer,
// containing any error or panic so the caller's poll/sync loop is never
// affected by observer failures.
func notifyDepositObserver(ctx context.Context, db *sql.DB, logger interface {
	Error(msg string, args ...any)
}, observer DepositObserver, event indexedEvent) {
	if observer == nil {
		return
	}
	if normalizeEventTypeString(event.EventType) != "deposit" {
		return
	}
	amount, ok := extractEventAmountUnits(event)
	if !ok {
		return
	}

	observeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var ownerID string
	err := db.QueryRowContext(observeCtx,
		`SELECT user_id FROM vaults WHERE contract_address = $1 AND deleted_at IS NULL`,
		event.ContractID,
	).Scan(&ownerID)
	if err != nil {
		if err != sql.ErrNoRows && logger != nil {
			logger.Error("deposit observer: vault owner lookup failed", "contract_id", event.ContractID, "error", err)
		}
		return
	}

	deposit := ConfirmedDeposit{
		VaultUserID: ownerID,
		EventID:     event.ID,
		AmountUnits: amount.String(),
		OccurredAt:  eventOccurredAt(event),
	}

	defer func() {
		if r := recover(); r != nil && logger != nil {
			logger.Error("deposit observer: panic", "event_id", event.ID, "recovered", r)
		}
	}()
	observer.OnConfirmedDeposit(observeCtx, deposit)
}

// eventOccurredAt returns the real on-chain deposit time when the RPC
// reported one, falling back to the time the indexer is observing the event
// otherwise (the RPC omitting ledgerClosedAt is the only case this applies
// to; it is not expected in normal operation).
func eventOccurredAt(event indexedEvent) time.Time {
	if !event.LedgerClosedAt.IsZero() {
		return event.LedgerClosedAt
	}
	return time.Now().UTC()
}
