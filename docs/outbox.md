# Transactional outbox

> Implements nester#1049. Code: `apps/api/internal/domain/outbox`,
> `apps/api/internal/repository/postgres/outbox_repository.go`, migration
> `102_create_outbox`.

## Why

A domain write and the side effect it causes — a notification, a webhook —
are two writes to two different systems. Sequencing them carefully does not
make them atomic, and **both** orderings lose:

- **Write the row, then dispatch.** The process dies in between. The side
  effect never happens: the user's balance changed and nobody told them.
- **Dispatch, then write the row.** The write fails. A notification went out
  for something that never happened.

The savings-goal milestone path had exactly the first shape: it marked a
milestone as notified and *then* spawned a goroutine to notify. A crash in
between left the goal permanently flagged "already told them" for a
notification that never happened — and because the flag said it was done,
nothing would ever retry it.

## How

The side effect is inserted into the `outbox` table **inside the domain
write's own transaction**. Atomicity comes from that shared transaction and
from nothing else: roll it back and the intent to notify rolls back too;
commit it and the intent is durable even if the process dies one instruction
later.

```
┌─ one transaction ──────────────────────┐
│  UPDATE savings_goals SET …            │
│  INSERT INTO outbox (…) VALUES (…)     │
└────────────────────────────────────────┘
             │ commit
             ▼
   relay ──▶ jobs (the existing durable queue, #824)
             │
             ▼
   webhook delivery / notification dispatch
```

The relay does **not** implement retry, backoff, or dead-lettering for
delivery. It hands each row to the job queue, keyed on the row's dedupe key,
and the queue owns everything after that. Two competing retry systems would
eventually disagree about how many attempts a thing has had.

## Delivery semantics

Delivery is **at-least-once**. The relay may hand a row over twice after a
crash, and the queue is at-least-once by design.

Every event therefore carries a **dedupe key** that is stable across every
redelivery of the same logical side effect. It is derived from the aggregate
and what happened to it (`savings_goal:{id}:milestone:50`), never generated
per attempt, so the same event produces the same key in this process and in
the one that picks it up after a restart.

The key is propagated all the way to the consumer:

- **Webhooks** — the `X-Nester-Dedupe-Key` header and a `dedupe_key` field in
  the JSON body, plus a delivery id derived from the key and the subscription
  id, so a redelivery carries the id the subscriber already saw. See
  [webhooks.md](webhooks.md).
- **Notifications** — passed to the dispatcher's deduplicator, so a
  redelivered event is recorded as suppressed rather than shown to the user
  twice.

**Consumers must be idempotent.** At-least-once delivery without that
requirement stated is a trap: a consumer that treats a webhook as a trigger
for a payout will eventually double-pay.

## Ordering

Ordering is guaranteed **per aggregate** and is explicitly **not** guaranteed
globally.

For one `(aggregate_type, aggregate_id)` the relay dispatches events in
insertion order and will not hand over event N+1 until event N has reached a
terminal state — delivered or dead. Events for different aggregates are
independent and race freely.

Mechanically: `ClaimDue` selects `DISTINCT ON (aggregate_type, aggregate_id)`
ordered by `(created_at, id)`, so only each aggregate's oldest non-terminal
row is ever visible to the relay. The due-ness filter is applied *after* that
selection — filtering inside the `DISTINCT ON` would let a backing-off head
be skipped in favour of the event behind it, silently breaking the ordering
it exists to preserve.

Per-aggregate rather than global is what keeps a poison message from stalling
the world. Choose the **narrowest** aggregate that actually needs ordering:
the wider it is, the more work one stuck event holds up.

## Poison messages

An event that can never be delivered — a consumer returning 400 forever, an
event type with no registered handler, a payload that will not deserialise —
must not block the queue behind it.

- The job queue exhausts its own attempt budget and dead-letters the job.
- The relay sees the dead job and moves the outbox row to `dead`.
- `dead` is terminal, so the aggregate's next event becomes claimable
  immediately. The aggregate resumes; other aggregates were never affected.

Dead-lettering is logged at `ERROR` with the aggregate, event type, dedupe
key, and reason, and increments an `outbox.dead_lettered` counter. **That
counter is the one worth alerting on** — a non-zero value means a side effect
will never happen.

An event whose *hand-off* keeps failing (the enqueue itself errors) is
retried up to `max_attempts` and then dead-lettered too. That bound exists so
a row that can never be enqueued stops holding its aggregate open.

## Statuses

| Status        | Meaning                                                  |
| ------------- | -------------------------------------------------------- |
| `pending`     | Written by the producer, not yet handed to the queue.     |
| `dispatching` | A queue job owns the delivery. Blocks its aggregate.      |
| `dispatched`  | The job succeeded. Terminal, prunable.                    |
| `dead`        | Poison. Terminal, retained longer for diagnosis.          |

## Retention

Without pruning the table only ever grows — every side effect the system has
ever produced, kept forever, on the write path of every domain transaction
that inserts into it.

A leader-gated sweep deletes terminal rows past their window. Delivered rows
go after `OUTBOX_DISPATCHED_RETENTION` (7 days); dead rows survive far longer
(`OUTBOX_DEAD_RETENTION`, 30 days) because they are the evidence somebody
needs to work out which side effect never happened.

**`pending` and `dispatching` rows are never pruned, at any age.** They are
undelivered work; deleting one is silently dropping the side effect the
outbox exists to guarantee.

## Adding a producer

1. Build the event inside your domain transaction and insert it with the same
   transaction handle:

   ```go
   e, err := outbox.NewEvent("savings_goal", goalID.String(),
       service.OutboxEventWebhookFanout, dedupeKey, payload)
   if err != nil { return err }
   if err := writer.Insert(ctx, tx, e); err != nil { return err }  // tx, not db
   ```

   Passing the connection pool instead of `tx` compiles and inserts the row —
   and gives up every guarantee above. Always pass the transaction.

2. Derive the dedupe key from stable inputs (the aggregate plus what
   happened). A `uuid.New()` here defeats the whole mechanism: a retried
   producer transaction would emit a second, unrecognisable side effect.

3. Make sure the event type has a route in `main.go` and that its handler is
   registered on the worker **before** `Run`. An unroutable event is
   dead-lettered on sight.

## Observability

| Signal                        | What it tells you                                    |
| ----------------------------- | ---------------------------------------------------- |
| `outbox.dead_lettered`        | A side effect will never happen. Alert on this.      |
| `outbox.pending_depth`        | Undelivered and due.                                 |
| `outbox.dispatching_depth`    | In flight.                                           |
| `outbox.oldest_pending_age`   | How long the oldest undelivered event has waited.    |

Watch the **age**, not just the depth: a relay that has stopped relaying
holds a constant depth, which looks healthy, while the age climbs.

## Configuration

| Variable                       | Default | Notes                                        |
| ------------------------------ | ------- | -------------------------------------------- |
| `OUTBOX_RELAY_ENABLED`         | `true`  | Kill switch. Events accumulate while off.    |
| `OUTBOX_RELAY_POLL_INTERVAL`   | `1s`    | Idle poll; a tick with work re-polls at once.|
| `OUTBOX_RELAY_BATCH_SIZE`      | `100`   | Aggregate heads claimed per tick.            |
| `OUTBOX_RELAY_LEASE`           | `30s`   | Reclaim window for a hand-off abandoned mid-flight. |
| `OUTBOX_RELAY_BACKOFF`         | `5s`    | Hand-off retry delay. **Not** delivery retry — that is `JOB_QUEUE_BACKOFF_*`. |
| `OUTBOX_RELAY_STATS_INTERVAL`  | `30s`   | Gauge refresh.                               |
| `OUTBOX_RETENTION_INTERVAL`    | `24h`   | Sweep cadence.                               |
| `OUTBOX_DISPATCHED_RETENTION`  | `168h`  | How long delivered rows are kept.            |
| `OUTBOX_DEAD_RETENTION`        | `720h`  | How long poison rows are kept.               |

Every API instance runs a relay. `ClaimDue` takes row locks with
`SKIP LOCKED`, so instances divide the backlog rather than duplicating it;
electing a single leader here would turn a horizontal scale-out into a single
point of failure for every side effect in the system.
