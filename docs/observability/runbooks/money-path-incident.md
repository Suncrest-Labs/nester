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
- **User symptom:** User reports funds submitted in dApp but balance not credited after >30 seconds.
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
- **If stuck in the pending queue:** The backlog behind `submission:pending:count` is rows in `transactions` with `status = 'pending'` and a non-null `tx_hash`; the transaction poller reconciles them against the chain. Inspect the oldest directly:

  ```sql
  SELECT id, vault_id, type, amount, tx_hash, created_at, error_reason
  FROM transactions
  WHERE status = 'pending' AND tx_hash IS NOT NULL
  ORDER BY created_at ASC
  LIMIT 20;
  ```

  Take each `tx_hash` to Horizon before touching the row — the chain, not the database, is the source of truth for whether the money moved:

  ```bash
  curl -s "$HORIZON_URL/transactions/<TX_HASH>" | jq '{successful, ledger, created_at}'
  ```

  If Horizon reports the transaction succeeded, the submission landed and only reconciliation is behind: leave the row alone and let the poller catch up, or restart the API to restart polling. If Horizon returns 404 well past the ledger close window, the transaction never landed and the row can be failed so the user can retry. Do not mark a row confirmed by hand.
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

---

## Rehearsal

**Status: not yet rehearsed.** Issue #1113 requires this runbook to be walked
through against staging and corrected from what the walkthrough exposes. That
has not happened, and it is the remaining work on this document — every
procedure below the detection queries is reasoned from the code rather than
confirmed by having done it.

Rehearse against the staging environment from #1114, not production. Reset it
first (`docs/observability/runbooks/staging-reset-procedure.md`) so the drill
starts from a known state.

Suggested drill order, cheapest to most disruptive:

1. **RPC outage.** Point `STELLAR_RPC_URL` at an unroutable address, submit a
   deposit, and confirm `nester_outbound_errors_total{upstream="soroban_rpc"}`
   climbs and the dApp degrades honestly rather than spinning.
2. **Indexer stall.** Stop the indexer, confirm `IndexerLagHigh` fires and that
   the lag figures in section 2 match what the runbook says to look at.
3. **Stuck deposit.** Kill the API between chain submission and reconciliation,
   then follow section 1 end to end — including the Horizon check — and confirm
   the row resolves the way the runbook claims.
4. **Database failover.** Restart the Postgres container under load and confirm
   the pool recovers without a manual API restart, or correct section 4 if it
   does not.

What a rehearsal is for is finding the steps that read plausibly and do not
work. Correct this document in place from what each drill exposes, and replace
this section with the date and findings once all six scenarios have been run.
