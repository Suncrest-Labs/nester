package stellar

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/backfill"
)

// backfillSafetyMargin bounds how close to the network head a backfill may
// touch. The forward indexer owns recent ledgers, which are still
// reorg-vulnerable; a backfill must operate only on already-final history
// (#840's "never touch the reorg window" requirement).
const backfillSafetyMargin = 100

// backfillBatchLedgers bounds how many ledgers' worth of events one RPC
// round-trip + apply pass covers before a progress checkpoint is persisted
// and the throttle sleep runs. Small enough that a crash loses at most one
// batch of work, large enough to not thrash the RPC endpoint.
const backfillBatchLedgers = 50

// ErrTooCloseToHead is returned when the requested range (or its unresolved
// remainder on resume) reaches into the safety margin from the chain head.
var ErrTooCloseToHead = fmt.Errorf("backfill range is within %d ledgers of the network head", backfillSafetyMargin)

// resettableEventTypes are the only event types ModeRebuild is permitted to
// reset+reprocess. Restricted to handlers that are PURE INSERTs into
// tables no other handler ever UPDATEs — genuinely idempotent to delete and
// replay. Excluded, deliberately:
//
//   - "deposit"/"withdraw": vaults.total_deposited/current_balance are
//     incremental (+=/-=), not absolute writes — replaying them after a
//     reset would double-count everything applied before the reset, since
//     there is no captured "value before this range" to restore.
//   - "pause"/"unpause": vaults.status is a last-write-wins UPDATE with no
//     event-scoped row to delete; safe to backfill (idempotent — the same
//     event sets the same status every time) but not meaningfully
//     "resettable" since there's nothing to clear.
//   - "emrg_reqd"/"emrg_fill"/"emrg_canc": emergency_withdrawal_queue rows
//     are shared across all three event types for the same (contract, seq),
//     and emrg_fill increments shares_filled — a reset would need to
//     coordinate clearing all three event types' effects on the same row
//     together, not per-type. Out of scope for this issue; backfill (not
//     rebuild) still works for these since forward dedup makes it safe.
//
// deleteSQL filters on ledger_sequence (added by migration 072 specifically
// for this) as well as contract, so a rebuild clears only rows from events
// within the requested range — not every row ever recorded for the
// contract. Without a ledger column, the tables' only other timestamp is
// occurred_at (insert time), which cannot be mapped back to a ledger range
// (a backfill run long after the fact would insert recent-occurred_at rows
// for old ledgers).
var resettableEventTypes = map[string]resettableEventType{
	"pnlty_chg": {table: "penalty_events", deleteSQL: `DELETE FROM penalty_events WHERE vault_contract_address = ANY($1) AND ledger_sequence BETWEEN $2 AND $3`},
	"pnlty_dst": {table: "penalty_distributions", deleteSQL: `DELETE FROM penalty_distributions WHERE vault_contract_address = ANY($1) AND ledger_sequence BETWEEN $2 AND $3`},
	"rebal_leg": {table: "vault_rebalance_legs", deleteSQL: `DELETE FROM vault_rebalance_legs WHERE vault_contract_address = ANY($1) AND ledger_sequence BETWEEN $2 AND $3`},
	"rebal_cmp": {table: "vault_rebalance_completions", deleteSQL: `DELETE FROM vault_rebalance_completions WHERE vault_contract_address = ANY($1) AND ledger_sequence BETWEEN $2 AND $3`},
}

type resettableEventType struct {
	table     string
	deleteSQL string
}

// NonResettableEventTypesInRange reports which of the classically
// incremental/shared event types (deposit, withdraw, and the emergency
// queue trio) are present in [from, to] for contractIDs. Rebuild refuses to
// proceed if any are found — see resettableEventTypes' doc comment for why.
// Exported for the admin handler to surface a clear pre-flight error rather
// than let Run fail mid-way.
func NonResettableEventTypesInRange(ctx context.Context, client *http.Client, rpcURL string, contractIDs []string, from, to uint64) ([]string, error) {
	// A pre-flight read on the admin path, so it takes the package's default
	// retry policy rather than threading the configured one through the admin
	// handler's signature for a call that runs once per operator action.
	rpc := newRPCClient(rpcURL, client, RPCOptions{}, false)

	found := map[string]bool{}
	cursor := from
	for cursor <= to {
		page, err := fetchSorobanEventsRange(ctx, rpc, contractIDs, cursor, to)
		if err != nil {
			return nil, err
		}
		for _, e := range page.Events {
			t := strings.ToLower(strings.TrimSpace(e.EventType))
			if _, resettable := resettableEventTypes[t]; !resettable && t != "" {
				found[t] = true
			}
		}
		if !page.HasMore {
			break
		}
		cursor = page.NextLedger
	}
	out := make([]string, 0, len(found))
	for t := range found {
		out = append(out, t)
	}
	return out, nil
}

// Runner executes backfill/rebuild runs (#840). It reuses applyIndexedEvent
// — the exact same handler dispatch the forward indexer uses — so
// backfilled and live-indexed events are processed identically; this is not
// a separate code path that could drift from live behavior.
type Runner struct {
	DB     *sql.DB
	Repo   backfill.Repository
	Client *http.Client
	RPCURL string
	Logger *slog.Logger

	// RPCOptions carries the shared retry policy (nester#1086). Every call a
	// backfill makes is getEvents, so all of it is retryable.
	RPCOptions RPCOptions

	// throttle is the pause between batches, giving live indexing priority
	// for database/RPC capacity (#840's non-interference requirement).
	// Defaults to 500ms when zero.
	Throttle time.Duration
}

// StartInput are the operator-supplied parameters for a new run.
type StartInput struct {
	FromLedger  uint64
	ToLedger    uint64
	ContractIDs []string // empty = all vault contracts, same default as the forward indexer
	Mode        backfill.Mode
	DryRun      bool
	InitiatedBy string
}

// Start validates and persists a new run, then executes it synchronously to
// completion (or failure). Callers that want a run in the background should
// invoke Start from their own goroutine — this mirrors EventSyncer.SyncEvents'
// synchronous shape, which the admin handler already knows how to wrap.
func (r *Runner) Start(ctx context.Context, in StartInput) (*backfill.Run, error) {
	if in.ToLedger < in.FromLedger {
		return nil, fmt.Errorf("to_ledger must be >= from_ledger")
	}
	if strings.TrimSpace(in.InitiatedBy) == "" {
		return nil, fmt.Errorf("initiated_by is required for audit")
	}
	if in.Mode == "" {
		in.Mode = backfill.ModeBackfill
	}
	if in.Mode != backfill.ModeBackfill && in.Mode != backfill.ModeRebuild {
		return nil, fmt.Errorf("unknown mode %q (want %q or %q)", in.Mode, backfill.ModeBackfill, backfill.ModeRebuild)
	}

	contractIDs := in.ContractIDs
	if len(contractIDs) == 0 {
		ids, err := loadVaultContractIDs(ctx, r.DB)
		if err != nil {
			return nil, fmt.Errorf("load contract ids: %w", err)
		}
		contractIDs = ids
	}

	if in.Mode == backfill.ModeRebuild {
		bad, err := NonResettableEventTypesInRange(ctx, r.httpClient(), r.RPCURL, contractIDs, in.FromLedger, in.ToLedger)
		if err != nil {
			return nil, fmt.Errorf("pre-flight scan for rebuild safety: %w", err)
		}
		if len(bad) > 0 {
			return nil, fmt.Errorf("rebuild refused: range contains non-resettable event types %v (see resettableEventTypes doc comment) — use mode=backfill instead", bad)
		}
	}

	run := &backfill.Run{
		FromLedger:  in.FromLedger,
		ToLedger:    in.ToLedger,
		ContractIDs: contractIDs,
		Mode:        in.Mode,
		DryRun:      in.DryRun,
		InitiatedBy: in.InitiatedBy,
	}
	if err := r.Repo.Create(ctx, run); err != nil {
		return nil, fmt.Errorf("create run: %w", err)
	}

	if err := r.execute(ctx, run); err != nil {
		if failErr := r.Repo.Fail(ctx, run.ID, err.Error()); failErr != nil {
			r.logger().Error("backfill: failed to persist failure state", "run_id", run.ID, "error", failErr)
		}
		run.Status = backfill.StatusFailed
		run.LastError = err.Error()
		return run, err
	}
	return run, nil
}

// Resume continues a previously started run from its last checkpoint —
// resumability after a crash (#840). Only meaningful for a run whose
// status is "running" (i.e. it was interrupted, not completed/failed cleanly).
func (r *Runner) Resume(ctx context.Context, runID uuid.UUID) (*backfill.Run, error) {
	run, err := r.Repo.GetByID(ctx, runID)
	if err != nil {
		return nil, err
	}
	if run.Status != backfill.StatusRunning {
		return run, fmt.Errorf("run %s is not resumable (status=%s)", runID, run.Status)
	}
	if err := r.execute(ctx, run); err != nil {
		if failErr := r.Repo.Fail(ctx, run.ID, err.Error()); failErr != nil {
			r.logger().Error("backfill: failed to persist failure state", "run_id", run.ID, "error", failErr)
		}
		run.Status = backfill.StatusFailed
		run.LastError = err.Error()
		return run, err
	}
	return run, nil
}

// execute drives one run (fresh or resumed) from run.ResumeFrom() through
// run.ToLedger, checkpointing after every batch.
func (r *Runner) execute(ctx context.Context, run *backfill.Run) error {
	rpc := r.rpcClient()

	if run.Mode == backfill.ModeRebuild && run.ResumeFrom() == run.FromLedger {
		// Reset scope is applied once, at the very start of a fresh run —
		// never on resume (which would re-delete rows the earlier part of
		// this same run already correctly rebuilt).
		if !run.DryRun {
			if err := r.resetScope(ctx, run); err != nil {
				return fmt.Errorf("reset scope: %w", err)
			}
		}
	}

	cursor := run.ResumeFrom()
	processed := run.EventsProcessed
	skipped := run.EventsSkippedDuplicate

	for cursor <= run.ToLedger {
		headroomLedger, err := r.headLedgerMinusMargin(ctx, rpc, run.ContractIDs)
		if err != nil {
			return fmt.Errorf("check chain head: %w", err)
		}
		batchEnd := min(cursor+backfillBatchLedgers-1, run.ToLedger, headroomLedger)
		if batchEnd < cursor {
			return fmt.Errorf("%w (requested up to ledger %d, safety margin currently allows up to %d)", ErrTooCloseToHead, run.ToLedger, headroomLedger)
		}

		page, err := fetchSorobanEventsRange(ctx, rpc, run.ContractIDs, cursor, batchEnd)
		if err != nil {
			return fmt.Errorf("fetch events [%d,%d]: %w", cursor, batchEnd, err)
		}

		for _, event := range page.Events {
			if run.DryRun {
				processed++
				continue
			}
			applied, err := applyIndexedEvent(ctx, r.DB, event)
			if err != nil {
				return fmt.Errorf("apply event %s (ledger %d): %w", event.ID, event.Ledger, err)
			}
			if applied {
				processed++
			} else {
				skipped++
			}
		}

		if !run.DryRun {
			if err := r.Repo.UpdateProgress(ctx, run.ID, batchEnd, processed, skipped); err != nil {
				return fmt.Errorf("checkpoint progress: %w", err)
			}
		}
		run.LastLedgerDone = &batchEnd
		run.EventsProcessed = processed
		run.EventsSkippedDuplicate = skipped

		r.logger().Info("backfill progress",
			"run_id", run.ID, "ledger", batchEnd, "to_ledger", run.ToLedger,
			"events_processed", processed, "events_skipped_duplicate", skipped, "dry_run", run.DryRun,
		)

		cursor = batchEnd + 1
		if cursor <= run.ToLedger {
			r.sleepThrottle(ctx)
		}
	}

	// A dry run reports what would happen but must not be left indistinguishable
	// from a real run in an operator's run list — Complete is still the right
	// terminal state (nothing failed), but backfill_runs.dry_run stays on the
	// row forever as the record that no writes actually occurred.
	if err := r.Repo.Complete(ctx, run.ID); err != nil {
		return err
	}
	run.Status = backfill.StatusCompleted
	return nil
}

// resetScope implements ModeRebuild's clear-before-reprocess step: deletes
// resettable derived rows for run.ContractIDs whose ledger_sequence falls
// within [run.FromLedger, run.ToLedger] — precisely the range being
// rebuilt, not every row ever recorded for the contract — plus their
// processed_events entries in the same range, so they are eligible to be
// reprocessed. Start already refused any range containing a non-resettable
// event type, so this is safe for the event types that actually appear in
// range.
func (r *Runner) resetScope(ctx context.Context, run *backfill.Run) error {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for _, rt := range resettableEventTypes {
		if _, err := tx.ExecContext(ctx, rt.deleteSQL, pq.Array(run.ContractIDs), run.FromLedger, run.ToLedger); err != nil {
			return fmt.Errorf("clear %s: %w", rt.table, err)
		}
	}

	// processed_events has no contract column, only event_id/ledger_sequence
	// — scope the dedup-entry clear by ledger range instead, which is exact
	// (every event in this range that belongs to run.ContractIDs was
	// necessarily fetched and marked within this same range).
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM processed_events WHERE ledger_sequence BETWEEN $1 AND $2
	`, run.FromLedger, run.ToLedger); err != nil {
		return fmt.Errorf("clear processed_events: %w", err)
	}

	return tx.Commit()
}

func (r *Runner) headLedgerMinusMargin(ctx context.Context, rpc *rpcClient, contractIDs []string) (uint64, error) {
	// A cheap, zero-event probe: getEvents at the current chain tip always
	// returns latestLedger, which is used purely as a head proxy — no event
	// data from this call is applied.
	page, err := fetchSorobanEventsRange(ctx, rpc, contractIDs, 0, 0)
	if err != nil {
		return 0, err
	}
	if page.LatestLedger < backfillSafetyMargin {
		return 0, nil
	}
	return page.LatestLedger - backfillSafetyMargin, nil
}

func (r *Runner) sleepThrottle(ctx context.Context) {
	d := r.Throttle
	if d <= 0 {
		d = 500 * time.Millisecond
	}
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

func (r *Runner) httpClient() *http.Client {
	if r.Client != nil {
		return r.Client
	}
	return &http.Client{Timeout: defaultRPCTimeout}
}

// rpcClient builds the shared Soroban caller for this run. Untraced: a
// backfill issues thousands of identical getEvents calls, and a span per page
// would swamp the trace store without telling an operator anything the run's
// own progress record does not.
func (r *Runner) rpcClient() *rpcClient {
	return newRPCClient(r.RPCURL, r.httpClient(), r.RPCOptions, false)
}

func (r *Runner) logger() *slog.Logger {
	if r.Logger != nil {
		return r.Logger
	}
	return slog.Default()
}

// eventsPage is one fetchSorobanEventsRange result: the events found within
// the requested ledger bound (paginated internally until either the range
// or the RPC's own page limit is exhausted for this call), the chain's
// current latestLedger, and whether more pages remain within [from,to].
type eventsPage struct {
	Events       []indexedEvent
	LatestLedger uint64
	NextLedger   uint64
	HasMore      bool
}

// fetchSorobanEventsRange is fetchSorobanEvents' range-bounded counterpart:
// the forward indexer only ever needs "from a cursor, forward, whatever
// comes back," but a backfill needs an explicit upper bound so it never
// wanders into ledgers outside the operator-specified range (or past the
// safety margin, enforced by the caller). Events beyond toLedger are
// dropped client-side, since Soroban RPC's getEvents does not itself
// support an endLedger filter — only startLedger + pagination.
func fetchSorobanEventsRange(
	ctx context.Context,
	rpc *rpcClient,
	contractIDs []string,
	fromLedger, toLedger uint64,
) (eventsPage, error) {
	if len(contractIDs) == 0 {
		// getEvents requires at least one contract filter; a head-probe
		// call (headLedgerMinusMargin) may have no contracts configured yet.
		return eventsPage{}, nil
	}

	params := map[string]any{
		"startLedger": fromLedger,
		"filters": []map[string]any{
			{"type": "contract", "contractIds": contractIDs},
		},
		"pagination": map[string]any{"limit": 200},
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
		return eventsPage{}, err
	}
	if rpcResp.Error != nil {
		return eventsPage{}, fmt.Errorf("rpc error: %s", rpcResp.Error.Message)
	}

	page := eventsPage{LatestLedger: rpcResp.Result.LatestLedger}
	maxLedgerSeen := fromLedger
	for _, raw := range rpcResp.Result.Events {
		if raw.Ledger > toLedger {
			page.HasMore = true
			continue
		}
		eventType := ""
		if len(raw.Topic) > 0 {
			if topic, ok := raw.Topic[0].(string); ok {
				eventType = topic
			}
		}
		if eventType == "" {
			continue
		}
		page.Events = append(page.Events, indexedEvent{
			ID:         raw.ID,
			ContractID: raw.ContractID,
			EventType:  eventType,
			Ledger:     raw.Ledger,
			Data:       raw.Value,
			TxHash:     raw.TxHash,
		})
		if raw.Ledger > maxLedgerSeen {
			maxLedgerSeen = raw.Ledger
		}
	}
	// The RPC page may have been full (hit the 200-event limit) while still
	// within [from,to] — if so, more events remain to fetch before toLedger.
	if len(rpcResp.Result.Events) == 200 && !page.HasMore {
		page.HasMore = true
	}
	page.NextLedger = maxLedgerSeen + 1
	return page, nil
}
