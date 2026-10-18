# Upgrade Policy

This document defines the upgradeability posture, timelock parameters, authorization model, and architectural rationale for all contracts and modules in the Nester protocol.

## Protocol Upgrade Matrix

The following table lists every deployable contract, adapter, and common module across the Nester workspace, detailing its upgradeability classification, minimum delay, and architectural justification:

| Contract / Component | Path | Classification | Minimum Timelock Delay | Role / Mechanism | Architectural Justification |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **`vault`** | `contracts/vault` | Upgradeable | 48 hours (172,800s) | `Role::Upgrader` (`nester_common::Upgrade`) | Core custodial contract holding user collateral and managing yield strategies. 48h timelock gives depositors sufficient time to exit if they disagree with code changes. |
| **`treasury`** | `contracts/treasury` | Upgradeable | 7 days (604,800s) | `Role::Upgrader` (`nester_common::Upgrade`) | Protocol fee receiver and reserve vault. Governs long-term protocol assets; 7-day timelock reflects the higher blast radius of treasury logic alterations. |
| **`yield_registry`** | `contracts/yield_registry` | Upgradeable | 48 hours (172,800s) | `Role::Upgrader` (`nester_common::Upgrade`) | Registry of yield sources, oracle integrations, and adapter mappings. Upgradeable to onboard new protocol integrations and adjust valuation methods. |
| **`allocation_strategy`** | `contracts/allocation_strategy` | Upgradeable | 48 hours (172,800s) | `Role::Upgrader` (`nester_common::Upgrade`) | Computes portfolio asset allocations across yield venues. Upgradeable to refine risk weighting algorithms and rebalance criteria. |
| **`recurring_deposit`** | `contracts/recurring_deposit` | Upgradeable | 48 hours (172,800s) | `Role::Upgrader` (`nester_common::Upgrade`) | Automated recurring deposit executor. Upgradeable to patch automation hooks and scheduler integrations. |
| **`vault_token`** | `contracts/vault_token` | Upgradeable | 48 hours (172,800s) | `Role::Upgrader` (`nester_common::Upgrade`) | SEP-41 share token representing depositor ownership in vaults. Equipped with `nester_common::Upgrade` to allow bug fixes and ERC-4626/SEP-41 spec evolutions without requiring costly state migrations of all share holders. |
| **`vault_factory`** | `contracts/vault_factory` | Governed Registry / Immutable Factory | N/A (Factory is Immutable; Child vaults governed via 48h timelock) | Timelocked WASM Hash Pointer | The factory contract itself is deliberately immutable to prevent rug-pull vectors on factory address registries. New vault deployments update the `VaultWasmHash` via timelock proposals (`propose_wasm_hash` / `apply_wasm_hash`), affecting only newly deployed vaults. |
| **`adapter_blend`** | `contracts/adapter_blend` | Deliberately Immutable | None (Immutable) | Address swap via `yield_registry` | Stateless protocol integration adapter. Holds no user funds or persistent balances. New versions are deployed as distinct contracts and swapped into `yield_registry` without timelocking individual adapter WASM updates. |
| **`adapter_soroswap`** | `contracts/adapter_soroswap` | Deliberately Immutable | None (Immutable) | Address swap via `yield_registry` | Stateless DEX swap adapter. Holds zero user assets; immutability guarantees deterministic execution. Replaced by updating adapter registrations in registry/vault. |
| **`adapter_lending`** | `contracts/adapter_lending` | Deliberately Immutable | None (Immutable) | Address swap via `yield_registry` | Stateless lending market adapter. Immutable to prevent unexpected changes to asset routing; superseded via address swap in registry. |
| **`adapter_pool`** | `contracts/adapter_pool` | Deliberately Immutable | None (Immutable) | Address swap via `yield_registry` | Stateless liquidity pool adapter. Holds no deposits; replaced via configuration updates on the parent vault/registry. |
| **`savings_goal`** | `contracts/savings_goal` | Deliberately Immutable | None (Immutable) | Non-custodial metadata | Non-custodial personal goal ledger. Does not hold custody of funds (funds reside in vaults). Upgrades or feature additions are introduced via new goal factory registries. |
| **`goal_rewards`** | `contracts/goal_rewards` | Deliberately Immutable | None (Immutable) | Incentive distributor deprecation | Growth and incentive distribution module with strictly isolated budget limits. If reward rules evolve, old contracts are depleted/sunset and new reward distributors are deployed. |
| **`referral`** | `contracts/referral` | Deliberately Immutable | None (Immutable) | Referral registry deprecation | Non-custodial referral tracker. Immutable to protect community referral records; upgrades deploy an updated tracker and redirect dapp routing. |
| **`nester`** | `contracts/nester` | Deliberately Immutable | None (Immutable) | Directory / Entrypoint | Stateless top-level entrypoint and contract directory. Router configuration updates point to new child contract deployments. |
| **`lp_aggregator`** | `contracts/lp_aggregator` | Deliberately Immutable | None (Immutable) | Pure Math / Simulation | Stateless AMM liquidity simulation and routing calculator. Pure math routines without state or custody. |
| **`access_control`** | `libs/access_control` | Library crate (`rlib`) | N/A | Embedded directly into host contracts | Not a deployable on-chain contract; compiled statically as a dependency library into contracts enforcing role-based permissions. |
| **`timelock`** | `libs/timelock` | Library crate (`rlib`) | N/A | Embedded directly into host contracts | Not a deployable on-chain contract; provides reusable storage and verification primitives compiled directly into deployable contracts. |
| **`rent_escrow`** | `contracts/rent_escrow` | Placeholder | N/A | Unimplemented | Placeholder stub crate (`compile_error!`), not deployable on testnet or mainnet. |

---

## Upgrade Authorization Model

For all contracts classified as **Upgradeable**:
- **Role Isolation:** Only an account assigned `Role::Upgrader` may call `propose_upgrade` or `cancel_upgrade`. `Role::Upgrader` is distinct from `Role::Admin` — possessing `Admin` alone is rejected by `nester_common::Upgrade::propose_upgrade`.
- **Mainnet Governance:** On public testnet and mainnet deployments, `Role::Upgrader` must be held by a multi-sig or timelocked governance safe, never an individual EOA/single key.
- **Permissionless Execution:** Once the proposed `eta` has elapsed, `execute_upgrade` can be invoked permissionlessly by any caller. The security boundary is enforced strictly by the elapsed timelock delay and the exact WASM hash verification.
- **Immediate Cancellation:** An active `Role::Upgrader` can call `cancel_upgrade` at any point prior to execution if an issue or vulnerability is detected in the proposed WASM bytecode.

---

## Timelock Delays & Rejection Guarantees

1. **48-Hour Standard Minimum Delay (`172_800s`):**
   - Applies to `vault`, `vault_token`, `yield_registry`, `allocation_strategy`, `recurring_deposit`, and `nester`.
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

Refer to:
- [`vault/src/test.rs`](../../packages/contracts/contracts/vault/src/test.rs)
- [`vault_token/src/test.rs`](../../packages/contracts/contracts/vault_token/src/test.rs)
- [`treasury/src/test.rs`](../../packages/contracts/contracts/treasury/src/test.rs)
