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

### 3.2 Instance TTL is renewed on initialized vault calls

`require_initialized` (`packages/contracts/contracts/vault/src/lib.rs:709`) now
calls `instance().extend_ttl(...)` using the shared constants before checking
initialization (`packages/contracts/libs/common/src/constants.rs:120`). This
covers vault public calls that pass through `require_initialized`, including
`deposit` (`vault/src/lib.rs:3815`) and `withdraw` (`vault/src/lib.rs:4008`).

### 3.3 Canonical TTL constants

The canonical constants are
`TTL_BUMP_THRESHOLD_LEDGERS = 120_960` and `TTL_EXTEND_TO_LEDGERS = 518_400`
(`packages/contracts/libs/common/src/constants.rs:120-123`). Their 30-day target
is below the network's mainnet `max_entry_ttl`; compatibility aliases retain
existing keeper entrypoints (`constants.rs:126-129`).

---

## 4. Rent Cost Reference

| Storage type | Approx. cost (Soroban) |
| :--- | :--- |
| Instance (global vault state, ~1-2 KB) | ~0.0006 XLM / month |
| Persistent per-user position (160 B) | ~0.00008 XLM / month |

Figures are illustrative; actual rent rates depend on the current network fee
schedule. Instance TTL is renewed from initialized vault calls, and persistent
per-user entries are renewed on matching read/write helpers
(`packages/contracts/contracts/vault/src/lib.rs:1126`,
`packages/contracts/contracts/vault_token/src/lib.rs:473`).

---

## 5. Verification

A CI step runs `packages/contracts/scripts/check_storage_ttl.py`, which fails if
a deployable contract persistent `.set()` lacks a matching `.extend_ttl()` in the
same write region (`.github/workflows/ci.yml:361`). The contract unit suite also
runs through `cargo test --lib` (`.github/workflows/ci.yml:365`).
