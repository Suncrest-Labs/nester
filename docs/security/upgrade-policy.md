# Upgrade Policy & As-Built Contract Posture

> **Audit Status (Issue [#1488](https://github.com/Suncrest-Labs/nester/issues/1488))**: Every statement in this document cites the actual implementation (`file#line`) or test in `packages/contracts`. Where a contract deviates from the shared timelock upgrade module, the exact code and tracking issue are documented in [Section 2 (Contract-by-Contract Upgrade Matrix)](#2-contract-by-contract-upgrade-matrix) and [Section 5 (Known Deviations & Open Issues)](#5-known-deviations--open-issues).

---

## 1. Shared Timelock Upgrade Module (`nester_common::Upgrade`)

The canonical upgrade mechanism lives in [`packages/contracts/libs/common/src/upgrade.rs#L33-L180`](../../packages/contracts/libs/common/src/upgrade.rs#L33-L180):

- **Proposal (`Upgrade::propose_upgrade`)** — [`upgrade.rs#L41-L76`](../../packages/contracts/libs/common/src/upgrade.rs#L41-L76):
  - Calls `proposer.require_auth()` ([`upgrade.rs#L48`](../../packages/contracts/libs/common/src/upgrade.rs#L48)). Calling entrypoints must enforce `AccessControl::require_role(&env, &admin, Role::Upgrader)` ([`upgrade.rs#L39-L40`](../../packages/contracts/libs/common/src/upgrade.rs#L39-L40)).
  - Rejects `eta < now.saturating_add(min_delay)` with `ContractError::TimelockInvalidDelay` ([`upgrade.rs#L50-L53`](../../packages/contracts/libs/common/src/upgrade.rs#L50-L53)).
  - Stores `PendingUpgrade { wasm_hash, eta, proposer }` under `UpgradeDataKey::PendingUpgrade` in instance storage ([`upgrade.rs#L55-L63`](../../packages/contracts/libs/common/src/upgrade.rs#L55-L63)) and emits `("UPGRADE", "PROP_UPG")` ([`upgrade.rs#L65-L75`](../../packages/contracts/libs/common/src/upgrade.rs#L65-L75)).
- **Cancellation (`Upgrade::cancel_upgrade`)** — [`upgrade.rs#L83-L106`](../../packages/contracts/libs/common/src/upgrade.rs#L83-L106):
  - Calls `canceller.require_auth()` ([`upgrade.rs#L84`](../../packages/contracts/libs/common/src/upgrade.rs#L84)), removes `UpgradeDataKey::PendingUpgrade` (or panics with `ContractError::NoPendingUpgrade` at [`upgrade.rs#L90`](../../packages/contracts/libs/common/src/upgrade.rs#L90)), and emits `("UPGRADE", "CAN_UPG")` ([`upgrade.rs#L96-L105`](../../packages/contracts/libs/common/src/upgrade.rs#L96-L105)).
- **Execution (`Upgrade::execute_upgrade`)** — [`upgrade.rs#L112-L148`](../../packages/contracts/libs/common/src/upgrade.rs#L112-L148):
  - Permissionless after maturity: requires `caller.require_auth()` ([`upgrade.rs#L113`](../../packages/contracts/libs/common/src/upgrade.rs#L113)), rejects `now < pending.eta` with `ContractError::UpgradeNotMatured` ([`upgrade.rs#L121-L124`](../../packages/contracts/libs/common/src/upgrade.rs#L121-L124)), and rejects `wasm_hash != pending.wasm_hash` with `ContractError::UpgradeHashMismatch` ([`upgrade.rs#L126-L128`](../../packages/contracts/libs/common/src/upgrade.rs#L126-L128)).
  - Swaps bytecode in-place via `env.deployer().update_current_contract_wasm(pending.wasm_hash.clone())` ([`upgrade.rs#L130-L131`](../../packages/contracts/libs/common/src/upgrade.rs#L130-L131)), clears `PendingUpgrade` ([`upgrade.rs#L133-L135`](../../packages/contracts/libs/common/src/upgrade.rs#L133-L135)), and emits `("UPGRADE", "EXEC_UPG")` ([`upgrade.rs#L137-L147`](../../packages/contracts/libs/common/src/upgrade.rs#L137-L147)).
- **Schema Versioning (`init_schema_version`, `get_schema_version`, `set_schema_version`)** — [`upgrade.rs#L158-L179`](../../packages/contracts/libs/common/src/upgrade.rs#L158-L179).

---

## 2. Contract-by-Contract Upgrade Matrix

### 2.1 Contracts Using `nester_common::Upgrade` with Enforced Timelock & `Role::Upgrader`

| Contract | Entrypoints (`file#line`) | Auth Check (`file#line`) | Minimum Delay Constant (`file#line`) |
| :--- | :--- | :--- | :--- |
| **`vault`** | [`contracts/vault/src/lib.rs#L5207-L5251`](../../packages/contracts/contracts/vault/src/lib.rs#L5207-L5251) | `AccessControl::require_role(&env, &admin, Role::Upgrader)` ([`vault/src/lib.rs#L5208`](../../packages/contracts/contracts/vault/src/lib.rs#L5208), [`#L5222`](../../packages/contracts/contracts/vault/src/lib.rs#L5222)) | `MIN_UPGRADE_DELAY_VAULT = 172_800` (48h) ([`libs/common/src/constants.rs#L58`](../../packages/contracts/libs/common/src/constants.rs#L58)) |
| **`treasury`** | [`contracts/treasury/src/lib.rs#L408-L452`](../../packages/contracts/contracts/treasury/src/lib.rs#L408-L452) | `AccessControl::require_role(&env, &admin, Role::Upgrader)` ([`treasury/src/lib.rs#L409`](../../packages/contracts/contracts/treasury/src/lib.rs#L409), [`#L423`](../../packages/contracts/contracts/treasury/src/lib.rs#L423)) | `MIN_UPGRADE_DELAY_TREASURY = 604_800` (7d) ([`libs/common/src/constants.rs#L64`](../../packages/contracts/libs/common/src/constants.rs#L64)) |
| **`yield_registry`** | [`contracts/yield_registry/src/lib.rs#L1340-L1384`](../../packages/contracts/contracts/yield_registry/src/lib.rs#L1340-L1384) | `AccessControl::require_role(&env, &admin, Role::Upgrader)` ([`yield_registry/src/lib.rs#L1341`](../../packages/contracts/contracts/yield_registry/src/lib.rs#L1341), [`#L1355`](../../packages/contracts/contracts/yield_registry/src/lib.rs#L1355)) | `MIN_UPGRADE_DELAY_YIELD_REGISTRY = 172_800` (48h) ([`libs/common/src/constants.rs#L60`](../../packages/contracts/libs/common/src/constants.rs#L60)) |
| **`allocation_strategy`** | [`contracts/allocation_strategy/src/lib.rs#L641-L685`](../../packages/contracts/contracts/allocation_strategy/src/lib.rs#L641-L685) | `AccessControl::require_role(&env, &admin, Role::Upgrader)` ([`allocation_strategy/src/lib.rs#L642`](../../packages/contracts/contracts/allocation_strategy/src/lib.rs#L642), [`#L656`](../../packages/contracts/contracts/allocation_strategy/src/lib.rs#L656)) | `MIN_UPGRADE_DELAY_ALLOCATION_STRATEGY = 172_800` (48h) ([`libs/common/src/constants.rs#L62`](../../packages/contracts/libs/common/src/constants.rs#L62)) |

### 2.2 Contracts Deviating from the Shared Timelock Posture (Tracked Gaps)

| Contract | Actual As-Built Behavior (`file#line`) | Gap & Tracking Issue |
| :--- | :--- | :--- |
| **`nester`** | [`contracts/nester/src/lib.rs#L172-L191`](../../packages/contracts/contracts/nester/src/lib.rs#L172-L191): exposes `pub fn upgrade(env: Env, admin: Address, new_wasm_hash: BytesN<32>)`, checks `Role::Admin` ([`#L174`](../../packages/contracts/contracts/nester/src/lib.rs#L174)), and immediately calls `env.deployer().update_current_contract_wasm(new_wasm_hash)` ([`#L190`](../../packages/contracts/contracts/nester/src/lib.rs#L190)) with **no timelock and no `Role::Upgrader` check**. | Tracked in [#1474](https://github.com/Suncrest-Labs/nester/issues/1474). |
| **`recurring_deposit`** | [`contracts/recurring_deposit/src/lib.rs#L476-L498`](../../packages/contracts/contracts/recurring_deposit/src/lib.rs#L476-L498): `propose_upgrade` checks `require_admin(&env, &admin)` ([`#L477`](../../packages/contracts/contracts/recurring_deposit/src/lib.rs#L477)) instead of `Role::Upgrader`, and calls `Upgrade::propose_upgrade(&env, &admin, new_wasm_hash, 0, u64::MAX)` ([`#L478`](../../packages/contracts/contracts/recurring_deposit/src/lib.rs#L478)) with `min_delay = 0` and hardcoded `eta = u64::MAX`. | Tracked in [#1475](https://github.com/Suncrest-Labs/nester/issues/1475) and excluded from mainnet launch scope in [#1501](https://github.com/Suncrest-Labs/nester/issues/1501). |
| **`vault_factory`** | [`contracts/vault_factory/src/lib.rs#L1-L234`](../../packages/contracts/contracts/vault_factory/src/lib.rs#L1-L234): contains **no** `Upgrade` usage and no `update_current_contract_wasm` entrypoint (currently immutable once deployed). | Tracked in [#1475](https://github.com/Suncrest-Labs/nester/issues/1475). |
| **`vault_token`**, **`access_control`**, **`timelock`**, **`governance`**, **`savings_goal`**, **`goal_rewards`**, **`referral`**, **`lp_aggregator`** | No `Upgrade` or `update_current_contract_wasm` entrypoints exist in [`vault_token/src/lib.rs`](../../packages/contracts/contracts/vault_token/src/lib.rs), [`access_control/src/lib.rs`](../../packages/contracts/contracts/access_control/src/lib.rs), [`timelock/src/lib.rs`](../../packages/contracts/contracts/timelock/src/lib.rs), [`governance/src/lib.rs`](../../packages/contracts/contracts/governance/src/lib.rs), [`savings_goal/src/lib.rs`](../../packages/contracts/contracts/savings_goal/src/lib.rs), [`goal_rewards/src/lib.rs`](../../packages/contracts/contracts/goal_rewards/src/lib.rs), [`referral/src/lib.rs`](../../packages/contracts/contracts/referral/src/lib.rs), or [`lp_aggregator/src/lib.rs`](../../packages/contracts/contracts/lp_aggregator/src/lib.rs). | Tracked in [#1475](https://github.com/Suncrest-Labs/nester/issues/1475) and [#1501](https://github.com/Suncrest-Labs/nester/issues/1501). |

---

## 3. On-Chain Events & Storage Preservation

- For contracts wired to `nester_common::Upgrade`, `("UPGRADE", "PROP_UPG")`, `("UPGRADE", "CAN_UPG")`, and `("UPGRADE", "EXEC_UPG")` events are emitted at proposal, cancellation, and execution ([`libs/common/src/upgrade.rs#L65-L75`](../../packages/contracts/libs/common/src/upgrade.rs#L65-L75), [`#L96-L105`](../../packages/contracts/libs/common/src/upgrade.rs#L96-L105), [`#L137-L147`](../../packages/contracts/libs/common/src/upgrade.rs#L137-L147); documented in [`packages/contracts/EVENTS.md`](../../packages/contracts/EVENTS.md)).
- `env.deployer().update_current_contract_wasm` ([`libs/common/src/upgrade.rs#L131`](../../packages/contracts/libs/common/src/upgrade.rs#L131)) swaps the contract bytecode in place without clearing instance or persistent storage.
- Schema version initialization (`Upgrade::init_schema_version(&env, 1)`) and `migrate(env, admin, target)` entrypoints are present in:
  - `vault` ([`contracts/vault/src/lib.rs#L2250`](../../packages/contracts/contracts/vault/src/lib.rs#L2250), [`#L5244-L5251`](../../packages/contracts/contracts/vault/src/lib.rs#L5244-L5251))
  - `treasury` ([`contracts/treasury/src/lib.rs#L86`](../../packages/contracts/contracts/treasury/src/lib.rs#L86), [`#L445-L452`](../../packages/contracts/contracts/treasury/src/lib.rs#L445-L452))
  - `yield_registry` ([`contracts/yield_registry/src/lib.rs#L257`](../../packages/contracts/contracts/yield_registry/src/lib.rs#L257), [`#L1377-L1384`](../../packages/contracts/contracts/yield_registry/src/lib.rs#L1377-L1384))
  - `allocation_strategy` ([`contracts/allocation_strategy/src/lib.rs#L155`](../../packages/contracts/contracts/allocation_strategy/src/lib.rs#L155), [`#L678-L685`](../../packages/contracts/contracts/allocation_strategy/src/lib.rs#L678-L685))

---

## 4. Test Evidence

1. **Role separation & timelock enforcement on `vault`**:
   - [`packages/contracts/contracts/vault/src/test.rs#L2940-L2947`](../../packages/contracts/contracts/vault/src/test.rs#L2940-L2947) (`propose_upgrade_rejects_caller_without_upgrader_role`): proves `Role::Admin` alone cannot propose an upgrade.
   - [`packages/contracts/contracts/vault/src/test.rs#L2950-L2963`](../../packages/contracts/contracts/vault/src/test.rs#L2950-L2963) (`propose_upgrade_rejects_delay_shorter_than_minimum`): proves proposals with `eta < now + MIN_UPGRADE_DELAY_VAULT` fail with `ContractError::TimelockInvalidDelay`.
   - [`packages/contracts/contracts/vault/src/test.rs#L2966-L3023`](../../packages/contracts/contracts/vault/src/test.rs#L2966-L3023): proves `execute_upgrade` fails before `eta` (`ContractError::UpgradeNotMatured`) and on hash mismatch (`ContractError::UpgradeHashMismatch`).
2. **Integration lifecycle tests**:
   - [`packages/contracts/tests/integration/src/integration/lifecycle_tests.rs#L329-L440`](../../packages/contracts/tests/integration/src/integration/lifecycle_tests.rs#L329-L440): tests proposal, cancellation, premature execution rejection, and hash-mismatch rejection on `vault`.
   - [`packages/contracts/tests/integration/src/integration/lifecycle_tests.rs#L442-L459`](../../packages/contracts/tests/integration/src/integration/lifecycle_tests.rs#L442-L459): tests the 7-day `MIN_UPGRADE_DELAY_TREASURY` timelock on `treasury`.
3. **Allocation strategy upgrade role check**:
   - [`packages/contracts/contracts/allocation_strategy/src/test.rs#L570-L583`](../../packages/contracts/contracts/allocation_strategy/src/test.rs#L570-L583): verifies `propose_upgrade` requires `Role::Upgrader`.

---

## 5. Known Deviations & Open Issues

- **[#1474](https://github.com/Suncrest-Labs/nester/issues/1474)**: Replace `nester::upgrade` ([`contracts/nester/src/lib.rs#L172-L191`](../../packages/contracts/contracts/nester/src/lib.rs#L172-L191)) with `propose_upgrade`, `cancel_upgrade`, and `execute_upgrade` backed by `nester_common::Upgrade`.
- **[#1475](https://github.com/Suncrest-Labs/nester/issues/1475)**: Give remaining deployable contracts (`vault_token`, `vault_factory`, `access_control`, `timelock`, `governance`, etc.) a timelocked upgrade path or formally document them as immutable.
- **[#1499](https://github.com/Suncrest-Labs/nester/issues/1499)**: End-to-end upgrade test from v1 WASM to v2 WASM exercising `execute_upgrade` and `migrate`.
- **[#1500](https://github.com/Suncrest-Labs/nester/issues/1500)**: Testnet rehearsal of a full timelocked upgrade using the production multisig signer topology.
