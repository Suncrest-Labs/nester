//! Nester timelock module.
//!
//! Enforces a mandatory delay between proposing and executing sensitive admin
//! operations. Like [`AccessControl`], this is a plain Rust library (`rlib`)
//! that reads and writes into the *calling* contract's storage.
//!
//! # Flow
//! 1. Admin calls [`Timelock::propose`] — queues an operation with a future execution timestamp.
//! 2. After the delay elapses, admin calls [`Timelock::execute`] — marks the operation executed
//!    and returns the payload for the caller to interpret.
//! 3. Optionally, admin calls [`Timelock::cancel`] to abort a pending operation.
//!
//! # Delay bounds
//! The delay is configurable between [`MIN_DELAY`] (1 hour) and [`MAX_DELAY`] (7 days).
//! The default is [`DEFAULT_DELAY`] (24 hours). Changing the delay itself is timelocked.
//!
//! # Expiry
//! Operations that are not executed within [`EXPIRY_WINDOW`] (7 days) after becoming
//! eligible are considered expired and can no longer be executed.
//!
//! # Events
//! Every state transition emits an event for off-chain indexers.

#![no_std]

use soroban_sdk::{contracttype, panic_with_error, symbol_short, Address, Bytes, Env, Symbol, Vec};

use nester_access_control::{AccessControl, Role};
use nester_common::{emit_event, ContractError};

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

/// 1 hour in seconds.
pub const MIN_DELAY: u64 = 3_600;
/// 7 days in seconds.
pub const MAX_DELAY: u64 = 604_800;
/// 24 hours in seconds.
pub const DEFAULT_DELAY: u64 = 86_400;
/// Operations expire 7 days after becoming eligible.
pub const EXPIRY_WINDOW: u64 = 604_800;

// Event symbols
const TIMELOCK: Symbol = symbol_short!("TIMELOCK");
const OP_PROPOSED: Symbol = symbol_short!("PROPOSE");
const OP_EXECUTED: Symbol = symbol_short!("EXECUTE");
const OP_CANCELLED: Symbol = symbol_short!("CANCEL");

// ---------------------------------------------------------------------------
// Public types
// ---------------------------------------------------------------------------

#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum TimelockStatus {
    Pending,
    Executed,
    Cancelled,
    Expired,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct TimelockOperation {
    pub id: u64,
    pub op_type: Symbol,
    pub proposed_by: Address,
    pub execute_after: u64,
    pub payload: Bytes,
    pub status: TimelockStatus,
}

// ---------------------------------------------------------------------------
// Event data structs
// ---------------------------------------------------------------------------

#[contracttype]
#[derive(Clone, Debug)]
pub struct ProposeEventData {
    pub op_type: Symbol,
    pub execute_after: u64,
    pub proposed_by: Address,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct ExecuteEventData {
    pub op_type: Symbol,
    pub executed_by: Address,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct CancelEventData {
    pub op_type: Symbol,
    pub cancelled_by: Address,
}

// ---------------------------------------------------------------------------
// Storage keys
// ---------------------------------------------------------------------------

#[contracttype]
#[derive(Clone)]
enum DataKey {
    /// The current timelock delay in seconds.
    Delay,
    /// Auto-incrementing operation counter.
    NextOpId,
    /// A single timelock operation, keyed by its ID.
    Operation(u64),
    /// List of all operation IDs (append-only).
    OperationIds,
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

pub struct Timelock;

impl Timelock {
    /// Initialise the timelock with the default delay.
    ///
    /// Safe to call multiple times — only sets the delay if not already present.
    pub fn initialize(env: &Env) {
        if !env.storage().instance().has(&DataKey::Delay) {
            env.storage()
                .instance()
                .set(&DataKey::Delay, &DEFAULT_DELAY);
            env.storage().instance().set(&DataKey::NextOpId, &0u64);
            env.storage()
                .instance()
                .set(&DataKey::OperationIds, &Vec::<u64>::new(env));
        }
    }

    /// Propose a new timelocked operation.
    ///
    /// # Authorization
    /// `caller` must hold [`Role::Admin`] and must have authorised this call.
    ///
    /// # Returns
    /// The ID of the newly created operation.
    pub fn propose(env: &Env, caller: &Address, op_type: Symbol, payload: Bytes) -> u64 {
        caller.require_auth();
        AccessControl::require_role(env, caller, Role::Admin);

        let delay = get_delay(env);
        let execute_after = env.ledger().timestamp() + delay;

        let id = next_op_id(env);

        let op = TimelockOperation {
            id,
            op_type: op_type.clone(),
            proposed_by: caller.clone(),
            execute_after,
            payload,
            status: TimelockStatus::Pending,
        };

        env.storage().persistent().set(&DataKey::Operation(id), &op);

        // Append to the ID list
        let mut ids: Vec<u64> = env
            .storage()
            .instance()
            .get(&DataKey::OperationIds)
            .unwrap_or(Vec::new(env));
        ids.push_back(id);
        env.storage().instance().set(&DataKey::OperationIds, &ids);

        emit_event(
            env,
            TIMELOCK,
            OP_PROPOSED,
            caller.clone(),
            ProposeEventData {
                op_type,
                execute_after,
                proposed_by: caller.clone(),
            },
        );

        id
    }

    /// Execute a timelocked operation after the delay has elapsed.
    ///
    /// # Authorization
    /// `caller` must hold [`Role::Admin`] and must have authorised this call.
    ///
    /// # Returns
    /// The operation's payload bytes for the caller to decode and apply.
    ///
    /// # Panics
    /// * [`ContractError::TimelockNotFound`] — no operation with this ID.
    /// * [`ContractError::TimelockAlreadyExecuted`] — already executed or cancelled.
    /// * [`ContractError::TimelockNotReady`] — delay has not yet elapsed.
    /// * [`ContractError::TimelockExpired`] — operation window has passed.
    pub fn execute(env: &Env, caller: &Address, op_id: u64) -> Bytes {
        caller.require_auth();
        AccessControl::require_role(env, caller, Role::Admin);

        let mut op = get_operation(env, op_id);

        // Must be pending
        if op.status != TimelockStatus::Pending {
            panic_with_error!(env, ContractError::TimelockAlreadyExecuted);
        }

        let now = env.ledger().timestamp();

        // Delay must have elapsed
        if now < op.execute_after {
            panic_with_error!(env, ContractError::TimelockNotReady);
        }

        // Must not be expired
        if now > op.execute_after + EXPIRY_WINDOW {
            op.status = TimelockStatus::Expired;
            env.storage()
                .persistent()
                .set(&DataKey::Operation(op_id), &op);
            panic_with_error!(env, ContractError::TimelockExpired);
        }

        let payload = op.payload.clone();
        op.status = TimelockStatus::Executed;
        env.storage()
            .persistent()
            .set(&DataKey::Operation(op_id), &op);

        emit_event(
            env,
            TIMELOCK,
            OP_EXECUTED,
            caller.clone(),
            ExecuteEventData {
                op_type: op.op_type,
                executed_by: caller.clone(),
            },
        );

        payload
    }

    /// Cancel a pending timelocked operation.
    ///
    /// # Authorization
    /// `caller` must hold [`Role::Admin`] and must have authorised this call.
    ///
    /// # Panics
    /// * [`ContractError::TimelockNotFound`] — no operation with this ID.
    /// * [`ContractError::TimelockAlreadyExecuted`] — not in Pending state.
    pub fn cancel(env: &Env, caller: &Address, op_id: u64) {
        caller.require_auth();
        AccessControl::require_role(env, caller, Role::Admin);

        let mut op = get_operation(env, op_id);

        if op.status != TimelockStatus::Pending {
            panic_with_error!(env, ContractError::TimelockAlreadyExecuted);
        }

        op.status = TimelockStatus::Cancelled;
        env.storage()
            .persistent()
            .set(&DataKey::Operation(op_id), &op);

        emit_event(
            env,
            TIMELOCK,
            OP_CANCELLED,
            caller.clone(),
            CancelEventData {
                op_type: op.op_type,
                cancelled_by: caller.clone(),
            },
        );
    }

    /// Return all pending operations.
    pub fn get_pending(env: &Env) -> Vec<TimelockOperation> {
        let ids: Vec<u64> = env
            .storage()
            .instance()
            .get(&DataKey::OperationIds)
            .unwrap_or(Vec::new(env));

        let mut pending = Vec::new(env);
        for i in 0..ids.len() {
            let id = ids.get(i).unwrap();
            if let Some(op) = env
                .storage()
                .persistent()
                .get::<DataKey, TimelockOperation>(&DataKey::Operation(id))
            {
                if op.status == TimelockStatus::Pending {
                    pending.push_back(op);
                }
            }
        }
        pending
    }

    /// Get a single operation by ID.
    ///
    /// # Panics
    /// * [`ContractError::TimelockNotFound`] — no operation with this ID.
    pub fn get_operation(env: &Env, op_id: u64) -> TimelockOperation {
        get_operation(env, op_id)
    }

    /// Return the current timelock delay in seconds.
    pub fn get_delay(env: &Env) -> u64 {
        get_delay(env)
    }

    /// Propose changing the timelock delay. This operation is itself timelocked.
    ///
    /// The `new_delay` (in seconds) is encoded as the payload. On execution,
    /// the caller must apply it via [`Timelock::apply_delay`].
    ///
    /// # Panics
    /// * [`ContractError::TimelockInvalidDelay`] — new_delay outside allowed bounds.
    pub fn propose_set_delay(env: &Env, caller: &Address, new_delay: u64) -> u64 {
        if new_delay < MIN_DELAY || new_delay > MAX_DELAY {
            panic_with_error!(env, ContractError::TimelockInvalidDelay);
        }

        let payload = encode_u64(env, new_delay);
        Self::propose(env, caller, symbol_short!("SET_DLY"), payload)
    }

    /// Apply a new delay from an executed timelock payload.
    ///
    /// Should be called by the host contract after [`Timelock::execute`]
    /// returns the payload for a `SET_DLY` operation.
    pub fn apply_delay(env: &Env, payload: &Bytes) {
        let new_delay = decode_u64(payload);
        if new_delay < MIN_DELAY || new_delay > MAX_DELAY {
            panic_with_error!(env, ContractError::TimelockInvalidDelay);
        }
        env.storage().instance().set(&DataKey::Delay, &new_delay);
    }
}

// ---------------------------------------------------------------------------
// Private helpers
// ---------------------------------------------------------------------------

fn get_delay(env: &Env) -> u64 {
    env.storage()
        .instance()
        .get(&DataKey::Delay)
        .unwrap_or(DEFAULT_DELAY)
}

fn get_operation(env: &Env, op_id: u64) -> TimelockOperation {
    env.storage()
        .persistent()
        .get(&DataKey::Operation(op_id))
        .unwrap_or_else(|| panic_with_error!(env, ContractError::TimelockNotFound))
}

fn next_op_id(env: &Env) -> u64 {
    let id: u64 = env
        .storage()
        .instance()
        .get(&DataKey::NextOpId)
        .unwrap_or(0);
    env.storage().instance().set(&DataKey::NextOpId, &(id + 1));
    id
}

/// Encode a u64 as 8 big-endian bytes.
fn encode_u64(env: &Env, val: u64) -> Bytes {
    let bytes_array = val.to_be_bytes();
    Bytes::from_slice(env, &bytes_array)
}

/// Decode a u64 from 8 big-endian bytes.
fn decode_u64(bytes: &Bytes) -> u64 {
    let mut buf = [0u8; 8];
    bytes.copy_into_slice(&mut buf);
    u64::from_be_bytes(buf)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod test;
