# Nester Event Schema

This document defines the standardized event schema for all Nester contracts.

## Topic Structure
All events follow a three-level topic structure:
1. **Contract**: `Symbol` identifying the contract (e.g., `VAULT`, `REGISTRY`, `STRATEGY`, `ACCESS`).
2. **Action**: `Symbol` identifying the operation (e.g., `DEPOSIT`, `SOURCE_ADDED`).
3. **Entity**: `Address` or `Symbol` identifying the primary entity affected (e.g., user address, source ID).

## Vault Events (Contract Symbol: `VAULT`)

### DEPOSIT
Emitted when a user deposits funds.
- **Topics**: `(VAULT, DEPOSIT, user: Address)`
- **Data**:
    ```rust
    {
        amount: i128,
        shares_minted: i128,
        new_balance: i128,
        total_deposits: i128
    }
    ```

### WITHDRAW
Emitted when a user withdraws funds.
- **Topics**: `(VAULT, WITHDRAW, user: Address)`
- **Data**:
    ```rust
    {
        amount: i128,
        shares_burned: i128,
        new_balance: i128,
        total_deposits: i128
    }
    ```

### EMRG_WD
Emitted once per position when a user emergency-exits all active positions via `emergency_withdraw_all`.
- **Topics**: `(VAULT, EMRG_WD, user: Address)`
- **Data**:
    ```rust
    {
        user: Address,
        protocol: Symbol,
        amount: i128
    }
    ```

### PAUSE
Emitted when the vault is paused.
- **Topics**: `(VAULT, PAUSE, admin: Address)`
- **Data**: `{ timestamp: u64 }`

### UNPAUSE
Emitted when the vault is unpaused.
- **Topics**: `(VAULT, UNPAUSE, admin: Address)`
- **Data**: `{ timestamp: u64 }`

## Yield Registry Events (Contract Symbol: `REGISTRY`)

### SOURCE_ADDED
Emitted when a new yield source is registered.
- **Topics**: `(REGISTRY, SOURCE_ADDED, source_id: Symbol)`
- **Data**:
    ```rust
    {
        contract_address: Address,
        protocol_type: ProtocolType
    }
    ```

### SOURCE_UPDATED
Emitted when a yield source status is updated.
- **Topics**: `(REGISTRY, SOURCE_UPDATED, source_id: Symbol)`
- **Data**:
    ```rust
    {
        old_status: SourceStatus,
        new_status: SourceStatus
    }
    ```

### SOURCE_REMOVED
Emitted when a yield source is removed.
- **Topics**: `(REGISTRY, SOURCE_REMOVED, source_id: Symbol)`
- **Data**: `{}`

## Allocation Strategy Events (Contract Symbol: `STRATEGY`)

### WEIGHTS_UPDATED
Emitted when allocation weights are updated.
- **Topics**: `(STRATEGY, WEIGHTS_UPDATED, admin: Address)`
- **Data**:
    ```rust
    {
        old_weights: Vec<AllocationWeight>,
        new_weights: Vec<AllocationWeight>
    }
    ```

## Access Control Events (Contract Symbol: `ACCESS`)

### ROLE_GRANTED
Emitted when a role is granted.
- **Topics**: `(ACCESS, ROLE_GRANTED, grantee: Address)`
- **Data**:
    ```rust
    {
        role: Role,
        grantor: Address
    }
    ```

### ROLE_REVOKED
Emitted when a role is revoked.
- **Topics**: `(ACCESS, ROLE_REVOKED, target: Address)`
- **Data**:
    ```rust
    {
        role: Role,
        revoker: Address
    }
    ```

### ADMIN_TRANSFER
Emitted when an admin transfer is completed.
- **Topics**: `(ACCESS, ADMIN_TRANSFER, new_admin: Address)`
- **Data**: `{ old_admin: Address }`

## Timelock Events (Contract Symbol: `TIMELOCK`)

### PROPOSE
Emitted when a timelocked operation is proposed.
- **Topics**: `(TIMELOCK, PROPOSE, proposed_by: Address)`
- **Data**:
    ```rust
    {
        op_type: Symbol,
        execute_after: u64,
        proposed_by: Address
    }
    ```

### EXECUTE
Emitted when a timelocked operation is executed.
- **Topics**: `(TIMELOCK, EXECUTE, executed_by: Address)`
- **Data**:
    ```rust
    {
        op_type: Symbol,
        executed_by: Address
    }
    ```

### CANCEL
Emitted when a timelocked operation is cancelled.
- **Topics**: `(TIMELOCK, CANCEL, cancelled_by: Address)`
- **Data**:
    ```rust
    {
        op_type: Symbol,
        cancelled_by: Address
    }
    ```

---

## Yield Adapters (#812)

### SRC_FAIL
Emitted by the registry each time an adapter interaction fails. Fires on every
failure, including ones below the degradation threshold.
- **Topics**: `(REGISTRY, SRC_FAIL, source_id: Symbol)`
- **Data**:
    ```rust
    {
        failure_count: u32,   // consecutive failures including this one
        threshold: u32,       // failures tolerated before degradation
        reporter: Address     // registry itself, or the vault
    }
    ```

### SRC_DEGR
Emitted once when consecutive failures exceed the threshold and the source is
flipped to `SourceStatus::Degraded`. Allocation logic then freezes the source's
existing allocation. Recovery is never automatic.
- **Topics**: `(REGISTRY, SRC_DEGR, source_id: Symbol)`
- **Data**:
    ```rust
    {
        failure_count: u32,
        previous_status: SourceStatus,
        degraded_at: u64
    }
    ```

### SRC_RECO
Emitted when an admin explicitly returns a degraded source to `Active` via
`recover_source`.
- **Topics**: `(REGISTRY, SRC_RECO, source_id: Symbol)`
- **Data**:
    ```rust
    {
        recovered_by: Address,
        recovered_at: u64
    }
    ```

### SRC_SKIP
Emitted by the vault when a rebalance skips a source because its adapter
failed, or when `record_source_allocation` refuses an unhealthy source. The
rebalance continues across the remaining sources.
- **Topics**: `(VAULT, SRC_SKIP, source_id: Symbol)`
- **Data**:
    ```rust
    {
        attempted_delta: i128,
        timestamp: u64
    }
    ```

### ADAPTER DEPOSIT / WITHDRAW
Emitted by each adapter when value moves through it.
- **Topics**: `(ADAPTER, DEPOSIT | WITHDRAW, counterparty: Address)`
- **Data**:
    ```rust
    {
        amount: i128,   // underlying assets in/out
        units: i128     // protocol position units minted/burned
    }
    ```
