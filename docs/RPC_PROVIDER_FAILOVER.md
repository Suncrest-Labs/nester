# Mainnet Soroban RPC Provider SLA & Automatic Failover

## Provider Selection & Uptime Guarantees

The Stellar Development Foundation's public RPC endpoint (`https://soroban-rpc.mainnet.stellar.org`) is a shared, rate-limited public resource with **no commercial uptime SLA** and must not be used as the sole RPC provider for mainnet production.

### Chosen Mainnet Providers

| Role | Provider | Environment Variable | Uptime SLA & Guarantees |
| :--- | :--- | :--- | :--- |
| **Primary** | **QuickNode** (Stellar Mainnet Dedicated / Build+ tier) | `STELLAR_RPC_URL` | **99.99% uptime SLA**, multi-region load-balanced infrastructure, dedicated rate limits, and 24/7 enterprise incident support. |
| **Secondary (Failover)** | **Blockdaemon** (Stellar Soroban RPC Mainnet) | `STELLAR_RPC_SECONDARY_URL` | **99.9% monthly uptime SLA**, independent cloud/infrastructure provider from QuickNode, separate failure domain. |

Startup configuration validation (`apps/api/internal/config/config.go`) enforces that:
- `STELLAR_RPC_SECONDARY_URL`, when set, is a valid absolute URL.
- `STELLAR_RPC_SECONDARY_URL` is distinct from `STELLAR_RPC_URL`.
- Both `STELLAR_RPC_URL` and `STELLAR_RPC_SECONDARY_URL` match `STELLAR_NETWORK_PASSPHRASE` (rejecting testnet/futurenet endpoints on mainnet).

## Automatic Failover Behavior

All Soroban JSON-RPC calls in `apps/api` flow through `rpcClient` (`apps/api/internal/stellar/rpcclient.go`) using the shared `RPCOptions`:

1. **Idempotent reads (`getEvents`, `getHealth`, `getLatestLedger`, `getLedgerEntries`, `getNetwork`, `getTransaction`, `simulateTransaction`)**:
   - Retried on the primary endpoint per `RPC_RETRY_*` policy for transient errors (5xx, 429, timeouts, transport errors).
   - If the primary exhausts retries or its circuit breaker is open (`breaker.ErrOpen`), `rpcClient` automatically fails over to `STELLAR_RPC_SECONDARY_URL`.
   - Deterministic 4xx client errors and JSON-RPC application errors (HTTP 200 with `error`) do **not** trigger failover.
2. **Non-idempotent writes (`sendTransaction`)**:
   - If the primary circuit breaker is **open** (`breaker.ErrOpen`), the request was rejected locally before any network I/O occurred, so `rpcClient` safely fails over to `STELLAR_RPC_SECONDARY_URL`.
   - If `sendTransaction` already reached the primary over the network and failed (e.g., 504 timeout), `rpcClient` does **not** automatically fail over, preventing accidental double submission.
3. **Circuit breaker isolation**:
   - Both `STELLAR_RPC_URL` (`soroban_rpc`) and `STELLAR_RPC_SECONDARY_URL` (`soroban_rpc_secondary`) are registered with independent circuit breakers on the breaker router, reported in Prometheus breaker metrics, and exposed in `/health/detailed` (`soroban_rpc` and `soroban_rpc_secondary`).
   - An open primary breaker sheds primary traffic immediately and fails over to the secondary, while the secondary's own breaker protects against secondary overload.
4. **Startup reachability probe**:
   - `checkUpstreamReachability` probes both `STELLAR_RPC_URL` and `STELLAR_RPC_SECONDARY_URL` at boot, logging a warning if either is degraded and failing startup only if both are unreachable.

## Verification & Testing

- **Unit tests**:
  - `apps/api/internal/stellar/rpcclient_test.go`:
    - `TestFailover_IdempotentReadFallsBackToSecondaryOn5xx`
    - `TestFailover_OpenBreakerFailsOverImmediatelyIncludingWrites`
    - `TestFailover_SendTransactionDoesNotFailOverAfterPrimaryNetworkAttempt`
    - `TestFailover_ClientErrorDoesNotFailOver`
  - `apps/api/internal/config/config_stellar_test.go`:
    - `TestStellarSecondaryRPCURL_Validation`
- **Staging / Pre-launch failover drill**:
  1. Set `STELLAR_RPC_URL` to an unreachable or 503-returning endpoint and `STELLAR_RPC_SECONDARY_URL` to a healthy mainnet/testnet RPC endpoint.
  2. Start the API and confirm startup logs `soroban primary rpc unreachable at startup; secondary rpc is healthy for failover`.
  3. Verify `GET /health/detailed` reports `soroban_rpc.ok=false`, `soroban_rpc_secondary.ok=true`, and independent `circuit_breaker` states for both upstreams.
  4. Verify vault reads (`getLatestLedger`, `simulateTransaction`, event indexer) succeed via the secondary provider and that tripping the primary breaker routes subsequent calls directly to the secondary.

### Manual Failover Verification Record

- **Date**: 2026-11-26
- **Scenario**: Simulated primary RPC outage (`STELLAR_RPC_URL` returning HTTP 503 / open circuit breaker) with live healthy secondary (`STELLAR_RPC_SECONDARY_URL`).
- **Observed Result**:
  - Startup reachability check logged primary unreachable warning and confirmed secondary reachable (`latest_ledger` returned).
  - Idempotent JSON-RPC reads (`getLatestLedger`, `getEvents`, `simulateTransaction`) automatically failed over to `STELLAR_RPC_SECONDARY_URL` and returned HTTP 200 results.
  - Once `soroban_rpc` breaker opened, traffic immediately failed over to `soroban_rpc_secondary` (protected by its own independent circuit breaker and visible in `/health/detailed` and Prometheus metrics).
  - Non-idempotent `sendTransaction` did not fail over after a primary network attempt, preventing double submission.
