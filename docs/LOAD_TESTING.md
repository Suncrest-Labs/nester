# Load testing

This directory contains **k6** workload definitions for the Nester launch-critical paths. They are intentionally external black-box tests: run them only against a disposable local environment or an approved, isolated staging environment. **Never run them against production.**

## Coverage and workload contract

| Surface | k6 scenario | target | Notes |
| --- | --- | ---: | --- |
| Vault deposit/write | `deposit` | 50 RPS | The current API route is `POST /api/v1/vaults/{VAULT_ID}/deposit`, not the issue's generic `/transactions/deposit`. |
| Vault listing/read | `vaults` | 500 RPS | p95 must remain below 500 ms in steady state. |
| Portfolio aggregation/read | `portfolio` | 200 RPS | Defaults to the implemented `GET /api/v1/portfolio/summary`; override `PORTFOLIO_URL` if testing a user-scoped deployment route. p95 must remain below 500 ms in steady state. |
| Intelligence chat | `chat` | 20 RPS | Calls `POST /intelligence/chat` directly, so set `INTELLIGENCE_BASE_URL`; do not spend real Claude budget without approval. |
| Auth challenge | `challenge` | 200 RPS | Exercises Redis-backed challenge creation. Set `CHALLENGE_WALLETS` to a comma-separated pool of valid Stellar addresses to exercise distinct Redis keys. |
| WebSocket hub | `websocket` | 1,000 connections | Steady state holds 500 connections, satisfying the acceptance gate; ramp/spike exercise the stated 2x/5x limits. |

The four scripts run all six scenarios concurrently:

- `steady.js`: 50% of every HTTP target for 10 minutes and 500 WebSocket connections.
- `ramp.js`: linear 0 → 2x target over 5 minutes.
- `spike.js`: 50% baseline, 5x burst for 30 seconds, then return to baseline.
- `soak.js`: 20% target for 2 hours and 200 persistent WebSocket connections.

The event indexer is not an HTTP endpoint. During each write workload, monitor its event lag, duplicate/failed event count, pending transaction count, Postgres pool saturation, Redis memory, and API RSS. This detects the data-desynchronisation risk without forging chain events.

## Prerequisites and configuration

Install [k6](https://grafana.com/docs/k6/latest/set-up/install-k6/) (v0.49 or newer). Start the local dependency stack first:

```bash
docker compose up -d postgres redis api intelligence
# Wait until: curl -f http://localhost:8080/readyz
```

Use a **dedicated load-test account/vault**, an authenticated token where the target requires it, and a testnet Stellar operator. Deposits are writes and can invoke chain logic.

```bash
export API_BASE_URL=http://localhost:8080/api/v1
export INTELLIGENCE_BASE_URL=http://localhost:8000/intelligence
export WS_URL=ws://localhost:8080/ws
export VAULT_ID='<dedicated-test-vault-uuid>'
export AUTH_TOKEN='<dedicated-test-jwt>'
# Optional only if route differs in that environment:
# export DEPOSIT_URL="$API_BASE_URL/transactions/deposit"
# export PORTFOLIO_URL="$API_BASE_URL/users/portfolio"
```

Before a run, ensure rate limits are sized for the test source (or allowlist that source only in staging). Default per-IP API limits intentionally reject these traffic levels; treating those expected `429`s as a performance pass would invalidate the result.

## Run and save evidence

```bash
mkdir -p results
k6 run --summary-export results/steady-summary.json tests/load/steady.js
k6 run --summary-export results/ramp-summary.json tests/load/ramp.js
k6 run --summary-export results/spike-summary.json tests/load/spike.js
k6 run --summary-export results/soak-summary.json tests/load/soak.js
```

Use a generator with enough CPU, file descriptors, ephemeral ports, and network capacity—especially for the 5,000-connection spike. If it emits `dropped_iterations`, increase `preAllocatedVUs`/`maxVUs` or distribute the test before attributing a failure to Nester.

## Interpretation and gates

k6 fails the run when any declared threshold fails:

- `http_req_duration{endpoint:vaults}` and `{endpoint:portfolio}`: p95 < **500 ms**;
- total HTTP failure rate < **1%** and checks > **99%**;
- WebSocket connection p95 < **1 s**.

Record p50, p95, p99, error rate, dropped iterations, websocket connection failures, API/worker RSS, goroutines, database connections/slow queries, Redis memory/evictions, and indexer lag. Compare soak start/end values: monotonic RSS, open FD/goroutine, connection, or lag growth is a leak/regression. HTTP `401`, `403`, `404`, `422`, and `429` are configuration or data errors—not successful load-test responses.

## CI

`.github/workflows/load-soak.yml` runs the two-hour soak nightly and can be launched manually. It requires the `STAGING_LOAD_API_BASE_URL`, `STAGING_LOAD_INTELLIGENCE_BASE_URL`, `STAGING_LOAD_WS_URL`, `STAGING_LOAD_VAULT_ID`, and `STAGING_LOAD_AUTH_TOKEN` repository secrets. The workflow exits safely when the staging API URL secret is absent and uploads the k6 JSON summary when it runs. Keep staging isolated and use a dedicated test vault.
