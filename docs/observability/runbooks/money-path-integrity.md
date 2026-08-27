# Runbook — Money-path integrity (reconciliation and pending submissions)

**Alerts:** `ReconciliationDivergence` (page), `ReconciliationStalled` (page), `ReconciliationFailing` (ticket), `PendingSubmissionsBacklogged` (page), `MoneyPathMetricsAbsent` (page)
**Dashboard:** [SLO — Money path integrity](/d/nester-slo-money-path/money-path-integrity)
**Issue:** nester#1108

---

## What the alert means

The flow SLOs answer "are deposits and withdrawals succeeding". Balance
freshness answers "is the indexer keeping up". This group answers the question
neither can: **does our record of the money still agree with the chain, and is
anything stuck in flight.**

| Alert | Meaning |
|---|---|
| `ReconciliationDivergence` | Our records and the chain disagree. A balance somewhere is wrong. |
| `ReconciliationStalled` | The reconciler has not completed a pass. **A zero divergence count right now means nothing.** |
| `ReconciliationFailing` | The loop is ticking but every pass aborts before inspecting anything. Same trap as above, narrower cause. |
| `PendingSubmissionsBacklogged` | A submission has been in flight far too long. Someone's money is in limbo. |
| `MoneyPathMetricsAbsent` | The series these alerts read are gone. Money-path monitoring is dark. |

**The trap, and it is the important part of this runbook:** three of these
alerts exist only because "no divergences reported" is ambiguous. It means
either "we checked and everything agrees" or "we did not check". Those are
opposite conclusions from an identical graph. If `ReconciliationStalled`,
`ReconciliationFailing`, or `MoneyPathMetricsAbsent` is firing, **treat
divergence silence as unknown, not clean**, and resolve the liveness alert
before drawing any conclusion about integrity.

---

## First three actions

**1. Establish whether the reconciler is actually running.**

```promql
reconcile:health:last_run_age_seconds
```

- Sawtoothing near zero → running. The divergence signal is trustworthy.
  Go to [Cause A](#cause-a--genuine-divergence).
- Climbing steadily → **stopped**. Go to
  [Cause B](#cause-b--reconciler-stalled).
- No series → go to [Cause D](#cause-d--metrics-absent).

**2. If it is running, read the divergence breakdown by kind.**

```promql
sum by (kind) (increase(nester_reconcile_divergences_total[1h]))
```

The kinds are not equally serious. See
[Reading the divergence kinds](#reading-the-divergence-kinds).

**3. Check the in-flight backlog.**

```promql
submission:pending:count
submission:pending:oldest_age_seconds
```

Depth alone proves nothing — a queue of 200 that drains in seconds is healthy.
The oldest-age series is the one that matters.

---

## Reading the divergence kinds

| Kind | Meaning | Severity |
|---|---|---|
| `mismatch` | Both sides have the record, the **values differ**. | Worst. A displayed balance is wrong. |
| `missing` | The chain has it, we do not. | Serious. We are under-crediting someone. |
| `extra` | We have it, the chain does not. | Serious. We may be crediting money that never moved. |
| `stuck` | Submitted, never reached a terminal state. | Usually transient; chronic means the pipeline is broken. |

A `mismatch` is the one to escalate first. `missing` and `extra` mean a record
is absent on one side, which is recoverable by re-indexing. `mismatch` means
both sides think they know the value and disagree, which cannot be resolved by
re-reading — someone has to decide which is right, and the chain is
authoritative.

A steady low rate of `stuck` during normal traffic is expected: it counts
transactions old enough to poll that the chain has not yet resolved. A `stuck`
count that grows without draining is the same condition
`PendingSubmissionsBacklogged` fires on.

---

## Cause A — genuine divergence

The reconciler ran and found real disagreement.

**Do not restart anything.** A restart clears no divergence and destroys the
in-memory context of what was mid-flight.

1. Identify the affected transactions from the poller logs — the divergence
   metric is deliberately unlabelled by user or hash (cardinality policy in
   `docs/observability/metrics.md`), so the logs are the join key:

   ```
   grep "transaction poller" <api logs> | grep -v "status reconciled"
   ```

2. For each affected transaction, treat **the chain as authoritative**. Confirm
   the on-chain state via Horizon before changing any record.

3. Assess blast radius before correcting: one stuck submission is an
   individual support case, a divergence rate that scales with traffic is a
   systematic bug in the submission or indexing path and correcting individual
   rows will not fix it.

4. If the divergence is systematic, consider pausing deposits before
   correcting. Continuing to accept money into a ledger known to disagree with
   the chain compounds the problem.

---

## Cause B — reconciler stalled

`ReconciliationStalled` is firing: no pass has completed in over 10 minutes,
against a 15s interval.

1. Confirm the poller is enabled. `TX_POLLER_ENABLED` unset disables it
   **silently at boot** — the same class of failure as an empty
   `STELLAR_RPC_URL` disabling the indexer.

2. Check whether passes are failing rather than not running:

   ```promql
   sum(rate(nester_reconcile_runs_total{outcome="failed"}[5m]))
   ```

   Non-zero → the loop is alive, passes are aborting. That is
   [Cause C](#cause-c--passes-failing).

   Zero, with no `completed` either → the goroutine is gone or wedged. Check
   for a panic in the poller goroutine, and for database connection exhaustion
   blocking `ListPendingOlderThan`.

3. Once it recovers, **re-check the divergence count**. Divergences that
   occurred during the stall were never counted; the first pass after recovery
   is the first honest reading.

---

## Cause C — passes failing

`ReconciliationFailing`: the loop ticks, but each pass aborts while listing
pending transactions and inspects nothing.

Almost always a database error on the list-pending query. Check:

- Connection pool exhaustion (`nester_db_pool_acquired_connections` against
  `nester_db_pool_max_connections`).
- Statement timeouts on the pending-transaction query as the table grows.
- Replica lag if the query is routed to a read replica.

This tickets rather than pages because `ReconciliationStalled` pages if the
loop stops entirely. The divergence count is equally false-clean in both cases.

---

## Cause D — metrics absent

`MoneyPathMetricsAbsent`: `nester_reconcile_last_run_age_seconds` has not been
reported for 10 minutes. Every alert in this group evaluates against it, so
reconciliation and pending-submission monitoring are both dark.

1. Is the API up? If other SLO alerts are firing, this is a symptom — fix the
   outage.
2. Is the scrape working? Check the `nester-api` target in Prometheus.
3. Did the poller ever start? `TX_POLLER_ENABLED` unset means these series are
   never produced and this alert is the only thing that will tell you.

---

## Pending submissions backlogged

`PendingSubmissionsBacklogged`: the oldest in-flight submission is over 30
minutes old.

This is a user whose deposit or withdrawal is in limbo — the funds have left
their visible balance and have not arrived anywhere they can act on.

1. Check Horizon ingestion before assuming the chain is at fault. The poller
   asks Horizon for status; if Horizon is behind, transactions look stuck when
   they have actually settled. Cross-check one hash directly against the RPC
   endpoint.

2. Check the submission pipeline for transactions that were built and signed
   but never submitted, which present identically to submitted-and-unresolved.

3. Check `nester_flow_attempts_total{outcome="failed_chain"}` — a rising
   failure rate alongside a growing backlog points at the chain or the
   invoker, not at our record-keeping.

4. For individual affected users, the admin money-path lookup
   (`GET /api/v1/admin/users/{id}/money-path`) shows positions, recent
   transactions, and pending submissions in one view.

---

## Verifying recovery

Do not resolve on the alert clearing alone. Confirm all three:

```promql
# 1. The reconciler is alive — sawtoothing, not climbing.
reconcile:health:last_run_age_seconds

# 2. Passes are completing, not failing.
sum by (outcome) (rate(nester_reconcile_runs_total[5m]))

# 3. No new divergences since the fix.
sum(increase(nester_reconcile_divergences_total[15m]))
```

The third only means anything once the first two are healthy — that is the
trap this whole runbook is built around.

---

## Related

- [Balance freshness](balance-freshness.md) — indexer lag. A lagging indexer
  can produce apparent divergences that resolve on their own once it catches
  up; check indexer lag before treating a divergence as real.
- [Flow success](flow-success.md) — deposit and withdrawal failure rates.
- [Flow latency](flow-latency.md) — settlement latency.
- `docs/observability/slo.md` — SLI definitions and burn-rate arithmetic.
