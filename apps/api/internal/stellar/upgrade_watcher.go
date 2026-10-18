package stellar

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// PendingUpgrade represents a timelocked contract upgrade tracked from on-chain
// UPGRADE events (PROP_UPG, CAN_UPG, EXEC_UPG).
type PendingUpgrade struct {
	ContractAddress string    `json:"contract_address"`
	WasmHash        string    `json:"wasm_hash"`
	ETA             int64     `json:"eta"`
	ETAISO          string    `json:"eta_iso"`
	Proposer        string    `json:"proposer"`
	Status          string    `json:"status"`
	ProposedLedger  uint64    `json:"proposed_ledger"`
	TxHash          string    `json:"tx_hash,omitempty"`
	ReleaseURL      string    `json:"release_url"`
	OccurredAt      time.Time `json:"occurred_at"`
}

// UpgradeObserver is notified after a PROP_UPG, CAN_UPG, or EXEC_UPG event
// commits so subscribed users can receive email or push notifications during
// the timelock window (#1549).
type UpgradeObserver interface {
	OnUpgradeEvent(ctx context.Context, eventType string, upgrade PendingUpgrade)
}

var knownContractCategoryTopics = map[string]bool{
	"vault":    true,
	"registry": true,
	"strategy": true,
	"access":   true,
	"breaker":  true,
	"factory":  true,
	"referral": true,
	"timelock": true,
	"adapter":  true,
	"sav_goal": true,
	"upgrade":  true,
	"mandate":  true,
}

// resolveTopicEventType extracts the action symbol from a Soroban event topic
// slice. Three-level topics follow (CONTRACT_SYMBOL, ACTION_SYMBOL, ENTITY);
// single-topic fixtures use (ACTION_SYMBOL).
func resolveTopicEventType(topics []interface{}) string {
	if len(topics) == 0 {
		return ""
	}
	first, _ := topics[0].(string)
	firstTrimmed := strings.TrimSpace(first)
	if len(topics) >= 2 && knownContractCategoryTopics[strings.ToLower(firstTrimmed)] {
		if second, ok := topics[1].(string); ok && strings.TrimSpace(second) != "" {
			return strings.TrimSpace(second)
		}
	}
	return firstTrimmed
}

// DefaultReleaseURLForWasmHash returns a link to the release/diff verification
// page for the proposed WASM hash.
func DefaultReleaseURLForWasmHash(wasmHash string) string {
	clean := strings.TrimSpace(wasmHash)
	return fmt.Sprintf("https://github.com/Suncrest-Labs/nester/releases?q=%s", clean)
}

func applyUpgradeProposed(ctx context.Context, tx *sql.Tx, event indexedEvent) error {
	wasmHash, ok := extractEventStringField(event, "wasm_hash")
	if !ok {
		return fmt.Errorf("prop_upg event missing wasm_hash")
	}
	etaDec, ok := extractEventField(event, "eta")
	if !ok {
		return fmt.Errorf("prop_upg event missing eta")
	}
	proposer, _ := extractEventStringField(event, "proposer")

	_, err := tx.ExecContext(ctx, `
INSERT INTO protocol_upgrades (contract_address, wasm_hash, eta, proposer, status, proposed_ledger, tx_hash)
VALUES ($1, $2, $3, $4, 'pending', $5, $6)
ON CONFLICT (contract_address, wasm_hash) DO UPDATE
SET status = 'pending',
    eta = EXCLUDED.eta,
    proposer = EXCLUDED.proposer,
    proposed_ledger = EXCLUDED.proposed_ledger,
    tx_hash = EXCLUDED.tx_hash,
    updated_at = NOW()`,
		event.ContractID, wasmHash, etaDec.IntPart(), proposer, event.Ledger, event.TxHash,
	)
	return err
}

func applyUpgradeCancelled(ctx context.Context, tx *sql.Tx, event indexedEvent) error {
	wasmHash, ok := extractEventStringField(event, "wasm_hash")
	if !ok {
		return fmt.Errorf("can_upg event missing wasm_hash")
	}
	cancelledBy, _ := extractEventStringField(event, "cancelled_by")

	_, err := tx.ExecContext(ctx, `
UPDATE protocol_upgrades
SET status = 'cancelled',
    cancelled_by = $1,
    updated_at = NOW()
WHERE contract_address = $2 AND wasm_hash = $3 AND status = 'pending'`,
		cancelledBy, event.ContractID, wasmHash,
	)
	return err
}

func applyUpgradeExecuted(ctx context.Context, tx *sql.Tx, event indexedEvent) error {
	wasmHash, ok := extractEventStringField(event, "wasm_hash")
	if !ok {
		return fmt.Errorf("exec_upg event missing wasm_hash")
	}
	executedBy, _ := extractEventStringField(event, "executed_by")
	execTs, _ := extractEventField(event, "execution_timestamp")

	_, err := tx.ExecContext(ctx, `
UPDATE protocol_upgrades
SET status = 'executed',
    executed_by = $1,
    executed_at_ts = $2,
    updated_at = NOW()
WHERE contract_address = $3 AND wasm_hash = $4`,
		executedBy, execTs.IntPart(), event.ContractID, wasmHash,
	)
	return err
}

func notifyUpgradeObserver(ctx context.Context, logger interface {
	Error(msg string, args ...any)
}, observer UpgradeObserver, event indexedEvent) {
	if observer == nil {
		return
	}
	evType := normalizeEventTypeString(event.EventType)
	switch evType {
	case "prop_upg", "can_upg", "exec_upg":
	default:
		return
	}

	wasmHash, ok := extractEventStringField(event, "wasm_hash")
	if !ok {
		return
	}
	etaDec, _ := extractEventField(event, "eta")
	proposer, _ := extractEventStringField(event, "proposer")
	etaSec := etaDec.IntPart()
	status := "pending"
	if evType == "can_upg" {
		status = "cancelled"
	} else if evType == "exec_upg" {
		status = "executed"
	}

	var etaISO string
	if etaSec > 0 {
		etaISO = time.Unix(etaSec, 0).UTC().Format(time.RFC3339)
	}

	upg := PendingUpgrade{
		ContractAddress: event.ContractID,
		WasmHash:        wasmHash,
		ETA:             etaSec,
		ETAISO:          etaISO,
		Proposer:        proposer,
		Status:          status,
		ProposedLedger:  event.Ledger,
		TxHash:          event.TxHash,
		ReleaseURL:      DefaultReleaseURLForWasmHash(wasmHash),
		OccurredAt:      eventOccurredAt(event),
	}

	defer func() {
		if r := recover(); r != nil && logger != nil {
			logger.Error("upgrade observer: panic", "event_id", event.ID, "recovered", r)
		}
	}()
	observer.OnUpgradeEvent(ctx, strings.ToUpper(evType), upg)
}
