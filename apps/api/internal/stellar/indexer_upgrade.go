package stellar

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/systemstate"
)

// ContractWasmHashKeyPrefix prefixes per-contract active WASM hash entries in
// the system_state store (issue #1523).
const ContractWasmHashKeyPrefix = "contract_wasm_hash."

var (
	// ErrUnknownEventSchema is returned when the indexer encounters an event
	// topic it does not know how to decode. The indexer halts cursor advancement
	// rather than silently skipping the event (issue #1523).
	ErrUnknownEventSchema = errors.New("stellar indexer: unknown event schema")

	// ErrEventSchemaDecode is returned when a known event type is missing
	// required fields or fails strict schema decoding (issue #1523).
	ErrEventSchemaDecode = errors.New("stellar indexer: event schema decode failure")
)

// IndexerDecodeFailuresTotal is the Prometheus counter incremented whenever an
// indexed contract event has an unknown topic or fails strict schema decoding
// (issue #1523: nester_indexer_decode_failures_total).
var IndexerDecodeFailuresTotal = prometheus.NewCounter(prometheus.CounterOpts{
	Namespace: "nester",
	Subsystem: "indexer",
	Name:      "decode_failures_total",
	Help:      "Total Soroban contract events that failed strict schema decoding or had an unknown event topic.",
})

var decodeFailuresCount atomic.Uint64

// IndexerDecodeFailuresCount returns the total number of event decode failures
// observed by this process.
func IndexerDecodeFailuresCount() uint64 {
	return decodeFailuresCount.Load()
}

// resetIndexerDecodeFailuresForTest resets the atomic counter for unit tests.
func resetIndexerDecodeFailuresForTest() {
	decodeFailuresCount.Store(0)
}

func isDecodeFailure(err error) bool {
	return errors.Is(err, ErrUnknownEventSchema) || errors.Is(err, ErrEventSchemaDecode)
}

func recordIndexerDecodeFailure(logger *slog.Logger, event indexedEvent, err error) {
	IndexerDecodeFailuresTotal.Inc()
	decodeFailuresCount.Add(1)
	if logger == nil {
		logger = slog.Default()
	}
	logger.Error(
		"PAGE: event indexer halted on unknown or malformed event schema",
		"alert", "page",
		"metric", "nester_indexer_decode_failures_total",
		"contract_id", event.ContractID,
		"event_id", event.ID,
		"event_type", event.EventType,
		"ledger", event.Ledger,
		"error", err,
	)
}

// ContractWasmHashStateKey returns the system_state key used to persist the
// active WASM hash for a contract.
func ContractWasmHashStateKey(contractID string) string {
	return ContractWasmHashKeyPrefix + strings.TrimSpace(contractID)
}

// GetContractWasmHash reads the stored active WASM hash for a contract from
// system_state.
func GetContractWasmHash(ctx context.Context, sysRepo systemstate.Repository, contractID string) (string, error) {
	if sysRepo == nil {
		return "", fmt.Errorf("systemstate repository is required")
	}
	return sysRepo.Get(ctx, ContractWasmHashStateKey(contractID))
}

// extractEventTypeFromTopics resolves the action topic from a Soroban event's
// topic list. Nester contracts emit either (action, ...) or the three-level
// (contract_symbol, action, entity) tuple documented in EVENTS.md (for example
// (UPGRADE, EXEC_UPG, caller) or (NESTER, UPGRADED, admin)).
func extractEventTypeFromTopics(topics []any) string {
	if len(topics) == 0 {
		return ""
	}
	first, _ := topics[0].(string)
	firstNorm := strings.ToUpper(strings.TrimSpace(first))
	if len(topics) > 1 {
		switch firstNorm {
		case "UPGRADE", "NESTER", "VAULT", "REGISTRY", "STRATEGY", "ACCESS", "SAV_GOAL", "MANDATE":
			if second, ok := topics[1].(string); ok && strings.TrimSpace(second) != "" {
				return strings.TrimSpace(second)
			}
		}
	}
	return strings.TrimSpace(first)
}

func extractUpgradeWasmHash(event indexedEvent) (string, bool) {
	if event.Data == nil {
		return "", false
	}
	for _, key := range []string{"wasm_hash", "new_wasm_hash", "hash", "next_version", "version"} {
		if s, ok := extractEventStringField(event, key); ok {
			return strings.TrimSpace(s), true
		}
		if raw, exists := event.Data[key]; exists {
			switch v := raw.(type) {
			case json.Number:
				if s := strings.TrimSpace(v.String()); s != "" {
					return s, true
				}
			case int, int64, uint64, uint32:
				return fmt.Sprintf("%v", v), true
			}
		}
	}
	return "", false
}

// applyUpgradeEvent persists the active WASM hash emitted by an EXEC_UPG or
// UPGRADED contract event inside the caller's transaction (issue #1523).
func applyUpgradeEvent(ctx context.Context, tx *sql.Tx, event indexedEvent) error {
	if strings.TrimSpace(event.ContractID) == "" {
		return fmt.Errorf("%w: %s event missing contract_id", ErrEventSchemaDecode, event.EventType)
	}
	wasmHash, ok := extractUpgradeWasmHash(event)
	if !ok {
		return fmt.Errorf("%w: %s event missing wasm_hash", ErrEventSchemaDecode, event.EventType)
	}

	_, err := tx.ExecContext(ctx, `
INSERT INTO system_state (key, value, updated_at)
VALUES ($1, $2, NOW())
ON CONFLICT (key) DO UPDATE
SET value = EXCLUDED.value,
    updated_at = NOW()`,
		ContractWasmHashStateKey(event.ContractID),
		wasmHash,
	)
	return err
}
