# Funded Bug Bounty Program (issue #1536)

A bounty is only credible if a researcher can answer three questions in under a
minute: what is in scope, what will I be paid, and when. This document is the
program of record for all three. The public summary is at
[`/security#bug-bounty`](https://nester.finance/security#bug-bounty).

**Status: self-hosted and published.** Nester is not listed on Immunefi — a
listing requires an approved program and a funded escrow account, neither of
which exists today. The program below is self-hosted, pays in USDC on Stellar,
and is binding from the date this document merged. See [§7](#7-launch-gate) for
the two items that must be true before mainnet deposits open.

## 1. Scope

### 1.1 Smart contracts (launch set)

Every Soroban contract in the launch deployment under
`packages/contracts/contracts/`:

| Contract | Why it is in scope |
| --- | --- |
| `vault` | Holds user principal: deposit, withdraw, share pricing, breaker |
| `vault_factory`, `vault_token` | Deploys vaults; share supply and transfer accounting |
| `allocation_strategy`, `yield_registry` | Where funds are deployed, and the attested APY/TVL that drives it |
| `nester`, `treasury`, `timelock`, `access_control` | Protocol config, fees, the upgrade delay, and the shared role model |
| `adapter_blend`, `adapter_lending`, `adapter_pool`, `adapter_soroswap` | Move vault funds into external protocols |
| `recurring_deposit`, `savings_goal`, `goal_rewards`, `referral`, `rent_escrow`, `lp_aggregator` | Launch feature contracts that custody or route user funds |

The deployed WASM hashes and the upgrade path that installs them are in scope
too — a contract is only as safe as the code the timelock accepts.

### 1.2 API money path

- **Deposits and withdrawals** — `POST /api/v1/vaults/{id}/deposit`,
  `.../withdraw`, `.../emergency-withdraw`, `POST /api/v1/transactions`, and the
  preview/convert endpoints that compute the amounts those calls submit.
- **Settlement and indexing** — on-chain confirmation before credit
  (`confirmed_at`), reorg handling, event indexer and reconciliation. Crediting
  a deposit that never settled is Critical, not a bug.
- **Auth and authorization** — `POST /api/v1/auth/*` (SEP-53 challenge-response,
  JWT issuance/refresh), wallet binding, and every role check on
  `/api/v1/admin/*`.
- **Admin money path** — global and per-vault pause switches, vault
  pause/unpause, allocations, backfill, `sync-events`.
- **Signer service** — `apps/api/cmd/signer`, its operation/amount policy and
  socket authorization (see [`signing-isolation.md`](./signing-isolation.md)).
- **Webhooks** — signature verification and the SSRF guard on user-supplied URLs.

### 1.3 Out of scope

Testnet-only deployments; bugs in third-party protocols Nester integrates with
(report those upstream — a bug in *our integration* is in scope); findings
already documented in `SECURITY.md`, `docs/security/`, or waived in
`.vulnignore`; findings from the commissioned audit unless the fix is
incomplete; UI/copy with no fund or data impact; self-XSS; social engineering;
physical access; volumetric DoS; header nits with no demonstrated impact.

## 2. Severity and rewards

Rewards scale with **funds at risk (FAR)**: the maximum USD value an attacker
could move, redirect, or permanently freeze, measured at the report timestamp.
If a proof of concept only demonstrates part of the impact, FAR is the
demonstrated amount, not the theoretical maximum.

| Severity | Qualifying impact | Reward | Ceiling |
| --- | --- | --- | --- |
| **Critical** | FAR ≥ $250,000 — principal loss or permanent freeze, minting unbacked shares, bypassing withdrawal authorization | 10% of FAR, floor **$25,000** | 10% of TVL, hard max **$150,000** |
| **High** | FAR $50,000–$249,999 — role/admin bypass reaching funds, or a DoS that locks withdrawals with no exit | 5% of FAR, floor **$10,000** | 10% of TVL, hard max **$75,000** |
| **Medium** | FAR $5,000–$49,999 — limited data exposure, partial auth weakness, non-critical DoS | greater of **$2,000** or 2% of FAR | 10% of TVL, hard max **$15,000** |
| **Low** | FAR < $5,000 — information disclosure, best practice with no fund path | **$500** flat | $500 |

- **The cap is 10% of TVL.** No single payout exceeds 10% of total value locked
  at the report timestamp, whatever the tier formula produces.
- **TVL** is the attested on-chain TVL across Nester vaults at the report
  timestamp, cross-checked against on-chain balances. A stale or manipulated
  attestation does not move the cap.
- Rewards are rounded to the nearest $500, paid in USDC on Stellar, and paid
  **per root cause**, not per instance.
- Severity is ours to set and we show the arithmetic: every triage decision
  states the FAR figure and the tier calculation, and a downgrade can be
  escalated to the Security Council.
- A fund-loss path that needs a role Nester never grants is scored on the
  reachable path, not the hypothetical one.

## 3. Earmarked payout pool

| Item | Value |
| --- | --- |
| Pool target | **$150,000 USDC** — equal to the Critical hard cap, so the worst-case single report is payable in full |
| Custody | Dedicated Stellar account controlled by the treasury multisig (3-of-5, see [`multisig-signers.md`](./multisig-signers.md)) |
| Segregation | Ring-fenced from operating funds; pays bounty rewards and nothing else |
| Accounting | Report id in the transaction memo; payout ledger reconciled monthly |
| Replenishment | Back to target within 10 business days of any payout |
| Growth | If 10% of TVL exceeds the pool when a valid Critical report lands, the treasury funds the difference within 10 business days — the reward is not reduced to fit the pool |

The pool address and its balance proof are published on `/security` before
mainnet deposits open, so the earmark is verifiable without trusting this page:

```bash
curl -s "https://horizon.stellar.org/accounts/<POOL_ACCOUNT>" \
  | jq -r '.balances[] | select(.asset_code=="USDC") | .balance'
```

## 4. Triage owner and SLA

| Role | Who |
| --- | --- |
| **Triage owner** | Security Lead |
| Backup (Security Lead unavailable > 12h) | Engineering on-call engineer |
| Escalation / severity disputes | Security Council lead |

| Stage | Target |
| --- | --- |
| Acknowledgment | **24 hours**, 7 days a week (Critical: **4 hours**) |
| Triage: validity, severity, FAR, reward band | **3 business days** |
| Fix deployed | Critical 72 hours · High 14 days · Medium 30 days · Low 90 days |
| Reward paid | **10 business days** after the fix is verified on mainnet |
| Public disclosure | 90 days after the report, or once the fix is deployed, whichever is earlier |

Accepted Critical and High reports are paid **50% at triage acceptance** and 50%
on verified fix. If we miss the fix target, the remaining half is paid at the
target date regardless — a missed SLA costs us, not the reporter.

## 5. Reporting and payment

Report through the channels in
[`SECURITY.md`](../../SECURITY.md#reporting-a-vulnerability): GitHub Private
Vulnerability Reporting (preferred) or **security@nester.dev** with the subject
`[BOUNTY] <description>`, including the component, impact, reproduction steps
and a proof of concept.

**Do not test against mainnet with real user funds.** Reproduce on testnet or a
local build; if a mainnet-only condition is unavoidable, stop and ask — we will
set up a funded test vault and, where needed, a temporary pause window.

Payouts go to an address the researcher controls and are screened against
sanctions lists; no identity verification is required at or below $10,000.
Researchers must be legally able to receive the reward and must not be current
Nester employees, contractors, or auditors of the affected code. Good-faith
research is covered by the safe harbor in
[`SECURITY.md`](../../SECURITY.md#safe-harbor); extortion ends the engagement.

## 6. Program changes

Terms are versioned in this file; a report is assessed under the terms in force
when it was submitted. Tiers, the TVL cap and the pool target change only with
30 days' public notice — the cap cannot be lowered to fit an open report.

## 7. Launch gate

Mainnet deposits must not open until all four are true:

- [x] Program terms published here and summarised at `/security#bug-bounty`.
- [x] Triage owner and SLA documented (§4).
- [ ] Pool account funded at the target (§3) and its address published on `/security`.
- [ ] Balance proof recorded in the launch readiness evidence.

Items 3 and 4 need the treasury multisig to move real funds; they are tracked as
launch gates, not claimed here.
