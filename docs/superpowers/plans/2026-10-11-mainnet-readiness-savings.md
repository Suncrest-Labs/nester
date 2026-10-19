# Mainnet Readiness (Savings Layer) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the Stage 1 savings layer (vaults, shares, allocation, indexer, dapp) to Stellar mainnet with guarded limits, audited contracts and a rehearsed rollback.

**Architecture:** No new product features. Work is (a) make deployment, config and ownership safe for mainnet, (b) verify the protocol adapters against real mainnet protocols, (c) close audit and security gates, (d) launch with caps and a pause drill. Every mainnet switch fails closed: a misconfigured network must refuse to start, not fall back to testnet or a placeholder.

**Tech Stack:** Rust/Soroban (`packages/contracts`), Go + Chi + Postgres (`apps/api`), Next.js 16 (`apps/dapp/frontend`), stellar-cli, GitHub Actions.

**Spec:** `docs/launch-readiness-register.md` (authoritative status), `docs/LAUNCH_GO_NO_GO.md` (gate checklist). **Out of scope by decision (2026-10-11):** fiat offramp (settlements, Paystack/Flutterwave payout, KYC, settlement security fixes) and the Prometheus AI layer.

## Launch blockers found while executing (2026-10-12)

1. **The vault contract is still too large to deploy to mainnet.** Mainnet's `contract_max_size_bytes` is 131,072 (read live with `stellar network settings`). Optimized, `vault_contract` was 169,061 bytes (168,157 on dev). This branch converts `///` doc comments to `//` (soroban embeds doc comments in the contract spec, 44 KB of it), which brings it to **150,037 bytes: still 18,965 over**. Measured, not guessed:
   - extra `wasm-opt` passes (`-Oz --converge`): about -3.5 KB, needs a non-standard toolchain.
   - feature-gating the 20 unreferenced read-only getters: -5.1 KB (about 257 bytes per entrypoint).
   - the 126 exported entrypoints, not any one function, are the cost; code is a flat spread over ~1,400 functions. The remaining unreferenced entrypoints are mostly safety controls (guardian halt, breaker recovery, emergency-withdraw queue), which I did not gate to reach a number.
   Getting under the limit needs a structural change: split the vault (for example move admin/config and the emergency queue into a companion contract) or cut features. That is an architecture and audit-scope decision. `scripts/check-wasm-size.sh` fails the deploy before anything is spent.
2. **The vault test `test_harvest_rate_limit_is_per_user` fails on clean dev.** Fix or explain before tagging an audit candidate.
3. **`soroban-rpc.mainnet.stellar.org` does not exist.** Fixed here for the deploy script, API example and dapp default; production needs a provider endpoint.

## Global Constraints

- Mainnet passphrase is exactly `Public Global Stellar Network ; September 2015`.
- Mainnet RPC: SDF runs no public mainnet Soroban RPC (`soroban-rpc.mainnet.stellar.org` does not resolve). Use a provider endpoint (`https://mainnet.sorobanrpc.com` is a working public node, checked 2026-10-12); Horizon `https://horizon.stellar.org`.
- No secret key is ever committed, logged, or passed on a command line that lands in shell history; deployer uses a key alias or hardware-backed identity.
- Contract admin on mainnet is a multisig/timelock, never the deployer account.
- High/critical audit findings block the launch tag (per `docs/launch-readiness-process.md`).
- Run `make fmt-check`, clippy, `go vet`, lint and the full test suites locally before every push.

## Review Focus

- Frontend loaded with `NEXT_PUBLIC_NETWORK=mainnet` but a placeholder contract ID: must refuse to build a transaction, not send to a bogus address.
- API started with mainnet passphrase but testnet Horizon/RPC URL (or the reverse): must refuse to start.
- Soroban RPC only retains recent events: an indexer cursor older than the retention window must not silently skip events.
- Deposit into a vault that is at its cap, or while paused: user sees an honest reason, contract rejects, no partial state.
- Admin function called by the old deployer key after ownership moves to the multisig: must revert.

---

## Phase A: Verify before building (Task 1)

### Task 1: Re-verify the readiness register against the code

The register is dated 2026-08-27 and is already stale: `preview_withdraw_net`, the impairment test, error boundaries, offline banner, loading states, Playwright e2e/a11y specs, WebSocket reconnect and the deposit quote (`useDepositPreview`) all exist now.

**Files:**
- Modify: `docs/launch-readiness-register.md`
- Modify: `docs/launch-readiness-evidence.json`

- [x] **Step 1:** For each Open row outside offramp/AI, find evidence with grep and record it. Run:
  `grep -n "Status:\*\* Open" -B2 docs/launch-readiness-register.md`
- [x] **Step 2:** Run the suites and capture results as evidence: `make test`, `make go-test-short`, `cd apps/dapp/frontend && npx vitest run && npx playwright test --list`.
- [x] **Step 3:** Flip rows to Resolved only with a `test:`/`file:` evidence line (register rule). Leave honest Open for anything without evidence; update the Quick Summary counts.
- [x] **Step 4:** Commit: `git commit -m "docs: re-verify launch readiness register against code"`

> **Status (2026-10-12):** DONE for the rows in scope. 8 rows moved to Resolved with evidence (SC-11, SC-12, DAPP-19 to DAPP-24); the summary counts were adjusted by that delta only, since the other rows were not re-checked. Suites run today: treasury 38/38, adapter_blend 16/16, vault 189 pass with 1 pre-existing failure (`test_harvest_rate_limit_is_per_user`, fails on clean dev), api config/stellar/signing ok, dapp 366 pass with 2 files that cannot load on baseline. `make go-test-short` and the full cargo workspace were not run end to end.

---

## Phase B: Contracts

### Task 2: Mainnet deploy script

**Files:**
- Create: `packages/contracts/scripts/deploy-mainnet.sh`
- Create: `packages/contracts/scripts/deployed-mainnet.env` (generated, committed after deploy so verification is public)

**Interfaces:**
- Produces: `deployed-mainnet.env` with the same variable names as `deployed-testnet.env`, plus `NEXT_PUBLIC_MAINNET_*` aliases consumed by `lib/networks.ts` (`NEXT_PUBLIC_MAINNET_VAULT_CONTRACT`, `..._VAULT_TOKEN_CONTRACT`, `..._YIELD_REGISTRY_CONTRACT`, `..._ALLOCATION_STRATEGY_CONTRACT`, `..._TREASURY_CONTRACT`, `..._ACCESS_CONTROL_CONTRACT`).

- [x] **Step 1: Copy and harden.** Start from `deploy-testnet.sh`. Replace the config block:

```bash
NETWORK="mainnet"
RPC_URL="https://soroban-rpc.mainnet.stellar.org"
NETWORK_PASSPHRASE="Public Global Stellar Network ; September 2015"
HORIZON_URL="https://horizon.stellar.org"
USDC_CONTRACT="${USDC_CONTRACT:?set mainnet USDC SAC id (Circle issuer)}"
ADMIN_ADDRESS="${ADMIN_ADDRESS:?set multisig/timelock address, never the deployer}"
OUTPUT_FILE="./scripts/deployed-mainnet.env"
```

- [x] **Step 2: Add guards** before any deploy: refuse secret-key argv (alias only), refuse if `ADMIN_ADDRESS` equals the deployer's address, require typing `DEPLOY MAINNET`, and support `--dry-run` that prints every command without executing.

```bash
if [ -n "${1:-}" ] && [[ "$1" == S* ]]; then echo "pass a key alias via DEPLOYER_KEY, not a secret"; exit 1; fi
DEPLOYER_ADDR="$(stellar keys address "$DEPLOYER_KEY")"
[ "$DEPLOYER_ADDR" != "$ADMIN_ADDRESS" ] || { echo "admin must differ from deployer"; exit 1; }
read -r -p "Type DEPLOY MAINNET to continue: " ok; [ "$ok" = "DEPLOY MAINNET" ] || exit 1
```

- [x] **Step 3: Verify the script without spending money.** `bash -n scripts/deploy-mainnet.sh`, `shellcheck scripts/deploy-mainnet.sh`, then `DEPLOYER_KEY=x ADMIN_ADDRESS=G... USDC_CONTRACT=C... bash scripts/deploy-mainnet.sh --dry-run`. Expected: full command list, no network calls, exit 0; and exit 1 when admin equals deployer.
- [x] **Step 4:** Commit: `git commit -m "feat(contracts): add guarded mainnet deploy script"`

> **Status (2026-10-11):** DONE. `scripts/deploy-mainnet.sh` + `scripts/test-deploy-mainnet.sh` (11 checks green). Fixed vs testnet script: Nester `initialize` takes a single `--config` struct (testnet script still passes flat flags and is stale). Sets cap + allowlist gate, proposes admin transfer; the multisig must still run `accept_admin` (printed at the end).

### Task 3: Admin ownership to multisig + timelock

**Files:**
- Modify: `packages/contracts/scripts/deploy-mainnet.sh`
- Test: `packages/contracts/contracts/vault/src/test.rs`, `packages/contracts/contracts/timelock/src/test.rs`
- Reference: `packages/contracts/AUTH_AUDIT.md`

- [x] **Step 1: Discovery.** List every admin-gated entrypoint and how admin is set: `grep -rn "require_auth\|fn set_admin\|fn transfer_admin\|fn initialize" packages/contracts/contracts/*/src/lib.rs`. Record in the PR which contracts can change admin and whether a two-step transfer exists.
- [x] **Step 2: Write the failing test** (adapt names to the entrypoints found in Step 1): after admin is set to a second address, a call authorised only by the original deployer must panic with the typed `Unauthorized` error.
- [x] **Step 3:** Run it, confirm it fails if ownership transfer is missing, implement the minimal transfer (two-step: propose then accept) where absent.
- [x] **Step 4:** In `deploy-mainnet.sh`, pass `ADMIN_ADDRESS` as admin at `initialize` for every contract, and route sensitive changes through the `timelock` contract. Assert post-deploy by invoking an admin read and comparing to `ADMIN_ADDRESS`; script exits non-zero on mismatch.
- [x] **Step 5:** `make test && make clippy && make fmt-check`, then commit.

> **Status (2026-10-11):** DONE. Dev already had two-step `transfer_admin`/`accept_admin` on vault, registry, allocation, nester; Treasury lacked it (deployer would have stayed fee-treasury admin forever). Added with 3 tests (treasury 38/38). Timelock routing of later changes still goes through `propose_upgrade` framework.

### Task 4: Verify the lending and pool adapters against real mainnet protocols

`adapter_lending` is "Blend-style" (230 lines) with no evidence it was exercised against real Blend. Real funds must not go to an untested integration.

**Files:**
- Test: `packages/contracts/contracts/adapter_lending/src/test.rs`, `packages/contracts/contracts/adapter_pool/src/test.rs`
- Modify: `packages/contracts/contracts/allocation_strategy/src/lib.rs` (only if needed)

- [x] **Step 1: Discovery.** Read each adapter and write down in the PR which external contract interface it calls (function names, argument types) and compare to the deployed Blend pool ABI from `@blend-capital/blend-sdk` (already a frontend dependency).
- [x] **Step 2:** Run the adapter against a Blend pool on testnet (or a mainnet fork via `stellar` RPC simulation) with a tiny amount; capture the tx hashes as evidence.
- [x] **Step 3: Decision gate.** If the adapter does not match the real interface or cannot be verified, **launch with the vault holding idle USDC only** (allocation strategy disabled, `allocation` weights 100% idle) and defer yield protocols. Record the decision in the register.
- [x] **Step 4:** Add a failing-then-passing test for the adversarial case: protocol returns less than deposited (impairment). Expected: vault share price reflects the loss, zero performance fee (matches existing `test_impairment_produces_zero_performance_fee`).
- [x] **Step 5:** Commit.

> **Status (2026-10-12):** DONE in-sandbox. `adapter_blend` already speaks the real pool interface and passes 16/16 against Blend's unmodified v1.0.0 pool WASM (`real_pool_test.rs`). Not done: a live call against a real mainnet pool; do it as a small-amount manual step at launch (Task 14). Decision gate stands: if that live check fails, launch idle-USDC-only.

### Task 5: Guarded-launch limits (deposit cap)

**Files:**
- Modify: `packages/contracts/contracts/vault/src/lib.rs`, `.../vault/src/test.rs`

- [x] **Step 1: Discovery.** `grep -n "cap\|max_deposit\|limit" packages/contracts/contracts/vault/src/lib.rs`. If a TVL or per-user cap already exists, skip to Step 4.
- [x] **Step 2: Write the failing test:** deposit that pushes total assets above the cap returns the typed `DepositCapExceeded` error and leaves balances and shares unchanged.
- [x] **Step 3:** Implement an admin-set `deposit_cap` (0 = unlimited is disallowed on mainnet; deploy script sets it) and enforce it in `deposit`.
- [x] **Step 4:** Deploy script sets an initial cap (e.g. 10,000 USDC, raised by timelock proposal as confidence grows). Frontend shows "Vault is at capacity" when the cap is hit (Task 8).
- [x] **Step 5:** Tests, commit.

> **Status (2026-10-12):** DONE. Found `set_max_deposit` only bounds a single deposit (repeat deposits bypass it) and defaults to unlimited. Added `set_tvl_cap`/`get_tvl_cap` enforced in `deposit` (4 tests); deploy script requires `MAX_DEPOSIT_STROOPS` and `TVL_CAP_STROOPS` and enables the allowlist gate. Frontend messaging is Task 11. NOTE: `test_harvest_rate_limit_is_per_user` already fails on clean dev (unrelated).

### Task 6: External audit

**Files:**
- Modify: `docs/backlog/contract-audit-followup.md`, `SECURITY.md`, `apps/website/src/app/security/page.tsx`
- Create: `docs/audits/` (report lands here)

- [ ] **Step 1:** Freeze scope: vault, vault_token, allocation_strategy, yield_registry, access_control, nester, treasury, timelock, and any adapter that ships. Tag the commit `audit-candidate-1`.
- [ ] **Step 2:** Run the pre-audit checklist in `contract-audit-followup.md` (tests, nightly property tests green, doc comments, ABI stable). Confirm `contracts-property-nightly.yml` and `contract-audit.yml` are green on the tag.
- [ ] **Step 3:** Engage the firm (owner: founder; this is a business action, not code). Timeline target from the backlog doc: Q3 2026 has slipped; set a new date and record it.
- [ ] **Step 4:** Fix all Critical/High, document Medium acceptance, retest. Publish the report, update `SECURITY.md` and the website security page status.
- [ ] **Step 5:** Mainnet deploy must use the audited commit hash; record it in `deployed-mainnet.env` header and `VERIFY.md` steps.

> **Status (2026-10-12):** Partially prepared; NOT done. Updated the audit scope doc (adapters, TVL cap, treasury admin transfer, deploy script) and recorded a blocker: `test_harvest_rate_limit_is_per_user` fails on clean dev and must be fixed or explained before an audit candidate is tagged. Engaging the firm, setting a date, tagging `audit-candidate-1`, remediation and publishing the report are yours and remain unchecked.

---

## Phase C: API and indexer

### Task 7: Network-consistency guard and mainnet config

**Files:**
- Modify: `apps/api/internal/config/config.go`
- Test: `apps/api/internal/config/config_test.go`
- Create: `apps/api/.env.mainnet.example`

**Interfaces:**
- Produces: config validation returning an error (process exits) when `STELLAR_NETWORK` and passphrase/Horizon/RPC URLs disagree.

- [x] **Step 1: Write the failing test:** table test with cases (mainnet+testnet passphrase, mainnet+testnet Horizon, testnet+mainnet RPC) each expecting a non-nil validation error, and the two consistent cases expecting nil.
- [x] **Step 2:** `cd apps/api && go test ./internal/config -run TestNetworkConsistency -v` → FAIL.
- [x] **Step 3:** Implement the check in config load (mainnet passphrase literal from Global Constraints; reject any URL containing `testnet`).
- [x] **Step 4:** Test passes; add `.env.mainnet.example` with mainnet values, `AUTO_MIGRATE=false` (migrations run in the deploy pipeline per the existing note in `.env.example`), real USDC issuer, strict `ALLOWED_ORIGINS`.
- [x] **Step 5:** `go vet ./... && go test -short ./...`, commit.

> **Status (2026-10-12):** DONE. Dev already rejected network/URL mismatches and missing registry/allocation addresses on mainnet. Added the missing piece: mainnet now also refuses to start unless `STELLAR_MAINNET_VAULT_TVL_CAP` is positive (unset/0 previously meant "no cap"). Added `apps/api/.env.mainnet.example`. Plan's `AUTO_MIGRATE` is `RUN_MIGRATIONS` in this codebase.

### Task 8: Indexer behaviour on mainnet RPC

**Files:**
- Modify: `apps/api/internal/stellar/poller.go`
- Test: `apps/api/internal/stellar/poller_test.go`

- [x] **Step 1: Write the failing test:** persisted cursor is older than the RPC's oldest available ledger. Expected: poller does not skip silently; it logs an error, increments a gap metric and halts advancing (or triggers the backfill command) rather than jumping to tip.
- [x] **Step 2:** Run → FAIL. **Step 3:** implement using the RPC's reported oldest ledger; wire the existing `cmd/backfill` as the recovery path and document in the runbook.
- [ ] **Step 4:** Soak: run the indexer against mainnet RPC read-only for 24h on staging; confirm cursor advances and `processed_events` has no duplicates.
- [x] **Step 5:** Add an alert (Prometheus rule in `deploy/observability`) for cursor lag > N ledgers. Commit.

> **Status (2026-10-12):** Steps 1-3 and 5 DONE; Step 4 (24h mainnet-RPC soak) is a staging/ops action, not code. Findings: the poller already never skips (it returns early on a fetch error and leaves the cursor alone), so the real gap was a generic repeating error. Added typed `CursorBehindRetentionError` (tests: `retention_gap_test.go`), plus runbook Cause D. Lag alerts (`IndexerLagHigh`/`IndexerLagStale`) already exist in `docker/prometheus/rules/slo_alerts.yml`, so no new rule.

### Task 9: Signer keys and secrets for mainnet

**Files:**
- Reference: `docs/security/signing-isolation.md`, `docs/security/key-rotation.md`, `apps/api/internal/signing/policy.go`
- Modify: `apps/api/internal/signing/policy_test.go`

- [ ] **Step 1:** Generate mainnet signer and JWT/cipher keys in the secret manager, not the repo; confirm `scripts/contract-audit.sh` secret scan passes.
- [x] **Step 2:** Add a policy test: with network = mainnet the signer rejects intents above the per-tx limit and to non-allowlisted contract IDs (the mainnet vault/treasury IDs from Task 2).
- [ ] **Step 3:** Rehearse rotation using `cmd/rotate_keys` on staging; record elapsed time.
- [x] **Step 4:** Commit and update the register.

---

## Phase D: Dapp

> **Status (2026-10-12):** Code side DONE with no change needed: `internal/signing/policy_test.go` already pins wrong-network, contract-allowlist, excessive-amount and replay rejection (signing suite passes). Steps 1 and 3 (generate mainnet keys in the secret manager, rehearse `cmd/rotate_keys`) are operator actions and remain unchecked. The repo secret pattern scan finds no secret-shaped strings outside tests. Also found `scripts/mainnet-deploy-dryrun.sh` (local-network rehearsal of deploy + multisig handover); reuse it for the Task 14 rehearsal.

### Task 10: Fail closed on mainnet placeholders

**Files:**
- Modify: `apps/dapp/frontend/lib/networks.ts`
- Test: `apps/dapp/frontend/lib/networks.test.ts`

**Interfaces:**
- Produces: `assertNetworkConfigured(id: 'testnet' | 'mainnet'): void` throwing when any contract ID contains `PLACEHOLDER`.

- [x] **Step 1: Write the failing test**

```ts
import { assertNetworkConfigured } from "./networks";
it("throws when mainnet still has placeholder contract ids", () => {
  expect(() => assertNetworkConfigured("mainnet")).toThrow(/PLACEHOLDER/);
});
it("does not throw for testnet", () => {
  expect(() => assertNetworkConfigured("testnet")).not.toThrow();
});
```

- [x] **Step 2:** `cd apps/dapp/frontend && npx vitest run lib/networks.test.ts` → FAIL (function missing).
- [x] **Step 3:** Implement: iterate `NETWORKS[id].contracts` values, throw if any includes `PLACEHOLDER`.
- [x] **Step 4:** Call it before building any transaction in `lib/stellar/transaction.ts` and in `NetworkProvider` when mainnet is selected; show an honest banner ("Mainnet is not available yet") instead of a broken flow. Add a test that selecting mainnet with placeholders disables Deposit.
- [x] **Step 5:** Tests pass, commit.

> **Status (2026-10-12):** DONE with a changed design. `lib/networks.ts` `contracts` (the `C_MAINNET_*_PLACEHOLDER` table) is dead config: nothing reads it. Real contract ids are one env set per build (`lib/contracts.ts`) and the Network selector only changes the passphrase, so the actual hazard is selecting a network the build is not deployed for. Added `lib/stellar/network-guard.ts` (`assertNetworkMatchesBuild`, `NetworkNotAvailableError`) and `getTransactionNetwork()`, now used by every transaction builder (deposit, withdraw, preview, vault factory). Tests: `__tests__/network-guard.test.ts` 6/6; builder test verified RED without the wiring. UI messaging for the error is Task 11.

### Task 11: Mainnet-facing UX gaps

**Files:**
- Modify: `apps/dapp/frontend/components/vault-action-modals.tsx`, `components/network/NetworkSelector.tsx`, `utils/explorer.ts`
- Test: matching `*.test.tsx`

- [ ] **Step 1:** Show the real mainnet warning for real funds (replace "tokens have no real value" banner logic when network is mainnet) with a test asserting the copy per network.
- [x] **Step 2:** Deposit modal handles `DepositCapExceeded` (Task 5) and the global pause (`503` reason) with specific messages; tests for both.
- [x] **Step 3:** Explorer links use the mainnet explorer on mainnet (assert in `utils/explorer` test).
- [x] **Step 4:** Commit.

> **Status (2026-10-12):** DONE except Step 1. Step 2: deposit and withdraw now show the contract's cap/allowlist/circuit-breaker/minimum rejections as plain text (`lib/stellar/contract-errors.ts`) and read the API's public `GET /api/v1/money-path/status`, which the dapp never called, to explain a global pause and block submit (`hooks/useMoneyPathStatus.ts`, deposit modal, withdraw modal). Step 3: nothing to change; `explorerUrl` is already per network. Step 1 (mainnet banner copy) left for you: it is product copy, not a safety control. Note the real deposit modal is `components/vault/depositModal.tsx`, not the one in `vault-action-modals.tsx`. 23 new tests; full suite 366 pass; 2 test files fail to load on baseline (`@sentry/nextjs` absent from linked node_modules).

### Task 12: Mainnet e2e smoke

**Files:**
- Reference: `apps/dapp/frontend/tests/smoke/`, `docs/smoke-test-runbook.md`, `.github/workflows/e2e-testnet-nightly.yml`

- [x] **Step 1:** Keep nightly testnet e2e green as the regression gate.
- [x] **Step 2:** Add a manual mainnet smoke section to the runbook: connect, deposit 1 USDC, check share and quote match, withdraw, check indexer and balances reconcile.
- [x] **Step 3:** Commit.

> **Status (2026-10-12):** DONE (documentation). Wrote `docs/mainnet-smoke-test.md` as a separate manual runbook (the existing `smoke-test-runbook.md` is the CI testnet one). It was checked against the code: the vault has no `has_role` entrypoint, so admin handover is verified by simulating an admin-only call; the treasury does expose `has_role`. Nightly testnet e2e (`e2e-testnet-nightly.yml`) is unchanged as the regression gate. Running it needs real funds and the deploy, so it is a launch-day action.

---

## Phase E: Security, ops and launch

### Task 13: Re-test and operations baseline

- [ ] **Step 1:** Re-run the pentest scope in `docs/security/pentest-report-v1.md` against staging with mainnet config; file findings as `security` issues; no open High/Critical.
- [ ] **Step 2:** Load baseline with mainnet config: `docs/LOAD_TESTING.md`, record results in `LOAD_TESTING_BASELINE.md`.
- [ ] **Step 3:** Alerts and SLOs (`docs/observability/slo.md`) wired to a real pager; game day from `docs/security/game-day.md`, including **pause drill** (engage/release global pause, confirm deposits and withdrawals halt independently and the UI explains it).
- [ ] **Step 4:** Commit evidence to the register.

### Task 14: Go/no-go and staged rollout

**Files:**
- Modify: `docs/LAUNCH_GO_NO_GO.md`

- [x] **Step 1:** Remove offramp-related rows from the checklist, assign a named owner and verification result to each remaining row (this is a team meeting; the file stays a template until then).
- [ ] **Step 2:** Rehearse rollback end-to-end and write down wall-clock time.
- [ ] **Step 3:** Launch sequence: deploy contracts (audited hash) → ownership to multisig → low cap → allowlist or invite-only users → monitor 48h → raise cap via timelock.
- [ ] **Step 4:** Tag release, publish `deployed-mainnet.env` and `VERIFY.md` verification so anyone can confirm deployed code matches source.

---

## Self-Review

- **Spec coverage:** every in-scope Open register theme maps to a task: contracts (2-6), API/indexer/signing (7-9), dapp (10-12), security/ops/launch (13-14). Offramp and AI are excluded by decision, including settlement security fixes (API-26..28) because they belong to the offramp.
- **Placeholders:** Tasks 3, 4 and 5 start with a discovery step because the exact entrypoint names were not read for this plan; the failing tests there must be written against the names discovered. This is the one deliberate gap.
- **Types:** `assertNetworkConfigured` (Task 10) is the only cross-task function; `DepositCapExceeded` (Task 5) is consumed by Task 11.
- **Review Focus:** placeholder IDs (Task 10), network mismatch (Task 7), indexer retention gap (Task 8), cap/pause UX (Tasks 5, 11), old-admin revert (Task 3).

> **Status (2026-10-12):** Step 1 DONE: the checklist had no offramp rows to remove; added 10 rows for this branch's work (`docs/LAUNCH_GO_NO_GO.md`). Owners and verification results stay blank on purpose: that is a team meeting. Steps 2-4 (rollback rehearsal, launch sequence, tagging and publishing verification) are launch-day actions and stay unchecked. Reuse `scripts/mainnet-deploy-dryrun.sh` for the rehearsal.
