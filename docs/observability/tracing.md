# Distributed tracing

Nester emits OpenTelemetry traces across the Go API and the Python
intelligence service, so a single request can be followed through HTTP,
PostgreSQL, Redis, Soroban RPC, and Anthropic.

Tracing answers *where did the time go for this request*. It does not replace
`X-Request-ID`, which remains the correlation identifier support asks users
for. The two coexist and serve different purposes.

---

## Contents

- [Quick start](#quick-start)
- [Architecture](#architecture)
- [Configuration](#configuration)
- [Span naming and attributes](#span-naming-and-attributes)
- [Prohibited attributes](#prohibited-attributes)
- [Sampling](#sampling)
- [Finding a trace from an X-Request-ID](#finding-a-trace-from-an-x-request-id)
- [Metrics exemplars](#metrics-exemplars)
- [Troubleshooting](#troubleshooting)
- [Runbook: an annotated deposit trace](#runbook-an-annotated-deposit-trace)

---

## Quick start

Bring the collector and Jaeger up detached, so the shell stays free:

```bash
docker compose --profile observability up -d otel-collector jaeger
```

Then run the application services with tracing on:

```bash
TRACING_ENABLED=true \
INTELLIGENCE_TRACING_ENABLED=true \
docker compose up
```

Open the Jaeger UI at <http://localhost:16686> and choose the `nester-api` or
`nester-intelligence` service.

Tracing is **off by default**. With `TRACING_ENABLED` unset the API installs a
no-op tracer provider, dials no collector, and every instrumentation call site
becomes a cheap no-op. A missing collector is never a startup dependency and
never fails a request.

---

## Architecture

```
Browser / client
      │  X-Request-ID (optional)
      ▼
┌─────────────────────────────────────────────┐
│ Go API                    service: nester-api│
│                                              │
│  middleware.Logging      mints X-Request-ID  │
│  middleware.Tracing      SERVER span         │
│    ├── otelpgx           PostgreSQL spans    │
│    ├── redisotel         Redis spans         │
│    ├── stellar invoker   Soroban spans       │
│    └── intelligence relay CLIENT span        │
└───────────────────┬──────────────────────────┘
                    │ traceparent + X-Request-ID
                    ▼
┌─────────────────────────────────────────────┐
│ Intelligence      service: nester-intelligence│
│                                              │
│  FastAPIInstrumentor     SERVER span         │
│    └── anthropic.chat    model call + TTFT   │
│          └── tool.<name> one per tool round  │
└───────────────────┬──────────────────────────┘
                    │ OTLP gRPC
                    ▼
        OpenTelemetry Collector  (tail sampling)
                    │
                    ▼
                 Jaeger
```

Trace context crosses the process boundary as the W3C `traceparent` and
`tracestate` headers. The Go relay injects them; FastAPI instrumentation
extracts them and continues the same trace rather than rooting a new one.

### Service names

| Service | `service.name` |
| --- | --- |
| Go API | `nester-api` |
| Python intelligence service | `nester-intelligence` |

Both are configurable (`OTEL_SERVICE_NAME`, `INTELLIGENCE_OTEL_SERVICE_NAME`).

---

## Configuration

Both services are configured entirely from the environment and follow their
existing configuration conventions — the Go API through `internal/config`,
the intelligence service through pydantic settings with the `INTELLIGENCE_`
prefix. No business-logic code reads a tracing environment variable directly.

### Go API

| Variable | Default | Meaning |
| --- | --- | --- |
| `TRACING_ENABLED` | `false` | Master switch. Off installs a no-op provider. |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `localhost:4317` | Collector address (OTLP/gRPC). |
| `OTEL_EXPORTER_OTLP_INSECURE` | `true` | Skip TLS to the collector. Set `false` off-host. |
| `OTEL_SERVICE_NAME` | `nester-api` | `service.name` on every span. |
| `OTEL_EXPORTER_TIMEOUT` | `10s` | Bounds one export round trip. |
| `TRACING_SAMPLE_RATIO` | `0.05` | Head sampling probability. See [Sampling](#sampling). |
| `TRACING_LATENCY_THRESHOLD` | `1s` | Requests at or above this are marked for retention. |

### Intelligence service

| Variable | Default | Meaning |
| --- | --- | --- |
| `INTELLIGENCE_TRACING_ENABLED` | `false` | Master switch. |
| `INTELLIGENCE_OTEL_EXPORTER_OTLP_ENDPOINT` | `http://localhost:4317` | Collector address. |
| `INTELLIGENCE_OTEL_EXPORTER_OTLP_INSECURE` | `true` | Skip TLS to the collector. |
| `INTELLIGENCE_OTEL_SERVICE_NAME` | `nester-intelligence` | `service.name`. |
| `INTELLIGENCE_OTEL_EXPORTER_TIMEOUT` | `10` | Export timeout, seconds. |
| `INTELLIGENCE_TRACING_SAMPLE_RATIO` | `0.05` | Head sampling probability. |

Invalid values fail fast at startup: a sample ratio outside `[0, 1]`, a
non-numeric ratio, or a negative latency threshold are configuration errors,
not silently clamped.

---

## Span naming and attributes

Span names must stay low-cardinality. A name containing an ID produces
effectively unbounded distinct names and degrades any trace backend.

| Layer | Span name | Example |
| --- | --- | --- |
| Go HTTP server | Route pattern | `GET /api/v1/users/{id}/savings-goals` |
| PostgreSQL | Trimmed SQL operation | `SELECT accounts` |
| Redis | Command name | `redis.GET` |
| Soroban contract call | `soroban.<operation>` | `soroban.invoke` |
| Soroban RPC | `soroban.rpc/<method>` | `soroban.rpc/simulateTransaction` |
| Intelligence relay | Fixed | `intelligence.relay/chat` |
| FastAPI server | Route template | `POST /intelligence/chat` |
| Anthropic call | `anthropic.<operation>` | `anthropic.chat` |
| Tool round | `tool.<name>` | `tool.get_portfolio` |

An unmatched HTTP path keeps the bare method (`GET`) as its span name, so a
404 scan cannot inflate span-name cardinality.

OpenTelemetry semantic conventions are used where they exist
(`http.route`, `http.response.status_code`, `db.query.text`,
`gen_ai.request.model`, `gen_ai.usage.input_tokens`). Measurements the
conventions have no name for are namespaced under `nester.` so they are
obviously local.

### Nester-specific attributes

| Attribute | Where | Meaning |
| --- | --- | --- |
| `nester.request_id` | Server spans, both services | The `X-Request-ID` for this request. |
| `nester.force_keep` | Any span | Marks the trace for tail-sampling retention. |
| `nester.slow_request` | Go server spans | Request exceeded the latency threshold. |
| `soroban.contract_id` | Soroban spans | Public contract address. |
| `soroban.function` | Soroban spans | Contract function name. |
| `soroban.rpc_method` | Soroban RPC spans | JSON-RPC method. |
| `soroban.transaction_hash` | Soroban spans | Public transaction hash. |
| `soroban.transaction_status` | Soroban spans | `SUCCESS`, `FAILED`, `NOT_FOUND`. |
| `nester.llm.streaming` | Anthropic spans | Whether the call streamed. |
| `nester.llm.time_to_first_token_ms` | Anthropic spans | TTFT; `-1` if no token arrived. |
| `nester.llm.duration_ms` | Anthropic spans | Total model call duration. |
| `nester.llm.stream_completed` | Anthropic spans | False if the stream was cut off. |
| `nester.llm.round` | Anthropic spans | Tool-loop round index. |
| `nester.tool.name` | Tool spans | Registered tool name. |
| `nester.tool.round` | Tool spans | Round the tool ran in. |
| `nester.tool.status` | Tool spans | `executed`, `rejected`, `not_found`. |

---

## Prohibited attributes

Nester moves user money, so telemetry is treated as an untrusted egress
channel: spans leave the process, land in a trace backend, and are readable by
anyone with dashboard access.

**Never put any of these on a span, in any service:**

- Stellar secret seeds or any private key material
- Signed or unsigned transaction XDR, and `errorResultXdr`
- JWTs, session tokens, API keys, `Authorization` headers
- SQL parameter values (bound arguments)
- Raw prompts, system prompts, or model responses
- Tool arguments or tool results
- Account numbers, balances, or other user financial records
- Passwords or database DSNs

Three mechanisms enforce this, in order of importance:

1. **Allow-listing at the call site.** Instrumentation records an explicitly
   chosen set of scalars, never a whole structure. This is the primary
   control.
2. **`internal/telemetry/redact.go`** is a single choke point that rejects
   sensitive attribute keys and strips secret patterns — Stellar seeds, JWTs,
   bearer headers, provider API keys, and base64 XDR — from any free-form
   value, with all values length-bounded.
3. **Tests.** Every instrumented surface has a test asserting the forbidden
   values do not appear in exported spans, and the redactor's core invariants
   are fuzzed.

Two library defaults were explicitly disabled because they leak:

- `otelpgx`'s `IncludeQueryParameters` is **not** set, so bound arguments
  never reach a span.
- The Python SDK's `record_exception` and `set_status_on_exception` are
  **disabled** on model and tool spans. Both write the raw exception message
  into the span, and an Anthropic `401` interpolates the API key while a
  validation error interpolates the user's prompt. Only the exception *type*
  is recorded.

Redis is configured with `WithDBStatement(false)`: command names only, never
keys or values.

---

## Sampling

Sampling is deliberately split across two tiers, and it is worth being precise
about why rather than pretending a single knob does it.

A **head sampler** decides at span *start*. Whether a request errors, and how
long it takes, are only known at span *end*. A head sampler therefore
physically cannot condition on either. So:

- **Head (in each service).** A `ParentBased` `TraceIDRatioBased` sampler at
  `TRACING_SAMPLE_RATIO`. It bounds how much normal traffic is recorded.
  `ParentBased` is what keeps a distributed trace whole: once the Go API has
  decided to sample a trace, the intelligence service honours that decision
  instead of re-rolling and producing a half-recorded trace.

- **Tail (in the collector).** `deploy/observability/otel-collector.yaml`
  keeps every trace that is explicitly marked, every trace containing an
  `ERROR` span, and every trace over the latency threshold, plus a 5%
  probabilistic baseline.

> **Important:** because the head sampler runs first, a trace it drops never
> reaches the collector. **Wherever tail sampling is deployed, set
> `TRACING_SAMPLE_RATIO=1.0`** (and the intelligence equivalent), or the head
> will discard traces the tail would have kept. The defaults of `0.05` assume
> direct-to-backend export with no tail sampler.

### What is always retained

Application code marks `nester.force_keep` on:

- any span whose operation returned an error (`telemetry.RecordError`)
- HTTP responses of 5xx
- requests at or above `TRACING_LATENCY_THRESHOLD`
- Anthropic calls slower than 10s
- tool rounds that were rejected, errored, or not found

---

## Finding a trace from an X-Request-ID

Support tickets quote an `X-Request-ID`, not a trace ID. Both services record
it on their server span as `nester.request_id`.

In the Jaeger UI, search with the tag:

```
nester.request_id=<the id from the ticket>
```

Or via the API:

```bash
curl -s "http://localhost:16686/api/traces?service=nester-api&tags=%7B%22nester.request_id%22%3A%22req-abc-123%22%7D"
```

The reverse direction also works: every log line already carries
`request_id`, so a trace found in Jaeger gives an ID to grep the logs with.

---

## Metrics exemplars

**Not yet implemented — sequenced behind #1043 (PR #1065).**

Issue #1054 asks for latency histograms to expose trace exemplars so an
operator can go from a Prometheus spike to a representative trace.

The metrics layer that exemplars attach to is implemented in
[PR #1065](https://github.com/Suncrest-Labs/nester/pull/1065) for #1043, but
that PR has not merged yet, so `dev` currently has no `internal/metrics`
package and nothing to attach an exemplar to. This branch is built on `dev`
and deliberately does not depend on an unmerged branch, so that the two PRs
stay independently reviewable and can merge in either order.

### What to do once #1065 has merged

`client_golang` v1.24.1 (the version #1065 pins) supports exemplars natively.
Wiring is small and touches only the metrics package:

1. Change the histogram observation in `internal/metrics/http.go` from
   `Observe(d)` to the exemplar-aware form:

   ```go
   observer := m.requestDuration.WithLabelValues(route, method)
   if exemplarObserver, ok := observer.(prometheus.ExemplarObserver); ok {
       if sc := trace.SpanContextFromContext(r.Context()); sc.IsSampled() {
           exemplarObserver.ObserveWithExemplar(duration, prometheus.Labels{
               "trace_id": sc.TraceID().String(),
           })
           return
       }
   }
   observer.Observe(duration)
   ```

   The `IsSampled` guard matters: an exemplar pointing at a trace the sampler
   discarded is a dead link in Grafana.

2. Serve the exposition with `promhttp.HandlerOpts{EnableOpenMetrics: true}`.
   Exemplars are an OpenMetrics feature and are silently dropped from the
   classic Prometheus text format.

3. Start Prometheus with `--enable-feature=exemplar-storage`, otherwise it
   scrapes exemplars and discards them.

Only `trace_id` belongs in an exemplar label set. Exemplar labels are exempt
from normal cardinality limits, which makes them an easy place to leak a user
ID or wallet address by accident.

---

## Troubleshooting

**No traces appear at all.**
Check `TRACING_ENABLED=true` is actually set — it defaults to off. On startup
the API logs either `tracing enabled` with the endpoint, or
`tracing disabled; using no-op tracer provider`.

**The service starts but no spans reach Jaeger.**
The OTLP exporter connects lazily, so a wrong endpoint does not fail startup.
Confirm the collector is listening (`docker compose --profile observability
ps`) and check its logs for `Everything is ready`. Remember the collector's
`decision_wait` is 30s — a trace will not appear in Jaeger until that elapses.

**Traces appear but are split into two.**
Propagation is broken between services. Confirm the relay is forwarding
`traceparent`, and that the intelligence service has the propagator installed
(it is installed even when tracing is disabled, precisely to avoid this).

**Some traces are missing but errors are present.**
Expected: that is sampling working. If *errors* are missing, check the head
ratio — see the warning in [Sampling](#sampling).

**Health checks flood the traces.**
They should not: `/health`, `/healthz`, `/readyz`, and `/metrics` are excluded
in the intelligence service and rate-limit-exempt in the API.

**A span name contains an ID.**
A regression. Span names must come from route patterns; see
[Span naming](#span-naming-and-attributes).

---

## Runbook: an annotated deposit trace

The trace below is from a local run against the observability profile. It is
synthetic in the sense that no real user or production credential is involved
— all identifiers are development values — but the structure, parent-child
relationships, and attribute names are exactly what production emits.

```
Trace 38959b65ed46b6850bea4a8710648638                        total 13.6ms

nester-api        POST /api/v1/transactions                   13.658ms  ROOT
│                   http.request.method   = POST
│                   http.route            = /api/v1/transactions
│                   http.response.status_code = 201
│                   nester.request_id     = req-e2e-verify-001
│                   nester.force_keep     = true
│
├── nester-api    postgres.query                               5.469ms
│                   db.system.name        = postgresql
│                   db.query.text         = INSERT INTO transactions
│                                           (user_id, amount, ...)
│                                           VALUES ($1, $2, ...)
│                   ── note the $1/$2 placeholders: bound values are
│                      never recorded
│
└── nester-api    soroban.invoke                               8.189ms
                    soroban.contract_id   = CDLZFC3SYJYDZT7K67VZ75HP...
                    soroban.function      = deposit
                    soroban.transaction_hash = a1b2c3d4e5f60718...
                    soroban.transaction_status = SUCCESS
```

The full hierarchy for a deposit that also consults the intelligence service
extends as follows. Each level is a real span this implementation emits:

```
nester-api            POST /api/v1/transactions             ROOT
├── nester-api        postgres.query                        db write
├── nester-api        redis.GET                             cache lookup
├── nester-api        intelligence.relay/chat               CLIENT span
│   └── nester-intelligence  POST /intelligence/chat        SERVER span
│         │                    ── same trace ID; parent is the
│         │                       relay's CLIENT span
│         └── nester-intelligence  anthropic.chat           model call
│               │   gen_ai.request.model              = claude-sonnet-5
│               │   gen_ai.usage.input_tokens         = 1200
│               │   gen_ai.usage.output_tokens        = 340
│               │   gen_ai.response.finish_reasons    = [tool_use]
│               │   nester.llm.streaming              = true
│               │   nester.llm.time_to_first_token_ms = 412.7
│               │   nester.llm.round                  = 1
│               │
│               └── nester-intelligence  tool.get_portfolio
│                     nester.tool.name   = get_portfolio
│                     nester.tool.round  = 1
│                     nester.tool.status = executed
│                     ── arguments and results are NOT recorded
│
└── nester-api        soroban.invoke                        chain write
    ├── soroban.rpc/simulateTransaction
    ├── soroban.rpc/sendTransaction
    └── soroban.rpc/getTransaction                       (polled)
```

### Reading this trace during an incident

- **Total is high but every child is fast.** Time is being spent in the API's
  own code between spans, not in a dependency.
- **`anthropic.chat` dominates.** Expected for chat requests. Check
  `nester.llm.time_to_first_token_ms`: a high TTFT with a normal total means
  the model was slow to start, which is what users actually perceive as
  sluggishness.
- **Several `tool.*` spans in sequence.** The tool loop is iterating.
  `nester.llm.round` shows how far it got; `max_tool_rounds` bounds it.
- **`soroban.rpc/getTransaction` repeated many times.** The transaction is
  taking a long time to confirm; each repetition is one poll.
- **`nester.force_keep = true`.** This trace was retained deliberately —
  because it errored, was slow, or both. It is not a random sample.

### Reproducing this locally

```bash
docker compose --profile observability up -d jaeger otel-collector
TRACING_ENABLED=true TRACING_SAMPLE_RATIO=1.0 \
  OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317 \
  go run ./cmd/api
# exercise an endpoint, wait ~30s for the collector's decision_wait,
# then search http://localhost:16686 for the request id
```
