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
| `ReconciliationStalled` | The transaction poller has not completed a pass. **A zero divergence count right now means nothing.** |
| `ReconciliationFailing` | A reconciliation loop is ticking but every pass aborts before completing. Same trap as above, narrower cause. |
| `BalanceReconciliationStalled` | The vault-balance reconciler (#1082) has not completed a sweep. Balance divergence silence is unknown, not clean. |
| `BalanceReconciliationMetricsAbsent` | The balance reconciler's liveness series is gone — disabled, leaderless, or dead. The balance safety net is off. |
| `PendingSubmissionsBacklogged` | A submission has been in flight far too long. Someone's money is in limbo. |
| `MoneyPathMetricsAbsent` | The series these alerts read are gone. Money-path monitoring is dark. |

Two loops feed this group. The **transaction poller** (#1108) reconciles
pending transaction *statuses* against Horizon every 15s. The **vault-balance
reconciler** (#1082) sweeps every active vault every 5 minutes
(`RECONCILE_INTERVAL`), reads the authoritative `total_assets` from the vault
contract, and compares it against `vaults.current_balance` **in raw stroops**
— the unit the event indexer stores (migration 103). Each divergence is
written to `reconciliation_findings`, logged, and counted on the shared
divergence metric; it is **never auto-corrected**. Each loop has its own
liveness alert because the shared age gauge is reset every 15s by the poller —
one loop's health must not vouch for the other's.
`RECONCILE_DRY_RUN=true` rehearses a sweep against production data: findings
go to the log only, nothing is written or counted, and liveness still emits.

**First rollout warning:** the balance comparison is exact and in stroops. A
deployment whose vault balances were ever written by the pre-#1082 service
paths (which store display USDC rather than stroops) will diverge on its
first live sweep — those ARE real record-vs-chain disagreements, but they
will page all at once. Run the first sweep on an existing deployment with
`RECONCILE_DRY_RUN=true`, triage the logged findings, reconcile the rows, and
only then go live.

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

1. Identify the affected records from the logs — the divergence metric is
   deliberately unlabelled by user, vault, or hash (cardinality policy in
   `docs/observability/metrics.md`), so the logs are the join key:

   ```
   # stuck transactions (poller):
   grep "transaction poller" <api logs> | grep -v "status reconciled"
   # balance mismatches (balance reconciler) — carries vault id, both values,
   # and the difference:
   grep "reconciliation divergence" <api logs>
   ```

   Balance findings are also durable rows: query `reconciliation_findings`
   (joined to `reconciliation_runs`) for entity ids, recorded vs on-chain
   values, severity, and resolution state.

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

`ReconciliationFailing`: a loop ticks, but its passes abort before completing.
Both loops feed this series — the transaction poller (a pass that cannot list
pending transactions) and the balance reconciler (a pass that cannot list
vaults, or whose chain reads all failed).

Usually a database error. Check:

- Connection pool exhaustion (`nester_db_pool_acquired_connections` against
  `nester_db_pool_max_connections`).
- Statement timeouts on the pending-transaction or vault listing queries as
  the tables grow.
- Replica lag if the query is routed to a read replica.
- For the balance loop: the Soroban RPC circuit breaker — every chain read
  failing fails the pass by design, so an RPC outage cannot read as "all
  vaults agree".

This tickets rather than pages because the stall alerts page if a loop stops
entirely — and the balance reconciler's liveness anchor only advances on
*successful* passes, so its persistent failure also climbs
`reconcile:balance:last_run_age_seconds` and pages via
`BalanceReconciliationStalled`. The divergence count is equally false-clean
in every one of these cases.

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

## Cause E — balance reconciler stalled or absent

`BalanceReconciliationStalled` (`reconcile:balance:last_run_age_seconds >
1800`) or `BalanceReconciliationMetricsAbsent` (the series is gone). Either
way, nothing is confirming vault balances against the chain.

1. **Absent:** the age series is emitted only by the elected leader while the
   reconciler runs. Check, in order: `RECONCILE_ENABLED=false` (disabling the
   safety net pages by design), scheduler leadership (no replica holding the
   advisory lock means no replica emits), and whether the API booted at all.
2. **Stalled:** the leader holds the series but passes are not completing
   successfully — the age anchor advances only on success, so a hanging
   sweep and a persistently failing one both land here. A full sweep reads
   `total_assets` once per active vault; check
   `nester_reconcile_runs_total{outcome="failed"}` (failing, not hanging →
   see [Cause C](#cause-c--passes-failing)), the Soroban RPC circuit
   breaker, and whether the vault count has outgrown the interval. A vault
   whose individual chain read fails is logged and skipped, never fatal —
   only every read failing fails the pass. `RECONCILE_INTERVAL` raises the
   cadence; the 1800s alert threshold must move with it if it exceeds 30
   minutes.
3. Once it recovers, the first completed pass is the first honest reading of
   balance divergence — same recovery rule as Cause B.

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
