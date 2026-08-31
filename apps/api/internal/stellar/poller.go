package stellar

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/systemstate"
)

// DefaultColdStartOffset is how many ledgers below the network tip a fresh
// deployment begins indexing from.
//
// Starting exactly at the tip races the network: ledgers close every ~5s, so
// events emitted between the getLatestLedger call and the first getEvents call
// would fall below the start bound and be missed forever. Rewinding a few
// ledgers makes the first poll overlap instead, and the overlap is harmless
// because every event is deduplicated by processed_events.event_id.
const DefaultColdStartOffset uint64 = 12

// EventFetcher retrieves contract events and reports the network tip.
//
// The indexer depends on this interface rather than on *http.Client so the
// replay harness can drive the real processing path from a recorded fixture
// with no network access. Production wiring uses rpcEventFetcher, which is the
// same getEvents call the indexer has always made.
type EventFetcher interface {
	// FetchEvents returns the events at or after startLedger for the given
	// contracts, plus the network's latest ledger sequence.
	FetchEvents(ctx context.Context, contractIDs []string, startLedger uint64) ([]indexedEvent, uint64, error)

	// LatestLedger returns the current network tip. It is used only on cold
	// start, to derive a valid startLedger when no cursor is persisted.
	LatestLedger(ctx context.Context) (uint64, error)
}

// EventPoller performs one indexing pass over the Stellar event stream.
//
// It is the unit the long-running indexer goroutine drives and the unit the
// replay harness exercises: there is deliberately no separate test-only
// implementation of event processing, because a harness that proved a
// different code path correct would prove nothing about production.
type EventPoller struct {
	DB      *sql.DB
	SysRepo systemstate.Repository
	Fetcher EventFetcher
	Logger  *slog.Logger

	// ColdStartOffset overrides DefaultColdStartOffset when non-zero.
	ColdStartOffset uint64
}

// PollEvents runs a single poll: resolve the cursor, fetch events, apply each
// one, and advance the cursor.
//
// It returns the number of events whose state mutation was applied on this
// pass. Duplicates are not counted: they are already durable, so re-delivering
// them is a no-op by design.
//
// Cursor semantics are the heart of the correctness argument:
//
//   - Each event's state mutation, its processed_events row, and the cursor
//     advance to that event's ledger all commit in ONE transaction. There is no
//     window in which the cursor has moved past an event whose balance write did
//     not land, which is the failure mode that loses events permanently (B-01).
//   - Events are applied in ledger order, so the cursor never advances past an
//     event that has not been committed.
//   - When a single event fails, the pass stops rather than skipping ahead. The
//     cursor still points at the last committed ledger, so the next pass retries
//     the failed event instead of stepping over it.
func (p *EventPoller) PollEvents(ctx context.Context) (int, error) {
	if p.DB == nil {
		return 0, fmt.Errorf("event poller: DB is required")
	}
	if p.SysRepo == nil {
		return 0, fmt.Errorf("event poller: SysRepo is required")
	}
	if p.Fetcher == nil {
		return 0, fmt.Errorf("event poller: Fetcher is required")
	}

	startLedger, err := p.resolveStartLedger(ctx)
	if err != nil {
		return 0, fmt.Errorf("resolve start ledger: %w", err)
	}

	contractIDs, err := loadVaultContractIDs(ctx, p.DB)
	if err != nil {
		return 0, fmt.Errorf("load vault contracts: %w", err)
	}
	if len(contractIDs) == 0 {
		return 0, nil
	}

	events, latestLedger, err := p.Fetcher.FetchEvents(ctx, contractIDs, startLedger)
	if err != nil {
		return 0, fmt.Errorf("fetch events: %w", err)
	}

	sortEventsForApply(events)

	applied := 0
	for _, event := range events {
		// The cursor is advanced to this event's ledger inside the same
		// transaction that applies it. Committing the cursor per event (rather
		// than once per batch) is what makes a mid-stream restart resume at the
		// exact event that was interrupted.
		processed, err := applyIndexedEventWithCursor(ctx, p.DB, event)
		if err != nil {
			// Stop rather than continue. Skipping a failed event while later
			// events advance the cursor would strand it behind the cursor
			// forever; the whole point of this loop is that it cannot happen.
			return applied, fmt.Errorf("apply event %s (ledger %d): %w", event.ID, event.Ledger, err)
		}
		if processed {
			applied++
		}
	}

	// When the batch contained events, the cursor stays at the last applied
	// event's ledger. It deliberately does NOT jump to the network tip.
	//
	// Jumping ahead is only safe if the batch is known to be complete, and the
	// poller cannot know that: the RPC pages results, and a batch shorter than
	// the page limit still may not be the whole story when the caller's fetcher
	// applies its own bounds. Advancing to the tip on an incomplete batch would
	// skip every undelivered event permanently — the exact class of silent loss
	// this issue exists to eliminate. Leaving the cursor at the last committed
	// event costs at most a re-fetch of already-processed events, and those are
	// deduplicated by processed_events.
	if len(events) > 0 {
		return applied, nil
	}

	// No events in range: the cursor can safely advance to the tip, because an
	// empty result means there is nothing between the cursor and the tip to
	// lose. This is what stops the cursor from stalling on a quiet network.
	if latestLedger > 0 {
		if err := setLastIndexedLedger(ctx, p.SysRepo, latestLedger); err != nil {
			return applied, fmt.Errorf("advance cursor to tip: %w", err)
		}
	}

	return applied, nil
}

// resolveStartLedger returns the ledger to begin this pass from.
//
// A persisted cursor above zero is authoritative and is resumed from directly.
// Zero means "never indexed": either the key is genuinely absent, or it holds
// the '0' seeded by migration 025. Both cases are cold start, and both must
// derive a real ledger from the network tip — passing startLedger=0 to
// getEvents is rejected by the Stellar RPC, which is exactly why a fresh
// deployment never started indexing (B-02).
func (p *EventPoller) resolveStartLedger(ctx context.Context) (uint64, error) {
	cursor, err := getLastIndexedLedger(ctx, p.SysRepo)
	if err != nil {
		return 0, err
	}
	if cursor > 0 {
		return cursor, nil
	}

	tip, err := p.Fetcher.LatestLedger(ctx)
	if err != nil {
		return 0, fmt.Errorf("cold start: %w", err)
	}
	if tip == 0 {
		return 0, fmt.Errorf("cold start: rpc reported latest ledger 0")
	}

	offset := p.ColdStartOffset
	if offset == 0 {
		offset = DefaultColdStartOffset
	}

	start := uint64(1)
	if tip > offset {
		start = tip - offset
	}

	// Persist immediately so a crash before the first event still resumes from
	// a valid ledger rather than re-deriving a newer tip and skipping the gap.
	if err := setLastIndexedLedger(ctx, p.SysRepo, start); err != nil {
		return 0, fmt.Errorf("cold start: persist initial cursor: %w", err)
	}

	if p.Logger != nil {
		p.Logger.Info("event indexer cold start", "tip", tip, "start_ledger", start, "offset", offset)
	}

	return start, nil
}

// sortEventsForApply orders events by (ledger, event id).
//
// Ledger order is required: the cursor advances per event, so applying a
// higher ledger before a lower one would strand the lower event behind the
// cursor. Within a single ledger the balance mutations are additive and
// therefore commutative, so any stable order yields identical final state;
// sorting by event ID simply makes the order deterministic and independent of
// RPC delivery order.
func sortEventsForApply(events []indexedEvent) {
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].Ledger != events[j].Ledger {
			return events[i].Ledger < events[j].Ledger
		}
		return events[i].ID < events[j].ID
	})
}

// applyIndexedEventWithCursor applies one event and advances the cursor to its
// ledger in a single transaction.
//
// This is the atomicity guarantee the replay harness asserts: either the
// balance mutation, the processed_events row, and the cursor all become
// durable together, or none of them do.
func applyIndexedEventWithCursor(
	ctx context.Context,
	db *sql.DB,
	event indexedEvent,
) (bool, error) {
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

	// A duplicate still advances the cursor: the event's effect is already
	// durable from the first delivery, so moving past it loses nothing. This
	// is what makes duplicate delivery a no-op rather than a double-apply.
	if inserted {
		if err := applyEventMutation(ctx, tx, event); err != nil {
			return false, err
		}
	}

	if err := advanceCursorTx(ctx, tx, event.Ledger); err != nil {
		return false, err
	}

	if err := tx.Commit(); err != nil {
		return false, err
	}

	return inserted, nil
}

// advanceCursorTx writes the indexer cursor inside the caller's transaction.
//
// It deliberately does not go through systemstate.Repository: that interface
// owns its own *sql.DB and would commit independently, reintroducing the split
// between "balance committed" and "cursor committed" that this function exists
// to remove. GREATEST keeps the cursor monotonic even if events arrive out of
// ledger order.
//
// That monotonicity assumes an append-only ledger history. A reorg needs the
// cursor moved BACKWARDS to the fork point, which GREATEST forbids. Reorg
// handling is an accepted, documented exposure rather than a gap to be closed
// here (#1089): see "Not covered: ledger reorganisations" in
// docs/event-indexer-replay.md for the concrete risk and the prerequisites.
// Do not simply drop GREATEST to enable a rewind: it is what stops
// out-of-order delivery from walking the cursor backwards over
// already-committed events.
func advanceCursorTx(ctx context.Context, tx *sql.Tx, ledger uint64) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO system_state (key, value, updated_at)
VALUES ($1, $2, NOW())
ON CONFLICT (key) DO UPDATE
SET value = GREATEST(system_state.value::bigint, EXCLUDED.value::bigint)::text,
    updated_at = NOW()`,
		systemstate.KeyLastLedger,
		strconv.FormatUint(ledger, 10),
	)
	return err
}
