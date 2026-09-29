# Upgrade Policy

**Decision: upgradeable, not immutable.** Every protocol contract (vault,
treasury, registry, allocation strategy, recurring deposit, nester,
vault_factory) ships with the shared timelock module
([`nester_common::Upgrade`](../../packages/contracts/libs/common/src/upgrade.rs))
rather than being deployed as fixed, non-upgradeable WASM.

## Who authorises an upgrade

- Only an address holding `Role::Upgrader` may `propose_upgrade` or
  `cancel_upgrade`. On mainnet that role is held by the **Admin multisig**
  (3-of-5), never a single key — see
  [admin-multisig.md](./admin-multisig.md).
- `execute_upgrade` is intentionally permissionless: once a proposal has
  matured, anyone can trigger it. The security boundary is the timelock and
  the `Role::Upgrader` gate on proposal, not who calls execute.

## Timelock

- Vault, registry, allocation strategy, nester, and recurring_deposit:
  minimum 48h delay (`MIN_UPGRADE_DELAY_VAULT` and siblings,
  [constants.rs](../../packages/contracts/libs/common/src/constants.rs)).
- Treasury: minimum 7 days (`MIN_UPGRADE_DELAY_TREASURY`), reflecting the
  larger blast radius of a treasury upgrade.
- `propose_upgrade` rejects any `eta` that doesn't satisfy
  `eta >= now + min_delay`; `execute_upgrade` rejects any call before `eta`
  and rejects a wasm hash that doesn't match the proposed one.

## User notification and existing balances

- `UpgradeProposed`, `UpgradeCancelled`, and `UpgradeExecuted` events are
  emitted on-chain at each step (see
  [EVENTS.md](../../packages/contracts/EVENTS.md)), so the timelock window is
  publicly observable off-chain (indexers, the dapp, alerting) before an
  upgrade takes effect.
- An upgrade swaps contract code in place (`update_current_contract_wasm`)
  and never touches or resets contract storage. Existing deposits, vault
  token balances, and accrued fees are unaffected by the upgrade itself; a
  new version is responsible for its own storage migrations if it changes
  the schema (see `get_schema_version` / `init_schema_version`).
- The 48h/7d window gives users the ability to withdraw before a proposed
  upgrade can execute, if they disagree with it.

## Enforcement

Contract behaviour matching this posture is asserted in
[`vault/src/test.rs`](../../packages/contracts/contracts/vault/src/test.rs):
holding `Role::Admin` alone is not sufficient to propose an upgrade, a delay
shorter than the minimum is rejected, and execution before the proposal's ETA
is rejected regardless of caller.
