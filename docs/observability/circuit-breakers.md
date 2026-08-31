# Chain resilience — retries and circuit breakers

**Packages:** `apps/api/internal/retry`, `apps/api/internal/breaker`
**Metrics:** `nester_circuit_breaker_state`, `_failure_ratio`, `_rejected_total`, `nester_rpc_attempts_total`, `_exhaustions_total`, `_call_duration_seconds`
**Health:** `GET /health/detailed` → `soroban_rpc.circuit_breaker`, `horizon.circuit_breaker`

Two mechanisms, layered, doing different jobs:

| | Absorbs | Bounded by |
|---|---|---|
| **Retry** (nester#1086) | A *single* transient failure, so it never reaches the user | Attempts, backoff, and a total budget |
| **Breaker** (nester#1087) | A *sustained* outage, by shedding load entirely | A failure ratio over a rolling window |

Retry handles the blip; the breaker handles the outage. See
[Where the breaker sits](#where-the-breaker-sits) for why retry wraps the
breaker and not the other way round.

## Retry policy

Exponential backoff with **full jitter** — the delay is drawn uniformly from
`[0, cap)` rather than being the cap exactly, so a wave of clients that failed
at the same moment does not retry in lockstep and knock the recovering
endpoint back over.

| Variable | Default | Meaning |
|---|---|---|
| `RPC_RETRY_MAX_ATTEMPTS` | `3` | Total calls including the first. `1` disables retrying without disabling the helper. |
| `RPC_RETRY_BASE_DELAY` | `100ms` | Backoff cap for the first retry; doubles per attempt. |
| `RPC_RETRY_MAX_DELAY` | `2s` | Ceiling on the exponential growth. |
| `RPC_RETRY_BUDGET` | `12s` | Total wall time for one logical call. Applied as a context deadline, so one hung attempt cannot outlive it. Must stay below `SERVER_WRITE_TIMEOUT`. |

**Only idempotent reads are retried.** The classification is a closed map in
`internal/stellar` keyed by JSON-RPC method — `getEvents`, `getLatestLedger`,
`simulateTransaction`, `getTransaction` and friends are retried;
`sendTransaction` is not, and an unrecognised method fails closed so a Soroban
method added later is safe by default. A resubmitted envelope is a second
attempt to move real money, and the write path's durability comes from the
submission record, which owns sequence allocation and double-submit prevention
because that cannot live in a stateless retry loop.

Retried: transport failures, read timeouts, 5xx, and 429. Not retried: any
other 4xx (the upstream is healthy and rejecting us), JSON-RPC application
errors inside a 200 (deterministic), caller cancellation, and `ErrOpen`.

Exhaustion produces a typed `*retry.Error` matching `retry.ErrExhausted`,
which the API maps to **503 `UPSTREAM_UNAVAILABLE`** with a `Retry-After` —
never a generic 500.

---

## What this protects against

When a chain endpoint degrades, every in-flight request sits on it until its
own timeout, holding a connection the whole time. Requests arrive faster than
they drain, the connection pool saturates, and a partial upstream outage
becomes a **total** application outage — including for the endpoints that never
needed the chain at all.

The breaker cuts that feedback loop. Once an upstream is demonstrably
unhealthy, calls to it fail immediately and locally: no connection is opened,
no name is resolved, no timeout is waited on.

There are **two independent breakers**, one per upstream:

| Upstream | Guards |
|---|---|
| `soroban_rpc` | Contract simulation and reads, transaction submission, the event indexer's polling |
| `horizon` | Operator account lookups, transaction confirmation polling, the XLM/USD rate provider |

They share a *policy* but never *state*. A Horizon outage must not shed Soroban
traffic — that would take deposits offline for a dependency they do not need,
which is the failure this feature exists to prevent.

---

## States

```
                    failure ratio exceeded
     ┌──────────────────────────────────────────┐
     │                                          ▼
  CLOSED                                      OPEN
     ▲                                          │
     │ probe succeeds                           │ open duration elapses
     │                                          ▼
     └────────────────────────────────────  HALF-OPEN
                                                │
                                                │ probe fails
                                                ▼
                                              OPEN
```

**CLOSED** — calls pass through. Outcomes are counted in a rolling window.

**OPEN** — calls are rejected immediately with a typed error. The caller sees
`errors.Is(err, breaker.ErrOpen)`, and the error carries how long until the
next probe.

**HALF-OPEN** — entered once the open duration elapses. **Exactly one** probe
call is admitted; every other caller keeps failing fast. This is deliberate: a
recovering upstream must not be hit by the entire accumulated backlog the
instant the open period ends. The probe's outcome decides — success closes the
breaker, failure re-opens it for another full period.

The `OPEN → HALF-OPEN` transition is *measured, not timed*. There is no
goroutine or timer waiting to move it, so a breaker that is never called again
costs nothing and leaks nothing.

---

## What counts as a failure

The rule is "does this suggest the upstream cannot serve us", not "did the
caller get what they wanted".

| Outcome | Counted as | Why |
|---|---|---|
| Connection refused, DNS failure, TLS error, read timeout | **failure** | Exactly the errors that hold a connection until it times out |
| `context.DeadlineExceeded` | **failure** | Waiting past the deadline is the symptom of a degrading upstream |
| HTTP 5xx | **failure** | The upstream is telling us it is unwell |
| HTTP 429 | **failure** | The upstream is asking for less load; pushing harder is the harm |
| HTTP 4xx (other) | success | The upstream is healthy and answering correctly |
| HTTP 2xx / 3xx | success | — |
| Caller cancellation (`context.Canceled`) | **ignored** | The client disconnected; the upstream never got its chance |

**Why other 4xx are successes.** A 404 for an unfunded account or a 400 for a
malformed contract call means the upstream is working. Counting them would let
ordinary user input open the breaker and take the chain offline for everyone —
a denial of service any client could trigger by looping an invalid request.

**Why cancellation is ignored, not counted.** A burst of abandoned requests
would otherwise open a breaker over a perfectly healthy endpoint. Ignored
outcomes still release a half-open probe slot; without that, a cancelled probe
would strand the breaker in half-open with no probe ever admissible again.

**Not inspected:** JSON-RPC errors returned inside a 200. Doing so would mean
reading and replacing the response body inside a transport, and Soroban's
application errors (`startLedger is before the oldest ledger`) are mostly
client mistakes rather than node ill-health. The failure mode this issue
addresses — an endpoint degrading under load — surfaces as transport errors and
5xx, which are covered.

---

## The failure window

The ratio is computed over a **rolling window**, subdivided into ten buckets
that expire as it advances. Failures from before the window do not contribute:
an incident half an hour ago cannot hold the breaker near its threshold now.

Closing the breaker **resets the window**. Carrying the failures that opened it
into the recovered state would re-trip it on the very first new failure, making
recovery impossible while any traffic still failed occasionally.

### Why there is a minimum request count

A ratio alone is unusable at small samples:

```
1 request, 1 failure → 100% failure ratio → breaker opens
```

`CIRCUIT_BREAKER_MIN_REQUESTS` is the floor that prevents a single timeout on
an idle upstream from shedding all its traffic. The cost is that a very quiet
upstream may never accumulate enough calls to trip — which is acceptable,
because with no traffic there is no pile-on to prevent.

---

## Configuration

| Variable | Default | Meaning |
|---|---|---|
| `CIRCUIT_BREAKER_ENABLED` | `true` | Kill switch. When false, no breaker is installed and every call goes straight to the upstream. |
| `CIRCUIT_BREAKER_FAILURE_RATIO` | `0.5` | Share of failed calls, within the window, at or above which the breaker opens. |
| `CIRCUIT_BREAKER_MIN_REQUESTS` | `10` | Calls that must be observed within the window before the ratio may open it. |
| `CIRCUIT_BREAKER_WINDOW` | `60s` | How far back outcomes are counted. |
| `CIRCUIT_BREAKER_OPEN_DURATION` | `15s` | How long the breaker sheds before admitting a probe. |

One policy governs both upstreams. Separate per-upstream thresholds would
double the configuration surface with no evidence that Soroban and Horizon need
different numbers; the failure *state* is what must stay separate, and it does.

**Why these defaults.** 0.5 is unambiguous degradation rather than noise. 10
calls in 60s is low enough that a real outage under normal traffic trips within
a window, high enough that one or two stray failures cannot. 15s is roughly
three ledger closes — long enough to actually shed load, short enough that a
transient blip costs one short outage rather than a long one.

An invalid policy is **refused at startup**. A disabled breaker's thresholds
are not validated, so the kill switch can always be used to recover from a bad
configuration.

---

## Where the breaker sits

```
caller
  ↓
retry loop                     ← internal/retry, Soroban reads only
  ↓
circuit breaker transport      ← rejects here; nothing below runs
  ↓
metrics transport              ← nester_outbound_* recorded here
  ↓
net/http transport
  ↓
upstream
```

**Breaker above metrics.** A rejected call never reaches the metrics
transport, so it is not counted as an outbound request and does not plant a
near-zero sample in the latency histogram or a transport error under a
made-up kind. `nester_outbound_*` keeps meaning "calls we actually made"; the
shed load is reported by `nester_circuit_breaker_rejected_total` instead.

**Retry above the breaker** (nester#1086), and this ordering is the one that
matters:

- Each retry attempt is a **real connection** against the upstream, so each is
  evidence the breaker should weigh. Putting retry underneath would hide N
  attempts behind one reported outcome.
- Once the breaker opens, the loop's next attempt fails fast **locally**. The
  retry stops immediately instead of burning its remaining backoff against an
  endpoint already known to be down. `breaker.ErrOpen` is classified
  non-retryable precisely so this happens.
- The failure *ratio* is unaffected: three failed attempts count three
  failures out of three observations, the same 100% as one out of one. Only
  the sample count grows, which reaches `MIN_REQUESTS` sooner — a sick
  upstream is detected faster, which is the desired direction.

The reverse ordering — retry inside the breaker — would let a retry storm run
to completion against a dead endpoint, holding a connection per attempt, which
is exactly the pile-on the breaker exists to stop.

Only idempotent reads are retried. `sendTransaction` is never repeated
automatically; the write path's durability comes from the submission record.

**Routing is per request URL, not per client.** `ContractInvoker` talks to
Soroban RPC and Horizon through a single `*http.Client` — it simulates against
the RPC, then reads the operator account from Horizon. Keying the breaker to
the client would force those upstreams to share state. Routing per request
keeps them independent without splitting any client.

---

## Observability

### Metrics

```promql
# 0 = closed, 1 = half-open, 2 = open.
nester_circuit_breaker_state{upstream="soroban_rpc"}
nester_circuit_breaker_state{upstream="horizon"}

# How close to tripping, or how fast the window is draining after recovery.
nester_circuit_breaker_failure_ratio

# Calls shed without touching the network.
rate(nester_circuit_breaker_rejected_total[5m])
```

The state values ascend with severity, so `> 0` reads as "not fully healthy"
and `max()` across replicas is the worst state rather than an arbitrary one.

These are read at scrape time rather than pushed, because an open breaker
becomes half-open when its open period elapses — a function of the clock, not
of anything happening. A pushed gauge would keep reporting "open" until the
next call arrived to move it.

There is deliberately **no alert rule** in this change; the natural one is
`nester_circuit_breaker_state > 0` sustained for a few minutes, alongside the
existing balance-freshness alerts.

### Health response

`GET /health/detailed`:

```json
{
  "status": "degraded",
  "soroban_rpc": {
    "ok": true,
    "latest_ledger": 493812,
    "circuit_breaker": {
      "state": "open",
      "failure_ratio": 0.83,
      "observed_requests": 24,
      "rejected_total": 1841,
      "retry_in_seconds": 9
    }
  },
  "horizon": {
    "ok": true,
    "circuit_breaker": { "state": "closed", "failure_ratio": 0, "observed_requests": 12, "rejected_total": 0 }
  }
}
```

An absent `circuit_breaker` field means that dependency is **not guarded**
(the breakers are disabled), not that it is healthy.

The probes (`ok`, `latest_ledger`) deliberately do **not** go through the
breaker. A health check is a diagnostic, and an open breaker must not report an
upstream as unreachable when it has in fact recovered. Seeing `"ok": true`
alongside `"state": "open"` is exactly what tells an operator recovery is one
probe away.

### Readiness

**An open breaker does not make the service unready.** `/readyz` continues to
depend only on the database, as it always has.

This is deliberate. Chain dependencies have never gated readiness here, and
making an open breaker return 503 would evict the pod from its load balancer
over an upstream outage — turning the partial failure into the total one this
feature exists to prevent. An open breaker sets `status: "degraded"` in
`/health/detailed`, which is the signal for humans and dashboards, not for the
orchestrator.

### API behaviour while open

Vault endpoints that need the chain return **503 Service Unavailable** with
code `UPSTREAM_UNAVAILABLE` and a `Retry-After` header set to the breaker's
remaining open period. This is a known, temporary upstream condition with a
known retry time, not a fault in this service, and 500 would tell a client to
treat it as a bug rather than to back off.

Rejections are **not logged individually**. An open breaker can reject
thousands of calls a second; a log line each would turn an upstream outage into
a logging outage. Only state transitions are logged — opening at `WARN`,
everything else at `INFO`.

---

## When a breaker is open

**1. Is the upstream actually down, or did we trip on something else?**

```bash
curl -s https://<api-host>/health/detailed | jq '.soroban_rpc, .horizon'
```

`"ok": true` with `"state": "open"` means the upstream has recovered and the
breaker will close on its next probe. Wait one open duration.

**1a. Were the retries already struggling before it tripped?**

```promql
rate(nester_rpc_attempts_total[5m]) / rate(nester_rpc_call_duration_seconds_count[5m])
rate(nester_rpc_exhaustions_total[5m])
```

Average attempts per call climbs before the breaker opens, so this usually
shows when the degradation actually started rather than when it became bad
enough to shed.

**2. What is the upstream doing?**

```promql
sum by (kind) (rate(nester_outbound_errors_total{upstream="soroban_rpc"}[5m]))
sum by (status_class) (rate(nester_outbound_requests_total{upstream="soroban_rpc"}[5m]))
histogram_quantile(0.95, sum by (le) (rate(nester_outbound_request_duration_seconds_bucket{upstream="soroban_rpc"}[5m])))
```

`kind="timeout"` climbing is the classic degradation. A 5xx spike is the
upstream failing outright. A 429 spike means we are being rate limited — check
whether a backfill or a misconfigured poll interval is the cause before blaming
the provider.

**3. Did we trip on our own traffic?**

A `429` spike alongside an operator-triggered backfill is self-inflicted.
Backfill and the admin one-shot sync are **not** routed through the breaker
(see below), so they can still saturate an upstream that live traffic is being
shed from.

**4. Is it flapping?**

```promql
changes(nester_circuit_breaker_state{upstream="soroban_rpc"}[15m])
```

Repeated open→half-open→open means the upstream is marginal rather than down.
Consider raising `CIRCUIT_BREAKER_OPEN_DURATION` so each probe cycle costs
less, or failing over to a secondary endpoint.

**5. Escalate or override.**

If the thresholds are wrong for the environment — the breaker is shedding
traffic an upstream could actually serve — set `CIRCUIT_BREAKER_ENABLED=false`
and restart. That is what the kill switch is for. Fix the thresholds before
re-enabling.

### Knock-on effect: indexer staleness

An open Soroban breaker stops the event indexer making progress, which is
**intended** — it is the traffic most worth shedding. The consequence is that
indexed balances go stale, and that is detected separately by the
balance-freshness SLI:

```
Soroban degradation → breaker opens → indexer stops progressing
                                    → nester_indexer_lag_seconds climbs
                                    → IndexerStalenessBudgetExceeded pages
```

See [the balance-freshness runbook](runbooks/balance-freshness.md). The two
mechanisms are deliberately independent: the breaker knows nothing about the
indexer, and the freshness signal knows nothing about why the indexer stopped.

---

## What is not guarded

| Caller | Guarded | Why |
|---|---|---|
| Contract reads / simulation | yes | User-facing read path |
| Transaction submission | yes | The money path |
| Event indexer polling | yes | Highest-volume Soroban caller |
| Transaction confirmation polling | yes | Steadiest Horizon caller |
| XLM/USD rate provider | yes | Horizon order book |
| `POST /api/v1/admin/sync-events` | **no** | Operator recovery action; blocking it during an incident would remove a tool the runbook depends on |
| `POST /api/v1/admin/backfill` | **no** | Same, and it is already bounded by its own sequential loop |
| `/health/detailed` probes | **no** | Diagnostic; must report the upstream's real state |
| CoinGecko, DeFiLlama, Anthropic relay, intelligence | **no** | Out of scope for this issue |

The admin exclusions are a deliberate trade: an operator running a recovery
backfill can still saturate an upstream that live traffic is being shed from.
That is visible as a 429 or 5xx spike with the breaker already open, and step 3
above calls it out.
