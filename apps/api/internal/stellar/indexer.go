package stellar

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/systemstate"
)

func StartEventIndexer(ctx context.Context, logger *slog.Logger, db *sql.DB, sysRepo systemstate.Repository, rpcURL string) {
	if strings.TrimSpace(rpcURL) == "" {
		logger.Warn("event indexer disabled: STELLAR_RPC_URL is empty")
		return
	}

	go func() {
		client := &http.Client{Timeout: 8 * time.Second}
		ticker := time.NewTicker(6 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				startLedger, err := getLastIndexedLedger(ctx, sysRepo)
				if err != nil {
					logger.Error("event indexer failed to load cursor", "error", err)
					continue
				}

				contractIDs, err := loadVaultContractIDs(ctx, db)
				if err != nil {
					logger.Error("event indexer failed to load vault contracts", "error", err)
					continue
				}
				if len(contractIDs) == 0 {
					continue
				}

				events, latestLedger, err := fetchSorobanEvents(ctx, client, rpcURL, contractIDs, startLedger)
				if err != nil {
					logger.Error("event indexer fetch failed", "error", err)
					continue
				}

				for _, event := range events {
					processed, err := applyIndexedEvent(ctx, db, event)
					if err != nil {
						logger.Error("event indexer failed to apply event", "event_id", event.ID, "contract_id", event.ContractID, "event_type", event.EventType, "error", err)
						continue
					}
					if !processed {
						logger.Debug("event indexer skipped duplicate event", "event_id", event.ID)
					}
				}

				if err := setLastIndexedLedger(ctx, sysRepo, latestLedger); err != nil {
					logger.Error("event indexer failed to persist cursor", "ledger", latestLedger, "error", err)
				}
			}
		}
	}()
}

func loadVaultContractIDs(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(
		ctx,
		`SELECT DISTINCT contract_address FROM vaults WHERE deleted_at IS NULL AND contract_address <> ''`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	contractIDs := make([]string, 0)
	for rows.Next() {
		var contractID string
		if err := rows.Scan(&contractID); err != nil {
			return nil, err
		}
		contractIDs = append(contractIDs, contractID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return contractIDs, nil
}

type indexedEvent struct {
	ID         string
	ContractID string
	EventType  string
	Ledger     uint64
	Data       map[string]any
}

func applyIndexedEvent(ctx context.Context, db *sql.DB, event indexedEvent) (bool, error) {
	if strings.TrimSpace(event.ID) == "" {
		return false, fmt.Errorf("event id is required")
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	inserted, err := markEventProcessed(ctx, tx, event)
	if err != nil {
		return false, err
	}
	if !inserted {
		return false, tx.Commit()
	}

	switch strings.ToLower(strings.TrimSpace(event.EventType)) {
	case "pause":
		_, err := tx.ExecContext(
			ctx,
			`UPDATE vaults SET status = 'paused', updated_at = NOW() WHERE contract_address = $1 AND deleted_at IS NULL`,
			event.ContractID,
		)
		if err != nil {
			return false, err
		}
	case "unpause":
		_, err := tx.ExecContext(
			ctx,
			`UPDATE vaults SET status = 'active', updated_at = NOW() WHERE contract_address = $1 AND deleted_at IS NULL`,
			event.ContractID,
		)
		if err != nil {
			return false, err
		}
	case "deposit":
		amount, ok := extractEventAmount(event)
		if !ok {
			return false, fmt.Errorf("deposit event missing parseable amount")
		}
		_, err := tx.ExecContext(
			ctx,
			`UPDATE vaults
			 SET total_deposited = total_deposited + $1::numeric,
			     current_balance = current_balance + $1::numeric,
			     updated_at = NOW()
			 WHERE contract_address = $2 AND deleted_at IS NULL`,
			amount.String(),
			event.ContractID,
		)
		if err != nil {
			return false, err
		}
	case "withdraw", "withdrawal":
		amount, ok := extractEventAmount(event)
		if !ok {
			return false, fmt.Errorf("withdraw event missing parseable amount")
		}
		_, err := tx.ExecContext(
			ctx,
			`UPDATE vaults
			 SET current_balance = current_balance - $1::numeric,
			     updated_at = NOW()
			 WHERE contract_address = $2 AND deleted_at IS NULL`,
			amount.String(),
			event.ContractID,
		)
		if err != nil {
			return false, err
		}
	default:
		// Keep cursor continuity even for unsupported events.
	}

	return true, tx.Commit()
}

func extractEventAmount(event indexedEvent) (decimal.Decimal, bool) {
	if event.Data == nil {
		return decimal.Zero, false
	}

	for _, key := range []string{"amount", "value"} {
		raw, ok := event.Data[key]
		if !ok {
			continue
		}

		switch v := raw.(type) {
		case string:
			value, err := decimal.NewFromString(strings.TrimSpace(v))
			if err != nil {
				return decimal.Zero, false
			}
			return value, true
		case json.Number:
			value, err := decimal.NewFromString(v.String())
			if err != nil {
				return decimal.Zero, false
			}
			return value, true
		case int:
			return decimal.NewFromInt(int64(v)), true
		case int64:
			return decimal.NewFromInt(v), true
		case float64:
			// float64 only represents integers exactly up to 2^53. Soroban
			// amounts are stroops and routinely exceed that for large vault
			// deposits, so a float64 amount beyond the safe range has already
			// lost precision and would silently corrupt the stored balance.
			// Reject it (surfacing "amount not extracted") instead of writing a
			// wrong value. Amounts normally arrive as json.Number (UseNumber),
			// so this only guards stray float64 inputs.
			if v != math.Trunc(v) || math.Abs(v) > float64(1<<53) {
				return decimal.Zero, false
			}
			return decimal.NewFromInt(int64(v)), true
		}
	}

	return decimal.Zero, false
}

func fetchSorobanEvents(
	ctx context.Context,
	client *http.Client,
	rpcURL string,
	contractIDs []string,
	startLedger uint64,
) ([]indexedEvent, uint64, error) {
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      "nester-indexer",
		"method":  "getEvents",
		"params": map[string]any{
			"startLedger": startLedger,
			"filters": []map[string]any{
				{
					"type":        "contract",
					"contractIds": contractIDs,
				},
			},
			"pagination": map[string]any{"limit": 200},
		},
	})
	if err != nil {
		return nil, 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rpcURL, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, 0, fmt.Errorf("rpc returned %d: %s", resp.StatusCode, string(payload))
	}

	var rpcResp struct {
		Result struct {
			LatestLedger uint64 `json:"latestLedger"`
			Events       []struct {
				ID         string         `json:"id"`
				ContractID string         `json:"contractId"`
				Ledger     uint64         `json:"ledger"`
				Topic      []interface{}  `json:"topic"`
				Value      map[string]any `json:"value"`
			} `json:"events"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	decoder := json.NewDecoder(resp.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&rpcResp); err != nil {
		return nil, 0, err
	}
	if rpcResp.Error != nil {
		return nil, 0, fmt.Errorf("rpc error: %s", rpcResp.Error.Message)
	}

	events := make([]indexedEvent, 0, len(rpcResp.Result.Events))
	for _, raw := range rpcResp.Result.Events {
		eventType := ""
		if len(raw.Topic) > 0 {
			if topic, ok := raw.Topic[0].(string); ok {
				eventType = topic
			}
		}
		if eventType == "" {
			continue
		}
		events = append(events, indexedEvent{
			ID:         raw.ID,
			ContractID: raw.ContractID,
			EventType:  eventType,
			Ledger:     raw.Ledger,
			Data:       raw.Value,
		})
	}

	return events, rpcResp.Result.LatestLedger, nil
}

// getLastIndexedLedger reads the event-indexer cursor from system_state.
// A missing key is treated as ledger 0 (start from genesis).
func getLastIndexedLedger(ctx context.Context, sysRepo systemstate.Repository) (uint64, error) {
	raw, err := sysRepo.Get(ctx, systemstate.KeyLastLedger)
	if errors.Is(err, systemstate.ErrKeyNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	ledger, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse last ledger %q: %w", raw, err)
	}
	return ledger, nil
}

// setLastIndexedLedger persists the event-indexer cursor to system_state.
// It only advances the cursor (never moves it backwards).
func setLastIndexedLedger(ctx context.Context, sysRepo systemstate.Repository, ledger uint64) error {
	// Read current value so we can apply GREATEST semantics without a raw SQL
	// UPDATE … GREATEST.
	current, err := getLastIndexedLedger(ctx, sysRepo)
	if err != nil {
		return err
	}
	if ledger <= current {
		return nil
	}
	return sysRepo.Set(ctx, systemstate.KeyLastLedger, strconv.FormatUint(ledger, 10))
}

func markEventProcessed(ctx context.Context, tx *sql.Tx, event indexedEvent) (bool, error) {
	result, err := tx.ExecContext(ctx, `
INSERT INTO processed_events (event_id, ledger_sequence)
VALUES ($1, $2)
ON CONFLICT (event_id) DO NOTHING`,
		event.ID,
		event.Ledger,
	)
	if err != nil {
		return false, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return rowsAffected == 1, nil
}

// EventSyncer is used by the admin handler to trigger a one-shot sync.
type EventSyncer struct {
	DB      *sql.DB
	SysRepo systemstate.Repository
	RPCURL  string
	Logger  *slog.Logger
}

func (s *EventSyncer) SyncEvents(ctx context.Context) (int, error) {
	startLedger, err := getLastIndexedLedger(ctx, s.SysRepo)
	if err != nil {
		return 0, fmt.Errorf("load cursor: %w", err)
	}

	contractIDs, err := loadVaultContractIDs(ctx, s.DB)
	if err != nil {
		return 0, fmt.Errorf("load contracts: %w", err)
	}
	if len(contractIDs) == 0 {
		return 0, nil
	}

	client := &http.Client{Timeout: 30 * time.Second}
	events, latestLedger, err := fetchSorobanEvents(ctx, client, s.RPCURL, contractIDs, startLedger)
	if err != nil {
		return 0, fmt.Errorf("fetch events: %w", err)
	}

	processed := 0
	for _, event := range events {
		ok, err := applyIndexedEvent(ctx, s.DB, event)
		if err != nil {
			s.Logger.Error("admin sync: failed to apply event", "event_id", event.ID, "error", err)
			continue
		}
		if ok {
			processed++
		}
	}

	if err := setLastIndexedLedger(ctx, s.SysRepo, latestLedger); err != nil {
		s.Logger.Error("admin sync: failed to persist cursor", "ledger", latestLedger, "error", err)
	}

	return processed, nil
}