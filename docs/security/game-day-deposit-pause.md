# Game Day: Deposit Pause & Release (End-to-End)

**Scenario:** an incident is detected that requires halting new deposits across
the protocol (or a specific vault) while keeping user withdrawals open, using
both the API money-path switches (#1322) and the on-chain deposit breaker
through the real testnet multisig signer set.

**Status: not yet executed live.** Automated tests pin each layer in isolation
(§3), but an end-to-end timed drill with a live on-call page and the real
testnet signer set has not yet been run. Nothing in this document describes a
live exercise that did not happen; §6 is the run log to fill in on the first
real drill. Do not cite this document as evidence that the 15-minute SLA has
been met until §6 contains a dated run with recorded timestamps and testnet
transaction hashes (tracked in #1552).

---

## 1. Objective and acceptance criteria

Pause controls exist at three layers — the vault contract (`guardian_halt_deposits`
/ `DepositsHalted`), the API (global and per-vault money-path switches, #1322),
and the dapp status banner. This game day measures the real wall-clock time from
incident detection to deposits being halted end-to-end, including paging on-call
and collecting multisig signatures.

### Success criteria

- **Time to pause (`T0 → T4`) is under 15 minutes**, or every bottleneck that
  pushed it over 15 minutes has a concrete follow-up issue filed in §6.
- **Deposits are halted** on-chain (`deposit` rejected) and at the API layer.
- **Withdrawals remain open** on-chain (`withdraw` succeeds) and at the API
  layer while deposits are paused.
- **Dapp banner is visible** displaying
  `"Deposits are paused. Withdrawals remain available."`
- **Clean release**: deposits are re-enabled on-chain and via the API, the dapp
  banner clears, and a post-release smoke-test deposit succeeds.

---

## 2. Prerequisites (before starting the clock)

- [ ] Testnet contracts deployed and `VAULT_ID` exported from
      `packages/contracts/scripts/deployed-testnet.env`.
- [ ] Testnet multisig / guardian signer set populated and verified in
      [`multisig-signers.md`](multisig-signers.md) (no placeholder `G…`
      entries).
- [ ] On-call rotation and paging channel active; facilitator agrees whether the
      drill is unannounced to the on-call engineer.
- [ ] Staging/testnet API running with per-vault money-path switches enabled
      (#1322) and pointing at the testnet `VAULT_ID`.
- [ ] Staging/testnet dapp reachable and connected to the staging API status
      endpoint.
- [ ] A funded testnet user account with an existing vault share balance so
      withdrawals can be verified while deposits are paused.

---

## 3. Pre-drill automated control inventory

Each individual layer is already pinned by automated tests in the repository.
These prove the controls work in isolation, not how fast humans and multisig
signers can operate them under pressure:

| Layer | Control | Automated coverage |
| --- | --- | --- |
| Contract | Deposit-only breaker (`guardian_halt_deposits`, severity `DepositsHalted`) blocks `deposit` while keeping `withdraw`, `harvest`, and `unlock_position` open | `pause_mode_matrix_matches_the_documented_table` and `prop_withdrawals_stay_open_while_only_deposits_are_halted` in `packages/contracts/contracts/vault/src/test.rs` (see [`protocol-health.md`](../protocol-health.md#pause-modes-what-still-works)) |
| API | Global `deposits_paused` switch and per-vault deposit pause (#1322) block deposits while keeping withdrawals open | `TestGlobalPauseMatrix` in `apps/api/internal/service/vault_money_path_pause_test.go`, `apps/api/internal/service/vault_scoped_money_path_pause_test.go`, `apps/api/internal/handler/admin_vault_pause_test.go` |
| Dapp | Status banner renders `"Deposits are paused. Withdrawals remain available."` with `error` severity and takes priority over RPC/indexer lag warnings | `apps/dapp/frontend/__tests__/status-banner.test.tsx` |

---

## 4. Exercise procedure (timed checkpoints)

Record UTC timestamps for every checkpoint in §5.

### T0 — Incident detection & page (start clock)

1. Facilitator triggers a simulated P1 alert to the on-call engineer (e.g.
   `"DRILL: simulated upstream protocol anomaly — halt deposits immediately, keep withdrawals open"`).
2. Record **T0** (page sent).

### T1 — On-call acknowledgment & triage

1. On-call acknowledges the page and confirms scope (deposit-only pause, **not**
   full-halt / per-vault pause, so user withdrawals remain open).
2. Record **T1** (acknowledged).

### T2 — Engage API money-path pause (#1322)

1. On-call engages the API deposit pause (global `deposits_paused` switch or
   per-vault money-path switch for the affected vault) so API-routed deposits
   stop immediately while on-chain multisig signing is in flight.
2. Confirm API returns a paused error on a test deposit request and still
   accepts a test withdrawal request.
3. Record **T2** (API deposit pause active).

### T3 — Engage on-chain deposit pause via real testnet signer set

1. On-call initiates the on-chain deposit-only pause (`guardian_halt_deposits`,
   severity `DepositsHalted`) on testnet `VAULT_ID`.
2. Collect the required signatures from the real testnet signer set and submit
   the transaction.
3. Record **T3** (on-chain transaction confirmed) and the **pause tx hash**.

### T4 — End-to-end verification (stop clock for Time to Pause)

1. **On-chain deposit blocked:** invoke `deposit` on testnet `VAULT_ID` and
   confirm it is rejected.
2. **On-chain withdrawal open:** invoke `withdraw` from the pre-funded testnet
   user account and confirm it succeeds; record the **withdrawal tx hash**.
3. **Dapp banner visible:** open the staging/testnet dapp and confirm the status
   banner displays `"Deposits are paused. Withdrawals remain available."`
4. Record **T4** (all checks verified). **Time to pause = `T4 - T0`.**

### T5 — Release pause

1. Submit the release transaction on testnet `VAULT_ID` through the required
   admin/multisig signer set to clear `DepositsHalted`; record the **release tx
   hash**.
2. Disengage the API `deposits_paused` switch.
3. Record **T5** (both layers released).

### T6 — Post-release verification

1. Confirm the dapp status banner clears.
2. Execute a small smoke-test deposit on testnet `VAULT_ID` and confirm it
   succeeds; record the **smoke-test deposit tx hash**.
3. Record **T6** (recovery verified).

---

## 5. Known friction points (pre-drill)

Friction points already visible from inspecting the repository before the first
live run:

| ID | Friction point | Impact on time to pause | Status / follow-up |
| --- | --- | --- | --- |
| F1 | [`multisig-signers.md`](multisig-signers.md) §2–§3 still contains placeholder `G…` addresses and unpopulated thresholds. | Blocks T3 until real testnet signer addresses and thresholds are documented and distributed to signers. | Open — populate before first drill. |
| F2 | Two-layer pause coordination: the dapp banner reflects the API `deposits_paused` flag, not on-chain `DepositsHalted` state directly. | Operators could mistake the banner appearing at T2 for on-chain containment before T3 lands. | Documented in T4 — verify both layers independently. |
| F3 | No checked-in helper script to assemble the `guardian_halt_deposits` XDR envelope for M-of-N signing (unlike the upgrade commands in [`testnet-upgrade-rehearsal.md`](testnet-upgrade-rehearsal.md)). | Adds manual `stellar` CLI friction during T3 under time pressure. | Open — consider adding a tested pause/release CLI script if T3 exceeds budget. |

---

## 6. Run log

*(Empty until the first live testnet drill. Append one row per run; do not
overwrite.)*

### Timing summary

| Date (UTC) | On-call | T0 (page) | T1 (ack) | T2 (API pause) | T3 (on-chain pause) | T4 (verified) | Time to pause (`T4 - T0`) | SLA (< 15m) | T5 (released) | T6 (recovered) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| — | — | — | — | — | — | — | — | *(no runs yet)* | — | — |

### Transaction hashes & verification evidence

| Date (UTC) | Network | `VAULT_ID` | Pause tx | Withdraw-while-paused tx | Dapp banner confirmed? | Release tx | Post-release deposit tx |
| --- | --- | --- | --- | --- | --- | --- | --- |
| — | testnet | — | — | — | — | — | — |

### Drill friction points & filed fixes

| Date (UTC) | Checkpoint | Friction observed | Minutes lost | Follow-up issue |
| --- | --- | --- | --- | --- |
| — | — | *(no runs yet)* | — | — |
