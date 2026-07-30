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

### RL_XFR_P (role_transfer_proposed)
Emitted when a generalised two-step role transfer is proposed (issue #820) — the same one-move-mistake protection as `ADMIN_TRANSFER`, extended to every role.
- **Topics**: `(ACCESS, RL_XFR_P, new_holder: Address)`
- **Data**: `{ role: Role, from: Address, to: Address }`

### RL_XFR_A (role_accepted)
Emitted when the proposed successor accepts a pending role transfer.
- **Topics**: `(ACCESS, RL_XFR_A, new_holder: Address)`
- **Data**: `{ role: Role, from: Address, to: Address }`

### RL_XFR_C (role transfer cancelled)
Emitted when the proposer cancels a pending role transfer before it is accepted.
- **Topics**: `(ACCESS, RL_XFR_C, proposer: Address)`
- **Data**: `{ role: Role, from: Address, to: Address }`

### RL_EXP (role_expired)
Emitted the first time `has_role` observes that a time-bounded grant (via `grant_role_until`) has passed its expiry. The flag is cleared and the address is removed from `get_role_members` at the same time.
- **Topics**: `(ACCESS, RL_EXP, account: Address)`
- **Data**: `{ role: Role, actor: Address }`

## Circuit Breaker Events (Contract Symbol: `BREAKER`, emitted by the vault — issue #817)

### BRK_TRIP (breaker_tripped)
Emitted every time a trip condition fires, whether or not it actually raises severity further (so the latest firing condition is always visible even while already at a higher severity).
- **Topics**: `(BREAKER, BRK_TRIP, vault_address: Address)`
- **Data**:
    ```rust
    {
        reason: TripReason,      // SharePriceMove | YieldSanity | WithdrawVelocity | SourceFailure | GuardianManual
        observed: i128,
        threshold: i128,
        severity: Severity       // Normal | Throttled | DepositsHalted | FullHalt
    }
    ```

### SEV_CHG (severity_changed)
Emitted whenever severity actually transitions, in either direction (automatic escalation or staged recovery).
- **Topics**: `(BREAKER, SEV_CHG, vault_address: Address)`
- **Data**: `(from: Severity, to: Severity)`

### BRK_RCVR (breaker_recovered)
Emitted on each staged recovery step (`recover_next_stage`), recording who authorised it.
- **Topics**: `(BREAKER, BRK_RCVR, authorised_by: Address)`
- **Data**: `{ from: Severity, to: Severity, authorised_by: Address }`

## Vault Factory Events (Contract Symbol: `FACTORY` — issue #816)

### VLT_NEW (vault created)
Emitted when `create_vault` successfully deploys and initialises a new vault.
- **Topics**: `(FACTORY, VLT_NEW, vault_address: Address)`
- **Data**: `{ salt: BytesN<32>, address: Address, wasm_hash: BytesN<32> }`

### VLT_DEP (vault deprecated)
Emitted when `deprecate_vault` marks a registry entry as no longer recommended.
- **Topics**: `(FACTORY, VLT_DEP, vault_address: Address)`
- **Data**: `{}`

### WASM_SET (wasm hash applied)
Emitted when a timelocked WASM-hash change is applied via `apply_wasm_hash`.
- **Topics**: `(FACTORY, WASM_SET, caller: Address)`
- **Data**: `new_hash: BytesN<32>`

## Referral Events (Contract Symbol: `REFERRAL` — issue #818)

### REF_REG (referral_registered)
Emitted when a referral relationship is bound via `register_referral`.
- **Topics**: `(REFERRAL, REF_REG, user: Address)`
- **Data**: `referrer: Address`

### REF_ACCR (referral_reward_accrued)
Emitted whenever a reward is credited to a referrer's claimable balance.
- **Topics**: `(REFERRAL, REF_ACCR, referrer: Address)`
- **Data**: `(referred_user: Address, reward: i128)`

### REF_CLAIM (referral_reward_claimed)
Emitted when a referrer claims their accrued balance.
- **Topics**: `(REFERRAL, REF_CLAIM, referrer: Address)`
- **Data**: `amount: i128`

### REF_BGT_X (referral_budget_exhausted)
Emitted once the global program budget reaches zero.
- **Topics**: `(REFERRAL, REF_BGT_X, referrer: Address)`
- **Data**: `remaining_budget: i128` (always `0`)

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

## Savings Goal Events (Contract Symbol: `SAV_GOAL` — issue #807)

### GOAL_NEW (goal_created)
Emitted when `create_goal` registers a new on-chain goal.
- **Topics**: `(SAV_GOAL, GOAL_NEW, goal_id: BytesN<32>)`
- **Data**: `{ owner: Address, vault: Address, target_amount: i128, deadline: u64 }`

### GOAL_MS (goal_milestone_reached)
Emitted the first time `contribute` observes `contributed` crossing a 25/50/75/100% threshold. The goal's milestone bitmask makes this idempotent — a threshold can never be attested twice, even across retried calls.
- **Topics**: `(SAV_GOAL, GOAL_MS, goal_id: BytesN<32>)`
- **Data**: `{ threshold_pct: u32, contributed: i128, timestamp: u64 }`

### GOAL_CMP (goal_completed)
Emitted when a goal's `contributed` reaches its `target_amount`, either inline during `contribute` or via the permissionless `finalize_goal`.
- **Topics**: `(SAV_GOAL, GOAL_CMP, goal_id: BytesN<32>)`
- **Data**: `{ contributed: i128, timestamp: u64 }`

### GOAL_EXP (goal_expired)
Emitted when the permissionless `expire_goal` transitions a goal past its deadline without completion.
- **Topics**: `(SAV_GOAL, GOAL_EXP, goal_id: BytesN<32>)`
- **Data**: `{ contributed: i128, timestamp: u64 }`

### GOAL_AB (goal_abandoned)
Emitted when the goal owner calls `abandon_goal`.
- **Topics**: `(SAV_GOAL, GOAL_AB, goal_id: BytesN<32>)`
- **Data**: `{ contributed: i128, timestamp: u64 }`

## Upgrade Events (Contract Symbol: `UPGRADE`)

### PROP_UPG (upgrade_proposed)
Emitted when a new contract WASM upgrade is proposed with a timelock ETA.
- **Topics**: `(UPGRADE, PROP_UPG, proposer: Address)`
- **Data**:
    ```rust
    {
        wasm_hash: BytesN<32>,
        eta: u64,
        proposer: Address
    }
    ```

### CAN_UPG (upgrade_cancelled)
Emitted when a pending WASM upgrade proposal is cancelled before execution.
- **Topics**: `(UPGRADE, CAN_UPG, cancelled_by: Address)`
- **Data**:
    ```rust
    {
        wasm_hash: BytesN<32>,
        cancelled_by: Address
    }
    ```

### EXEC_UPG (upgrade_executed)
Emitted when a matured WASM upgrade is executed, updating the contract WASM.
- **Topics**: `(UPGRADE, EXEC_UPG, executed_by: Address)`
- **Data**:
    ```rust
    {
        wasm_hash: BytesN<32>,
        executed_by: Address,
        execution_timestamp: u64
    }
    ```

