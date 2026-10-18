# Launch go/no-go checklist

**Status: template — owners and verification not yet filled in.** Issue
#1144 asks for this to be reviewed as a team before launch with named
owners; that review is a real meeting that needs to happen with the
actual team, which this pass can't perform. What follows is the checklist
structure — every launch-blocking condition this backlog has raised,
traceable to its issue — ready for owners to be assigned and verification
to be recorded against.

## Launch-blocking checklist

| Condition | Traces to | Owner | Verification method | Status |
|---|---|---|---|---|
| Support can inspect a user's money-path state without a DB console | #1141 | | Manually look up a test user via the money-path endpoint | |
| Support triage guide covers the top 4 report types | #1142 | | Guide reviewed by someone who will actually staff support | |
| Mainnet support contact channels and escalation SLAs defined | #1144 | | Escalation tree and SLAs reviewed and published in `docs/SUPPORT_ESCALATION.md` | |
| In-app problem report attaches context and shows it before sending | #1143 | | Manually trigger a report on staging, confirm no secrets in the payload | |
| Deposit UI shows a real simulated quote, not amount==shares | #1129 (nester) | | Deposit on testnet, compare quoted vs. actual shares received | |
| Contract tests exist for at least one core API response shape | #1130 (nester) | | `contracts.test.ts` passes in CI | |
| Every admin-gated contract entrypoint requires the caller's own signature | #1132 (nester) | | See `packages/contracts/AUTH_AUDIT.md` | |
| Deployed testnet contracts are verifiable against source | #1131 (nester) | | Follow `packages/contracts/scripts/VERIFY.md` against a real deployment | |
| Trace sampling won't drop the one failing request that matters | #457 (Trident) | | Trigger a deliberate error on staging, confirm its trace exports | |
| `/v1/` API contract is frozen with a stated versioning policy | #458 (Trident) | | `docs/API_STABILITY.md` (Trident repo) reviewed and published | |
| Full pre-launch verification checklist executed | #459 (Trident) | | `docs/LAUNCH_CHECKLIST.md` (Trident repo), all rows Pass | |
| Rollback rehearsed end-to-end with wall-clock time recorded | #460 (Trident) | | `docs/ROLLBACK_RUNBOOK.md` (Trident repo) rehearsal section completed | |
| Staged deposit cap rollout table with numeric gates defined | #1568 (nester) | `@0xDeon` (Protocol Lead) | Stage 1 cap (`10,000 USDC`) verified on-chain via `get_deposit_cap` before enabling public deposits | Ready |

## Staged deposit cap rollout plan (#1568)

Mainnet launches with a strict initial deposit cap that is raised only through the 48-hour timelock and 2-of-3 admin multisig after every numeric gate for the preceding stage is satisfied. No cap raise may bypass a stage or proceed while any numeric gate is unmet.

### Numeric gate table

| Stage | Target Vault Deposit Cap (USDC) | Min Days at Prior Cap | Max Reconciliation Drift Alerts | Open Critical / High Findings | Public Bug Bounty Status | Min Successful Withdrawals (% of Cumulative Deposits) | Additional Operational Gates | Decision Owner | Required Evidence Before Proposing Timelock Raise |
|---|---|---|---|---|---|---|---|---|---|
| **Stage 1 (Launch)** | **10,000 USDC** (`100_000_000_000` stroops) | 0 days (initial launch) | `0` | `0` | Live & funded | N/A (pre-deposit smoke withdrawal verified) | Mainnet deploy dry-run passed; pause drill < 15m; 24h indexer soak passed | `@0xDeon` (Protocol Lead) + Security Lead | Signed launch checklist (`docs/LAUNCH_CHECKLIST.md`), on-chain `get_deposit_cap` output, bug bounty URL |
| **Stage 2 (Early Live)** | **50,000 USDC** (`500_000_000_000` stroops) | **>= 7 days** at 10k USDC cap | `0` alerts over trailing 7d | `0` open Critical/High | Live (`>= 7d` continuous) | **>= 10%** of cumulative deposits withdrawn cleanly across `>= 5` distinct wallets | 0 unrecovered RPC/indexer gaps; API `/readyz` SLO >= 99.9% over 7d | `@0xDeon` (Protocol Lead) + Security Lead | 7-day Prometheus/Grafana reconciliation & SLO snapshot, list of >= 5 completed mainnet withdrawal tx hashes, security sign-off comment |
| **Stage 3 (Expansion)** | **250,000 USDC** (`2_500_000_000_000` stroops) | **>= 14 days** at 50k USDC cap | `0` alerts over trailing 14d | `0` open Critical/High | Live (`>= 21d` continuous) | **>= 15%** of cumulative deposits withdrawn cleanly across `>= 15` distinct wallets | At least 1 full harvest & rebalance cycle settled on-chain with 0 share-price rounding drift | `@0xDeon` (Protocol Lead) + Security Lead | 14-day reconciliation report, harvest/rebalance audit log, fee & share-price verification diff, dual maintainer review |
| **Stage 4 (Scale)** | **1,000,000 USDC** (`10_000_000_000_000` stroops) | **>= 30 days** at 250k USDC cap | `0` alerts over trailing 30d | `0` open Critical/High (incl. post-launch bounty submissions) | Live (`>= 51d` continuous, tier limits reviewed) | **>= 20%** of cumulative deposits withdrawn cleanly across `>= 30` distinct wallets | Quarterly key-rotation & backup-restore drill current (< 30d old); TVL anomaly detector active with 0 false-negative misses | `@0xDeon` (Protocol Lead) + Security Lead | 30-day SLO/reconciliation dossier, external audit + bounty status attestation, timelock proposal PR approved by both maintainers |

### Cap-raise execution rules

1. **Evidence PR before timelock proposal**: For each stage transition (Stage 1 -> 2, 2 -> 3, 3 -> 4), the Decision Owner opens a PR updating `docs/LAUNCH_GO_NO_GO.md` with links to the required evidence artifacts and obtains review approval from both maintainers.
2. **Timelock + Multisig enforcement**: Once the evidence PR is merged, the cap update is queued on-chain via the 48-hour timelock (`propose_set_deposit_cap`) and executed by the N-of-M admin multisig (`docs/security/admin-multisig.md`).
3. **Automatic hold / rollback trigger**: If any reconciliation drift alert (`ReconciliationDriftDetected`), P1/P2 incident, or High/Critical vulnerability occurs during the observation window or the 48-hour timelock delay, the pending cap raise is cancelled immediately and the minimum days counter resets once the root cause fix is deployed.

## No-go criteria

- Any row above is unresolved or has no assigned owner.
- Any row's verification method has been run and failed, with no fix
  merged and re-verified since.
- A P1/P2 incident is open on a launch-critical path.
- Any numeric gate in the Staged deposit cap rollout table is unmet for the target stage.

## Rollback decision procedure

Same procedure as `docs/ROLLBACK_RUNBOOK.md` in the Trident repo — this
protocol launches its dApp/API/contracts together, so a rollback call on
one component's incident should consider whether the others need to roll
back in step (e.g. a contract rollback almost certainly requires an API
rollback if the API's ABI assumptions changed).

## After this checklist is real

Once the team has reviewed this, assigned owners, and verification has
actually been run, check the completed version into the repo so it's a
record of what was actually confirmed before launch — not just a plan.

