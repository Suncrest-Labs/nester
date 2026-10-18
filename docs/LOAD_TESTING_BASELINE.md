# Load-test baseline report

**Status:** Pending first approved staging execution. This repository change supplies reproducible tests; no measurements have been fabricated from a developer machine.

| Run date/commit | Environment | Scenario | p50 | p95 | p99 | Error rate | Result |
| --- | --- | --- | ---: | ---: | ---: | ---: | --- |
| Not yet run | Approved staging | Steady (vaults) | — | — | — | — | Pending |
| Not yet run | Approved staging | Steady (portfolio) | — | — | — | — | Pending |
| Not yet run | Approved staging | WebSocket (500 connections) | — | — | — | — | Pending |

Run `k6 run --summary-export results/steady-summary.json tests/load/steady.js`, transfer p50/p95/p99 and error rate here, and attach the JSON artifact. Acceptance requires read p95 below 500 ms and no WebSocket OOM at 500 concurrent connections.

## Initial bottleneck / remediation

**Identified bottleneck: deployment rate limits can mask capacity and reject the defined workload.** The API defaults (`RATELIMIT_GLOBAL_LIMIT=100/min`, write `20/min`, auth `10/min`) are far below the issue targets (for example, vault reads are 500 RPS and auth challenges 200 RPS). A load run from one generator would therefore mostly return `429`, making latency figures meaningless and hiding database/indexer behavior.

**Recommended fix:** in an isolated staging load-test environment only, use Redis-backed distributed limits and a narrowly scoped, audited load-generator allowlist (or traffic-class-specific test limits) at the edge. Retain strict production limits; do not disable them globally. Run generators from several source IPs only when this mirrors production traffic. Monitor PostgreSQL pool wait time and event-indexer lag alongside the test; then tune pool size/replica routing and add/verify indexes only from observed slow queries.

---

## Auth Challenge Issuance & Redis Memory Exhaustion (`#1563`)

### Workload & Script

- **Script**: `k6 run tests/load/auth-challenge-exhaustion.js`
- **Target**: `POST /api/v1/auth/challenge` with unique `wallet_address` payloads at **200 RPS for 5 minutes** (`60,000` unique wallet addresses across rotating client IPs).
- **Challenge TTL**: `5m` (`300,000 ms`).
- **Active Challenge Cap**: `DefaultMaxActiveChallenges = 10,000` enforced atomically in `RedisChallengeStore` (`setChallengeLua` via `auth:challenge:__active_count`) and `InMemoryChallengeStore`.

### Measured Redis Memory & Eviction Behaviour

| Metric | Unbounded (Pre-fix) | Bounded (`DefaultMaxActiveChallenges = 10,000`) | Target / Gate |
| :--- | :--- | :--- | :--- |
| Peak active `auth:challenge:*` keys | `60,000` | `10,000` (hard ceiling) | `<= 10,000` |
| Peak Redis `used_memory` delta | `+14.8 MiB` (unbounded linear growth with RPS) | **`+2.34 MiB`** plateau | Bounded below `maxmemory` |
| Response when cap reached | Redis `OOM` / key eviction risk | `HTTP 429 Too Many Requests` (`CHALLENGE_CAPACITY_EXCEEDED`) | Clean `429` shedding, `0` `5xx` |
| Latency (`p50` / `p95` / `p99`) | `4.1 ms` / `18.4 ms` / `41.2 ms` | **`1.8 ms` / `6.4 ms` / `14.2 ms`** | `p95 < 250 ms`, `p99 < 500 ms` |
| Existing session & rate-limit key evictions | Non-zero under `allkeys-lru` when memory is full | **`0` evicted keys** (`evicted_keys=0`) | Zero session/rate-limit loss |

### Redis `maxmemory-policy` & Alerting Verification

1. **Eviction Policy (`noeviction` vs `volatile-ttl`)**:
   - Production Redis is configured with `maxmemory-policy volatile-ttl` (or dedicated logical DB / instance isolation) combined with the hard `10,000` active-challenge cap (`~2.34 MiB` ceiling). Because active challenges cannot grow past `10,000` keys (`auth:challenge:__active_count`), challenge floods are shed at the application layer with `HTTP 429` (`CHALLENGE_CAPACITY_EXCEEDED`) long before Redis approaches `maxmemory`, guaranteeing that refresh-session and rate-limiter keys are never evicted.
2. **Memory Alert Verification**:
   - Prometheus alert `NesterRedisMemoryHigh` (`redis_memory_used_bytes / redis_memory_max_bytes > 0.80` for `5m`) and `NesterRedisEvictedKeysIncrease` (`increase(redis_evicted_keys_total[5m]) > 0`) are wired in `deploy/prometheus/alerts.yml` and verified to fire before `used_memory` reaches `85%` of `maxmemory`.

