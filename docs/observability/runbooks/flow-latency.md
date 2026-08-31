# Runbook — Deposit / withdrawal latency

**Alerts:** `FlowLatencyFastBurn`, `FlowLatencySlowBurn` (both ticket)
**Dashboard:** [SLO — Deposits and withdrawals](/d/nester-slo-flow/deposits-withdrawals)
**SLO:** 99% of successful settlements complete within 30s, 28-day window

---

## What the alert means

Settlements are completing but taking longer than 30s (~6 ledgers) more often
than the budget permits.

**Money is still arriving.** That is the essential distinction from
[flow-success](flow-success.md), and it is why this tickets rather than pages:
slow is strictly better than failed, and there is rarely a 3am action that
speeds up ledger close timing.

**If settlement has stopped entirely,** `FlowSuccessFastBurn` is the alert that
covers it and it pages. Check whether that is also firing before treating this
as a latency problem.

**What is measured:** successful settlements only, from the service accepting
the request to the chain call returning and the ledger row being written.
Failures are excluded — a failure's duration measures how long the failure took
to surface, which would let a fast-failing outage look like a latency
improvement.

**User impact:** users watch a spinner, retry, or contact support. A retried
deposit is a support burden even when both attempts eventually succeed.

---

## First three actions

**1. Check whether Soroban is slow.** This is the cause most of the time:

```promql
histogram_quantile(0.95, sum by (le) (rate(nester_outbound_request_duration_seconds_bucket{upstream="soroban_rpc"}[5m])))
sum by (kind) (rate(nester_outbound_errors_total{upstream="soroban_rpc"}[5m]))
```

Elevated p95 with `kind="timeout"` appearing means the endpoint is degraded.

**2. Check whether both flows are affected.**

```promql
flow:latency:p95_5m
```

- Both → a shared dependency (RPC, database, network).
- One only → something specific to that path, which is unusual and points at a
  code change.

**3. Confirm success rate is holding.**

```promql
flow:success:error_ratio_rate5m
```

Latency degrading *and* failures rising means the path is on its way to
breaking outright — treat it as the more serious incident and use
[flow-success](flow-success.md).

---

## Dashboards

| Panel | Question it answers |
|---|---|
| **Settlement latency p95** | How slow, per flow |
| **Share slower than 30s** | The SLI itself, against thresholds |
| **Soroban RPC health** | Is the chain dependency the cause |
| **Failure ratio by flow** | Is this becoming a failure incident |

---

## Logs

```bash
# Slow requests on the money-path routes
kubectl logs -l app=nester-api --since=30m | grep -E "deposits|withdrawals" | grep -E '"duration_ms":[0-9]{5,}'

# Database contention
kubectl logs -l app=nester-api --since=30m | grep -iE "slow query|context deadline"
```

Locally: `docker compose logs api --since 30m`.

---

## Traces

Traces are the most direct tool for this alert, because the question is *where*
the time goes.

In [Jaeger](http://localhost:16686), service `nester-api`, filter the deposit
or withdrawal handler with a minimum duration of 30s. The span tree splits the
elapsed time between:

- the Soroban invocation (usually dominant),
- the database write,
- our own handler work.

Tracing from #1054 retains slow requests regardless of sample ratio
(`TRACING_LATENCY_THRESHOLD`), so slow settlements are specifically the ones
most likely to have a trace.

---

## Three most likely causes

### Cause A — Soroban RPC degraded

**Distinguishing evidence:** outbound p95 to `soroban_rpc` elevated, tracking
the settlement p95. Timeouts appearing without full failure.

By far the most common cause. Ledger close timing and RPC responsiveness are
outside our control.

**Mitigation:** fail over to a secondary RPC endpoint if one is configured.
Otherwise monitor — this usually resolves without action. Escalate only if it
persists for hours or begins producing failures.

### Cause B — Database write contention

**Distinguishing evidence:**

```promql
nester_db_pool_acquired_connections / nester_db_pool_max_connections
rate(nester_db_pool_empty_acquire_waits_total[5m])
histogram_quantile(0.95, sum by (le) (rate(nester_http_request_duration_seconds_bucket[5m])))
```

Confirmed when pool utilisation is high and API latency is broadly elevated,
not only on money paths. The ledger write is waiting for a connection.

**Mitigation:** terminate long-running queries; roll back if a deploy
introduced a slow query or a missing index.

### Cause C — Network conditions or ledger congestion

**Distinguishing evidence:** Soroban p95 elevated but error-free, and the
degradation is visible on public network monitoring rather than only on our
side.

**Mitigation:** none available to us. Monitor, and communicate if settlements
routinely exceed a minute.

---

## Immediate mitigation

1. **Fail over the RPC endpoint** if degraded and a secondary exists.
2. **Relieve database pressure** if the pool is the cause.
3. **Roll back** if a deploy correlates.
4. **Communicate** if settlements routinely exceed a minute. Users who know a
   deposit is delayed do not retry; users who do not know, do — and retries
   make the load worse.

---

## Escalation

Escalate when:

- p95 exceeds 120s, at which point users reliably assume failure.
- Latency degradation is accompanied by rising failures — this is becoming an
  outage.
- Degradation persists beyond four hours with no identified cause.

Escalate to the repository maintainers (CODEOWNERS). Business-hours work unless
it is turning into a failure incident.

---

## Recovery verification

1. `flow:latency:error_ratio_rate5m` below threshold for 15 minutes.
2. `flow:latency:p95_5m` back inside the normal 5–15s band.
3. Settlements are actually occurring — the ratio is meaningless at zero volume.
4. Probes passing and fast:
   ```promql
   nester_probe_duration_seconds{probe=~"deposit|withdrawal"}
   ```
5. Success rate unaffected.

---

## Follow-up

**Postmortem required** only when the cause was ours — a slow query, a missing
index, a code change — or when latency degradation became a failure incident.

RPC or network degradation does not need a postmortem, but **is** worth
recording. Repeated occurrences are the evidence base for adding a secondary
RPC endpoint, and that decision needs data rather than impressions.

If the budget is in warning or exhausted, apply
[the error-budget policy](../error-budget-policy.md).

If this alert fires frequently without user complaints, the 30s threshold may
be tighter than user tolerance — a question for the next
[SLO review](../slo-review.md). The opposite is also worth watching: users
complaining about slowness while this SLO stays green means the threshold is
too loose.
