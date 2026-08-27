# Stellar Event Indexer — Replay Guarantees

The event indexer turns Soroban contract events into vault balance changes. It
is the only path by which on-chain activity becomes a number a user sees, so a
defect here does not throw an error — it silently produces a wrong balance and
leaves it wrong.

This document states what the indexer guarantees, how those guarantees are
enforced, and where the evidence lives.

## The invariant

> The same logical event stream produces the same final state, regardless of how
> that stream is delivered.

Delivery may vary in four ways, and all four converge on identical state:

| Delivery | Behaviour |
|---|---|
| Clean single pass | Baseline. |
| Interrupted and resumed | Resumes from the persisted cursor; nothing re-applied. |
| Every event delivered twice | Second delivery is a no-op. |
| Events shuffled within a ledger | Final state unchanged. |

Each is a test in `apps/api/internal/stellar/replay_harness_test.go`, and every
one compares the **complete** vault state — `total_deposited`,
`current_balance`, `yield_earned`, and `status` — serialised canonically and
compared byte-for-byte. Comparing a single total would let a corrupted
dimension pass unnoticed.

## Architecture

Indexing logic lives in `internal/stellar`, not in `main.go`:

- **`EventPoller.PollEvents`** (`poller.go`) — one complete indexing pass:
  resolve cursor, fetch, apply, advance. This is the unit both production and
  the replay harness drive. There is no test-only reimplementation; a harness
  that exercised a parallel code path would prove nothing about production.
- **`EventFetcher`** (`poller.go`) — the seam that makes the harness hermetic.
  Production uses `rpcEventFetcher` (`fetcher.go`); tests use a scripted
  fetcher backed by a recorded fixture.
- **`applyEventMutation`** (`indexer.go`) — the per-event state change, shared
  by the poller and the admin one-shot sync so the two cannot drift.
- `startEventIndexer` (`indexer.go`) — owns only scheduling and telemetry.

## Cursor persistence and transactionality

The cursor lives in `system_state` under `event_indexer.last_ledger`.

**Each event's state mutation, its `processed_events` row, and the cursor
advance to that event's ledger commit in a single transaction.** This is the
central correctness property. The two failure modes it eliminates:

- *Cursor advanced, balance write failed* — the event is skipped forever and
  the balance is permanently short.
- *Balance written, cursor not advanced* — safe here, because redelivery is
  deduplicated, but only because idempotency is enforced independently.

`advanceCursorTx` deliberately bypasses `systemstate.Repository`: that
interface owns its own `*sql.DB` and would commit separately, reintroducing
the split the design exists to remove. It uses `GREATEST` so the cursor is
monotonic.

**The cursor never advances past the last event actually committed.** When a
batch contains events, the cursor rests at the last applied event's ledger
rather than jumping to the network tip, because the poller cannot know a batch
was complete — the RPC pages results. Advancing past an incomplete batch would
skip undelivered events permanently. Re-reading a few already-processed ledgers
costs nothing, since they are deduplicated. The cursor advances to the tip only
when a poll returns no events at all, which is what stops it stalling on a
quiet network.

**A failing event stops the pass.** The poller does not skip past an error onto
later events; doing so would strand the failed event behind an advanced cursor.
The next pass retries it.

## Idempotency

`processed_events.event_id` is a PRIMARY KEY. Each event is claimed with
`INSERT … ON CONFLICT (event_id) DO NOTHING` **inside the same transaction as
the mutation**, and the mutation runs only when the insert reports a row.

This is database-enforced, not application-enforced: there is no
`SELECT`-then-`INSERT` window for two workers to race through. A duplicate
still advances the cursor, because the event's effect is already durable.

## Cold start

A fresh deployment has no meaningful cursor and must not ask the RPC for
ledger 0 — `getEvents` rejects it, which is why fresh deployments never
started indexing.

`resolveStartLedger` treats **both** an absent key and a stored `"0"` as "never
indexed". The stored-zero case matters: migration `025` seeds the key with the
literal value `'0'`, so a fresh database has a present-but-zero cursor.

On cold start the poller reads the network tip via `getLatestLedger` and starts
at `tip - DefaultColdStartOffset` (12 ledgers), clamped to a minimum of 1.

The offset exists because starting exactly at the tip races the network: ledgers
close roughly every 5 seconds, so events emitted between reading the tip and the
first `getEvents` call would fall below the start bound and be lost. Rewinding
makes the first poll overlap instead, and the overlap is harmless because every
event is deduplicated. Override with `EventPoller.ColdStartOffset`.

If the tip cannot be read, cold start **fails loudly** rather than falling back
to 0.

## Ordering guarantees

**Across ledgers, order is required and enforced.** The cursor advances per
event, so applying a higher ledger before a lower one would strand the lower
event. `sortEventsForApply` enforces ledger order regardless of the order the
RPC delivered events in.

**Within a ledger, order is not required.** Every balance mutation is an
additive or subtractive delta on a vault row, and addition commutes, so any
permutation within a ledger converges on the same totals. Ties break by event
ID purely for determinism.

One boundary worth naming: `current_balance` carries `CHECK (>= 0)`, so a
permutation that moves a withdrawal ahead of the deposit funding it fails the
constraint rather than corrupting the balance. Failing loudly is correct, and
because the poller stops on error rather than skipping, the next pass retries.

## Not covered: ledger reorganisations

**The replay guarantees above assume an append-only ledger history. Reorgs are
out of scope for this harness and are not handled by `EventPoller`.**

This is a real gap, stated here rather than left implicit, because a document
titled "Replay Guarantees" that silently omits the one replay case caused by
the *chain* rather than the *delivery* would be misleading.

What exists today:

- `ReorgSafeIndexer` (`reorg_indexer.go`) and migration
  `100_reorg_safe_indexer` (`ledger_checkpoints`, plus `tx_hash` /
  `event_index` columns on `processed_events`) implement parent-hash
  verification and checkpoint rollback.
- **Neither has a non-test caller.** `EventPoller` does not reference
  `ledger_checkpoints`, `parent_hash`, or `ReorgSafeIndexer`, and routing
  production through the poller leaves that machinery unreached.

Two specific incompatibilities to resolve before the two can be joined:

1. **Cursor monotonicity blocks rewind.** `advanceCursorTx` uses `GREATEST`, so
   the cursor can only move forward. A reorg requires moving it *backwards* to
   the fork point. Any reorg integration must either bypass `advanceCursorTx`
   for the rollback or make the rewind explicit and auditable, rather than
   quietly dropping the monotonicity that protects the forward path.
2. **`revertToCheckpoint` does not rewind the cursor at all.** It deletes
   `processed_events` and `ledger_checkpoints` rows above the fork point, but
   leaves `event_indexer.last_ledger` untouched. On its own that would delete
   the dedup records while the cursor still points past them, so the reverted
   events would never be re-fetched — the rollback would drop events rather
   than replay them.

Until that integration lands, the indexer is correct on an append-only history
and undefined under a reorg. Soroban/Stellar finality makes deep reorgs rare,
which is presumably why this has not bitten yet, but "rare" is not "handled".
Tracking this as follow-up work is deliberate: it is outside issue #1051's
acceptance criteria, and folding an unreviewed reorg path into this change
would weaken rather than strengthen the guarantees proven here.

## Integer precision

Soroban amounts are `i128` stroops and routinely exceed `float64`'s exact
integer limit of 2^53 (~9.007e15). A 1e18 stroop deposit is an ordinary vault
deposit.

Two distinct things have to hold — the amount must *parse* exactly, and it must
*persist* exactly. Parsing was already correct before this work; persistence
was not.

- **Parsing (pre-existing).** Amounts are decoded with
  `json.Decoder.UseNumber()`, so they arrive as `json.Number` and are parsed
  into `decimal.Decimal` — never through `float64`.
- **The `case float64` branch** in `extractEventAmount` is a bounds check
  rather than a pure guard, and it is worth being precise about which: it
  *rejects* values that are non-integral or whose magnitude exceeds 2^53
  (precision is already lost by the time the indexer sees them), and it
  *converts* smaller values via `int64(v)`, which is exact in that range. The
  branch is only reachable for stray `float64` inputs, since the RPC path
  yields `json.Number`.
- **Persistence (fixed here).** Migration `103` widens
  `vaults.total_deposited`, `current_balance`,
  `yield_earned`, and `fees_paid` from `NUMERIC(20,8)` to `NUMERIC(48,8)`.
  The old type allowed only 12 integer digits and raised `numeric field
  overflow` for any amount at or above 10^12, so large deposits were rejected
  outright. `NUMERIC(48,8)` covers the full i128 range.

`TestAmountPathHasNoFloat64` is a source-level guard that fails if a `float64`
conversion is reintroduced into `indexer.go`, `poller.go`, or `fetcher.go`.

### Rolling back migration 103

Narrowing back is lossy by nature: a balance that needed the widened range
cannot be represented at `NUMERIC(20,8)`, and PostgreSQL raises an overflow
rather than silently truncating. That failure is correct — a balance must never
be rounded away by a schema change. Reconcile affected rows before rolling back.

## The fixture

`apps/api/internal/stellar/testdata/replay_events.json` is a recorded
`getEvents` JSON-RPC response covering ledgers 58101–58125 across two vault
contracts.

It contains deposits, withdrawals, harvests, and pause/unpause events; multiple
events within a single ledger (so within-ledger shuffling is meaningful); and
amounts up to 1e18 stroops, well above the `float64` limit.

The fixture is **synthetic but schema-faithful**: it is hand-authored to match
the exact wire shape the production parser consumes, rather than captured from
live testnet. It is decoded in tests through the same `UseNumber` decoder
settings the production fetcher uses, so a change to the wire format breaks the
tests rather than silently bypassing them.

### Updating the fixture safely

1. Keep the envelope shape (`result.latestLedger`, `result.events[]`) — the
   production parser reads `id`, `contractId`, `ledger`, `topic[0]`, `value`.
2. Keep `result.latestLedger` at or just past the highest event ledger.
   A tip far beyond the events makes cold start skip them entirely.
3. Keep every event ID unique — it is the deduplication key.
4. Preserve at least one amount above 2^53, or the precision tests lose value.
5. Keep each ledger's events individually satisfiable against
   `CHECK (current_balance >= 0)`, so the commutativity claim is what gets
   tested rather than the constraint.
6. Update the hand-computed expected balances in
   `TestIntegrationReplay_CleanSinglePass`. They are deliberately computed by
   hand, not captured from output — a test that only compares runs to each
   other passes happily on a uniformly wrong implementation.
7. Amounts are stored as emitted; this path does not rescale stroops into
   display units.

## Running the tests

The replay harness needs PostgreSQL and skips cleanly without it:

```bash
export TEST_DATABASE_DSN="postgres://user:pass@localhost:5432/nester_test?sslmode=disable"
cd apps/api
go test ./internal/stellar/ -run TestIntegrationReplay -v
```

The harness applies the full migration chain via `testutil.ApplyAllMigrations`,
so the schema under test is the schema production runs against.

Cold-start, ordering, and precision-guard tests need no database and run in the
default unit job.

## PRD mapping

| Ref | Issue | Evidence |
|---|---|---|
| **B-01** | In-memory cursor resets on restart, doubling balances | `TestIntegrationReplay_RestartMidStream` — a fresh poller instance resumes from the persisted cursor and reaches byte-identical state, with `processed_events` proving each event applied exactly once. Enforced by per-event transactional cursor advance. |
| **B-02** | `startLedger = 0` rejected by RPC, indexer never starts | `TestColdStart_DerivesValidLedgerFromTip` and `TestColdStart_NeverRequestsLedgerZero` — the latter asserts on the values actually handed to the RPC, covering both the absent-key and seeded-`'0'` cases. |
| **B-03** (as scoped in issue #1051: duplicate-delivery idempotency) | Duplicate delivery corrupts balances | `TestIntegrationReplay_DuplicateDelivery` and `TestIntegrationReplay_RepeatedFullReplay` — enforced by the `processed_events` primary key inside the mutation transaction. Note: PRD B-03 itself refers to a settlement BOLA issue; issue #1051 uses B-03 for indexer idempotency. |
| **B-11** | `float64` precision loss in `extractEventAmount` | `TestIntegrationLargeAmountRoundTripsExactly`, `TestExtractEventAmount_RejectsUnsafeFloat64`, `TestAmountPathHasNoFloat64`, plus migration `103`. |

Transactionality itself is covered by
`TestIntegrationCursorAndBalanceCommitAtomically`, which forces a balance write
to fail against a real `CHECK` constraint and asserts the cursor did not move,
no partial state committed, and the event remains retryable.
