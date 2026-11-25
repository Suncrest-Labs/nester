# Nester Soroban Storage Strategy & TTL Architecture

> **Accuracy note (issue #1488):** This document was rewritten to match the code
> as it exists today. Claims that were aspirational but not implemented have been
> replaced with what the code actually does, with file/line citations.
> Open issues track each remaining gap.

---

## 1. Overview

In Soroban, every storage entry pays ongoing rent. An entry whose TTL reaches
zero is archived (persistent / instance) or deleted (temporary). This document
describes the storage types currently used across Nester's deployable contracts
and the TTL extension behaviour that is **actually implemented**.

---

## 2. Storage Classification Matrix

| Contract | DataKey / Storage Entry | Storage Type | Notes |
| :--- | :--- | :--- | :--- |
| **`VaultContract`** | `DataKey::Token` | `Instance` | Checked by `require_initialized` (`vault/src/lib.rs:704`) |
| **`VaultContract`** | `DataKey::Status` | `Instance` | Vault pause state |
| **`VaultContract`** | Lock positions (`DataKey::Lock*`) | `Persistent` | Extend-on-access; see §3 |
| **`VaultTokenContract`** | Balances, allowances | `Persistent` | Managed by token contract |
| **`YieldRegistryContract`** | Source registry entries | `Instance` / `Persistent` | |
| **`ReferralContract`** | Referrer, CountedTowardCap, ReferredCount, RewardBalance, LifetimeReward | `Persistent` | Per-address entries renew on reads and writes; global configuration/budget remain in instance storage |
| **`NesterContract`** | `DataKey::Version`, admin state | `Instance` | |
| **`AllocationStrategyContract`** | Allocation weights | `Instance` | |

---

## 3. TTL Extension — What the Code Actually Does

### 3.1 Lock-entry TTL (vault only)

The only `extend_ttl` calls in the vault today are for **lock-position entries**
(`vault/src/lib.rs:1768`, `1778`, `1793`):

```rust
// vault/src/lib.rs:1755-1756
const LOCK_TTL_THRESHOLD: u32 = 1_000;
const LOCK_TTL_EXTEND_TO: u32 = 10_000;

// vault/src/lib.rs:1768
env.storage().persistent().extend_ttl(&key, LOCK_TTL_THRESHOLD, LOCK_TTL_EXTEND_TO);
```

These extend lock-position entries by ~10,000 ledgers (~0.6 days at 5 s/ledger)
whenever a lock is settled or queried. This is intentionally short because locks
have a bounded natural lifetime.

### 3.2 `require_initialized` does NOT extend TTL

The `require_initialized` helper (`vault/src/lib.rs:704`) only **checks** that
`DataKey::Token` is set in instance storage — it does not call `extend_ttl`:

```rust
// vault/src/lib.rs:704-708
fn require_initialized(env: &Env) {
    if !env.storage().instance().has(&DataKey::Token) {
        panic_with_error!(env, ContractError::NotInitialized);
    }
}
```

Instance storage TTL extension on user interactions is **not yet implemented**.
This is a known gap — if the vault becomes dormant, instance storage can expire.
Tracked as a follow-up to issue #1220.

### 3.3 No `TTL_BUMP_THRESHOLD_LEDGERS` / `TTL_EXTEND_TO_LEDGERS` constants

The previous version of this document described two named constants
(`TTL_BUMP_THRESHOLD_LEDGERS = 120_960`, `TTL_EXTEND_TO_LEDGERS = 518_400`).
**These constants do not exist** anywhere in the codebase today. The only TTL
constants live locally in `vault/src/lib.rs:1755-1756` (see §3.1).

---

## 4. Rent Cost Reference

| Storage type | Approx. cost (Soroban) |
| :--- | :--- |
| Instance (global vault state, ~1-2 KB) | ~0.0006 XLM / month |
| Persistent per-user position (160 B) | ~0.00008 XLM / month |

Figures are illustrative; actual rent rates depend on the current network fee
schedule. Because instance TTL auto-extension is not implemented, operators
should monitor instance TTL directly and bump manually until the auto-extension
feature ships.

---

## 5. Verification

End-to-end TTL tests added under issue #1220 live in
`tests/integration/src/integration/lifecycle_tests.rs` and assert that user
balances survive a large ledger-sequence advance. Those tests are run by
`cargo test -p nester-integration-tests` (no filter) -- see
`.github/workflows/ci.yml` `contracts` job, step "Run integration tests".


## Referral account storage (#1471)

`contracts/referral/src/lib.rs` centralizes per-address reads/writes in
`read_user` and `write_user`. Existing entries renew on access using
`POSITION_TTL_THRESHOLD_LEDGERS` (120,960) and
`POSITION_TTL_EXTEND_TO_LEDGERS` (518,400). Missing reads do not create entries.
Claims retain a zero reward balance and preserve lifetime rewards and cap state.
The 500-referral regression compares serialized instance storage after every
registration, so both key growth and growing collections are detected.

This changes the storage layout for new deployments. It does not migrate an
already deployed instance-backed referral contract: existing relationships,
balances and cap records must be migrated before using this layout in an upgrade.
Persistent entries that become archived require restoration before access.
