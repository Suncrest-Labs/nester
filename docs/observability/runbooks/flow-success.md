# Runbook — Deposit / withdrawal failure rate

**Alerts:** `FlowSuccessFastBurn` (page), `FlowSuccessSlowBurn` (ticket)
**Dashboard:** [SLO — Deposits and withdrawals](/d/nester-slo-flow/deposits-withdrawals)
**SLO:** 99.5% success per flow, 28-day window

---

## What the alert means

Deposits or withdrawals are failing at a rate that will exhaust the 28-day
error budget in ~50 hours (fast) or ~5 days (slow).

The `flow` label says which one. **They have separate budgets** — a withdrawal
alert firing while deposits are healthy is normal and means the failure is
specific to the withdrawal path.

**What is counted:** only `failed_chain` and `failed_internal`. User
cancellations and rejected (invalid) requests are excluded from both halves of
the ratio, so this alert is never caused by users changing their mind or by a
client sending bad requests.

**User impact:** money is not moving. A failed deposit is funds that did not
start earning; a failed withdrawal is funds the user cannot reach. The second
is worse and generates support contact within minutes.

---

## First three actions

**1. Determine which failure kind dominates.** This single query decides
everything that follows:

```promql
sum by (flow, outcome) (rate(nester_flow_attempts_total[5m]))
```

- `failed_chain` dominant → the chain or the RPC endpoint. Go to
  [Cause A](#cause-a--soroban-rpc-degraded-or-unreachable).
- `failed_internal` dominant → our code or our database. Go to
  [Cause B](#cause-b--database-or-internal-fault).
- Both roughly equal → likely a deploy that broke both paths. Go to
  [Cause C](#cause-c--recent-deploy).

**2. Check whether a deploy correlates.**

```bash
gh run list --repo Suncrest-Labs/nester --workflow ci.yml --limit 10
git log --oneline -15 origin/main
```

Compare the deploy time against when the ratio started climbing on the
dashboard. This is the fastest path to a mitigation, because a rollback is
available immediately.

**3. Confirm real user impact and its scale.**

```promql
sum by (flow) (rate(nester_flow_attempts_total{outcome=~"failed_chain|failed_internal"}[5m])) * 300
```

Failures in the last five minutes. Note this number — it goes in the incident
record and determines whether users need proactive contact.

---

## Dashboards

| Panel | Question it answers |
|---|---|
| **Failure ratio by flow** | Which flow, and how far over threshold |
| **Attempts by outcome** | Which failure kind dominates |
| **Soroban RPC health** | Is the chain dependency the cause |
| **Settlement latency p95** | Are survivors also slow (partial degradation) |

Cross-check the [API availability dashboard](/d/nester-slo-api/api-availability):
flow failures alongside a general 5xx spike means a service-wide problem, not a
flow-specific one.

---

## Logs

```bash
# Chain invocation failures
kubectl logs -l app=nester-api --since=30m | grep -i "on-chain deposit failed\|on-chain withdrawal failed"

# Internal failures around the money path
kubectl logs -l app=nester-api --since=30m | grep -iE "RecordDeposit|RecordWithdrawal" | grep -i error
```

The wrapped error carries the underlying Soroban error, which usually names the
contract error code directly.

Locally: `docker compose logs api --since 30m`.

---

## Traces

Tracing from #1054 is the fastest way to see where in the path time or failures
concentrate.

In [Jaeger](http://localhost:16686) (or the deployed equivalent), select
service `nester-api` and filter on the deposit or withdrawal handler with
`error=true`. The span for the Soroban invocation shows whether the failure was
a timeout, a transport error, or a contract rejection.

Traces are sampled, so absence of a trace is not evidence of absence. Use them
to understand a failure, not to count them.

---

## Three most likely causes

### Cause A — Soroban RPC degraded or unreachable

**Distinguishing evidence:**

```promql
sum by (kind) (rate(nester_outbound_errors_total{upstream="soroban_rpc"}[5m]))
sum by (status_class) (rate(nester_outbound_requests_total{upstream="soroban_rpc"}[5m]))
histogram_quantile(0.95, sum by (le) (rate(nester_outbound_request_duration_seconds_bucket{upstream="soroban_rpc"}[5m])))
```

Confirmed when `failed_chain` dominates **and** outbound errors or 5xx to
`soroban_rpc` rise at the same time. `kind="timeout"` with rising p95 means the
endpoint is slow rather than down; `kind="connect"` or `kind="dns"` means it is
unreachable.

Check network status directly before concluding it is only us — this affects
every Stellar application, so a public status page or block explorer will
confirm quickly.

**Mitigation:** if a secondary RPC endpoint is configured, fail over by
updating `STELLAR_RPC_URL` and restarting. If not, this is a wait-and-monitor
incident: post user-facing status, and do not roll back — a rollback changes
nothing and adds risk.

### Cause B — Database or internal fault

**Distinguishing evidence:**

```promql
nester_db_pool_acquired_connections / nester_db_pool_max_connections
rate(nester_db_pool_canceled_acquires_total[5m])
rate(nester_db_pool_empty_acquire_waits_total[5m])
```

Confirmed when `failed_internal` dominates. Pool exhaustion (acquired near max,
rising empty-acquire waits) means the write cannot get a connection.

This is the more dangerous cause: the chain call may have **succeeded** while
the ledger write failed, so on-chain state and our records disagree. The SLI
counts this as a failure precisely because the user's balance does not reflect
their money.

**Mitigation:** if pool-exhausted, find and kill the long-running queries
holding connections. If a deploy introduced it, roll back. **Then reconcile:**
identify attempts where the chain succeeded and the write did not, because
those users' balances are wrong and will not self-correct.

### Cause C — Recent deploy

**Distinguishing evidence:** the ratio steps up sharply at a deploy boundary
rather than climbing gradually. A step change is a code change; a ramp is a
capacity or dependency problem.

Check the diff for changes to `vault_service.go`, the invoker, validation
logic, or anything touching the contract interface.

**Mitigation:** roll back first, diagnose after. A rollback on a money path is
almost always correct — the cost of an unnecessary rollback is far below the
cost of continued failures.

---

## Immediate mitigation

In order of preference:

1. **Roll back** if a deploy correlates. Fastest and most reliable.
2. **Fail over the RPC endpoint** if a secondary is configured and the chain
   dependency is the cause.
3. **Relieve database pressure** — kill blocking queries, scale the pool — if
   internal failures dominate.
4. **Communicate** if none of the above applies (a genuine network outage).
   Users retrying a failing deposit repeatedly is a worse outcome than users
   who know to wait.

**Do not** disable the metrics or silence the alert to stop the noise. Silence
during an ongoing money-path failure is the worst available state.

---

## Escalation

Escalate when any of these hold:

- Failure ratio above 25% for more than 15 minutes.
- Any evidence of chain/database divergence (Cause B) — this needs reconciliation
  and possibly user contact.
- Two or more mitigations attempted without improvement.
- The withdrawal flow specifically is failing: users unable to reach their money
  escalates faster than users unable to deposit.

Escalate to the repository maintainers (CODEOWNERS). For suspected divergence,
escalate immediately rather than continuing to investigate alone — the
reconciliation window matters.

---

## Recovery verification

The alert clearing is **not** sufficient. The short confirmation window means it
resolves within minutes of the failures stopping, which is by design but says
nothing about whether the underlying issue is fixed.

Verify all of:

1. `flow:success:error_ratio_rate5m` below threshold for **15 minutes**.
2. Attempts are actually occurring — a ratio of zero with zero attempts is not
   recovery.
3. Synthetic probes passing:
   ```promql
   nester_probe_success{probe=~"deposit|withdrawal"}
   ```
   These exercise the path independently of user traffic, so they confirm
   recovery even during a quiet period.
4. If Cause B: reconciliation complete, no divergence remaining.
5. `budget remaining` on the dashboard noted for the incident record.

---

## Follow-up

**Postmortem required** for any of:

- Fast-burn alert firing (page severity).
- Any chain/database divergence, regardless of duration.
- More than 10% of the 28-day budget consumed in one incident.

The postmortem covers cause, detection time, mitigation time, budget consumed,
and whether the runbook led to the cause. **If the runbook did not help, fix it
in the same PR as the postmortem** — a runbook is most accurate immediately
after someone has used it under pressure.

If the budget is now in warning or exhausted, apply
[the error-budget policy](../error-budget-policy.md).

Record whether an alert fired *before* users reported the problem. Detection
lagging user reports means the SLI or its threshold needs attention at the next
[SLO review](../slo-review.md).
