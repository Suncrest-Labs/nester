# Upgrade Policy

> **Accuracy note (issue #1488):** This document was rewritten against the actual
> contract code with file/line citations. The previous version described a uniform
> timelock-governed upgrade path for all contracts; that is not what the code does.
> Gaps are noted inline.

---

## Upgrade posture by contract

| Contract | Upgrade mechanism | Timelock? | Evidence |
| :--- | :--- | :--- | :--- |
| `vault` | `Upgrade::propose_upgrade` / `execute_upgrade` | Yes — 48 h (`MIN_UPGRADE_DELAY_VAULT`) | `vault/src/lib.rs`, `libs/common/src/upgrade.rs` |
| `treasury` | `Upgrade::propose_upgrade` / `execute_upgrade` | Yes — 7 days (`MIN_UPGRADE_DELAY_TREASURY`) | `treasury/src/lib.rs` |
| `yield_registry` | `Upgrade::propose_upgrade` / `execute_upgrade` | Yes — 48 h (`MIN_UPGRADE_DELAY_YIELD_REGISTRY`) | `yield_registry/src/lib.rs` |
| `allocation_strategy` | `Upgrade::propose_upgrade` / `execute_upgrade` | Yes — 48 h (`MIN_UPGRADE_DELAY_ALLOCATION_STRATEGY`) | `allocation_strategy/src/lib.rs` |
| `vault_token` | `Upgrade::propose_upgrade` / `execute_upgrade` | Yes — 48 h (`MIN_UPGRADE_DELAY_VAULT_TOKEN`) | `vault_token/src/lib.rs`, `libs/common/src/upgrade.rs` |
| **`nester`** | **Instant admin `upgrade`** | **No timelock** | `nester/src/lib.rs:172` |
| **`vault_factory`** | **No upgrade function** | **N/A** | `vault_factory/src/lib.rs` (no `upgrade` entry point) |

Delay constants are defined in
`libs/common/src/constants.rs:55-64`.

---

## Timelock path (vault, treasury, registry, allocation_strategy, vault_token)

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

## `vault_factory` — no upgrade function

`vault_factory/src/lib.rs` has no `upgrade` entry point and no `Upgrade` usage.
The contract is currently not upgradeable. This may be intentional (factory
logic is minimal) but should be documented as a deliberate decision or the
missing path should be added. Tracked as a follow-up.

---

## Storage safety on upgrade

`update_current_contract_wasm` swaps WASM in-place and never touches contract
storage. Existing deposits, vault token balances, and accrued fees are unaffected
by the upgrade itself. New versions are responsible for their own storage
migrations if the schema changes (`get_schema_version` / `init_schema_version`
in `libs/common/src/upgrade.rs`).

---

## Immutability & Adapter Upgrades

Stateless adapters (`adapter_blend`, `adapter_soroswap`, `adapter_lending`, `adapter_pool`), trackers (`referral`, `savings_goal`), and math modules (`lp_aggregator`) are intentionally immutable. They hold no user funds or persistent balances. New versions are deployed as distinct contracts and swapped into `yield_registry` or configured via the parent contracts without timelocking individual adapter WASM updates.

---

## Timelock Delays & Rejection Guarantees

1. **48-Hour Standard Minimum Delay (`172_800s`):**
   - Applies to `vault`, `vault_token`, `yield_registry`, `allocation_strategy`, and `recurring_deposit`.
   - Defined in [`constants.rs`](../../packages/contracts/libs/common/src/constants.rs).
   - Any proposal specifying `eta < now + min_delay` is immediately rejected.
2. **7-Day Treasury Minimum Delay (`604_800s`):**
   - Applies to `treasury` (`MIN_UPGRADE_DELAY_TREASURY`).
   - Protects protocol reserves against hasty parameter or logic migrations.
3. **Early Execution Rejection:**
   - Any invocation of `execute_upgrade` before the ledger timestamp reaches `eta` is rejected, even if called by the proposer, upgrader, or contract admin.
4. **Bytecode Hash Integrity:**
   - `execute_upgrade` asserts that the caller supplies the exact `wasm_hash` committed in the pending proposal. Passing any other hash fails, preventing bait-and-switch bytecode substitutions.

---

## User Notification & State Preservation

- **On-Chain Audit Trail:** The lifecycle steps emit contract events:
  - `PROP_UPG`: Emitted on `propose_upgrade` with `wasm_hash`, `proposer`, and `eta`.
  - `CAN_UPG`: Emitted on `cancel_upgrade`.
  - `EXEC_UPG`: Emitted on `execute_upgrade`.
  See [`EVENTS.md`](../../packages/contracts/EVENTS.md) for event topic schemas.
- **State Invariance:** An upgrade executes Soroban host function `update_current_contract_wasm`. Contract storage keys, balances, allowances, and configurations persist unchanged.
- **Schema Migration:** Contracts support version tracking via `get_schema_version` and `init_schema_version`. A new contract revision must expose a `migrate` function if structural storage changes are required.

---

## Test Verification

Every upgradeable contract includes unit and integration tests enforcing this upgrade lifecycle:
- `propose_upgrade_rejects_caller_without_upgrader_role`
- `propose_upgrade_rejects_delay_shorter_than_minimum`
- `execute_upgrade_before_eta_is_rejected_even_for_upgrader`
- `cancel_upgrade_by_upgrader_succeeds`

Timelock behaviour is asserted in `vault/src/test.rs`, `vault_token/src/test.rs`, and `treasury/src/test.rs`: `Role::Admin` alone
cannot propose an upgrade, a delay below the minimum is rejected, and execution
before the proposal's ETA is rejected regardless of caller.
