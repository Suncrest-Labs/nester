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

// LagRecorder receives balance-freshness samples from the event indexer
// (nester#1056). Implemented by *metrics.Metrics; declared as an interface
// here so the stellar package does not depend on the metrics package and a
// nil recorder disables sampling without a branch at every call site.
type LagRecorder interface {
	SetIndexerLag(lagLedgers uint64)
	SetIndexerLagSampleAge(age time.Duration)
	RecordIndexerLagSampleError()
}

// StartEventIndexerWithMetrics is StartEventIndexer with balance-freshness
// instrumentation. recorder may be nil, in which case no samples are taken
// and the indexer behaves exactly as before: telemetry must never be able to
// stop the indexer from indexing.
func StartEventIndexerWithMetrics(ctx context.Context, logger *slog.Logger, db *sql.DB, sysRepo systemstate.Repository, rpcURL string, recorder LagRecorder) {
	startEventIndexer(ctx, logger, db, sysRepo, rpcURL, recorder)
}

func StartEventIndexer(ctx context.Context, logger *slog.Logger, db *sql.DB, sysRepo systemstate.Repository, rpcURL string) {
	startEventIndexer(ctx, logger, db, sysRepo, rpcURL, nil)
}

func startEventIndexer(ctx context.Context, logger *slog.Logger, db *sql.DB, sysRepo systemstate.Repository, rpcURL string, recorder LagRecorder) {
	if strings.TrimSpace(rpcURL) == "" {
		logger.Warn("event indexer disabled: STELLAR_RPC_URL is empty")
		return
	}

	// The long-running loop owns scheduling and telemetry only. All indexing
	// behaviour — cursor resolution, cold start, ordering, idempotency, and
	// the atomic cursor/balance commit — lives in EventPoller, which is the
	// unit the deterministic replay harness exercises (issue #1051).
	poller := &EventPoller{
		DB:      db,
		SysRepo: sysRepo,
		Fetcher: NewRPCEventFetcher(&http.Client{Timeout: 8 * time.Second}, rpcURL),
		Logger:  logger,
	}

	go func() {
		ticker := time.NewTicker(6 * time.Second)
		defer ticker.Stop()

		// Age of the last successful lag sample. Published on every tick so
		// that a stalled or erroring indexer makes the freshness signal
		// visibly stale instead of leaving the lag gauge frozen at a healthy
		// value, which is the one failure mode a balance-freshness SLI must
		// not have.
		lastLagSampleAt := time.Now()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Sampled before polling so a slow or failing pass shows up as
				// staleness rather than being hidden behind a frozen gauge.
				lag, lagErr := indexerLagLedgers(ctx, sysRepo, poller.Fetcher)

				if _, err := poller.PollEvents(ctx); err != nil {
					logger.Error("event indexer poll failed", "error", err)
					recordLagSampleError(recorder, lastLagSampleAt)
					continue
				}

				// A zero cursor means the indexer has never persisted a
				// ledger, so there is no meaningful lag to report yet;
				// publishing tip-minus-zero would report the entire ledger
				// history as lag and page instantly on a fresh deploy.
				if recorder != nil {
					if lagErr != nil {
						recordLagSampleError(recorder, lastLagSampleAt)
						continue
					}
					recorder.SetIndexerLag(lag)
					recorder.SetIndexerLagSampleAge(0)
					lastLagSampleAt = time.Now()
				}
			}
		}
	}()
}

// indexerLagLedgers reports how many ledgers behind the network tip the
// persisted cursor currently is.
//
// It returns an error when the cursor has never been set, because "lag" is
// undefined before the first successful index and reporting zero there would
// masquerade as a healthy indexer.
func indexerLagLedgers(ctx context.Context, sysRepo systemstate.Repository, fetcher EventFetcher) (uint64, error) {
	cursor, err := getLastIndexedLedger(ctx, sysRepo)
	if err != nil {
		return 0, err
	}
	if cursor == 0 {
		return 0, fmt.Errorf("indexer cursor not yet initialised")
	}

	tip, err := fetcher.LatestLedger(ctx)
	if err != nil {
		return 0, err
	}
	if tip <= cursor {
		return 0, nil
	}
	return tip - cursor, nil
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

// applyIndexedEvent applies one event in its own transaction, without
// touching the indexer cursor.
//
// It remains the entry point for callers that manage cursor advancement
// separately (the admin one-shot sync). The long-running poller uses
// applyIndexedEventWithCursor instead, which commits the cursor alongside the
// mutation; see poller.go for why that distinction matters.
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

	if err := applyEventMutation(ctx, tx, event); err != nil {
		return false, err
	}

	return true, tx.Commit()
}

// applyEventMutation performs the state change for a single event inside the
// caller's transaction.
//
// Split out of applyIndexedEvent so that the cursor-advancing poller path and
// the admin sync path apply events through the exact same code. Duplicating
// this switch would let the two paths drift, and the replay harness would then
// be proving the wrong one correct.
func applyEventMutation(ctx context.Context, tx *sql.Tx, event indexedEvent) error {
	switch strings.ToLower(strings.TrimSpace(event.EventType)) {
	case "pause":
		_, err := tx.ExecContext(
			ctx,
			`UPDATE vaults SET status = 'paused', updated_at = NOW() WHERE contract_address = $1 AND deleted_at IS NULL`,
			event.ContractID,
		)
		if err != nil {
			return err
		}
	case "unpause":
		_, err := tx.ExecContext(
			ctx,
			`UPDATE vaults SET status = 'active', updated_at = NOW() WHERE contract_address = $1 AND deleted_at IS NULL`,
			event.ContractID,
		)
		if err != nil {
			return err
		}
	case "deposit":
		amount, ok := extractEventAmount(event)
		if !ok {
			return fmt.Errorf("deposit event missing parseable amount")
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
			return err
		}
	case "withdraw", "withdrawal":
		amount, ok := extractEventAmount(event)
		if !ok {
			return fmt.Errorf("withdraw event missing parseable amount")
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
			return err
		}

	// Yield harvest (issue #1051). A harvest credits yield to the vault
	// without changing total_deposited: the principal the user paid in is
	// unchanged, only the earned yield and the spendable balance grow.
	case "harvest", "harvested", "yield_harvest":
		amount, ok := extractEventAmount(event)
		if !ok {
			return fmt.Errorf("harvest event missing parseable amount")
		}
		_, err := tx.ExecContext(
			ctx,
			`UPDATE vaults
			 SET yield_earned      = yield_earned + $1::numeric,
			     current_balance   = current_balance + $1::numeric,
			     last_harvested_at = NOW(),
			     updated_at        = NOW()
			 WHERE contract_address = $2 AND deleted_at IS NULL`,
			amount.String(),
			event.ContractID,
		)
		if err != nil {
			return err
		}

	// Fair-ordering emergency withdrawal queue (issue #814).
	case "emrg_reqd":
		if err := applyEmergencyQueueRequested(ctx, tx, event); err != nil {
			return err
		}
	case "emrg_fill":
		if err := applyEmergencyQueueFilled(ctx, tx, event); err != nil {
			return err
		}
	case "emrg_canc":
		if err := applyEmergencyQueueCancelled(ctx, tx, event); err != nil {
			return err
		}

	// Early-exit penalty escrow (issue #805).
	case "pnlty_chg":
		if err := applyPenaltyCharged(ctx, tx, event); err != nil {
			return err
		}
	case "pnlty_dst":
		if err := applyPenaltyDistributed(ctx, tx, event); err != nil {
			return err
		}

	// Slippage-safe multi-hop rebalance (issue #810).
	case "rebal_leg":
		if err := applyRebalanceLegExecuted(ctx, tx, event); err != nil {
			return err
		}
	case "rebal_cmp":
		if err := applyRebalanceCompleted(ctx, tx, event); err != nil {
			return err
		}

	default:
		// Keep cursor continuity even for unsupported events.
	}

	return nil
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

// extractEventField reads an arbitrary numeric field (not just "amount"/
// "value") from an event's data map, using the same tolerant type handling
// as extractEventAmount.
func extractEventField(event indexedEvent, key string) (decimal.Decimal, bool) {
	if event.Data == nil {
		return decimal.Zero, false
	}
	raw, ok := event.Data[key]
	if !ok {
		return decimal.Zero, false
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
		if v != math.Trunc(v) || math.Abs(v) > float64(1<<53) {
			return decimal.Zero, false
		}
		return decimal.NewFromInt(int64(v)), true
	}
	return decimal.Zero, false
}

// extractEventStringField reads a string-valued field (e.g. a Stellar
// address or a symbol) from an event's data map.
func extractEventStringField(event indexedEvent, key string) (string, bool) {
	if event.Data == nil {
		return "", false
	}
	raw, ok := event.Data[key]
	if !ok {
		return "", false
	}
	s, ok := raw.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return "", false
	}
	return s, true
}

// extractEventBoolField reads a boolean field, tolerating the RPC's actual
// JSON encoding of a Soroban bool.
func extractEventBoolField(event indexedEvent, key string) (bool, bool) {
	if event.Data == nil {
		return false, false
	}
	raw, ok := event.Data[key]
	if !ok {
		return false, false
	}
	b, ok := raw.(bool)
	return b, ok
}

// extractEventEnumVariant reads a unit-variant Soroban enum (e.g.
// PenaltyReason), tolerating the couple of shapes an RPC/XDR-to-JSON
// encoder might reasonably use for it: a bare string, a single-element
// array, or a single-key object.
func extractEventEnumVariant(event indexedEvent, key string) (string, bool) {
	if event.Data == nil {
		return "", false
	}
	raw, ok := event.Data[key]
	if !ok {
		return "", false
	}
	switch v := raw.(type) {
	case string:
		return v, true
	case []any:
		if len(v) == 0 {
			return "", false
		}
		s, ok := v[0].(string)
		return s, ok
	case map[string]any:
		for k := range v {
			return k, true
		}
	}
	return "", false
}

func penaltyReasonToDB(variant string) string {
	switch strings.ToLower(strings.TrimSpace(variant)) {
	case "earlywithdrawal":
		return "early_withdrawal"
	case "lockbreak":
		return "lock_break"
	case "emergencyexit":
		return "emergency_exit"
	case "weightdeviation":
		return "weight_deviation"
	default:
		return "early_withdrawal"
	}
}

// applyEmergencyQueueRequested persists a new (or extended) fair-queue
// position from an `emergency_requested` event (issue #814).
func applyEmergencyQueueRequested(ctx context.Context, tx *sql.Tx, event indexedEvent) error {
	user, ok := extractEventStringField(event, "user")
	if !ok {
		return fmt.Errorf("emrg_reqd event missing user")
	}
	seq, ok := extractEventField(event, "seq")
	if !ok {
		return fmt.Errorf("emrg_reqd event missing seq")
	}
	sharesRequested, ok := extractEventField(event, "shares_requested")
	if !ok {
		return fmt.Errorf("emrg_reqd event missing shares_requested")
	}

	_, err := tx.ExecContext(ctx, `
INSERT INTO emergency_withdrawal_queue (vault_contract_address, user_address, seq, shares_requested, shares_filled, status)
VALUES ($1, $2, $3, $4, 0, 'open')
ON CONFLICT (vault_contract_address, seq) DO UPDATE
SET shares_requested = EXCLUDED.shares_requested,
    updated_at = NOW()`,
		event.ContractID, user, seq.String(), sharesRequested.String(),
	)
	return err
}

// applyEmergencyQueueFilled updates an entry's filled amount from an
// `emergency_filled` event.
func applyEmergencyQueueFilled(ctx context.Context, tx *sql.Tx, event indexedEvent) error {
	seq, ok := extractEventField(event, "seq")
	if !ok {
		return fmt.Errorf("emrg_fill event missing seq")
	}
	fillShares, ok := extractEventField(event, "fill_shares")
	if !ok {
		return fmt.Errorf("emrg_fill event missing fill_shares")
	}
	fullyFilled, _ := extractEventBoolField(event, "fully_filled")

	status := "open"
	if fullyFilled {
		status = "filled"
	}

	_, err := tx.ExecContext(ctx, `
UPDATE emergency_withdrawal_queue
SET shares_filled = shares_filled + $1::numeric,
    status = $2,
    updated_at = NOW()
WHERE vault_contract_address = $3 AND seq = $4`,
		fillShares.String(), status, event.ContractID, seq.String(),
	)
	return err
}

// applyEmergencyQueueCancelled marks an entry cancelled from an
// `emergency_cancelled` event.
func applyEmergencyQueueCancelled(ctx context.Context, tx *sql.Tx, event indexedEvent) error {
	seq, ok := extractEventField(event, "seq")
	if !ok {
		return fmt.Errorf("emrg_canc event missing seq")
	}
	_, err := tx.ExecContext(ctx, `
UPDATE emergency_withdrawal_queue
SET status = 'cancelled', updated_at = NOW()
WHERE vault_contract_address = $1 AND seq = $2`,
		event.ContractID, seq.String(),
	)
	return err
}

// applyPenaltyCharged persists a `penalty_charged` event (issue #805).
func applyPenaltyCharged(ctx context.Context, tx *sql.Tx, event indexedEvent) error {
	user, ok := extractEventStringField(event, "user")
	if !ok {
		return fmt.Errorf("pnlty_chg event missing user")
	}
	amount, ok := extractEventField(event, "amount")
	if !ok {
		return fmt.Errorf("pnlty_chg event missing amount")
	}
	sharesBurned, _ := extractEventField(event, "shares_burned")
	reasonVariant, _ := extractEventEnumVariant(event, "reason")

	_, err := tx.ExecContext(ctx, `
INSERT INTO penalty_events (vault_contract_address, user_address, amount, shares_burned, reason, ledger_sequence)
VALUES ($1, $2, $3, $4, $5, $6)`,
		event.ContractID, user, amount.String(), sharesBurned.String(), penaltyReasonToDB(reasonVariant), event.Ledger,
	)
	return err
}

// applyPenaltyDistributed persists a `penalty_distributed` event.
func applyPenaltyDistributed(ctx context.Context, tx *sql.Tx, event indexedEvent) error {
	depositorAmount, _ := extractEventField(event, "depositor_amount")
	treasuryAmount, _ := extractEventField(event, "treasury_amount")
	retainedDust, _ := extractEventField(event, "retained_dust")

	_, err := tx.ExecContext(ctx, `
INSERT INTO penalty_distributions (vault_contract_address, depositor_amount, treasury_amount, retained_dust, ledger_sequence)
VALUES ($1, $2, $3, $4, $5)`,
		event.ContractID, depositorAmount.String(), treasuryAmount.String(), retainedDust.String(), event.Ledger,
	)
	return err
}

// applyRebalanceLegExecuted persists a `rebalance_leg_executed` event
// (issue #810) so realised slippage can be analysed off-chain.
func applyRebalanceLegExecuted(ctx context.Context, tx *sql.Tx, event indexedEvent) error {
	sourceID, ok := extractEventStringField(event, "source_id")
	if !ok {
		return fmt.Errorf("rebal_leg event missing source_id")
	}
	delta, ok := extractEventField(event, "delta")
	if !ok {
		return fmt.Errorf("rebal_leg event missing delta")
	}
	amountOut, _ := extractEventField(event, "amount_out")
	minOut, _ := extractEventField(event, "min_out")

	_, err := tx.ExecContext(ctx, `
INSERT INTO vault_rebalance_legs (vault_contract_address, source_id, delta, amount_out, min_out, ledger_sequence)
VALUES ($1, $2, $3, $4, $5, $6)`,
		event.ContractID, sourceID, delta.String(), amountOut.String(), minOut.String(), event.Ledger,
	)
	return err
}

// applyRebalanceCompleted persists the once-per-call summary emitted by
// `execute_rebalance` (issue #810). Kept in its own table rather than joined
// onto `vault_rebalance_legs` because the event carries no shared key back
// to individual legs — same reasoning as `penalty_distributions` living
// apart from `penalty_events`.
func applyRebalanceCompleted(ctx context.Context, tx *sql.Tx, event indexedEvent) error {
	planHash, ok := extractEventField(event, "plan_hash")
	if !ok {
		return fmt.Errorf("rebal_cmp event missing plan_hash")
	}
	totalValueMoved, _ := extractEventField(event, "total_value_moved")
	realizedSlippageBps, _ := extractEventField(event, "realized_slippage_bps")

	_, err := tx.ExecContext(ctx, `
INSERT INTO vault_rebalance_completions (vault_contract_address, plan_hash, total_value_moved, realized_slippage_bps, ledger_sequence)
VALUES ($1, $2, $3, $4, $5)`,
		event.ContractID, planHash.String(), totalValueMoved.String(), realizedSlippageBps.IntPart(), event.Ledger,
	)
	return err
}

// sorobanEventPageLimit is the getEvents pagination limit. It is also the
// signal for a truncated batch: a full page means more events may exist beyond
// the last one returned, so the cursor must not jump past them to the tip.
const sorobanEventPageLimit = 200

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
			"pagination": map[string]any{"limit": sorobanEventPageLimit},
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

// recordLagSampleError counts a failed freshness sample and publishes how
// stale the last good reading now is. Split out so every error path in the
// indexer loop reports staleness identically.
func recordLagSampleError(recorder LagRecorder, lastSampleAt time.Time) {
	if recorder == nil {
		return
	}

	recorder.RecordIndexerLagSampleError()
	recorder.SetIndexerLagSampleAge(time.Since(lastSampleAt))
}
