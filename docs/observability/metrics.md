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
