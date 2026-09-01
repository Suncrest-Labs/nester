# API Metrics

The Go API exposes Prometheus metrics covering request behaviour, database
pool health, Redis, and outbound dependencies.

Instrumentation lives in `apps/api/internal/metrics`. Everything is registered
once at startup on a dedicated registry and threaded to the instrumentation
points; nothing in the request path creates a collector.

## Contents

- [Accessing /metrics](#accessing-metrics)
- [Cardinality policy](#cardinality-policy)
- [Metric reference](#metric-reference)
- [Local observability stack](#local-observability-stack)
- [Adding a metric](#adding-a-metric)

## Accessing /metrics

`/metrics` is served on a **separate internal listener**, not on the public
API router.

| Setting | Default | Meaning |
| --- | --- | --- |
| `METRICS_ENABLED` | `true` | Starts the internal listener. |
| `METRICS_ADDR` | `127.0.0.1:9090` | Address the internal listener binds to. |

The internal listener serves exactly two paths:

- `GET /metrics` — Prometheus exposition format
- `GET /healthz` — liveness for the metrics listener itself, so "the scrape
  target is down" is distinguishable from "the API is down"

### Why it is not public

An open `/metrics` publishes the full internal route table, per-route traffic
volumes, and error rates — a map of the service plus a live signal of when it
is degraded. It is unauthenticated read access to operational intelligence.

Two options were available: gate it behind the existing `Authenticate`
middleware, or bind it to a separate listener. This uses a **separate
listener** because:

- `Authenticate` matches by path prefix and defaults unmatched routes to
  protected. That is correct today, but it makes the guarantee depend on rule
  ordering staying correct as the route table grows. A separate listener has
  no rule to misorder.
- The public mux never has the `/metrics` pattern registered at all, so a
  request for it on the public port falls through to normal 404 handling.
  There is no handler to reach, authenticated or otherwise.
- The default bind is loopback, so the endpoint is unreachable from another
  host unless someone deliberately changes it.

Enforced by tests in `apps/api/internal/metrics/server_test.go`:
`TestMetricsNotReachableOnPublicRouter`, `TestMetricsServerBindsSeparateListener`,
and `TestMetricsServerOnlyExposesExpectedPaths`.

### Deployment notes

- Keep `METRICS_ADDR` on loopback unless a scraper must reach it across the
  network. If it must, bind it to an interface the public ingress does not
  route to, and confirm no ingress rule or load-balancer target group
  forwards to that port.
- In Docker, bind to `0.0.0.0:9090` **and publish no host port** for it, so
  the endpoint is reachable on the container network only. This is what
  `docker-compose.yml` does; the `api` service publishes `8080` and nothing
  else.

## Cardinality policy

> Prometheus labels must use bounded operational dimensions. Raw paths, IDs,
> wallet addresses, transaction hashes, request IDs, query parameters, and
> arbitrary user-controlled values must never be labels.

Every label multiplies series count. A label whose values grow with traffic
does not degrade gracefully — it eventually takes the metrics backend down.

Rules applied here:

1. **Routes are labelled by pattern, never by path.** `/api/v1/vaults/{id}`,
   never `/api/v1/vaults/550e8400-…`. Unmatched paths collapse to `other`, so
   scanner traffic cannot mint a series per invented URL.
2. **Methods are allowlisted.** The method comes off the request line and is
   client-controlled, so anything outside the standard set becomes `other`.
3. **Status is bucketed into classes.** `2xx`, `4xx`, `5xx` — five values
   instead of one per code. The exact code stays in logs, where it belongs.
4. **Upstreams are constants.** `metrics.Upstream` is a named type with fixed
   values, so a hostname or URL cannot become a label.
5. **Error kinds are a closed set.** Transport failures map to
   `timeout`/`canceled`/`dns`/`connect`/`other`. The raw error text is never
   used: it commonly contains the target URL, which can carry credentials.
6. **Redis commands are allowlisted**, and keys are never labelled — keys in
   this codebase embed user IDs, session IDs, and wallet addresses.

Never acceptable as a label: user ID, wallet address, transaction hash,
request ID, raw path, query parameter, email, session ID, idempotency key,
arbitrary exception text, upstream URL.

Cardinality assertions live alongside the tests they protect —
`TestRouteLabelUsesPatternNotRawPath`, `TestMethodLabelIsBounded`,
`TestUpstreamLabelIsBounded`, `TestOutboundNeverLabelsSecrets`, and
`TestClassifyTransportErrorNeverReturnsErrorText`.

## Metric reference

All metrics are prefixed `nester_`. Histograms are in **seconds**, counters
end in `_total`, per Prometheus convention.

### HTTP requests

Source: `internal/metrics/http.go`, via middleware wrapping the public router.

| Metric | Type | Labels | Meaning |
| --- | --- | --- | --- |
| `nester_http_requests_total` | Counter | `route`, `method`, `status_class` | Requests handled. |
| `nester_http_request_duration_seconds` | Histogram | `route`, `method` | Request latency. |
| `nester_http_requests_in_flight` | Gauge | none | Requests currently being served. |

**Cardinality:** `route` is bounded by the route table (~93 patterns) plus
`other`; `method` by 9 standard methods plus `other`; `status_class` by 6.
Worst case for `requests_total` is roughly `routes × methods × classes`, but
in practice each route serves one or two methods and two or three classes, so
the real series count is a small multiple of the route count.

**Buckets:** `0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10` seconds.
Chosen for this workload rather than library defaults: handlers that call
Soroban RPC, Horizon, or the Anthropic relay routinely land in the 250ms–5s
range, and that band needs resolution. The 10s bucket plus `+Inf` keeps
timeouts visible.

The in-flight gauge is unlabelled deliberately. Per-route breakdown would
hold a series per route for the process lifetime to report a number that is
almost always zero; the single value is what a saturation alert reads. It is
decremented in a `defer`, so a panicking handler cannot leak it upward.

The middleware sits directly inside `RecoverPanic` and outside every other
layer, so latency and status include time in CORS, rate limiting, and auth. A
429 from the limiter or a 401 from the authenticator is a real outcome of a
real request.

### Database pool

Source: `internal/metrics/pgxpool.go`, a pull collector reading
`pgxpool.Pool.Stat()` at scrape time. No goroutine, no sampling, no staleness.
Instruments the existing pool — no second pool is opened.

| Metric | Type | Labels | Meaning |
| --- | --- | --- | --- |
| `nester_db_pool_acquired_connections` | Gauge | none | Connections checked out. |
| `nester_db_pool_idle_connections` | Gauge | none | Idle connections. |
| `nester_db_pool_total_connections` | Gauge | none | Connections owned by the pool. |
| `nester_db_pool_max_connections` | Gauge | none | Configured maximum. |
| `nester_db_pool_constructing_connections` | Gauge | none | Connections being established. |
| `nester_db_pool_new_connections_total` | Counter | none | Connections opened. |
| `nester_db_pool_acquires_total` | Counter | none | Successful acquisitions. |
| `nester_db_pool_empty_acquire_waits_total` | Counter | none | Acquisitions that had to wait. |
| `nester_db_pool_acquire_wait_seconds_total` | Counter | none | Cumulative time spent waiting. |
| `nester_db_pool_canceled_acquires_total` | Counter | none | Acquisitions aborted by cancellation. |
| `nester_db_pool_max_lifetime_destroys_total` | Counter | none | Connections closed at max lifetime. |
| `nester_db_pool_max_idle_destroys_total` | Counter | none | Connections closed at max idle. |

**Cardinality:** 12 series total. Unlabelled — there is one pool.

**Saturation.** `empty_acquire_waits_total` and `acquire_wait_seconds_total`
are the pair that matter. Both stay flat on a healthy pool and climb the
moment it saturates:

```promql
# Acquisitions per second that had to wait for a free connection.
rate(nester_db_pool_empty_acquire_waits_total[5m])

# Mean wait per blocked acquire.
rate(nester_db_pool_acquire_wait_seconds_total[5m])
  / rate(nester_db_pool_empty_acquire_waits_total[5m])

# Pool utilisation.
nester_db_pool_acquired_connections / nester_db_pool_max_connections
```

Verified by `TestPoolMetricsExposedIntegration`, which builds a
`MaxConns: 1` pool, holds the only connection, forces a second acquire to
queue behind it, and asserts both counters rise. It runs when
`TEST_DATABASE_DSN` or `DATABASE_URL` is set; CI sets the latter.

To reproduce manually: set `DATABASE_POOL_SIZE=1`, issue two concurrent
requests that hit the database, and scrape — the wait counters will be
non-zero.

No query text, DSN, or connection detail is exposed; this reports pool shape
only.

### Redis

Source: `internal/metrics/redis.go`, a `redis.Hook` on the shared client plus
a pull collector for pool statistics.

| Metric | Type | Labels | Meaning |
| --- | --- | --- | --- |
| `nester_redis_commands_total` | Counter | `command` | Commands issued. |
| `nester_redis_command_duration_seconds` | Histogram | `command` | Command latency. |
| `nester_redis_errors_total` | Counter | `command` | Commands that returned an error. |
| `nester_redis_pool_hits_total` | Counter | none | Free connection found in pool. |
| `nester_redis_pool_misses_total` | Counter | none | No free connection in pool. |
| `nester_redis_pool_timeouts_total` | Counter | none | Waits for a connection that timed out. |
| `nester_redis_pool_total_connections` | Gauge | none | Connections owned by the pool. |
| `nester_redis_pool_idle_connections` | Gauge | none | Idle connections. |
| `nester_redis_pool_stale_connections_total` | Counter | none | Connections removed as stale. |

**Cardinality:** `command` is an allowlist of ~50 command names plus `other`.
Keys and command arguments are never touched.

`redis.Nil` (key not found) is **not** counted as an error. It is ordinary
control flow for a cache lookup; counting it would make the error rate track
cache misses and render any alert on it meaningless.

A pipeline is recorded as one observation under `command="pipeline"` — it is
one round trip, and fanning out per queued command would inflate the count
and attribute the whole round trip's latency to each member.

### Outbound HTTP

Source: `internal/metrics/outbound.go`, an `http.RoundTripper` wrapping each
client's existing transport.

| Metric | Type | Labels | Meaning |
| --- | --- | --- | --- |
| `nester_outbound_requests_total` | Counter | `upstream`, `method`, `status_class` | Outbound requests that got a response. |
| `nester_outbound_request_duration_seconds` | Histogram | `upstream`, `method` | Outbound latency, including failed attempts. |
| `nester_outbound_errors_total` | Counter | `upstream`, `kind` | Failures before any response. |

`upstream` values: `soroban_rpc`, `horizon`, `coingecko`, `defillama`,
`anthropic_relay`, `intelligence`, `other`.

`kind` values: `timeout`, `canceled`, `dns`, `connect`, `other`.

**Cardinality:** bounded by the constants — 7 upstreams × 10 methods × 6
classes worst case, far less in practice.

Transport failures are counted separately from status codes because they
never produce one; folding them into `requests_total` under a fake class
would hide them. Duration is still observed for a failed call — it consumed
real time, and hiding it would understate the latency the caller experienced.

Only the request method and response status are read. Headers are never
inspected, so `Authorization`, API keys, and bearer tokens cannot reach a
metric. Bodies are never read.

```promql
# Error rate per upstream, transport failures and 5xx together.
sum by (upstream) (rate(nester_outbound_errors_total[5m]))
  + sum by (upstream) (rate(nester_outbound_requests_total{status_class="5xx"}[5m]))

# p99 latency per upstream.
histogram_quantile(0.99,
  sum by (le, upstream) (rate(nester_outbound_request_duration_seconds_bucket[5m])))
```

### Soroban RPC retries

Source: `internal/metrics/rpc.go`, recorded by the shared retry helper in
`internal/retry` via `internal/stellar`'s RPC client.

| Metric | Type | Labels | Meaning |
| --- | --- | --- | --- |
| `nester_rpc_attempts_total` | Counter | `upstream` | Individual attempts, retries included. |
| `nester_rpc_exhaustions_total` | Counter | `upstream` | Logical calls that used up their attempts or budget. |
| `nester_rpc_call_duration_seconds` | Histogram | `upstream` | End-to-end latency of a logical call, retries and backoff included. |

These describe **logical calls**; the outbound metrics above describe **HTTP
attempts**. The pair is the useful signal:

```promql
# Average attempts per call. Moves long before anything fails outright, so
# it is the earliest warning an upstream is degrading.
rate(nester_rpc_attempts_total[5m])
  / rate(nester_rpc_call_duration_seconds_count[5m])

# Retries have stopped being enough — these reach the user as a 503.
rate(nester_rpc_exhaustions_total[5m])

# What a user actually waited, retries included.
histogram_quantile(0.95,
  sum by (le, upstream) (rate(nester_rpc_call_duration_seconds_bucket[5m])))
```

**Cardinality:** three metrics × the upstreams actually called. No method
label — per-method detail comes from the `soroban.rpc/<method>` spans, which
already carry it without minting series.

**Only idempotent reads are retried.** `sendTransaction` is never repeated
automatically; the write path's durability comes from the submission record.
A `sendTransaction` still appears here with `attempts=1`, so the metrics cover
every call rather than only the retryable ones.

### Circuit breakers

Source: `internal/metrics/breaker.go`, a pull collector over
`internal/breaker`. See [circuit-breakers.md](circuit-breakers.md) for the
state machine, the failure classification, and the operator guide.

| Metric | Type | Labels | Meaning |
| --- | --- | --- | --- |
| `nester_circuit_breaker_state` | Gauge | `upstream` | 0 closed, 1 half-open, 2 open. |
| `nester_circuit_breaker_failure_ratio` | Gauge | `upstream` | Failure ratio within the rolling window. |
| `nester_circuit_breaker_rejected_total` | Counter | `upstream` | Requests rejected without contacting the upstream. |

`upstream` is `soroban_rpc` or `horizon` — the same bounded constant set the
outbound metrics use, never a URL or a host.

**Cardinality:** three metrics × two upstreams = six series, fixed at startup.
The breakers come from configuration, so nothing about a request can move the
count. A test asserts both the series count and that every label value is one
of the constants.

State values ascend with severity, so `> 0` reads as "not fully healthy" and
`max()` across replicas is the worst state rather than an arbitrary one.

```promql
# Any chain upstream being shed right now.
max by (upstream) (nester_circuit_breaker_state) > 0

# Load actually shed.
sum by (upstream) (rate(nester_circuit_breaker_rejected_total[5m]))

# Flapping: repeated open/half-open cycling means a marginal upstream.
changes(nester_circuit_breaker_state[15m])
```

### Indexer freshness

Source: `internal/metrics/freshness.go`, a pull collector over
`internal/freshness`. Nothing here is pushed.

| Metric | Type | Labels | Meaning |
| --- | --- | --- | --- |
| `nester_indexer_lag_seconds` | Gauge | none | Age of the indexed view of the chain. |
| `nester_indexer_lag_ledgers` | Gauge | none | Network tip minus last indexed ledger. **Absent until the indexer reports a position.** |
| `nester_indexer_staleness_budget_seconds` | Gauge | none | The budget the API is enforcing (`INDEXER_STALENESS_BUDGET`). |
| `nester_indexer_lag_last_sample_age_seconds` | Gauge | none | Seconds since the indexer last reported a position. |
| `nester_indexer_lag_sample_errors_total` | Counter | none | Failed sampling attempts. |

**Cardinality:** five series, fixed. The freshness of the indexed view is a
single process-wide fact, so there is nothing to break it down by and no way
for traffic to move the count. A test asserts none of these carry a label.

**Why a pull collector.** These were gauges the indexer pushed on each
successful poll, which meant a dead indexer left every one of them frozen at
its last healthy value — lag 0, sample age 0, alerts silent. Deriving them at
scrape time means `lag_seconds` and `lag_last_sample_age_seconds` climb on the
clock, so an indexer that has stopped is visible without anything of ours still
having to run. This is the failure mode the balance-freshness SLI exists to
catch, so it is worth the collector.

`lag_seconds` is `(now − last sample) + ledger lag × 5s`; see
[the SLO document](slo.md#5-balance-freshness) for why both terms are required.

#### API response headers

The same freshness model annotates every `/api/` response
(`internal/middleware/freshness.go`), so a client can degrade honestly instead
of presenting a stale balance as live. Stale data is still served with a 2xx —
failing the request would tell the user nothing useful about their money.

| Header | Meaning |
| --- | --- |
| `X-Indexer-Stale` | `true` / `false`. The authoritative answer; decided by the same budget the alert uses. |
| `X-Indexer-Lag-Seconds` | Staleness in whole seconds, rounded **up** so it never understates. |
| `X-Indexer-Lag-Ledgers` | Lag in ledgers. **Omitted** when the indexer has not reported a position — absent means "unknown", which a `0` would misreport as "exactly at the tip". |
| `X-Indexer-Staleness-Budget-Seconds` | The budget the flag was decided against. |

All four are listed in `Access-Control-Expose-Headers`, so a browser client on
another origin can read them.

```promql
# Is the served data inside its budget?
nester_indexer_lag_seconds <= nester_indexer_staleness_budget_seconds

# Running but behind, or stopped? Near zero is behind; climbing is stopped.
nester_indexer_lag_last_sample_age_seconds
```

### Reconciliation

Emitted by the two loops that compare our record of the money path against
the chain: the transaction poller (#1108, `internal/service/transaction_poller.go`)
and the vault-balance reconciler (#1082, `internal/reconciliation/runner.go`).
Alerted on by the money-path integrity group
(`docs/observability/runbooks/money-path-integrity.md`).

| Metric | Type | Labels | Meaning |
| --- | --- | --- | --- |
| `nester_reconcile_runs_total` | Counter | `outcome` (`completed`/`failed`) | Reconciliation passes, both loops. `failed` means a pass aborted before completing — a false-clean divergence count. |
| `nester_reconcile_divergences_total` | Counter | `kind` (`missing`/`extra`/`mismatch`/`stuck`) | Findings where our record and the chain disagree. One increment per finding. The kind vocabulary mirrors `reconciliation.DiscrepancyType`. |
| `nester_reconcile_last_run_age_seconds` | Gauge | none | Seconds since the transaction poller completed a pass. Aged by a ticker in `main.go`; reset on each pass. |
| `nester_reconcile_balance_last_run_age_seconds` | Gauge | none | Seconds since the vault-balance reconciler finished a sweep, derived at scrape time (pull collector, same reasoning as indexer freshness). **Absent** on non-leader replicas and when the reconciler is not running — absence pages via `BalanceReconciliationMetricsAbsent`. |

**Cardinality:** every label is a closed compile-time set. Divergences are
deliberately unlabelled by vault, user, or hash — the structured logs and the
`reconciliation_findings` table are the join key from a firing alert to the
affected records.

**Why two age series.** `RecordReconcileRun` resets the poller's gauge every
15 seconds, so a vault-balance reconciler sharing it could die invisibly
behind a healthy poller (and vice versa). Each loop's liveness stands alone.

### Runtime

The standard Go and process collectors are registered: `go_goroutines`,
`go_memstats_*`, `process_resident_memory_bytes`, `process_cpu_seconds_total`,
and the rest. Free to collect, and the first thing to check when the API
misbehaves for reasons unrelated to any route.

## Local observability stack

```bash
docker compose --profile observability up
```

Brings up the API, Prometheus, and Grafana. Services without a profile start
as before, so the default developer workflow is unchanged.

| Service | Host URL | Notes |
| --- | --- | --- |
| API | http://localhost:8080 | Public API. `/metrics` is **not** here. |
| Prometheus | http://localhost:9091 | Scrapes `api:9090` every 15s. |
| Grafana | http://localhost:3002 | Prometheus pre-provisioned as the default data source. |

The API's metrics port is bound to `0.0.0.0:9090` **inside the container** and
is not published to the host, so Prometheus reaches it over the compose
network while it stays off the host's interface.

### Verifying the scrape

1. Target is up — open http://localhost:9091/targets. The `nester-api` job
   should show `State: UP` with a recent "Last Scrape".

2. Query returns data — at http://localhost:9091/graph, run:

   ```promql
   nester_http_requests_total
   ```

   Send a request to the API first (`curl http://localhost:8080/health`) so
   there is something to count.

3. From the command line:

   ```bash
   # Is the target healthy?
   curl -s http://localhost:9091/api/v1/targets \
     | grep -o '"health":"[^"]*"'

   # Has the series arrived?
   curl -s 'http://localhost:9091/api/v1/query?query=nester_http_requests_in_flight'
   ```

4. Confirm the endpoint is *not* published to the host — this must fail:

   ```bash
   curl -s --max-time 3 http://localhost:9090/metrics   # connection refused
   ```

   To read the exposition directly during development, go through the
   container:

   ```bash
   docker compose exec api wget -qO- http://localhost:9090/metrics | head -40
   ```

## Adding a metric

1. Define the collector in `internal/metrics/metrics.go` and register it in
   `New()`. Never register per request.
2. Justify every label: write down its complete value set and why an operator
   needs that breakdown. If the set is not enumerable in advance, it is not a
   label — put it in a log or a trace.
3. Follow naming conventions — `_total` for counters, `_seconds` for
   durations, base units, `nester_` prefix via `Namespace`.
4. Add a test asserting the label values are bounded, in the style of the
   existing cardinality tests.
5. Document it in the reference table above, including expected cardinality.
