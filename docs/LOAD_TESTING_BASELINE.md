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
