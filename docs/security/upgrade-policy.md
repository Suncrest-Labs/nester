# Upgrade Policy

> **Accuracy note (issue #1488):** This document was rewritten against the actual
> contract code with file/line citations. The previous version described a uniform
> timelock-governed upgrade path for all contracts; that is not what the code does.
> Gaps are noted inline.

---

## Upgrade posture by contract

Every crate that `scripts/list_contract_packages.py` reports as deployable
(a `cdylib` under `packages/contracts/contracts/`, excluding test fixtures) is
listed here with its decision (#1475). "Upgradeable" always means the shared
`nester_common::Upgrade` module: `Role::Upgrader` proposes, the ETA must be at
least the minimum delay away, and execution is permissionless after the ETA.

| Contract | Decision | Minimum delay | Notes |
| :--- | :--- | :--- | :--- |
| `vault` | Upgradeable | 48 h (`MIN_UPGRADE_DELAY_VAULT`) | |
| `vault_token` | Upgradeable | 48 h (`MIN_UPGRADE_DELAY_VAULT_TOKEN`) | Holds every share balance, so a bug must be fixable without migrating holders. `initialize` takes an `admin` that can grant `Role::Upgrader`. The admin cannot touch balances directly, but through a timelocked upgrade (48 h) it can replace the code. |
| `treasury` | Upgradeable | 7 days (`MIN_UPGRADE_DELAY_TREASURY`) | |
| `yield_registry` | Upgradeable | 48 h (`MIN_UPGRADE_DELAY_YIELD_REGISTRY`) | |
| `allocation_strategy` | Upgradeable | 48 h (`MIN_UPGRADE_DELAY_ALLOCATION_STRATEGY`) | |
| `recurring_deposit` | Upgradeable | 48 h (`MIN_UPGRADE_DELAY_RECURRING_DEPOSIT`) | Before #1475 it skipped the Upgrader check and stored an ETA of `u64::MAX`, so no proposal could ever execute. |
| `vault_factory` | Upgradeable | 48 h (`MIN_UPGRADE_DELAY_VAULT_FACTORY`) | Separate from `propose_wasm_hash`, which only changes the WASM used for vaults created later. |
| `savings_goal` | Upgradeable | 48 h (`MIN_UPGRADE_DELAY_SAVINGS_GOAL`) | |
| `goal_rewards` | Upgradeable | 48 h (`MIN_UPGRADE_DELAY_GOAL_REWARDS`) | |
| `referral` | Upgradeable | 48 h (`MIN_UPGRADE_DELAY_REFERRAL`) | |
| `nester` | Upgradeable through `nester_common::Upgrade` | 48 h (target; not enforced until #1474) | Today `upgrade` swaps the WASM immediately on `Role::Admin` (`nester/src/lib.rs`), with no proposal or delay. Moving it onto `nester_common::Upgrade` is #1474; its propose/execute tests belong there, since those entrypoints do not exist yet. |
| `adapter_blend` | Immutable | n/a | Adapters have no admin; only the vault moves value through them. A faulty adapter is replaced by deploying a new one and pointing the source at it with `yield_registry::set_source_adapter`. The vault always calls the adapter the registry currently lists, so funds held through the old adapter have to be withdrawn through it before the switch. A bug in the old adapter's `withdraw` therefore cannot be fixed in place. |
| `adapter_lending` | Immutable | n/a | As `adapter_blend`. |
| `adapter_pool` | Immutable | n/a | As `adapter_blend`. |
| `adapter_soroswap` | Immutable | n/a | As `adapter_blend`. |
| `lp_aggregator` | Immutable | n/a | Swaps are stubs (#1476). A working version is deployed as a new contract. |
| `rent_escrow` | Not deployable | n/a | `src/lib.rs` is a `compile_error!` placeholder. |
| `access_control` | Library, not deployed on its own | n/a | No `#[contract]`; compiled into the contracts that use `AccessControl`, which upgrade with them. |
| `timelock` | Library, not deployed on its own | n/a | No `#[contract]`; compiled into `vault_factory`. |

Delay constants are defined in `libs/common/src/constants.rs`.

---

## Timelock path (every contract marked Upgradeable above except `nester`)

These contracts use the shared `nester_common::Upgrade` module
(`libs/common/src/upgrade.rs`):

1. `propose_upgrade(proposer, wasm_hash, min_delay, eta)` — requires
   `Role::Upgrader`; stores a `PendingUpgrade`; rejects `eta < now + min_delay`.
2. `cancel_upgrade(canceller)` — requires `Role::Upgrader`.
3. `execute_upgrade(caller, wasm_hash)` — permissionless after `eta`; wasm hash
   must match the proposal; calls `update_current_contract_wasm`.

Events `PROP_UPG`, `CAN_UPG`, and `EXEC_UPG` are emitted at each step
(see `EVENTS.md`), making the window publicly observable before an upgrade
executes.

---

## `nester` — instant admin upgrade (no timelock)

`nester`'s `upgrade` entry point (`nester/src/lib.rs:172`) requires only
`Role::Admin` and executes the WASM swap immediately — there is no proposal
step, no delay, and no `Role::Upgrader` distinction:

```rust
// nester/src/lib.rs:172-185
pub fn upgrade(env: Env, admin: Address, new_wasm_hash: BytesN<32>) {
    admin.require_auth();
    require_initialized(&env);
    AccessControl::require_role(&env, &admin, Role::Admin);
    // ... increments version counter, emits event, calls update_current_contract_wasm
}
```

Tests confirming Admin-only enforcement are in `nester/src/test.rs:195-215`.

**Gap:** nester does not go through the shared timelock module. This should be
addressed before mainnet if the deployment intent is to give users a withdrawal
window. Tracked in a follow-up issue.

---

## Storage safety on upgrade

`update_current_contract_wasm` swaps WASM in-place and never touches contract
storage. Existing deposits, vault token balances, and accrued fees are unaffected
by the upgrade itself. New versions are responsible for their own storage
migrations if the schema changes (`get_schema_version` / `init_schema_version`
in `libs/common/src/upgrade.rs`).

---

## Enforcement

Each upgradeable contract except `vault` and `nester` has unit tests in its
`src/test.rs` (module `upgrade`) covering: `Role::Admin` alone cannot propose,
a matured proposal executes, and an ETA shorter than the minimum delay or an
execution before the ETA is rejected. The vault is covered by
`vault/src/test.rs` (Upgrader role, minimum delay, execution before the ETA)
and `tests/integration/src/integration/upgrade_tests.rs` (a matured upgrade
executes and keeps state).
