# Cost-weighted rate limit quotas

The limiters in `apps/api/internal/middleware/ratelimit.go` count requests. That
is the wrong unit for this API. A profile read and a multi-protocol APY
comparison that fans out to CoinGecko, DeFiLlama and Soroban RPC both count as
one, so a caller can sit comfortably inside 100 requests/minute while saturating
every expensive dependency we have — and the limiter will not notice, because it
is measuring the wrong thing.

`CostQuota` adds a second, orthogonal bound: how much downstream *work* one
caller may cause. Both apply. The request-rate limiters still bound request
volume per IP; the quota bounds cost per user.

## Route costs

Costs are declared in one place, `apps/api/internal/middleware/ratelimit_cost.go`.
Anything not listed costs `DefaultRouteCost` (1).

| Tier | Cost | What it covers |
|---|---:|---|
| `DefaultRouteCost` | 1 | Ordinary database read or write |
| `CostSimulation` | 3 | In-process projection or scenario run over fetched data |
| `CostChainRead` | 4 | Soroban simulation — preview deposit/withdraw, share price |
| `CostAggregation` | 6 | Multi-protocol APY comparison, TVL rollups |
| `CostChainWrite` | 10 | Soroban contract invocation — deposit, withdraw, harvest, rebalance |

The numbers are a fan-out ordering, not a latency budget. They only need to be
right relative to each other, and they are deliberately coarse so that re-tuning
a tier is one edit rather than an audit.

Patterns use the same wildcard syntax as the Go 1.22 `ServeMux` patterns the
routes are registered with, so an entry can be copied straight from a handler's
`Register` method. Where several patterns match, the one with more literal
segments wins — `/api/v1/vaults/tvl` is not shadowed by `/api/v1/vaults/{id}/tvl`.

**Adding an expensive endpoint means adding it to that table.** The failure mode
is silent: an unlisted route costs 1 and the quota stops describing reality.
`TestExpensiveRoutesCostMoreThanOne` guards the routes we know about, but it
cannot know about yours.

## Accounting

A **token bucket**, not a fixed window. A fixed window resets its counter
wholesale at the boundary, so a caller can spend the whole allowance in the last
instant of one window and the whole allowance again in the first instant of the
next — twice the configured limit, in a couple of milliseconds, at exactly the
moment the expensive dependencies are already loaded. A bucket refills
continuously at `LIMIT/WINDOW` and has no such boundary.

Keyed by authenticated user ID, falling back to client IP for anything still
anonymous when the middleware runs. The two namespaces are prefixed (`u:`, `ip:`)
so a user ID can never collide with an address.

State lives in Redis when `REDIS_ADDR` is configured, in a Lua script that does
the whole read-refill-charge-write cycle atomically, so two API instances
charging the same user concurrently cannot both see the pre-charge balance.
Without Redis it falls back to a process-local bucket, which bounds one instance
only — the same trade-off the existing limiters make, and still better than not
counting cost at all.

### Placement in the chain

`costQuota` sits after `authenticator` (so it can key by user) and after
`idempotencyMiddleware` (so a replayed idempotent write, which returns a stored
response and calls nothing downstream, is not charged as though it did).
Liveness, readiness and metrics endpoints are excluded outright.

## What clients see

On **every accounted response**, not only rejections:

```
RateLimit-Limit: 300
RateLimit-Remaining: 275
RateLimit-Reset: 5
```

`Reset` is whole seconds until the bucket is full again, rounded up. A client
that reads these can slow down before it is pushed.

On exhaustion, `429` with `Retry-After` and a body that says which quota ran out
and when it recovers:

```json
{
  "success": false,
  "error": {
    "code": 429,
    "message": "request cost quota exhausted",
    "reason": "QUOTA_EXHAUSTED",
    "quota": "request-cost",
    "cost": 25,
    "limit": 300,
    "remaining": 0,
    "retry_after_seconds": 5,
    "reset_seconds": 60
  }
}
```

`cost` is what this request would have consumed, which is how a client works out
that backing off from chat is worth more than backing off from profile reads.

## Redis outage

The limiter **fails open**: requests pass through uncounted and the failure is
logged. A limiter outage must not become a service outage — the worst case for
passing traffic through is a dependency bill, and the worst case for rejecting it
is a dead API.

Rate limit headers are **omitted** while degraded. Any number reported then would
be invented, and a client that trusted it would be self-throttling against
fiction.

## Configuration

Per environment, so staging can run tighter limits than production:

| Variable | Default | Meaning |
|---|---|---|
| `RATELIMIT_QUOTA_ENABLED` | `true` | Master switch |
| `RATELIMIT_QUOTA_LIMIT` | `300` | Bucket capacity in cost units |
| `RATELIMIT_QUOTA_WINDOW` | `1m` | Time to refill from empty |
| `RATELIMIT_QUOTA_BYPASS_TOKEN` | *(empty)* | Per-request opt-out; disabled when empty |

At 300/minute an ordinary session never notices — the global 100 requests/minute
per IP binds first — while the quota allows 30 chain writes per
minute. Tightening staging is the cheapest way to find out whether clients
actually honour `Retry-After` before production does.

## Opting out for load tests

Two options, in order of preference:

1. **`RATELIMIT_QUOTA_ENABLED=false`** for the duration of the run. Simplest, and
   the right choice for a dedicated load-test environment. See
   [LOAD_TESTING.md](LOAD_TESTING.md).
2. **`RATELIMIT_QUOTA_BYPASS_TOKEN=<secret>`**, sent as `X-RateLimit-Bypass` on
   load-generator requests. Use this when the run has to share an environment
   with traffic that should stay metered.

Empty by default, so an environment that has not opted in cannot be bypassed by
guessing. Treat a configured value as a credential: anyone holding it can spend
the expensive dependencies without limit. The API logs a warning at startup if
one is set in production.

## Metrics

Every decision is emitted through `QuotaConfig.Observer` as a `QuotaEvent`
(subject key, matched route pattern, cost, allowed, degraded). The callback keeps
this package free of a metrics dependency; wiring it to a counter is the
integration point for [#1043](https://github.com/Suncrest-Labs/nester/issues/1043).

`Route` is the matched *pattern*, never the raw path — paths carry IDs and would
give the metric unbounded cardinality. Requests matching no declared route report
their method alone (`GET *`).

Individual rejections are logged at debug, not warn: the volume is chosen by
whoever is being throttled, so warning on each one would hand an abusive caller a
log-amplification lever. Visibility belongs on the metric.
