# Runbook — Money Path Incident Response (Stuck Deposits, Withdrawals & Infrastructure)

**Alerts:** `FlowSuccessFastBurn` (page), `PendingSubmissionsBacklogged` (page), `IndexerLagStale` (page), `ReconciliationDivergence` (page), `APIAvailabilityFastBurn` (page)
**Dashboard:** [SLO — Money Path Integrity](/d/nester-slo-money-path/money-path-integrity)
**Phase:** Testnet / Production Ops

---

## Overview

When a deposit, withdrawal, or core money-path transaction is stuck or failing at 3am, this runbook provides the exact steps to detect, diagnose, remediate, and verify recovery without needing a codebase tour.

---

## 1. Stuck Deposit / Withdrawal

### How to Detect
- **Alerts:** `FlowSuccessFastBurn`, `PendingSubmissionsBacklogged`
- **User symptom:** User reports funds submitted in dApp but balance not credited or fiat offramp not initiated after >30 seconds.
- **Metrics:**
  ```promql
  sum by (flow, outcome) (rate(nester_flow_attempts_total[5m]))
  submission:pending:count
  submission:pending:oldest_age_seconds
  ```

### How to Diagnose
1. **Check if the transaction reached the chain:**
   ```bash
   kubectl logs -l app=nester-api --since=30m | grep -iE "deposit failed|withdrawal failed|RecordDeposit|RecordWithdrawal"
   ```
2. **Check Soroban RPC status and latency:**
   ```promql
   rate(nester_outbound_errors_total{upstream="soroban_rpc"}[5m])
   histogram_quantile(0.95, sum by (le) (rate(nester_outbound_request_duration_seconds_bucket{upstream="soroban_rpc"}[5m])))
   ```
3. **Check database transaction lock / pool exhaustion:**
   ```promql
   nester_db_pool_acquired_connections / nester_db_pool_max_connections
   rate(nester_db_pool_empty_acquire_waits_total[5m])
   ```

### How to Remediate
- **If Soroban RPC is timing out / degraded:** Fail over to backup RPC endpoint by updating `STELLAR_RPC_URL` in Vault secret / environment config and restarting pods.
- **If stuck in pending submission queue due to nonce or gas mismatch:** Use the admin CLI tool or database transaction review to inspect the submission. If safe and unconfirmed on-chain after 15 minutes, re-queue or cancel via administrative override.
- **If database connection pool is exhausted:** Scale up connection limit or restart API instances if leaked connections are holding locks.

### How to Verify Recovery
- `submission:pending:oldest_age_seconds` drops below 60s.
- `nester_flow_attempts_total{outcome="success"}` rate returns to baseline.
- Synthetic probes pass successfully.

---

## 2. Indexer Stalled or Diverged

### How to Detect
- **Alerts:** `IndexerLagHigh`, `IndexerLagStale`, `ReconciliationDivergence`
- **Metrics:**
  ```promql
  nester_indexer_lag_last_sample_age_seconds
  sum by (kind) (increase(nester_reconcile_divergences_total[1h]))
  ```

### How to Diagnose
1. **Check indexer logs for cursor or fetch errors:**
   ```bash
   kubectl logs -l app=nester-api --since=30m | grep -i "event indexer"
   ```
   - `event indexer failed to persist cursor` → database write lock or constraint failure.
   - `event indexer fetch failed` → Soroban RPC issue.
2. **Inspect reconciliation divergence breakdown:**
   - `mismatch`: Chain and local records disagree on amounts (Critical).
   - `missing`: Chain has event, local DB does not (recoverable by backfill).
   - `extra`: Local DB has record not found on-chain.

### How to Remediate
- **For a stalled indexer (frozen cursor):** Restart the api/indexer container. If cursor persistence is failing due to bad state, reset the cursor back to the last known good ledger in `system_state` and let it replay.
- **For indexer divergence (`mismatch` / `missing`):** Trigger a targeted backfill or reconciliation sweep for the affected vault/account using the backfill command:
  ```bash
  ./backfill --from-ledger <ledger-number>
  ```

### How to Verify Recovery
- `nester_indexer_lag_last_sample_age_seconds` returns to <10s.
- `nester_reconcile_divergences_total` drops to 0 across all kinds (`mismatch`, `missing`, `extra`).

---

## 3. RPC Outage

### How to Detect
- **Alerts:** `APIAvailabilityFastBurn`, `IndexerLagStale`, elevated outbound errors.
- **Metrics:**
  ```promql
  sum by (upstream, kind) (rate(nester_outbound_errors_total{upstream="soroban_rpc"}[5m]))
  ```

### How to Diagnose
1. Test RPC health directly:
   ```bash
   curl -X POST -H "Content-Type: application/json" -d '{"jsonrpc":"2.0","id":1,"method":"getLatestLedger"}' <STELLAR_RPC_URL>
   ```
2. Check if public Stellar/Soroban testnet/mainnet RPC is experiencing a broader network outage.

### How to Remediate
- Switch `STELLAR_RPC_URL` to secondary/backup RPC provider immediately in secret manager / environment variables and trigger rolling restart of API and worker services.

### How to Verify Recovery
- Outbound error rate for `soroban_rpc` drops to 0.
- Ledger height advances normally in metrics.

---

## 4. Database Failover

### How to Detect
- **Alerts:** `APIAvailabilityFastBurn`, `SLOTargetDown`, DB connection errors in logs.
- **Metrics:**
  ```promql
  rate(nester_db_pool_empty_acquire_waits_total[5m])
  ```

### How to Diagnose
1. Check database pod/instance status and connectivity:
   ```bash
   kubectl get pods -l app=postgres
   kubectl logs -l app=nester-api --since=15m | grep -iE "connection refused|database is starting|sql: no rows"
   ```

### How to Remediate
- **If primary DB instance crashed:** Trigger read-write failover to replica via cloud provider console / database orchestrator.
- Update `DATABASE_URL` connection string if DNS failover is not automatic.
- Restart API services to re-establish connection pool against the new primary.

### How to Verify Recovery
- `/healthz` endpoint returns HTTP 200.
- Database connection pool metrics stabilize (`nester_db_pool_acquired_connections` well below max).
- API 5xx rate drops back to baseline (<0.1%).
