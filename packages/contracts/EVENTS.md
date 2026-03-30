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
