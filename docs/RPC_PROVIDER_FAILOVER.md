# Mainnet Soroban RPC Provider SLA & Automatic Failover

## Provider Selection & Uptime Guarantees

The Stellar Development Foundation's public RPC endpoint (`https://soroban-rpc.mainnet.stellar.org`) is a shared, rate-limited public resource with **no commercial uptime SLA** and must not be used as the sole RPC provider for mainnet production.

Mainnet production deployments must configure **two independent Soroban RPC providers**:

| Role | Environment Variable | Requirement |
| :--- | :--- | :--- |
| Primary | `STELLAR_RPC_URL` | Commercial Soroban RPC provider (e.g., Blockdaemon, QuickNode, or Nodies dedicated tier) with a contractual **>= 99.9% monthly uptime SLA**, dedicated rate limits, and 24/7 incident support. |
| Secondary | `STELLAR_RPC_SECONDARY_URL` | Independent commercial Soroban RPC provider on separate infrastructure (distinct provider and host from `STELLAR_RPC_URL`) with **>= 99.9% uptime SLA**. |

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
   - The primary `STELLAR_RPC_URL` is guarded by the Soroban circuit breaker; `STELLAR_RPC_SECONDARY_URL` is a distinct host so an open primary breaker does not block failover traffic.
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
  3. Verify vault reads (`getLatestLedger`, `simulateTransaction`, event indexer) succeed via the secondary provider.
