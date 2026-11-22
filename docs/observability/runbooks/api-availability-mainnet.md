# Runbook — Mainnet API Availability

**Alerts:** `APIAvailabilityMainnetFastBurn` (page), `APIAvailabilityMainnetSlowBurn` (ticket)
**Dashboard:** [SLO — API Availability (Mainnet)](/d/nester-slo-api/api-availability?var-environment=mainnet)
**Issue:** nester#1421

---

## What the alert means

**This is the mainnet production SLO.** Degraded availability here affects real users and real money.

| Alert | Meaning |
|---|---|
| `APIAvailabilityMainnetFastBurn` | Mainnet is burning error budget at 13.4×. Full 28-day budget exhausted in ~50 hours. **Page immediately.** |
| `APIAvailabilityMainnetSlowBurn` | Sustained elevated 5xx ratio. Budget exhausted in ~5 days. **Ticket: fix in business hours.** |

The 99.9% target permits 40 minutes of unavailability per 28 days. These alerts fire when the observed error rate will exhaust that budget faster than the SLO permits.

**What counts as unavailable:**
- 5xx responses on registered routes
- Panics, unhandled errors, timeouts that surface as 500/503

**What is excluded:**
- 4xx (client errors — correctly rejected requests)
- `route="other"` (unmatched paths: scanners, probes, typos)
- 429 (rate limiting is the system defending itself, not failing)

A fast-burn alert means "this is consuming budget quickly **right now**" — an active incident. A slow-burn alert means "this has been degraded for hours and will not resolve itself."

---

## Error budget policy (nester#1421)

Mainnet has a formal error budget policy (see [`docs/observability/error-budget-policy.md`](../error-budget-policy.md)) that defines four states based on budget remaining:

| State | Budget Remaining | Response |
|---|---|---|
| **Healthy** | > 50% | Normal development |
| **Watch** | 25–50% | Named in SLO review; known issues get owners |
| **Warning** | 0–25% | Reliability work prioritized; changes require justification |
| **Exhausted** | ≤ 0% | Feature work paused; deploys limited to fixes and rollbacks |

**The alerts fire on burn rate. The policy gates work based on accumulated budget consumption over 28 days.** Check budget state in the dashboard after resolving the active incident.

---

## First three actions

**1. Confirm this is mainnet, not a false positive.**

```promql
api:availability:error_ratio_rate1h{environment="mainnet"}
api:availability:error_ratio_rate5m{environment="mainnet"}
```

If the series are absent or zero while the alert is firing, the environment label may be misconfigured. Check `external_labels` in Prometheus config.

**2. Identify the failing routes.**

```promql
sum by (route) (rate(nester_http_requests_total{environment="mainnet",route!="other",status_class="5xx"}[5m]))
```

This breaks down 5xx traffic by route. A single route failing points to a specific handler; all routes failing points to infrastructure (database, circuit breaker, network).

**3. Check budget state and determine policy response.**

```promql
1 - (api:availability:error_ratio_rate28d{environment="mainnet"} / 0.001)
```

- If budget remaining is negative (exhausted), follow the exhausted-state policy immediately after incident resolution.
- If budget remaining is between 0–25% (warning), reliability work must be prioritized.
- The 28-day window means recovery cannot be rushed — budget state reflects trailing history, not just current behavior.

---

## Cause A — database connectivity

**Symptoms:** All routes returning 5xx, high `nester_db_pool_acquired_connections` against `nester_db_pool_max_connections`.

1. Check connection pool exhaustion:

   ```promql
   nester_db_pool_acquired_connections / nester_db_pool_max_connections
   ```

   If near 1.0, the pool is exhausted. Check for slow queries (`pg_stat_activity`), long-held transactions, or a query that acquired a lock and never released it.

2. Check for database availability:

   ```bash
   psql $DATABASE_DSN -c "SELECT 1"
   ```

   If unreachable, the database is down or network-partitioned. Check RDS/Aurora status, security groups, and DNS resolution.

3. Check statement timeouts and query latency. A slow query under high load can exhaust the pool even when the database is healthy.

**Recovery:** If the pool is exhausted and no individual query is at fault, consider raising `DATABASE_POOL_SIZE` — but only after confirming the database can handle the increased connection count.

---

## Cause B — circuit breaker open

**Symptoms:** Soroban RPC or Horizon circuit breaker is open. Routes that call the chain return 503.

```promql
nester_circuit_breaker_state{name=~"soroban_rpc|horizon"}
```

- `0` = closed (healthy)
- `1` = half-open (testing recovery)
- `2` = open (shedding traffic)

If open:
1. Check the upstream itself. Is Soroban RPC or Horizon reachable?

   ```bash
   curl -s https://soroban-mainnet.stellar.org/health
   curl -s https://horizon.stellar.org/
   ```

2. Check breaker metrics to see what triggered it:

   ```promql
   rate(nester_circuit_breaker_requests_total{name="soroban_rpc",outcome="success"}[5m])
   rate(nester_circuit_breaker_requests_total{name="soroban_rpc",outcome="error"}[5m])
   ```

   High error rate against a healthy upstream means the breaker policy may be too aggressive. Check `BREAKER_*` config.

3. The breaker self-recovers via half-open probes. Do not restart the service to "reset" it — that destroys the in-memory context of what was mid-flight.

**Recovery:** If the upstream is healthy but the breaker is stuck open, consider manually transitioning it to half-open (if tooling exists) or adjusting breaker thresholds.

---

## Cause C — specific route failing

**Symptoms:** A single route or small set of routes is returning 5xx while others are healthy.

1. Identify the failing route(s) from [First actions step 2](#first-three-actions).

2. Check logs for panics or unhandled errors in that route's handler:

   ```bash
   grep "route=GET /api/v1/vaults" <api logs> | grep -i "error\|panic"
   ```

3. Check whether the failure is conditional. Does it only happen for certain inputs, certain vaults, certain users?

4. Check the route's dependencies:
   - Does it query the database? Check for missing indexes, slow queries, or statement timeouts.
   - Does it call the chain? Check circuit breaker state and RPC latency.
   - Does it read from cache? Check Redis connectivity.

**Recovery:** If the handler is panicking, the stack trace in logs points to the line. If the failure is conditional, reproduce it in staging and fix the bug. If the failure is load-dependent, consider rate-limiting or caching that route.

---

## Cause D — deployment or configuration change

**Symptoms:** 5xx ratio spiked immediately following a deploy or config change.

1. Check recent deployments:

   ```bash
   gh run list --repo Suncrest-Labs/nester --workflow deploy.yml --limit 5
   ```

2. If the spike correlates with a deploy, check whether the new version introduced a bug:
   - Check logs for new errors or panics.
   - Check traces for new slow or failing spans.
   - Compare request latency before and after deploy.

3. If the deploy is bad, **roll back immediately**. Do not wait to diagnose — mainnet availability takes precedence over understanding the cause.

   ```bash
   # Roll back to previous image or trigger rollback via deploy workflow
   ```

4. If a config change caused it, revert the config and redeploy.

**Recovery:** After rollback, investigate in staging. Do not retry the deploy until the root cause is understood and fixed.

---

## Cause E — traffic spike or abuse

**Symptoms:** 5xx ratio climbs under traffic far higher than normal. Request rate is elevated.

```promql
rate(nester_http_requests_total{environment="mainnet"}[5m])
```

1. Check whether the spike is legitimate (marketing campaign, press mention, viral tweet) or abusive (scraping, DDOS, misconfigured client looping).

2. If abusive, check the rate limiter:

   ```promql
   rate(nester_http_requests_total{environment="mainnet",status_class="4xx",status="429"}[5m])
   ```

   If not shedding, the rate limiter may be too permissive. Adjust `RATE_LIMIT_*` config or block the abusive source at the load balancer.

3. If legitimate, the service is under-provisioned. Scale up replicas, raise database pool size, or cache more aggressively.

**Recovery:** For abuse, block the source or tighten rate limits. For legitimate traffic, scale horizontally and consider circuit breakers or backpressure to gracefully degrade rather than fail hard.

---

## Verifying recovery

Do not resolve on the alert clearing alone. Confirm:

```promql
# 1. 5xx rate is back to baseline (near zero)
rate(nester_http_requests_total{environment="mainnet",status_class="5xx"}[5m])

# 2. Error ratio on both windows is below threshold
api:availability:error_ratio_rate1h{environment="mainnet"}
api:availability:error_ratio_rate5m{environment="mainnet"}

# 3. Request success rate is normal (near 100%)
(
  sum(rate(nester_http_requests_total{environment="mainnet",route!="other",status_class=~"2xx|3xx"}[5m]))
  /
  sum(rate(nester_http_requests_total{environment="mainnet",route!="other"}[5m]))
) * 100
```

**After recovery, check error budget state:**

```promql
1 - (api:availability:error_ratio_rate28d{environment="mainnet"} / 0.001)
```

If the incident consumed significant budget (budget remaining now < 25%), follow the error budget policy:
- Schedule a postmortem
- Assign an owner to reliability work
- Check whether feature work should be gated per the policy

---

## Related

- [API Availability (general)](api-availability.md) — non-mainnet availability alerts
- [Error Budget Policy](../error-budget-policy.md) — formal policy for exhausted/warning/watch states
- [SLO documentation](../slo.md) — SLI definitions, burn-rate arithmetic, targets
- [Flow success](flow-success.md) — deposit/withdrawal failure SLOs (separate from API availability)
