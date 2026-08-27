# Runbook — Balance freshness (indexer lag)

**Alerts:** `IndexerLagHigh` (page), `IndexerLagStale` (page), `IndexerMetricsAbsent` (page)
**Dashboard:** [SLO — Balance freshness](/d/nester-slo-balance/balance-freshness)
**SLO:** indexer lag ≤ 60 ledgers (~5 minutes)

---

## What the alert means

**Read which alert fired first — they mean different things and one of them is
a trap.**

| Alert | Meaning |
|---|---|
| `IndexerLagHigh` | The indexer is running but falling behind the chain tip. |
| `IndexerLagStale` | The indexer stopped reporting. **The lag value may look perfectly healthy and must not be trusted.** |
| `IndexerMetricsAbsent` | The freshness series is gone entirely — API down, scrape broken, or the indexer never started. |

`IndexerLagStale` exists because a lag gauge alone cannot distinguish "lag is
2 ledgers" from "the sampler died and the value is frozen at 2". The second
silently reports perfect health, which is the worst possible failure for a
freshness signal. If this alert is firing, **ignore the lag number entirely**
and treat the indexer as stopped.

**User impact:** users see stale balances. A deposit that has settled on-chain
does not appear in the app. This pages — rather than tickets — because a
savings product showing the wrong balance is a trust failure, not a cosmetic
one, and users conclude their money is missing.

---

## First three actions

**1. Establish whether the indexer is running at all.**

```promql
nester_indexer_lag_last_sample_age_seconds
```

- Near zero and resetting → running, genuinely behind. Go to
  [Cause A](#cause-a--indexer-falling-behind).
- Climbing steadily → **stopped**. Go to
  [Cause B](#cause-b--indexer-stopped-or-wedged).
- No series → go to [Cause C](#cause-c--indexer-never-started).

**2. Check for sampling errors.**

```promql
rate(nester_indexer_lag_sample_errors_total[5m])
```

Non-zero means the indexer loop is erroring — cursor load, contract load, or
the Soroban fetch. The logs in the next section name which.

**3. Check the RPC dependency.**

```promql
sum by (kind) (rate(nester_outbound_errors_total{upstream="soroban_rpc"}[5m]))
histogram_quantile(0.95, sum by (le) (rate(nester_outbound_request_duration_seconds_bucket{upstream="soroban_rpc"}[5m])))
```

The indexer polls Soroban every 6s. A slow or failing endpoint stalls it
directly.

---

## Dashboards

| Panel | Question it answers |
|---|---|
| **Indexer lag** | How far behind, right now |
| **Lag sample age** | Whether the lag number can be trusted at all |
| **Sample errors** | Whether the loop is erroring |
| **Indexer lag over time** | Sawtooth (normal) vs monotonic climb (stall) |

A **sawtooth** pattern is healthy — the cursor advances in steps. A **monotonic
climb** is a stall.

---

## Logs

```bash
kubectl logs -l app=nester-api --since=30m | grep -i "event indexer"
```

The loop logs a distinct message per failure mode:

| Log message | Meaning |
|---|---|
| `event indexer failed to load cursor` | `system_state` read failing — database problem |
| `event indexer failed to load vault contracts` | Contract query failing — database problem |
| `event indexer fetch failed` | Soroban RPC failing |
| `event indexer failed to apply event` | Per-event write failure; loop continues |
| `event indexer failed to persist cursor` | **Cursor not advancing — the loop reprocesses forever** |
| `event indexer disabled: STELLAR_RPC_URL is empty` | Never started at all |

The last two are the most consequential. A failing cursor persist means the
indexer runs continuously without ever making progress.

---

## Traces

Indexer polls are background work, not request-scoped, so tracing coverage is
thinner here than on the request path. Use the outbound metrics above for RPC
health; use Jaeger for `soroban_rpc` spans if the invoker is instrumented in
your deployment.

---

## Three most likely causes

### Cause A — Indexer falling behind

Running, sampling normally, but lag is climbing.

**Distinguishing evidence:** `sample_age_seconds` resets to zero regularly
(loop alive) while `lag_ledgers` climbs.

Sub-causes, in order of likelihood:

1. **Soroban RPC slow.** p95 outbound duration elevated; each poll takes longer
   than the 6s tick, so it cannot keep up.
2. **Event volume spike.** Many contracts or a burst of activity; the apply
   loop is the bottleneck.
3. **Database write contention.** `applyIndexedEvent` slow; check pool metrics.

**Mitigation:** usually self-correcting once the underlying pressure clears.
Watch the trend rather than acting immediately — if lag is falling, it is
recovering. If it climbs past several hundred ledgers, escalate: catch-up time
grows with the gap.

### Cause B — Indexer stopped or wedged

**Distinguishing evidence:** `sample_age_seconds` climbing steadily.
`IndexerLagStale` firing. The lag gauge is frozen — **it will often read a
perfectly healthy value, which is exactly the trap this alert exists for.**

Sub-causes:

1. **Cursor persist failing** — `failed to persist cursor` in the logs. The
   loop runs, reprocesses the same ledgers, never advances. Usually a database
   write problem.
2. **Goroutine dead.** The loop exited or panicked. Check `go_goroutines` for a
   step down, and the logs for a panic.
3. **Context cancelled.** Shutdown began but the process did not exit cleanly.

**Mitigation:** restart the API. The indexer starts with the process and a
restart is the reliable way to recover a wedged loop. If cursor persistence is
failing, fix the database problem first or it will wedge again immediately.

### Cause C — Indexer never started

**Distinguishing evidence:** `IndexerMetricsAbsent` firing, or the log line
`event indexer disabled: STELLAR_RPC_URL is empty`.

The indexer disables itself silently at boot when `STELLAR_RPC_URL` is empty.
This is a common misconfiguration after an environment change, and without
`IndexerMetricsAbsent` it would produce no signal whatsoever.

**Mitigation:** set `STELLAR_RPC_URL` and restart.

---

## Immediate mitigation

1. **Restart the API** if the indexer is wedged (Cause B). Most effective and
   most common.
2. **Fix the configuration** and restart if it never started (Cause C).
3. **Fail over the RPC endpoint** if Soroban is degraded and a secondary exists.
4. **Wait and monitor** if it is catching up under load (Cause A) — restarting
   loses no progress, the cursor is persisted, but it also does not help.

**Communicate** if lag exceeds ~1000 ledgers (roughly 90 minutes): at that
point users are seeing materially stale balances and will contact support.
Telling them balances are delayed is far better than users concluding funds
have vanished.

**Never** silence these alerts to stop the noise. A stale balance that nobody
is alerted about is how a trust incident starts.

---

## Escalation

Escalate when:

- Lag exceeds 1000 ledgers, or is still climbing after a restart.
- `IndexerLagStale` persists after a restart — indicates a deeper fault.
- Cursor persistence is failing (progress impossible until fixed).
- Users are reporting missing deposits.

Escalate to the repository maintainers (CODEOWNERS). Missing-deposit reports
escalate immediately — they need reconciliation, not only recovery.

---

## Recovery verification

1. `nester_indexer_lag_ledgers` below 60 **and falling or stable**.
2. `nester_indexer_lag_last_sample_age_seconds` near zero and resetting — this
   is the one that proves the signal is live rather than frozen.
3. `rate(nester_indexer_lag_sample_errors_total[5m])` at zero.
4. Balance probe passing:
   ```promql
   nester_probe_success{probe="balance"}
   ```
5. Spot-check one recently settled deposit appearing in the app.

**Check 2 is not optional.** A lag of zero with a climbing sample age means the
indexer is still dead and the number is stale.

---

## Follow-up

**Postmortem required** when lag exceeded 1000 ledgers, when users reported
missing deposits, or when the indexer was stopped for more than 30 minutes.

Cover cause, how long balances were stale, whether users noticed before the
alert fired, and whether this runbook led to the cause. **If it did not, fix it
in the same PR.**

Balance freshness has no error budget — it is a gauge, not a ratio of events —
so [the error-budget policy](../error-budget-policy.md) does not apply
mechanically. A sustained breach is still grounds for prioritising reliability
work by judgement.

If the indexer was stopped and `IndexerLagStale` did **not** fire, that is a
serious finding for the next [SLO review](../slo-review.md): the staleness
guard is the mechanism that makes this SLI trustworthy at all.
