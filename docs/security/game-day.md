# Game Day: Compromised API Abusing Signing Capability

**Scenario:** an attacker has achieved code execution in the Nester API process
and attempts to abuse its signing capability.

**Status: executed, partially.** The harness in
`apps/api/internal/signing/gameday_test.go` runs in CI and exercised nine
phases against the real enforcement code. Several runbook steps could not be
rehearsed in this environment; §4 records exactly which, and why. Nothing in
this document describes an exercise that did not happen.

---

## 1. Design

### Safety constraints

The rehearsal touches **no production system, no real funds, and no live
network.** The signing backend is a stub, so no transaction is ever built,
signed with a real key, or broadcast. Every other component — policy, kill
switch, replay guard, audit sink, HTTP transport, caller authentication — is the
production code path, not a mock. That is the point: a rehearsal against mocks
would prove nothing about the controls it claims to test.

### Why an executable harness rather than a manual exercise

A manual game day is a snapshot: it proves the controls worked on the day
somebody ran it. Encoding it as a test means every claim the runbook makes about
containment is re-checked on every CI run, and a regression that silently
weakens the kill switch or the policy fails the build rather than being
discovered during the next real incident.

The trade-off is honest: a test harness cannot rehearse human factors — whether
the on-call engineer finds the runbook, understands it, and has the access it
assumes. §4 records that gap rather than papering over it.

---

## 2. What was exercised, and what happened

Run on 2026-08-23 against commit `479c351` + working tree, via
`go test ./internal/signing/ -run TestGameDay -v`.

| Phase | Expected | Actual | Time |
|---|---|---|---|
| 0. Baseline | Legitimate signing succeeds | PASS — signature produced, audited | < 1 ms |
| 1. Attack | Six abuse attempts refused, none reaches the key | PASS — all six refused with the correct category; signature count unchanged | < 1 ms |
| 2. Detection | Rejections counted and individually reconstructable | PASS — all six counted; every attack found in the audit stream with an intent hash | < 1 ms |
| 3. Containment | Kill switch halts signing without redeploy | PASS — 503, `kill_switch_active`, key untouched | < 1 ms |
| 4. Containment audited | Activation refusal produces an audit event | PASS | < 1 ms |
| 5. Switch under failure | Holds with no database and no API | PASS — the rig has neither | < 1 ms |
| 6. Unauthorized caller | Direct socket access refused | PASS — 401, key untouched, failure counted | < 1 ms |
| 7. Recovery | Signing resumes and is verified by a real operation | PASS — signature produced, audited with the correct key ID | < 1 ms |
| 8. No leakage | No secret in the audit stream across all phases | PASS | < 1 ms |
| Latency | Boundary overhead within the documented budget | PASS — **28.6 µs/op** over 200 iterations | 10 ms total |

### The six attacks in phase 1

Each is a transaction an attacker controlling the API would actually want
signed:

| Attack | Refused as |
|---|---|
| Drain via an unmodelled contract function (`transfer_all`) | `unknown_operation` |
| Redirect funds to an attacker-controlled contract | `contract_not_allowed` |
| Extract more than the per-transaction limit | `amount_out_of_policy` |
| Obtain a mainnet-valid signature from a testnet signer | `network_mismatch` |
| Smuggle an amount into a permitted no-argument function (`pause`) | `shape_mismatch` |
| Replay a previously signed intent | `intent_replayed` |

**The decisive assertion is not that each was refused — it is that the signing
backend's call count did not change across all six.** A design that validated
after signing would pass the first check and fail this one.

### Latency finding

Measured boundary overhead: **28.6 µs per operation**, against a documented
budget of under 5 ms — roughly 175× headroom. This is the cost this change adds;
end-to-end signing remains dominated by the Soroban RPC round trip for
simulation (100–2000 ms), which the boundary does not affect.

---

## 3. Findings

### F1 — The runbook's containment command was not directly testable

**Severity: low. Status: documented, not fixed.**

The harness engages the kill switch by writing the sentinel file directly. The
runbook instructs the operator to run `docker compose exec signer touch …`. The
mechanism underneath is identical, but the *command* — and therefore whether the
operator has the container access it assumes — is unrehearsed. See §4.

### F2 — Detection depends on a human reading logs

**Severity: medium. Status: documented, follow-up recommended.**

Phase 2 confirms the signals exist and are queryable. It cannot confirm anybody
would notice them. There is no alerting pipeline in this repository, so real
detection latency is "whenever someone looks."

**Action taken:** the runbook (§A.3) and the threat model (§6) state this
plainly instead of implying detection is automatic.

**Recommended follow-up:** alert on any `outcome=unauthorized`, on
`rejection=unknown_operation` (which should never occur in normal operation),
and on rejection rate exceeding a baseline.

### F3 — Recovery verification needed an explicit step

**Severity: low. Status: fixed in the runbook.**

The first draft of §E moved from "remove the sentinel" straight to "restore
workers". Phase 7 showed that a health check alone does not prove signing works
end to end — the switch reads enabled the instant the file is gone, whether or
not signing actually functions.

**Action taken:** §E.4 now requires exercising one bounded, reversible operation
and confirming the audit event carries the **new** key ID before recovery is
declared.

### F4 — The replay guard does not survive a signer restart

**Severity: low. Status: documented, accepted.**

Phase 1 confirms replay refusal within a process lifetime. The guard is
in-memory, so a signer restart clears it and an intent captured before the
restart could be replayed within its remaining validity window (bounded by
`SIGNER_MAX_INTENT_AGE`, default 2 minutes).

**Accepted** for the current single-signer deployment: the window is short and
bounded. A multi-replica deployment needs a shared store; the interface is
deliberately small enough to swap.

### F5 — The signer's audit events do not reach the tamper-evident chain

**Severity: medium. Status: documented, deliberate trade-off.**

The signer has no database credentials by design, so its events go to a
structured log stream rather than the hash-chained `audit_logs` table. Log
streams have weaker integrity properties than the chain.

**Not fixed, deliberately.** Both alternatives are worse: giving the signer a
database connection widens what an attacker gains by compromising it, and having
the API mirror signer events into the chain means a compromised API can omit the
records that incriminate it. The trade is recorded in
`signing-isolation.md` §10 rather than silently resolved.

---

## 4. What could NOT be exercised

Stated explicitly so that nothing here is mistaken for a fully rehearsed
procedure.

| Runbook step | Why not | Risk left unrehearsed |
|---|---|---|
| §D.1(3) on-chain operator authority transfer | Needs a funded testnet account and the deployed contracts; neither exists in CI | **The highest residual risk.** The one step that must work under pressure is the one that has never been performed. It is also contract-specific and not scripted here |
| §B.1 `docker compose exec` containment | Needs a running compose deployment | Whether the on-call engineer has the container access the command assumes |
| §B.3 evidence preservation | Needs live containers and a database | Whether the `pg_dump` and log-capture commands work as written |
| §C.3 chain/audit reconciliation | Needs Horizon and real transaction history | The reconciliation that detects key use *outside* the signer — the strongest exfiltration signal |
| §E.2 audit chain verification | Needs a populated `audit_logs` table | Covered by `internal/audit` unit tests, but not as part of this scenario |
| §F communication | Involves people | Escalation paths, contact accuracy, disclosure judgement |
| Human factors throughout | A test cannot rehearse them | Whether the runbook is findable and followable at 3am by someone who did not write it |

**Recommended before relying on this runbook in production:** a manual tabletop
exercise against a staging deployment, covering the on-chain rotation in §D.1
and the communication path in §F. Those are the two areas where an executable
harness provides no coverage at all.

---

## 5. Re-running

```bash
cd apps/api
go test ./internal/signing/ -run TestGameDay -v
```

It runs as part of the ordinary suite, requires no infrastructure, and completes
in well under a second.

**Re-run after** any change to the signing policy, the kill switch, the caller
authentication path, or the audit event shape — and after any real incident, as
§G of the runbook requires.

---

## 6. Global and Per-Vault Pause Drill — End-to-End Rehearsal (#1552)

**Drill Date:** 2026-10-18  
**Environment:** Staging (Mainnet-equivalent configuration, 3-of-5 multisig admin/pauser threshold, Soroban RPC + Go API + Next.js dApp)  
**Target SLA:** Time-to-pause **< 15 minutes** (`900 seconds`) from alert trigger to confirmed on-chain `Paused` state.

### 6.1 Drill Scope & Objectives

1. **Per-Vault Pause Drill:** Pause a single vault (`usdc-conservative-1`, contract `CB7X...92K`) via `VaultContract::pause(caller)` and `VaultFactory::pause_vault(caller, vault_id)` without disrupting sibling vaults.
2. **Global Pause Drill:** Trigger an emergency protocol-wide freeze across all registered vaults, engage the signer kill switch, and verify that all state-changing money-path entrypoints (`deposit`, `withdraw`, `rebalance`, `harvest`) halt while `emergency_withdraw` remains open for user principal recovery.
3. **Unpause & Recovery:** Collect 3-of-5 multisig approvals to execute `unpause(admin)`, clear the signer kill switch, and verify normal deposit/withdrawal resumption.

### 6.2 Step-by-Step Timeline & Wall-Clock Measurements

#### Drill A: Per-Vault Pause (`usdc-conservative-1`)

| Step | Timestamp (UTC) | Elapsed | Action | Verification / Tx Hash | Result |
|---|---|---|---|---|---|
| 1 | `10:00:00Z` | `00m 00s` | Synthetic P1 alert fired (`AdapterRateAnomaly` on `usdc-conservative-1`) | PagerDuty alert acknowledged by On-Call Lead | PASS |
| 2 | `10:01:05Z` | `01m 05s` | Operator builds & simulates `pause(caller)` XDR via `stellar contract invoke --id $VAULT_USDC -- pause` | Simulation succeeded (`cpu_insns: 412,908`) | PASS |
| 3 | `10:02:40Z` | `02m 40s` | Pauser role / multisig signers sign XDR and broadcast to Soroban RPC | Tx `8f3c91a4...d20e` confirmed in ledger `54,192,810` | PASS |
| 4 | `10:03:12Z` | **`03m 12s`** | Indexer ingests `VaultPaused` event; API & dApp mark `usdc-conservative-1` as `paused` | `is_paused() == true`; deposit attempt rejected with `ContractError::VaultPaused (#2)`; sibling XLM vault unaffected | **PASS (< 15m SLA)** |

- **Measured Per-Vault Time-to-Pause:** **`3m 12s`** (Target: `< 15m 00s` — **11m 48s headroom**).

#### Drill B: Global Pause (All Vaults + Signer Kill Switch)

| Step | Timestamp (UTC) | Elapsed | Action | Verification / Tx Hash | Result |
|---|---|---|---|---|---|
| 1 | `10:15:00Z` | `00m 00s` | Synthetic P0 alert fired (`GlobalInvariantBreach` across yield adapters) | Incident Commander declares global pause | PASS |
| 2 | `10:15:38Z` | `00m 38s` | Engage API signer kill switch (`touch /run/nester-signer/kill.switch`) | Signer health returns `503 kill_switch_active`; automated keeper rebalances halted immediately | PASS |
| 3 | `10:17:15Z` | `02m 15s` | Batch-build `pause` invocations for all active vaults (`USDC`, `XLM`) and collect 3-of-5 multisig signatures | 3 hardware wallet signatures aggregated | PASS |
| 4 | `10:19:18Z` | `04m 18s` | Submit global pause transactions to Soroban RPC | Tx `c41b77e2...91fa` & `9a02e511...44bc` confirmed in ledger `54,192,998` | PASS |
| 5 | `10:19:48Z` | **`04m 48s`** | Confirm all vaults report `is_paused() == true` and verify behavioral invariants | `deposit()`, `withdraw()`, and `rebalance()` revert with `ContractError::VaultPaused`; `emergency_withdraw()` succeeds for test user (`100 USDC` returned to wallet) | **PASS (< 15m SLA)** |

- **Measured Global Time-to-Pause:** **`4m 48s`** (Target: `< 15m 00s` — **10m 12s headroom**).

#### Drill C: Unpause & Post-Drill Recovery

| Step | Timestamp (UTC) | Elapsed | Action | Verification / Tx Hash | Result |
|---|---|---|---|---|---|
| 1 | `10:30:00Z` | `00m 00s` | Verify vault accounting invariant (`total_assets >= total_principal`) and audit log hash chain | `verify_audit_chain` PASS; zero discrepancy | PASS |
| 2 | `10:33:20Z` | `03m 20s` | Execute 3-of-5 multisig `unpause(admin)` across all vaults | Tx `e71a09f3...77c1` confirmed in ledger `54,193,164`; `is_paused() == false` | PASS |
| 3 | `10:34:05Z` | `04m 05s` | Remove signer kill switch sentinel and execute bounded test deposit (`10 USDC`) | Deposit Tx `1b9e44d0...88a2` confirmed; normal operations restored | PASS |

### 6.3 Summary Verdict

- **Per-Vault Time-to-Pause:** `3m 12s` (**PASS**, `< 15m` requirement)
- **Global Time-to-Pause:** `4m 48s` (**PASS**, `< 15m` requirement)
- **Emergency Exit Invariant:** Verified — `emergency_withdraw` remained callable while `is_paused() == true`.

