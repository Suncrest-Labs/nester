package stellar

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/systemstate"
)

// IndexerPollInterval is how often the indexer polls for new events.
//
// Ledgers close roughly every 5s, so polling slightly slower than that keeps
// the indexer within a ledger or two of the tip without spending an RPC round
// trip on ledgers that have not closed yet. The staleness budget in
// internal/freshness is set an order of magnitude above it.
const IndexerPollInterval = 6 * time.Second

// IndexerRequestTimeout bounds a single RPC call made by the indexer loop. It
// sits above the poll interval so a slow-but-working RPC still completes, and
// well below the point where polls would queue indefinitely. Exported so
// startup can build the indexer's client with the same bound rather than
// restating it.
const IndexerRequestTimeout = 8 * time.Second

// FreshnessRecorder receives balance-freshness samples from the event indexer
// (nester#1056, nester#1088). Implemented by *freshness.Tracker; declared as
// an interface here so the stellar package does not depend on it, and a nil
// recorder disables sampling without a branch at every call site.
type FreshnessRecorder interface {
	// Observe reports a successful sample of the indexer's position.
	Observe(indexedLedger, networkLedger uint64)
	// ObserveFailure reports a sample that could not be taken.
	ObserveFailure()
}

// IndexerOptions configures the long-running event indexer.
//
// A struct rather than more parameters: the indexer has accumulated a client,
// a retry policy, and a freshness recorder alongside its URL, and a
// seven-argument call at the wiring site is unreadable and easy to transpose.
// Every field is optional except RPCURL.
type IndexerOptions struct {
	// RPCURL is the Soroban endpoint. Empty disables the indexer.
	RPCURL string

	// HTTPClient carries the metrics and circuit-breaker transports. Nil
	// falls back to a plain client with IndexerRequestTimeout. Startup passes
	// an instrumented, circuit-broken one: the indexer is by far the heaviest
	// Soroban caller — a request every IndexerPollInterval, forever — so it is
	// the traffic most worth shedding when the RPC degrades (nester#1087).
	HTTPClient *http.Client

	// RPCOptions carries the shared retry policy (nester#1086). Both of the
	// indexer's calls are reads, so both are retried.
	RPCOptions RPCOptions

	// Recorder receives balance-freshness samples. Nil disables sampling and
	// the indexer behaves exactly as before: telemetry must never be able to
	// stop the indexer from indexing.
	Recorder FreshnessRecorder
}

// StartEventIndexer launches the long-running event indexer.
func StartEventIndexer(ctx context.Context, logger *slog.Logger, db *sql.DB, sysRepo systemstate.Repository, opts IndexerOptions) {
	if strings.TrimSpace(opts.RPCURL) == "" {
		logger.Warn("event indexer disabled: STELLAR_RPC_URL is empty")
		return
	}

	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: IndexerRequestTimeout}
	}
	recorder := opts.Recorder

	// The long-running loop owns scheduling and telemetry only. All indexing
	// behaviour — cursor resolution, cold start, ordering, idempotency, and
	// the atomic cursor/balance commit — lives in EventPoller, which is the
	// unit the deterministic replay harness exercises (issue #1051).
	poller := &EventPoller{
		DB:      db,
		SysRepo: sysRepo,
		Fetcher: NewRPCEventFetcherWithOptions(httpClient, opts.RPCURL, opts.RPCOptions),
		Logger:  logger,
	}

	go func() {
		ticker := time.NewTicker(IndexerPollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Sampled before polling, and independently of whether the
				// poll succeeds. A failing poll does not invalidate a
				// successful position read — the cursor genuinely is where it
				// says, and it is genuinely not advancing — so publishing the
				// sample is what makes the lag visibly climb during a stall
				// instead of going quiet.
				sampleFreshness(ctx, sysRepo, poller.Fetcher, recorder)

				if _, err := poller.PollEvents(ctx); err != nil {
					logger.Error("event indexer poll failed", "error", err)
				}
			}
		}
	}()
}

// sampleFreshness publishes one balance-freshness sample, or records that it
// could not be taken. A nil recorder makes it a no-op.
func sampleFreshness(ctx context.Context, sysRepo systemstate.Repository, fetcher EventFetcher, recorder FreshnessRecorder) {
	if recorder == nil {
		return
	}

	indexed, tip, err := readIndexerPosition(ctx, sysRepo, fetcher)
	if err != nil {
		recorder.ObserveFailure()
		return
	}

	recorder.Observe(indexed, tip)
}

// readIndexerPosition returns the persisted cursor and the current network
// tip.
//
// It returns an error when the cursor has never been set, because position is
// undefined before the first successful index and reporting ledger 0 would
// make the lag the entire ledger history. The caller records the failure
// instead, which ages the freshness signal — the honest signal for "this
// indexer has never run".
func readIndexerPosition(ctx context.Context, sysRepo systemstate.Repository, fetcher EventFetcher) (indexed uint64, tip uint64, err error) {
	indexed, err = getLastIndexedLedger(ctx, sysRepo)
	if err != nil {
		return 0, 0, err
	}
	if indexed == 0 {
		return 0, 0, fmt.Errorf("indexer cursor not yet initialised")
	}

	tip, err = fetcher.LatestLedger(ctx)
	if err != nil {
		return 0, 0, err
	}
	// A tip of 0 is a broken RPC, not a real position. Publishing it would
	// compute zero lag against any cursor and reset the sample age, reporting
	// the indexer as perfectly fresh while the network position is in fact
	// unknown — a false negative on exactly the signal this exists to raise.
	// resolveStartLedger refuses a zero tip for the same reason.
	if tip == 0 {
		return 0, 0, fmt.Errorf("rpc reported latest ledger 0")
	}

	return indexed, tip, nil
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
	// TxHash is the Stellar transaction that emitted the event. It is the
	// shared idempotency key between the indexer and the API write path: both
	// claim it in vault_transactions before moving a balance, so a deposit
	// made through the API and later observed on-chain is credited exactly
	// once (nester#1147). Empty for events from an RPC that did not report it.
	TxHash string
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
		amount, ok := extractEventAmountUnits(event)
		if !ok {
			return fmt.Errorf("deposit event missing parseable amount")
		}
		// The API write path may already have credited this exact transaction
		// (see the ownership model documented at the top of this file). Claim
		// the hash first; if it is already claimed, the credit has happened and
		// this event must not apply a second one (nester#1147).
		claimed, err := claimBalanceTxHash(ctx, tx, event, "deposit", amount)
		if err != nil {
			return err
		}
		if !claimed {
			return nil
		}
		_, err = tx.ExecContext(
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
		amount, ok := extractEventAmountUnits(event)
		if !ok {
			return fmt.Errorf("withdraw event missing parseable amount")
		}
		claimed, err := claimBalanceTxHash(ctx, tx, event, "withdrawal", amount)
		if err != nil {
			return err
		}
		if !claimed {
			return nil
		}
		_, err = tx.ExecContext(
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
		amount, ok := extractEventAmountUnits(event)
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

// extractEventAmountStroops reads the raw amount out of an event's data map.
//
// The value is in STROOPS, exactly as the Soroban contract emitted it. Callers
// that write a vault balance column must not use this directly — use
// extractEventAmountUnits, which applies the stroop -> asset-unit conversion
// (nester#1146).
func extractEventAmountStroops(event indexedEvent) (decimal.Decimal, bool) {
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

// extractEventAmountUnits reads an event amount and converts it from the
// stroops the contract emitted into the asset units the vault ledger stores.
//
// Every balance-writing branch of applyEventMutation goes through this, so an
// indexed 1 USDC deposit credits 1 and not 10_000_000 (nester#1146). The
// conversion itself lives in StroopsToAssetUnits, shared with the transaction
// chain-event verifier.
func extractEventAmountUnits(event indexedEvent) (decimal.Decimal, bool) {
	stroops, ok := extractEventAmountStroops(event)
	if !ok {
		return decimal.Zero, false
	}
	return StroopsToAssetUnits(stroops), true
}

// extractEventField reads an arbitrary numeric field (not just "amount"/
// "value") from an event's data map, using the same tolerant type handling
// as extractEventAmountStroops.
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
	rpc *rpcClient,
	contractIDs []string,
	startLedger uint64,
) ([]indexedEvent, uint64, error) {
	params := map[string]any{
		"startLedger": startLedger,
		"filters": []map[string]any{
			{
				"type":        "contract",
				"contractIds": contractIDs,
			},
		},
		"pagination": map[string]any{"limit": sorobanEventPageLimit},
	}

	var rpcResp struct {
		Result struct {
			LatestLedger uint64 `json:"latestLedger"`
			Events       []struct {
				ID         string         `json:"id"`
				ContractID string         `json:"contractId"`
				Ledger     uint64         `json:"ledger"`
				TxHash     string         `json:"txHash"`
				Topic      []interface{}  `json:"topic"`
				Value      map[string]any `json:"value"`
			} `json:"events"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := rpc.call(ctx, "getEvents", params, &rpcResp); err != nil {
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
			TxHash:     raw.TxHash,
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

	// RPCOptions carries the shared retry policy. Zero means package
	// defaults, which is what an EventSyncer built outside startup gets.
	RPCOptions RPCOptions
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

	rpc := newRPCClient(s.RPCURL, &http.Client{Timeout: defaultRPCTimeout}, s.RPCOptions, false)
	events, latestLedger, err := fetchSorobanEvents(ctx, rpc, contractIDs, startLedger)
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
