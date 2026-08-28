# Nester Soroban Storage Strategy & TTL Architecture

## 1. Overview & Objective

In Soroban (Stellar Smart Contracts), state storage requires rent to remain accessible. If a storage entry's Time-To-Live (TTL) reaches zero without being extended, the entry is archived or deleted, causing user positions to become inaccessible.

This document establishes the storage architecture, storage type classifications, and automatic TTL extension mechanisms implemented across Nester's smart contracts (`VaultContract`, `VaultTokenContract`, `YieldRegistryContract`, `TreasuryContract`, and `AllocationStrategyContract`).

---

## 2. Storage Classification Matrix

| DataKey / Storage Entry | Storage Type | Rationale | Extension Trigger |
| :--- | :--- | :--- | :--- |
| **`DataKey::Token`** | `Instance` | Global vault configuration; needed on every transaction. | Extended on every deposit/withdraw/rebalance. |
| **`DataKey::TotalAssets`** | `Instance` | Global accounting state representing aggregate vault reserves. | Extended on deposit, withdraw, harvest, yield report. |
| **`DataKey::AccruedFees`** | `Instance` | Global fee balance owed to treasury. | Extended on fee accrual and collection. |
| **`DataKey::CircuitBreaker`** | `Instance` | Autonomous safety thresholds and rate-limiting windows. | Extended on state evaluations and admin updates. |
| **`DataKey::ExitFeeTiers`** | `Instance` | Fee tiers and tenure discount schedule. | Extended on fee lookups. |
| **`DataKey::UserPrincipal(Address)`** | `Persistent` | User's personal deposit principal; survives instance dormancy. | Extended on deposit, withdrawal, and balance check. |
| **`DataKey::UserShares(Address)`** | `Persistent` | User's minted LP shares inside `VaultTokenContract`. | Extended on mint, burn, transfer, and balance query. |
| **`DataKey::Tenure(Address)`** | `Persistent` | User's deposit timestamp for tenure-tiered fee discounts. | Extended on deposits and fee previews. |
| **`DataKey::EmergencyRequest(Address)`** | `Persistent` | Queued emergency exit tickets. | Extended on queue processing and status queries. |
| **`Temporary Data`** | `Temporary` | Transient reentrancy mutex guards and one-block calculation caches. | Automatically reclaimed at end of lifetime. |

---

## 3. TTL Extension Constants & Strategy

Soroban ledgers close approximately every **5 seconds** (~17,280 ledgers per day, ~518,400 ledgers per month).

```rust
/// Minimum remaining ledger count before extending TTL (~7 days).
pub const TTL_BUMP_THRESHOLD_LEDGERS: u32 = 120_960;

/// Number of ledgers to extend TTL to upon triggering (~30 days).
pub const TTL_EXTEND_TO_LEDGERS: u32 = 518_400;
```

### Automatic Extension Behavior:
1. **Instance Storage**:
   ```rust
   env.storage().instance().extend_ttl(TTL_BUMP_THRESHOLD_LEDGERS, TTL_EXTEND_TO_LEDGERS);
   ```
   Invoked inside `require_initialized(&env)` on every user or operator interaction.
2. **Persistent User Balances**:
   ```rust
   env.storage().persistent().extend_ttl(&key, TTL_BUMP_THRESHOLD_LEDGERS, TTL_EXTEND_TO_LEDGERS);
   ```
   Invoked whenever a user interacts with their vault positions (`deposit`, `withdraw`, `preview`).

---

## 4. Rent Cost & Budget Modeling

- **Base Cost**: Soroban charges ~0.000005 XLM per 1 KB of persistent storage per 10,000 ledgers.
- **Vault Footprint**:
  - Global Vault Instance State: ~1.2 KB ($\approx 0.0006$ XLM / month).
  - Per User Position (Principal + Shares + Tenure): ~160 Bytes ($\approx 0.00008$ XLM / month).
  - 10,000 active users: $\approx 0.8$ XLM / month in rent.
- **Budgeting Mechanism**:
  The protocol treasury subsidizes vault instance storage from management fees ($0.10\%$ annual AUM), ensuring zero maintenance burden on dormant depositors.

---

## 5. Verification & Lifecycle Persistence Tests

State survival past TTL boundaries is asserted via integration tests:
- `test_storage_ttl_persistence_after_long_ledger_advance`: Simulates advancing ledger sequence by $> 500,000$ ledgers and proves user balances and vault state remain 100% accessible.
