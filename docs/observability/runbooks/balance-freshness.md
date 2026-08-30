# Runbook — Balance freshness (indexer lag)

**Alerts:** `IndexerStalenessBudgetExceeded` (page), `IndexerLagHigh` (page),
`IndexerLagStale` (page), `IndexerMetricsAbsent` (page)
**Dashboard:** [SLO — Balance freshness](/d/nester-slo-balance/balance-freshness)
**Staleness budget:** **300s (5 minutes)** — `INDEXER_STALENESS_BUDGET`,
equivalently 60 ledgers at a ~5s close interval

---

## What the alert means

Balances in the app come from the event indexer's view of the chain. When the
indexer falls behind, that view is out of date, and the API says so: past the
budget every `/api/` response carries `X-Indexer-Stale: true`.

**Read which alert fired — they mean different things.**

| Alert | Meaning |
|---|---|
| `IndexerStalenessBudgetExceeded` | **The budget is blown.** Indexed data is older than 5 minutes and clients are being told their balances are stale. Fires whether the indexer is behind *or* dead. This is the authoritative one. |
| `IndexerLagHigh` | The indexer is alive and reporting, but falling behind in ledgers. |
| `IndexerLagStale` | The indexer stopped reporting a position. **The ledger lag figure beside it is frozen and must not be trusted.** |
| `IndexerMetricsAbsent` | The freshness series is gone entirely — API down, scrape broken, or the process never started. |

`IndexerStalenessBudgetExceeded` cannot be fooled by a dead indexer:
`nester_indexer_lag_seconds` is derived from the clock at scrape time, not
pushed by the indexer, so it keeps climbing with nothing of ours still running
to push it. If it is firing and `IndexerLagHigh` is not, the indexer has
**stopped**, not slowed.

**User impact:** users see stale balances. A deposit that has settled on-chain
does not appear in the app. This pages — rather than tickets — because a
savings product showing the wrong balance is a trust failure, not a cosmetic
one, and users conclude their money is missing.

---

# 1. Diagnose

## First four checks, in order

**1. How stale is the data, and by how much is the budget blown?**

```promql
nester_indexer_lag_seconds
nester_indexer_staleness_budget_seconds
```

The first number is what users are seeing, in seconds. Anything under the
second is fine. This is the same comparison the alert and the API's stale flag
make, so if it is over, clients are already being told.

**2. Is the indexer running at all?** This is the branch that decides
everything below.

```promql
nester_indexer_lag_last_sample_age_seconds
```

- **Near zero, resetting every ~6s** → running, genuinely behind. Go to
  [Cause A](#cause-a--indexer-falling-behind).
- **Climbing steadily** → **stopped**. Go to
  [Cause B](#cause-b--indexer-stopped-or-wedged).
- **No series at all** → go to [Cause C](#cause-c--indexer-never-started).

**3. How far behind in ledgers, and is it moving?**

```promql
nester_indexer_lag_ledgers
```

**No data here is itself a finding**: the gauge is withheld until the indexer
reports a position, so an absent series means it has never committed a cursor.
It is deliberately not published as `0`, because `0` would claim the indexer is
exactly at the tip — the one value that looks perfect.

**4. Are samples erroring?**

```promql
rate(nester_indexer_lag_sample_errors_total[5m])
```

Non-zero means the loop cannot read its cursor or reach the RPC. The logs below
name which.

## Read the numbers directly

Current indexer cursor, straight from the source of truth:

```bash
docker compose exec postgres psql -U nester nester_dev -c \
  "SELECT value AS indexed_ledger, updated_at FROM system_state WHERE key = 'event_indexer.last_ledger';"
```

`updated_at` is when the cursor last advanced. If it is minutes old, the
indexer is not making progress regardless of what any gauge says.

Current network ledger, from the same RPC the indexer polls:

```bash
curl -s -X POST "$STELLAR_RPC_URL" \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"getLatestLedger"}' | jq '.result.sequence'
```

The difference between those two numbers **is** the ledger lag, computed by
hand. Use it when you do not trust the metric.

## What a client is being told

```bash
curl -s -D - -o /dev/null https://<api-host>/api/v1/vaults -H "Authorization: Bearer $TOKEN" \
  | grep -i '^x-indexer'
```

```
X-Indexer-Stale: true
X-Indexer-Lag-Seconds: 743
X-Indexer-Lag-Ledgers: 140
X-Indexer-Staleness-Budget-Seconds: 300
```

This is exactly what the UI sees. If support is asking "are users being shown a
warning?", this answers it. `X-Indexer-Lag-Ledgers` absent means the indexer
has never reported a position.

## Is upstream healthy?

```promql
sum by (kind) (rate(nester_outbound_errors_total{upstream="soroban_rpc"}[5m]))
histogram_quantile(0.95, sum by (le) (rate(nester_outbound_request_duration_seconds_bucket{upstream="soroban_rpc"}[5m])))
```

The indexer polls Soroban every 6s with an 8s client timeout. A slow endpoint
stalls it directly.

Both dependencies at once:

```bash
curl -s https://<api-host>/api/v1/admin/health -H "Authorization: Bearer $ADMIN_TOKEN" | jq
```

## Is storage healthy?

```promql
nester_db_pool_acquired_connections / nester_db_pool_max_connections
rate(nester_db_pool_empty_acquire_waits_total[5m])
```

A saturated pool starves the indexer's per-event transaction, and every event
it applies takes one.

## Logs

```bash
kubectl logs -l app=nester-api --since=30m | grep -i "event indexer"
```

| Log message | Meaning |
|---|---|
| `event indexer poll failed` | The pass errored — the wrapped error names the stage (`resolve start ledger`, `load vault contracts`, `fetch events`, `apply event …`) |
| `event indexer cold start` | First run; `start_ledger` is where it began |
| `event indexer disabled: STELLAR_RPC_URL is empty` | Never started at all |

`apply event <id> (ledger N)` repeating with the same event ID is the worst
case: one event fails permanently and the cursor cannot advance past it, so the
indexer runs forever without progressing. Note the ledger number — you will
need it for the catch-up path.

## Dashboards

| Panel | Question it answers |
|---|---|
| **Data staleness** | How stale, in seconds — the number the budget governs |
| **Data staleness against the budget** | Where the crossing happened, and whether it is recovering |
| **Indexer lag** | How far behind in ledgers |
| **Lag sample age** | Behind, or stopped |
| **Sample errors** | Whether the loop is erroring |

A **sawtooth** in ledger lag is healthy — the cursor advances in steps. A
**monotonic climb** is a stall.

---

## Three most likely causes

### Cause A — Indexer falling behind

Running and sampling normally, but lag is climbing.

**Distinguishing evidence:** `lag_last_sample_age_seconds` resets to near zero
every few seconds (loop alive) while `lag_ledgers` and `lag_seconds` climb.

In order of likelihood:

1. **Soroban RPC slow.** p95 outbound duration elevated; each poll takes longer
   than the 6s tick, so it cannot keep up.
2. **Event volume spike.** Many contracts or a burst of activity; the apply
   loop is the bottleneck. Each event is its own transaction.
3. **Database write contention.** Check the pool metrics above.

### Cause B — Indexer stopped or wedged

**Distinguishing evidence:** `lag_last_sample_age_seconds` climbing steadily.
`IndexerStalenessBudgetExceeded` firing while `IndexerLagHigh` is not — the
ledger gauge is frozen or absent, so it has nothing to fire on.

1. **A single event failing permanently.** `apply event …` repeating for the
   same ID. The pass stops at that event by design rather than skipping it, so
   the cursor never advances past it. This is the most common wedge.
2. **Cursor persist failing.** A database write problem: the loop runs,
   reprocesses the same ledgers, never advances. `system_state.updated_at`
   stops moving.
3. **Goroutine dead.** The loop exited or panicked. Check `go_goroutines` for a
   step down, and the logs for a panic.
4. **Context cancelled.** Shutdown began but the process did not exit cleanly.

### Cause C — Indexer never started

**Distinguishing evidence:** `IndexerMetricsAbsent` firing, or
`nester_indexer_lag_ledgers` absent while `lag_seconds` climbs from zero since
process start, or the log line `event indexer disabled: STELLAR_RPC_URL is
empty`.

The indexer disables itself at boot when `STELLAR_RPC_URL` is empty — a common
misconfiguration after an environment change.

---

# 2. Catch up

Pick the procedure that matches the cause. **Restarting loses no progress:**
the cursor is committed in the same transaction as the balance write, so the
indexer always resumes exactly where it stopped.

### Restart the API — Cause B, and the default action

The indexer starts with the process. A restart is the reliable way to recover a
wedged loop.

```bash
kubectl rollout restart deployment/nester-api
kubectl rollout status deployment/nester-api
```

If cursor persistence is failing, **fix the database problem first** or it will
wedge again immediately.

### Fix the configuration and restart — Cause C

Set `STELLAR_RPC_URL` and restart. On the next tick the indexer cold-starts 12
ledgers below the tip (`DefaultColdStartOffset`) and persists that cursor
immediately.

### Let it catch up on its own — Cause A

Usually self-correcting once the underlying pressure clears. Watch the trend
rather than acting: if `lag_seconds` is falling, it is recovering, and
restarting does not make it faster. Escalate if it is still climbing after 15
minutes.

### Force a one-shot sync — after a bounded RPC outage

Runs an indexing pass synchronously from the current cursor and returns how
many events it applied. Safe to repeat: every event is deduplicated by
`processed_events.event_id`.

```bash
curl -s -X POST https://<api-host>/api/v1/admin/sync-events \
  -H "Authorization: Bearer $ADMIN_TOKEN" | jq
# => {"success":true,"data":{"processed":37}}
```

Use this when the indexer is healthy again and you want the gap closed now
rather than at the next tick.

### Replay a ledger range — when events were missed

For a known window (an outage with a start and end ledger), use the backfill
runner rather than moving the cursor by hand. It checkpoints, so a long run can
be resumed.

```bash
# Reprocess a range; relies on processed_events deduplication.
curl -s -X POST https://<api-host>/api/v1/admin/backfill \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H 'Content-Type: application/json' \
  -d '{"from_ledger":493000,"to_ledger":494000,"mode":"backfill","dry_run":true}' | jq

# Resume an interrupted run from its last checkpoint.
curl -s -X POST https://<api-host>/api/v1/admin/backfill/<run-id>/resume \
  -H "Authorization: Bearer $ADMIN_TOKEN" | jq
```

**Always `dry_run` first.** `mode: "rebuild"` additionally clears derived rows
and their `processed_events` entries so they are reapplied — it is destructive
and should only be used when the derived state is known to be wrong, not merely
behind.

### Get past one permanently failing event — last resort

Only when Cause B sub-cause 1 is confirmed: the same `apply event <id> (ledger
N)` repeating, and the underlying defect cannot be fixed quickly.

Advancing the cursor past a failing event **permanently skips it** — that
balance mutation will never be applied and the vault's balance will be wrong
until it is reconciled. Get a second engineer on the call, record the event ID
and ledger, and open a reconciliation ticket in the same breath.

```sql
-- Record what you are skipping FIRST.
SELECT * FROM processed_events WHERE ledger_sequence = <N>;

UPDATE system_state
SET value = '<N+1>', updated_at = NOW()
WHERE key = 'event_indexer.last_ledger';
```

Prefer fixing the event handler and restarting. This is the only procedure here
that loses data.

## Communicate

If staleness exceeds ~30 minutes, tell users balances are delayed. That is far
better than users concluding funds have vanished. The API is already returning
`X-Indexer-Stale: true`, so the app should be showing an "as of" indicator —
confirm it is.

**Never** silence these alerts to stop the noise. A stale balance nobody is
alerted about is how a trust incident starts.

---

# 3. Verify recovery

Work down this list. **Check 2 is not optional.**

**1. The cursor is advancing.** Run it twice, ten seconds apart:

```bash
docker compose exec postgres psql -U nester nester_dev -c \
  "SELECT value, updated_at FROM system_state WHERE key = 'event_indexer.last_ledger';"
```

`value` must increase and `updated_at` must be recent. This is ground truth and
does not depend on any metric being correct.

**2. The freshness signal is live, not frozen.**

```promql
nester_indexer_lag_last_sample_age_seconds
```

Near zero and resetting. A staleness of zero with a climbing sample age means
the indexer is still dead and the number beside it is stale.

**3. Staleness is back inside the budget and falling.**

```promql
nester_indexer_lag_seconds < nester_indexer_staleness_budget_seconds
```

The dashboard's "Data staleness against the budget" panel shows this as the
line dropping back under the budget line. Falling matters as much as being
under: a flat value just below the budget is not recovery.

**4. Ledger lag is present and low.**

```promql
nester_indexer_lag_ledgers
```

The series must **exist** — its return is what proves the indexer is reporting
a position again — and sit well under 60.

**5. Samples are clean.**

```promql
rate(nester_indexer_lag_sample_errors_total[5m])
```

Zero.

**6. The API reports fresh data to clients.**

```bash
curl -s -D - -o /dev/null https://<api-host>/api/v1/vaults -H "Authorization: Bearer $TOKEN" \
  | grep -i '^x-indexer-stale'
# => X-Indexer-Stale: false
```

This is the acceptance test: it is what a user's app sees.

**7. The alert clears.** `IndexerStalenessBudgetExceeded` resolves within a
scrape interval or two of check 3 passing. If it does not, the budget series
and the lag series are not both being scraped — check the target is up.

**8. Spot-check a real deposit.** Confirm one recently settled deposit appears
in the app.

---

## Escalation

Escalate when:

- Staleness exceeds 30 minutes, or is still climbing after a restart.
- `IndexerLagStale` persists after a restart — indicates a deeper fault.
- Cursor persistence is failing (progress impossible until fixed).
- You are considering skipping a failing event.
- Users are reporting missing deposits.

Escalate to the repository maintainers (CODEOWNERS). Missing-deposit reports
escalate immediately — they need reconciliation, not only recovery.

---

## Follow-up

**Postmortem required** when staleness exceeded 30 minutes, when users reported
missing deposits, or when an event was skipped.

Cover cause, how long balances were stale, whether users noticed before the
alert fired, and whether this runbook led to the cause. **If it did not, fix it
in the same PR.**

Balance freshness has no error budget — it is a gauge, not a ratio of events —
so [the error-budget policy](../error-budget-policy.md) does not apply
mechanically. A sustained breach is still grounds for prioritising reliability
work by judgement.

If the indexer was stopped and `IndexerStalenessBudgetExceeded` did **not**
fire, that is a serious finding for the next [SLO review](../slo-review.md):
that alert is the mechanism that makes this SLI trustworthy at all.
