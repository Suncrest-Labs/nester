# Nester Soroban Storage Strategy & TTL Architecture

> **Audit Status (Issue [#1488](https://github.com/Suncrest-Labs/nester/issues/1488))**: This document records the **actual as-built** storage layout and Time-To-Live (TTL) behavior in `packages/contracts` with `file#line` references. Planned or missing mechanisms are listed explicitly in [Section 4 (Known Gaps & Tracked Issues)](#4-known-gaps--tracked-issues) rather than described as current behavior.

---

## 1. Overview

In Soroban (Stellar Smart Contracts), ledger entries occupy one of three storage tiers (`Instance`, `Persistent`, or `Temporary`), each requiring rent to keep its Time-To-Live (TTL) above the current ledger sequence.

---

## 2. As-Built Storage Classification Matrix

### 2.1 `VaultContract` (`packages/contracts/contracts/vault/src/lib.rs`)

| DataKey / Entry | Actual Storage Tier | Evidence (`file#line`) | As-Built TTL Extension Behavior |
| :--- | :--- | :--- | :--- |
| `DataKey::Token`, `DataKey::VaultToken`, `DataKey::Treasury`, `DataKey::Status` | `Instance` | [`vault/src/lib.rs#L420-L424`](contracts/vault/src/lib.rs#L420-L424), [`vault/src/lib.rs#L2218-L2229`](contracts/vault/src/lib.rs#L2218-L2229) | **No explicit `extend_ttl` call** in `require_initialized` ([`vault/src/lib.rs#L704-L708`](contracts/vault/src/lib.rs#L704-L708)). Tracked in [#1485](https://github.com/Suncrest-Labs/nester/issues/1485) and [#1486](https://github.com/Suncrest-Labs/nester/issues/1486). |
| `DataKey::TotalAssets`, `DataKey::FeeConfig`, `DataKey::LastFeeAccrual`, `DataKey::AccruedFees` | `Instance` | [`vault/src/lib.rs#L425-L428`](contracts/vault/src/lib.rs#L425-L428), [`vault/src/lib.rs#L2222-L2240`](contracts/vault/src/lib.rs#L2222-L2240) | **No explicit `extend_ttl` call**. Tracked in [#1485](https://github.com/Suncrest-Labs/nester/issues/1485). |
| `DataKey::CircuitBreakerConfig`, `DataKey::BreakerConfigV2`, `DataKey::Severity` | `Instance` | [`vault/src/lib.rs#L434`](contracts/vault/src/lib.rs#L434), [`vault/src/lib.rs#L464-L474`](contracts/vault/src/lib.rs#L464-L474), [`vault/src/lib.rs#L990-L1025`](contracts/vault/src/lib.rs#L990-L1025) | **No explicit `extend_ttl` call**. Tracked in [#1485](https://github.com/Suncrest-Labs/nester/issues/1485). |
| `DataKey::ExitFeeTiers`, `DataKey::PerformanceFeeTiers`, `DataKey::ManagementFeeTiers` | `Instance` | [`vault/src/lib.rs#L453-L455`](contracts/vault/src/lib.rs#L453-L455), [`vault/src/lib.rs#L1920-L1965`](contracts/vault/src/lib.rs#L1920-L1965) | **No explicit `extend_ttl` call**. Tracked in [#1485](https://github.com/Suncrest-Labs/nester/issues/1485). |
| `DataKey::UserPrincipal(Address)` | `Persistent` | [`vault/src/lib.rs#L436`](contracts/vault/src/lib.rs#L436), [`vault/src/lib.rs#L3897-L3900`](contracts/vault/src/lib.rs#L3897-L3900), [`vault/src/lib.rs#L4070`](contracts/vault/src/lib.rs#L4070), [`vault/src/lib.rs#L5118`](contracts/vault/src/lib.rs#L5118) | Written/read via `env.storage().persistent()`, **without explicit `extend_ttl`**. Tracked in [#1485](https://github.com/Suncrest-Labs/nester/issues/1485). |
| `DataKey::DepositTime(Address)`, `DataKey::FirstDepositAt(Address)` | `Persistent` | [`vault/src/lib.rs#L430`](contracts/vault/src/lib.rs#L430), [`vault/src/lib.rs#L452`](contracts/vault/src/lib.rs#L452), [`vault/src/lib.rs#L1166-L1186`](contracts/vault/src/lib.rs#L1166-L1186), [`vault/src/lib.rs#L1971-L2008`](contracts/vault/src/lib.rs#L1971-L2008) | Written/read via `env.storage().persistent()`, **without explicit `extend_ttl`**. Tracked in [#1485](https://github.com/Suncrest-Labs/nester/issues/1485). |
| `DataKey::UserYield(Address)`, `DataKey::LastHarvestAt(Address)` | `Persistent` | [`vault/src/lib.rs#L448-L450`](contracts/vault/src/lib.rs#L448-L450), [`vault/src/lib.rs#L1377-L1422`](contracts/vault/src/lib.rs#L1377-L1422) | Written/read via `env.storage().persistent()`, **without explicit `extend_ttl`**. Tracked in [#1485](https://github.com/Suncrest-Labs/nester/issues/1485). |
| `DataKey::DepositAllowlisted(Address)` | `Persistent` | [`vault/src/lib.rs#L473`](contracts/vault/src/lib.rs#L473), [`vault/src/lib.rs#L1110-L1118`](contracts/vault/src/lib.rs#L1110-L1118), [`vault/src/lib.rs#L2346-L2362`](contracts/vault/src/lib.rs#L2346-L2362) | Written/read via `env.storage().persistent()`, **without explicit `extend_ttl`**. |
| `DataKey::UserLocks(Address)`, `DataKey::NextLockId(Address)` | `Persistent` | [`vault/src/lib.rs#L500-L505`](contracts/vault/src/lib.rs#L500-L505), [`vault/src/lib.rs#L1755-L1794`](contracts/vault/src/lib.rs#L1755-L1794) | **Explicitly calls `extend_ttl(&key, LOCK_TTL_THRESHOLD, LOCK_TTL_EXTEND_TO)`** (`1_000` / `10_000` ledgers) on `get_user_locks`, `set_user_locks`, and `set_next_lock_id` ([`vault/src/lib.rs#L1768`](contracts/vault/src/lib.rs#L1768), [`#L1778`](contracts/vault/src/lib.rs#L1778), [`#L1793`](contracts/vault/src/lib.rs#L1793)). |
| `QueueDataKey::Entry(u64)`, `UserTicket(Address)`, `Head`, `Tail`, `TotalQueued` | `Persistent` | [`vault/src/queue.rs#L25-L34`](contracts/vault/src/queue.rs#L25-L34), [`vault/src/queue.rs#L121-L152`](contracts/vault/src/queue.rs#L121-L152) | Stored in `env.storage().persistent()`, **without explicit `extend_ttl`**. |

### 2.2 `VaultTokenContract` (`packages/contracts/contracts/vault_token/src/lib.rs`)

| DataKey / Entry | Actual Storage Tier | Evidence (`file#line`) | As-Built TTL / Storage Note |
| :--- | :--- | :--- | :--- |
| `DataKey::Vault`, `Name`, `Symbol`, `Decimals`, `TotalSupply`, `TotalAssets` | `Instance` | [`vault_token/src/lib.rs#L48-L57`](contracts/vault_token/src/lib.rs#L48-L57), [`vault_token/src/lib.rs#L86-L91`](contracts/vault_token/src/lib.rs#L86-L91) | Global token metadata and supply in `Instance` storage; no `extend_ttl` call. |
| `DataKey::Balance(Address)` | **`Instance`** (gap) | [`vault_token/src/lib.rs#L45`](contracts/vault_token/src/lib.rs#L45), [`vault_token/src/lib.rs#L417-L428`](contracts/vault_token/src/lib.rs#L417-L428) | Currently stored in `env.storage().instance()`, not `persistent()`. Tracked as bug [#1470](https://github.com/Suncrest-Labs/nester/issues/1470). |
| `DataKey::Allowance(Address, Address)` | **`Instance`** (gap) | [`vault_token/src/lib.rs#L47`](contracts/vault_token/src/lib.rs#L47), [`vault_token/src/lib.rs#L155-L161`](contracts/vault_token/src/lib.rs#L155-L161), [`vault_token/src/lib.rs#L464-L495`](contracts/vault_token/src/lib.rs#L464-L495) | Currently stored in `env.storage().instance()` without TTL tied to `expiration_ledger`. Tracked in [#1470](https://github.com/Suncrest-Labs/nester/issues/1470) and [#1479](https://github.com/Suncrest-Labs/nester/issues/1479). |
| `DataKey::DepositTimestamp(Address)` | `Persistent` | [`vault_token/src/lib.rs#L60`](contracts/vault_token/src/lib.rs#L60), [`vault_token/src/lib.rs#L111-L120`](contracts/vault_token/src/lib.rs#L111-L120), [`vault_token/src/lib.rs#L292-L295`](contracts/vault_token/src/lib.rs#L292-L295), [`vault_token/src/lib.rs#L378-L383`](contracts/vault_token/src/lib.rs#L378-L383) | Stored in `env.storage().persistent()`; no explicit `extend_ttl` call. Tracked in [#1485](https://github.com/Suncrest-Labs/nester/issues/1485). |

### 2.3 Shared Libraries & Other Contracts

| Component | Actual Storage Tier | Evidence (`file#line`) | Notes |
| :--- | :--- | :--- | :--- |
| `ReentrancyGuard::enter` / `exit` | `Temporary` | [`libs/common/src/reentrancy.rs#L15-L34`](libs/common/src/reentrancy.rs#L15-L34) | Uses `env.storage().temporary()` for intra-transaction mutex locking (`ReentrancyGuard::enter_persistent` at [`#L57-L84`](libs/common/src/reentrancy.rs#L57-L84) uses `persistent()`). |
| `AccessControl` roles & config | `Instance` | [`libs/common/src/access_control.rs#L61-L215`](libs/common/src/access_control.rs#L61-L215) | Stored in `env.storage().instance()`. |
| `Upgrade` (`PendingUpgrade`, `SchemaVersion`) | `Instance` | [`libs/common/src/upgrade.rs#L61-L63`](libs/common/src/upgrade.rs#L61-L63), [`#L158-L178`](libs/common/src/upgrade.rs#L158-L178) | Stored in `env.storage().instance()`. |
| `recurring_deposit` schedules & user indices | `Persistent` & `Instance` | [`contracts/recurring_deposit/src/lib.rs#L140-L420`](contracts/recurring_deposit/src/lib.rs#L140-L420) | Schedules in `persistent()`; global counters in `instance()`; no `extend_ttl` call. |
| `timelock` operations | `Persistent` | [`contracts/timelock/src/lib.rs#L164-L395`](contracts/timelock/src/lib.rs#L164-L395) | `DataKey::Operation(id)` in `persistent()`; no `extend_ttl` call. |
| `referral` per-user records | **`Instance`** (gap) | [`contracts/referral/src/lib.rs#L38-L60`](contracts/referral/src/lib.rs#L38-L60) | Per-user referral records stored in `instance()`. Tracked in [#1471](https://github.com/Suncrest-Labs/nester/issues/1471). |
| `savings_goal` goals & contributors | **`Instance`** (gap) | [`contracts/savings_goal/src/lib.rs#L60-L95`](contracts/savings_goal/src/lib.rs#L60-L95) | Goals and contributor maps stored in `instance()`. Tracked in [#1472](https://github.com/Suncrest-Labs/nester/issues/1472). |
| `goal_rewards` claim records | **`Instance`** (gap) | [`contracts/goal_rewards/src/lib.rs#L45-L75`](contracts/goal_rewards/src/lib.rs#L45-L75) | `Claimed` and `ActiveClaimWindow` stored in `instance()`. Tracked in [#1473](https://github.com/Suncrest-Labs/nester/issues/1473). |

---

## 3. As-Built TTL Constants & Extension Calls

The only TTL constants and `extend_ttl` invocations currently present in production contract code are in [`contracts/vault/src/lib.rs#L1755-L1794`](contracts/vault/src/lib.rs#L1755-L1794):

```rust
const LOCK_TTL_THRESHOLD: u32 = 1_000;
const LOCK_TTL_EXTEND_TO: u32 = 10_000;
```

- Called in `get_user_locks` ([`vault/src/lib.rs#L1758-L1771`](contracts/vault/src/lib.rs#L1758-L1771)), `set_user_locks` ([`vault/src/lib.rs#L1773-L1779`](contracts/vault/src/lib.rs#L1773-L1779)), and `set_next_lock_id` ([`vault/src/lib.rs#L1788-L1794`](contracts/vault/src/lib.rs#L1788-L1794)).
- `require_initialized` in [`contracts/vault/src/lib.rs#L704-L708`](contracts/vault/src/lib.rs#L704-L708) and [`contracts/nester/src/lib.rs#L87-L91`](contracts/nester/src/lib.rs#L87-L91) currently only checks `env.storage().instance().has(...)` and does **not** bump instance TTL.

---

## 4. Known Gaps & Tracked Issues

1. **Per-user state in `Instance` storage**:
   - [`vault_token/src/lib.rs#L417-L428`](contracts/vault_token/src/lib.rs#L417-L428) and [`#L464-L495`](contracts/vault_token/src/lib.rs#L464-L495) store `Balance(Address)` and `Allowance(Address, Address)` in instance storage — tracked in [#1470](https://github.com/Suncrest-Labs/nester/issues/1470) and [#1479](https://github.com/Suncrest-Labs/nester/issues/1479).
   - `referral`, `savings_goal`, and `goal_rewards` store unbounded user records in instance storage — tracked in [#1471](https://github.com/Suncrest-Labs/nester/issues/1471), [#1472](https://github.com/Suncrest-Labs/nester/issues/1472), and [#1473](https://github.com/Suncrest-Labs/nester/issues/1473).
2. **Missing permissionless TTL-extension entrypoints and automatic instance bumps**:
   - No contract currently exposes `extend_instance_ttl()` / `extend_user_ttl(user)` or extends instance TTL inside `require_initialized` — tracked in [#1485](https://github.com/Suncrest-Labs/nester/issues/1485).
3. **Off-chain TTL keeper and client restoration**:
   - Backend TTL keeper job to proactively extend instance and user entries before expiration — tracked in [#1486](https://github.com/Suncrest-Labs/nester/issues/1486).
   - Frontend handling of `simulateTransaction` `restorePreamble` (`RestoreFootprintOp`) when archived entries are encountered — tracked in [#1487](https://github.com/Suncrest-Labs/nester/issues/1487).

---

## 5. Existing Tests

- [`tests/integration/src/integration/lifecycle_tests.rs#L464-L491`](tests/integration/src/integration/lifecycle_tests.rs#L464-L491) (`test_storage_ttl_persistence_after_long_ledger_advance`): advances `li.timestamp` by `3_000_000` seconds (~35 days) and verifies `total_assets`, `total_supply`, `share_price`, `balance`, and `withdraw`. Note that advancing `timestamp` alone in `soroban-sdk` testutils does not trigger ledger-sequence TTL archival; ledger-sequence TTL expiry and restoration tests are tracked under [#1485](https://github.com/Suncrest-Labs/nester/issues/1485).
- [`contracts/vault/src/test.rs#L4635-L4665`](contracts/vault/src/test.rs#L4635-L4665): exercises locked-position persistence across `deposit_locked`, `unlock_position`, and `break_lock` ([`vault/src/lib.rs#L1755-L1794`](contracts/vault/src/lib.rs#L1755-L1794)).
