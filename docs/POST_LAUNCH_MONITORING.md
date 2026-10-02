# Post-Launch 30-Day Heightened-Monitoring Plan

Automated alerting is necessary but not sufficient in the first weeks of
mainnet: novel failure modes, by definition, have no alert written for them
yet. For **30 days after the mainnet launch** (Day 0 = the day the Go/No-Go
sign-off in [LAUNCH_CHECKLIST.md](LAUNCH_CHECKLIST.md) is executed and the
switch is flipped) the team runs an elevated-scrutiny period built around a
**daily manual review**.

The plan adds human review on top of existing alerting. It does not replace
it, and it does not relax any alert threshold.

## 1. Roles

| Role | Responsibility |
| --- | --- |
| **Daily reviewer** (rotating, named per day in the on-call rota) | Completes the checklist in §3 and posts the log entry in §4 |
| **Backup reviewer** | Covers if the daily reviewer is unavailable; the review is never skipped |
| **Incident commander** | Whoever is on call; owns escalation from §6 |
| **Security Council lead** | Signs off the end-of-period review (§7) |

## 2. Schedule and cadence

| Window | Cadence | Notes |
| --- | --- | --- |
| Day 0 – Day 7 | Daily review **twice** (start and end of the working day) plus a check at launch +1h, +6h, +24h | Highest risk; deployments frozen except for fixes |
| Day 8 – Day 30 | Daily review once, including weekends and holidays | Reviewer rota covers every day |
| Day 31 | End-of-period review (§7) | Decides whether to relax to the steady-state cadence |

Change freeze: during Day 0–7, only fixes for live issues ship to mainnet
contracts, API, and indexer. Every change goes through the normal review and
gets a log entry.

## 3. Daily manual review checklist

Copy this into the daily log (§4) and tick each item. "N/A" needs a reason.
Record evidence (link, query output, or screenshot) for each item, not just a tick.

### 3.1 Monitoring is itself healthy

Silence is ambiguous: "no divergences" can mean "all agrees" or "we didn't
look". Confirm the checkers are running **before** trusting their quiet.

- [ ] No alerts are firing, and none fired in the last 24h that were silenced
      or auto-resolved without a written reason.
- [ ] Liveness alerts are green: `ReconciliationStalled`,
      `ReconciliationFailing`, `BalanceReconciliationStalled`,
      `BalanceReconciliationMetricsAbsent`, `MoneyPathMetricsAbsent`
      (see [money-path-integrity](observability/runbooks/money-path-integrity.md)).
- [ ] Synthetic probes passed over the last 24h
      ([synthetic-probes](observability/runbooks/synthetic-probes.md)).
- [ ] Event indexer lag is within normal range and not growing
      ([event-indexer-replay](event-indexer-replay.md)).
- [ ] SLO / error-budget burn for the last 24h reviewed
      ([slo](observability/slo.md), [error-budget-policy](observability/error-budget-policy.md)).

### 3.2 Daily reconciliation review

- [ ] The vault-balance reconciler completed sweeps throughout the last 24h
      (about one every 5 minutes; check the last-success timestamp, not only the count).
- [ ] Review **every** row added to `reconciliation_findings` in the last
      24h, including ones already triaged. For each: cause, whether it is
      real, and who owns the fix. Findings are never auto-corrected.
- [ ] The transaction poller shows no pending submissions older than the
      expected window (`PendingSubmissionsBacklogged` quiet and the oldest
      pending age is plausible).
- [ ] Any failed or stuck deposit/withdrawal in the last 24h has been traced
      end to end and the user's funds accounted for.

### 3.3 Manual TVL sanity check

Do this **independently of our own database** so a bug in our accounting
cannot hide itself.

- [ ] For each mainnet vault, read `total_assets()` and the share price
      directly from the contract (RPC / explorer), not through the API.
- [ ] Compare against `vaults.current_balance` (raw stroops) and the TVL the
      dapp displays. They must agree; note any gap, however small.
- [ ] Check that the vault's token balance on chain covers
      `total_assets` less funds deployed to protocol adapters, and that
      deployed amounts match `get_source_allocation` per source.
- [ ] Compare 24h change in TVL against deposits minus withdrawals plus
      reported yield. A change that net flows do not explain is an anomaly.
- [ ] Share price has not decreased (outside a documented loss event) and
      has not jumped unexpectedly.
- [ ] Spot-check three random users: on-chain shares and redeemable value
      versus what the API and dapp show.
- [ ] Largest 5 depositors and withdrawers of the day look plausible; no
      single address accounts for an unusual share of volume.

### 3.4 Anomaly report review

- [ ] Review protocol TVL drop flags and the predictive deterioration scores
      ([protocol-health](protocol-health.md)); confirm any flagged protocol
      against its public data and our exposure to it.
- [ ] Review circuit-breaker state and any trips ([circuit-breakers](observability/circuit-breakers.md)),
      including near misses where a metric neared its threshold.
- [ ] Review rejected-request spikes, rate-limit hits, and abuse signals
      ([API_ABUSE_PROTECTION](API_ABUSE_PROTECTION.md)), including repeated
      `harvest` rejections for a single address.
- [ ] Review signing audit stream rejections: any rejected sign request is
      examined, not just counted ([incident-response](security/incident-response.md)).
- [ ] Review the privileged-action log: every `Admin`/`Manager` call,
      role grant, parameter change, and upgrade proposal in the last 24h
      maps to an approved change. Pending timelock entries are all expected.
- [ ] Review support tickets and user reports for the last 24h
      ([SUPPORT_TRIAGE_GUIDE](SUPPORT_TRIAGE_GUIDE.md)) for patterns the
      alerts missed.

### 3.5 Wrap-up

- [ ] Every finding has an owner and a due date, or a written reason it is
      benign.
- [ ] Log entry posted (§4). Anything unexplained is escalated (§6), not
      carried to tomorrow.

## 4. Daily log

Keep one entry per review in the team's ops channel and mirror a summary in
the [launch readiness register](launch-readiness-register.md) weekly.

```
Date / Day #:            e.g. 2026-10-14 / Day 3
Reviewer:                
Items completed:         3.1 ☐  3.2 ☐  3.3 ☐  3.4 ☐  3.5 ☐
Vault TVL (on chain):    
Vault TVL (API/DB):      
Delta and explanation:   
Reconciliation findings (new / open / resolved):  
Anomalies observed:      
Actions / owners:        
Escalated? (Y/N, link):  
```

## 5. Heightened thresholds (temporary)

For the 30 days, operate with extra caution, without changing code or alert rules:

- Treat any unexplained TVL or share-price discrepancy as an incident
  candidate rather than a ticket (§6).
- Keep deposit caps and per-transaction limits at their conservative launch
  values; raise them only after the end-of-period review.
- Any `ReconciliationDivergence` is paged and investigated the same day, even
  for tiny amounts.
- Keep the emergency pause path rehearsed and available
  ([MAINNET_LAUNCH_ROLLBACK](MAINNET_LAUNCH_ROLLBACK.md)).

## 6. Escalation

| Finding | Action |
| --- | --- |
| Unexplained on-chain vs. recorded TVL gap | Open an incident immediately; follow [money-path-incident](observability/runbooks/money-path-incident.md). Do not edit records to "fix" it. |
| Share price drop, or funds that cannot be accounted for | Page the incident commander; consider the global money-path pause ([MAINNET_LAUNCH_ROLLBACK](MAINNET_LAUNCH_ROLLBACK.md) §3). |
| Unexpected privileged action or signing rejection | Treat as a possible key compromise ([incident-response](security/incident-response.md), [multisig-signers](security/multisig-signers.md)). |
| Monitoring itself down or stale | Fix it first; until restored, treat the affected checks as unknown, not clean. |
| Protocol TVL drop flag on an adapter we use | Assess exposure; follow the deterioration action process in [protocol-health](protocol-health.md). |
| Anything else unexplained after 24h | Escalate to the Security Council lead. |

## 7. End-of-period review (Day 31)

The Security Council lead and core maintainers review the 30 daily logs and decide:

1. **Incidents and near misses** during the period, and whether new alerts
   are needed for each. Every novel failure mode found by hand should become an
   automated alert or test before the manual check is retired.
2. **Review value**: which checks found something. Keep those; drop or
   automate the ones that never did.
3. **Cadence**: move to the steady-state schedule, or extend heightened
   monitoring for a further defined period (no open-ended extension).
4. **Limits**: whether deposit caps and the change freeze can be relaxed.

Record the decision, signed off by the Security Council lead, in the launch
readiness register.
