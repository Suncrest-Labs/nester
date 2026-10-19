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
| Every contract fits mainnet's 131072-byte size limit after optimization | mainnet plan blocker 1 | | `bash packages/contracts/scripts/check-wasm-size.sh` exits 0 on a fresh optimized build (vault is currently 150037 bytes: FAILS) | |
| External contract audit complete; no open Critical/High findings | SC-14 | | Report linked from `SECURITY.md`; mainnet deploy uses the audited commit hash | |
| Mainnet contracts deployed from the audited commit and verifiable against source | mainnet plan T2 | | `deployed-mainnet.env` commit hash matches the audit; `packages/contracts/scripts/VERIFY.md` against the mainnet ids | |
| Contract admin held by the multisig, not the deployer, on every contract | mainnet plan T3 | | `accept_admin` run for all six; treasury `has_role` and a `--send=no` admin call from the deployer (see `docs/mainnet-smoke-test.md` step 0) | |
| Vault deposit cap, TVL cap and allowlist set before the first user deposit | mainnet plan T5 | | `get_max_deposit`, `get_tvl_cap` are finite; `deposit_allowlist_enabled` is true | |
| API refuses to start on a mismatched network or an unset mainnet TVL cap | mainnet plan T7 | | `go test ./internal/config` passes; staging start with a bad value exits non-zero | |
| Indexer tracked a full day on mainnet RPC with no gap and no duplicate events | mainnet plan T8 | | 24h staging soak; `processed_events` duplicate check; no `CursorBehindRetentionError` in logs | |
| Mainnet signer and JWT keys generated in the secret manager and rotation rehearsed | mainnet plan T9 | | `cmd/rotate_keys` rehearsal on staging with elapsed time recorded | |
| Pause drill: deposits and withdrawals halt independently and the dapp explains it | mainnet plan T11/T13 | | `docs/mainnet-smoke-test.md` step 4 | |
| Manual mainnet smoke test passed with real funds | mainnet plan T12 | | `docs/mainnet-smoke-test.md`, all steps pass; transaction hashes recorded | |
| Pentest re-run against mainnet config with no open High/Critical | #1118 | | Findings filed as `security` issues and closed; `docs/security/pentest-report-v1.md` updated | |

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

## After this checklist is real

Once the team has reviewed this, assigned owners, and verification has
actually been run, check the completed version into the repo so it's a
record of what was actually confirmed before launch — not just a plan.
