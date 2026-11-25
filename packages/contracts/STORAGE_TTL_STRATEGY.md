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
| **`VaultTokenContract`** | `Balance`, `DepositTimestamp`, `Allowance` (per account) | `Persistent` | Renewed on read and write; see §3.4 |
| **`VaultTokenContract`** | Vault, name, symbol, decimals, totals | `Instance` | Renewed by `extend_ttl(None)` |
| **`YieldRegistryContract`** | Source registry entries | `Instance` / `Persistent` | |
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

### 3.4 vault_token per-account entries

Each holder's `Balance` and `DepositTimestamp`, and each `Allowance`, is its
own persistent entry, so the cost of a call does not depend on the number of
holders (issue #1470).

- **Balance and deposit timestamp.** Every write and every read inside a
  transaction extends the entry to `POSITION_TTL_EXTEND_TO_LEDGERS` (518,400
  ledgers, about 30 days) once it is below `POSITION_TTL_THRESHOLD_LEDGERS`
  (`vault_token/src/lib.rs:484` `bump_persistent`, `:494` `get_balance`,
  `:506` `set_balance`, `:583` `set_deposit_timestamp`, `:390`
  `get_deposit_time`). A zero balance is removed instead of stored.
- **Holders who do nothing for 30 days.** Nothing reads their entries, so
  they have to be renewed from outside: `extend_ttl(Some(user))`
  (`vault_token/src/lib.rs:407`, issue #1485) is permissionless and renews
  that holder's `Balance` and `DepositTimestamp`. Calling it for every
  holder is the job of the keeper in issue #1486, which is not built yet.
  Until it is, an entry that expires is archived, not deleted: `balance()`,
  withdrawals and transfers for that holder fail until the entry is restored
  with a `RestoreFootprintOp`.
- **Allowances.** `approve` sets the allowance's TTL to
  `expiration_ledger - current ledger` (`vault_token/src/lib.rs:172`), the
  SEP-41 rule, so an approval lives exactly as long as it is valid.
  `extend_ttl` does not touch allowances.

**Deploying this layout.** vault_token has no upgrade entrypoint, and tokens
deployed before #1470 keep balances in instance storage, so this layout needs
a fresh vault_token deployment. Nothing migrates the old entries: shares held
in an existing testnet token stay in that token at its old address, and the
new token starts with no holders.

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
