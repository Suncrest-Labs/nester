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

## No-go criteria

- Any row above is unresolved or has no assigned owner.
- Any row's verification method has been run and failed, with no fix
  merged and re-verified since.
- A P1/P2 incident is open on a launch-critical path.

## Rollback decision procedure

Same procedure as `docs/ROLLBACK_RUNBOOK.md` in the Trident repo — this
protocol launches its dApp/API/contracts together, so a rollback call on
one component's incident should consider whether the others need to roll
back in step (e.g. a contract rollback almost certainly requires an API
rollback if the API's ABI assumptions changed).

## Stage 1 Launch Scope Freeze & Change-Control Rule (#1554)

### 1. Scope Freeze Date & Anchor Tag
- **Stage 1 Scope Freeze Date:** `2026-10-20T00:00:00Z`
- **Anchor Git Tag:** `audit-candidate-1`
- **Frozen Scope:**
  - All Soroban smart contracts under `packages/contracts/` (`vault`, `vault_token`, `yield_registry`, `blend_adapter`, `soroswap_adapter`, `aquarius_adapter`, `common`).
  - Core `/api/v1/` money-path endpoints (`deposit`, `withdraw`, `rebalance`, `settlement`) under `apps/api/`.
  - Stage 1 Mainnet vault parameters (USDC Conservative Vault, $10k Stage 1 cap, 3-of-5 multisig admin/governance threshold).

### 2. Post-`audit-candidate-1` Change-Control Rule
Once `audit-candidate-1` is tagged:
1. **No New Features:** Feature work for Stage 2+ must target post-launch feature branches and may **not** merge into the Stage 1 release branch.
2. **Permitted Changes Only:** Only the following change classes may land after `audit-candidate-1`:
   - Remediation of external/internal security audit findings (`[audit-fix]`, `AUDIT-###`).
   - Fixes for `P0` / `P1` regressions discovered during mainnet dry-run, 24h indexer soak, or game-day pause/restore drills (`[p0-fix]`, `[p1-fix]`, `INC-###`).
   - Documentation, runbook, and environment configuration updates that do not alter contract WASM bytecode.
3. **Contract Modification Gate (`packages/contracts/**`):**
   - Any commit or PR modifying files under `packages/contracts/` after `audit-candidate-1` **MUST** include:
     - A change-control marker in the commit message or PR body (`[audit-fix]`, `AUDIT-###`, `[p0-fix]`, or `[p1-fix]`).
     - `Approved-By:` trailers from at least **2 of 3** designated leads (`Contracts Lead`, `Security Lead`, `Protocol Lead`).
     - Updated deterministic WASM SHA-256 checksums verified via `scripts/verify-deployment.sh`.
   - Automated enforcement is provided by `scripts/check-contract-freeze.sh` (invocable locally and in CI via `bash scripts/check-contract-freeze.sh`).

### 3. Emergency Exception & Re-Tagging Procedure
If a contract change is approved under the rule above after `audit-candidate-1`:
1. Merge the fix with the required `[audit-fix]` / `AUDIT-###` and `Approved-By:` trailers.
2. Cut a new annotated candidate tag (`audit-candidate-2`, etc.) recording the exact diff range (`git diff audit-candidate-1..audit-candidate-2 -- packages/contracts/`) for the external auditors.
3. Re-execute the staging mainnet dry-run rehearsal (`docs/MAINNET_DEPLOY_DRYRUN_REPORT.md`) before proceeding to Stage 1 Go/No-Go sign-off.

## After this checklist is real

Once the team has reviewed this, assigned owners, and verification has
actually been run, check the completed version into the repo so it's a
record of what was actually confirmed before launch — not just a plan.
