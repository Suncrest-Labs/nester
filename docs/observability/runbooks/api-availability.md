# Runbook — API availability

**Alerts:** `APIAvailabilityFastBurn` (page), `APIAvailabilitySlowBurn` (ticket)
**Dashboard:** [SLO — API availability](/d/nester-slo-api/api-availability)
**SLO:** 99.9% non-5xx, 28-day window (budget ≈ 40 minutes)

---

## What the alert means

The API is returning 5xx at a rate that will exhaust the 28-day budget in ~50
hours (fast burn, 1.344%) or ~5 days (slow burn, 0.56%).

**What is counted:** 5xx only, over requests that matched a registered route.
4xx is excluded from both halves of the ratio, and `route="other"` (scanners,
typos, probes) is excluded entirely. So this alert cannot be triggered by a
client sending bad requests, however many — it is always server-side failure.

**User impact:** requests are failing for users who did nothing wrong. Because
every user-facing action passes through this API, availability failures are
maximally visible.

---

## First three actions

**1. Find which routes are failing.** This narrows the search immediately:

```promql
topk(10, sum by (route) (rate(nester_http_requests_total{status_class="5xx"}[5m])))
```

- One route → a specific handler or its dependency.
- All routes → a shared dependency (database, Redis) or the process itself.

**2. Check whether a deploy correlates.**

```bash
gh run list --repo Suncrest-Labs/nester --workflow ci.yml --limit 10
git log --oneline -15 origin/main
```

A step change at a deploy boundary is a code change. A gradual ramp is capacity
or a dependency.

**3. Check the shared dependencies.**

```promql
nester_db_pool_acquired_connections / nester_db_pool_max_connections
sum(rate(nester_redis_errors_total[5m]))
sum by (upstream, kind) (rate(nester_outbound_errors_total[5m]))
```

If any of these is unhealthy, it is almost certainly the cause rather than a
symptom.

---

## Dashboards

| Panel | Question it answers |
|---|---|
| **5xx ratio vs burn thresholds** | How far over, and on which windows |
| **Eligible request rate** | Is volume also collapsing (see note below) |
| **Requests by status class** | Is 4xx also moving (points at a client change) |
| **Slowest routes (p95)** | Which handler is struggling |
| **Dependency health** | Upstream, Redis, and transport failures |

**Read the volume panel alongside the ratio.** A ratio improving while volume
collapses is not recovery — it usually means requests are being shed before
they reach the handler.

---

## Logs

```bash
# 5xx responses
kubectl logs -l app=nester-api --since=30m | grep -E '"status":5[0-9]{2}'

# Panics
kubectl logs -l app=nester-api --since=30m | grep -iE "panic|runtime error"

# Restarts — a crash loop shows here before anywhere else
kubectl get pods -l app=nester-api
```

Locally: `docker compose logs api --since 30m`.

Correlate a failing request with its trace using the `X-Request-Id` header,
which appears in both the log line and the span.

---

## Traces

In [Jaeger](http://localhost:16686), service `nester-api`, filter `error=true`
on the failing route from step 1. The span tree shows which downstream call
failed — database, Redis, Soroban RPC, or the intelligence service — without
guessing from logs.

Sampled, so use traces to understand a failure rather than to count them.

---

## Three most likely causes

### Cause A — Database unavailable or pool exhausted

**Distinguishing evidence:**

```promql
nester_db_pool_acquired_connections / nester_db_pool_max_connections
rate(nester_db_pool_empty_acquire_waits_total[5m])
rate(nester_db_pool_canceled_acquires_total[5m])
```

Confirmed when failures span **many routes** and acquired connections sit near
max with rising empty-acquire waits. Read-only routes failing alongside writes
strongly indicates the pool rather than a specific query.

**Mitigation:** identify and terminate long-running queries holding connections:

```sql
SELECT pid, now() - query_start AS duration, state, left(query, 120)
FROM pg_stat_activity
WHERE state <> 'idle' AND now() - query_start > interval '30 seconds'
ORDER BY duration DESC;
```

If a deploy introduced a slow query, roll back.

### Cause B — Upstream dependency failing

**Distinguishing evidence:**

```promql
sum by (upstream, kind) (rate(nester_outbound_errors_total[5m]))
sum by (upstream, status_class) (rate(nester_outbound_requests_total[5m]))
```

Confirmed when failures concentrate on routes that call one upstream, and that
upstream's error rate rises at the same time. The `upstream` label names it
directly: `soroban_rpc`, `horizon`, `coingecko`, `defillama`,
`anthropic_relay`, `intelligence`.

**Mitigation:** if the failing upstream backs a non-critical feature, disabling
that feature restores availability for everything else. If it is
`soroban_rpc`, see [flow-success](flow-success.md) — the money paths are the
larger concern.

### Cause C — Bad deploy

**Distinguishing evidence:** sharp step at a deploy boundary; failures
concentrated on routes touched by the change; possibly panics in the logs.

**Mitigation:** roll back. Do not diagnose first — the diagnosis is easier
afterwards with the pressure removed.

---

## Immediate mitigation

1. **Roll back** if a deploy correlates. Fastest, most reliable.
2. **Restart** if the process is wedged (goroutine or memory growth visible in
   `go_goroutines`, `process_resident_memory_bytes`). Buys time; find the leak
   afterwards.
3. **Relieve the database** — kill blocking queries — if the pool is the cause.
4. **Disable the affected feature** if a non-critical upstream is failing.

**Do not** silence the alert or disable metrics. Availability failures are
already user-visible; removing the signal does not remove the problem.

---

## Escalation

Escalate when:

- 5xx ratio above 10% for more than 10 minutes.
- The API is fully down (`SLOTargetDown` also firing).
- Two or more mitigations attempted without improvement.
- Availability failures accompanied by flow failures — money paths take
  priority, see [flow-success](flow-success.md).

Escalate to the repository maintainers (CODEOWNERS).

---

## Recovery verification

1. `api:availability:error_ratio_rate5m` below threshold for **15 minutes**.
2. Eligible request volume back to normal — a low ratio with no traffic is not
   recovery.
3. Synthetic probes passing:
   ```promql
   nester_probe_success
   ```
4. No pod restarts in the last 15 minutes.
5. `budget remaining` noted for the incident record.

The alert clearing on its own is not sufficient: the 5-minute confirmation
window makes it resolve quickly by design.

---

## Follow-up

**Postmortem required** for a fast-burn (page) firing, or for any incident
consuming more than 10% of the 28-day budget.

Cover cause, detection time, mitigation time, budget consumed, and whether this
runbook led to the cause. **If it did not, fix it in the same PR** — it is most
accurate right after being used under pressure.

If the budget is now in warning or exhausted, apply
[the error-budget policy](../error-budget-policy.md).

Note whether the alert fired before users reported the problem. Detection
lagging user reports is a finding for the next [SLO review](../slo-review.md).
