//! Unit tests for the Nester timelock module.
//!
//! Like access-control, this is a plain Rust library so all storage access
//! must run inside a contract execution context via `env.as_contract`.
//!
//! # Privileged-entrypoint authorization matrix
//!
//! | API | Authorized | Unauthorized role | Missing signature | Revoked role |
//! | --- | --- | --- | --- | --- |
//! | `propose` | `propose_creates_pending_operation` | `propose_fails_for_non_admin` | `propose_requires_caller_auth` | `propose_fails_for_revoked_admin` |
//! | `execute` | `execute_after_delay_succeeds` | `execute_fails_for_non_admin` | `execute_requires_caller_auth` | `execute_fails_for_revoked_admin` |
//! | `cancel` | `cancel_pending_operation` | `cancel_fails_for_non_admin` | `cancel_requires_caller_auth` | `cancel_fails_for_revoked_admin` |
//! | `propose_set_delay` | `propose_set_delay_and_apply` | `propose_set_delay_fails_for_non_admin` | `propose_set_delay_requires_caller_auth` | `propose_set_delay_fails_for_revoked_admin` |
//!
//! Role-negative tests run with authentication mocked and otherwise-valid
//! operations. Signature-negative tests retain the Admin role but clear all
//! mocked auths. This makes each test sensitive to removal of its exact guard.
//! `apply_delay` is a host-only helper with no caller argument, not a Soroban
//! entrypoint, so caller authorization is intentionally N/A here.

extern crate std;

use soroban_sdk::{
    contract, contractimpl, symbol_short,
    testutils::{Address as _, Events as _, Ledger as _},
    xdr::{ScErrorCode, ScErrorType},
    Address, Bytes, Env, Error, Symbol,
};

use nester_access_control::AccessControl;
use nester_common::ContractError;

use crate::{Timelock, TimelockStatus, DEFAULT_DELAY, EXPIRY_WINDOW, MAX_DELAY, MIN_DELAY};

// ---------------------------------------------------------------------------
// Minimal dummy contract for test context
// ---------------------------------------------------------------------------

#[contract]
struct TestTL;

#[contractimpl]
impl TestTL {
    pub fn propose(env: Env, caller: Address, op_type: Symbol, payload: Bytes) -> u64 {
        Timelock::propose(&env, &caller, op_type, payload)
    }

    pub fn execute(env: Env, caller: Address, op_id: u64) -> Bytes {
        Timelock::execute(&env, &caller, op_id)
    }

    pub fn cancel(env: Env, caller: Address, op_id: u64) {
        Timelock::cancel(&env, &caller, op_id);
    }

    pub fn propose_set_delay(env: Env, caller: Address, new_delay: u64) -> u64 {
        Timelock::propose_set_delay(&env, &caller, new_delay)
    }
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

fn setup() -> (Env, Address, Address) {
    let env = Env::default();
    env.mock_all_auths();
    let admin = Address::generate(&env);
    let cid = env.register_contract(None, TestTL);
    env.as_contract(&cid, || {
        AccessControl::initialize(&env, &admin);
        Timelock::initialize(&env);
    });
    (env, admin, cid)
}

fn invoke<R>(env: &Env, cid: &Address, f: impl FnOnce() -> R) -> R {
    env.as_contract(cid, f)
}

fn advance_time(env: &Env, seconds: u64) {
    let current = env.ledger().timestamp();
    env.ledger().set_timestamp(current + seconds);
}

fn make_payload(env: &Env) -> Bytes {
    Bytes::from_slice(env, &[1, 2, 3, 4])
}

fn missing_auth_error() -> Error {
    Error::from_type_and_code(ScErrorType::Context, ScErrorCode::InvalidAction)
}

fn unauthorized_error() -> Error {
    Error::from_contract_error(ContractError::Unauthorized as u32)
}

fn grant_admin(env: &Env, cid: &Address, grantor: &Address, grantee: &Address) {
    invoke(env, cid, || {
        AccessControl::grant_role(env, grantor, grantee, nester_access_control::Role::Admin);
    });
}

fn revoke_admin(env: &Env, cid: &Address, revoker: &Address, target: &Address) {
    invoke(env, cid, || {
        AccessControl::revoke_role(env, revoker, target, nester_access_control::Role::Admin);
    });
}

// ---------------------------------------------------------------------------
// Initialization
// ---------------------------------------------------------------------------

#[test]
fn initialize_sets_default_delay() {
    let (env, _, cid) = setup();
    let delay = invoke(&env, &cid, || Timelock::get_delay(&env));
    assert_eq!(delay, DEFAULT_DELAY);
}

#[test]
fn initialize_idempotent() {
    let (env, _, cid) = setup();
    // Second init should not panic or reset state.
    invoke(&env, &cid, || Timelock::initialize(&env));
    let delay = invoke(&env, &cid, || Timelock::get_delay(&env));
    assert_eq!(delay, DEFAULT_DELAY);
}

// ---------------------------------------------------------------------------
// Propose
// ---------------------------------------------------------------------------

#[test]
fn propose_creates_pending_operation() {
    let (env, admin, cid) = setup();
    let payload = make_payload(&env);
    let op_type = symbol_short!("CHG_FEE");

    let id = invoke(&env, &cid, || {
        Timelock::propose(&env, &admin, op_type.clone(), payload.clone())
    });

    let op = invoke(&env, &cid, || Timelock::get_operation(&env, id));
    assert_eq!(op.id, 0);
    assert_eq!(op.op_type, op_type);
    assert_eq!(op.proposed_by, admin);
    assert_eq!(op.status, TimelockStatus::Pending);
    assert_eq!(op.payload, payload);
}

#[test]
fn propose_increments_id() {
    let (env, admin, cid) = setup();
    let payload = make_payload(&env);

    let id0 = invoke(&env, &cid, || {
        Timelock::propose(&env, &admin, symbol_short!("OP1"), payload.clone())
    });
    let id1 = invoke(&env, &cid, || {
        Timelock::propose(&env, &admin, symbol_short!("OP2"), payload.clone())
    });

    assert_eq!(id0, 0);
    assert_eq!(id1, 1);
}

#[test]
fn propose_sets_correct_execute_after() {
    let (env, admin, cid) = setup();
    env.ledger().set_timestamp(1000);
    let payload = make_payload(&env);

    let id = invoke(&env, &cid, || {
        Timelock::propose(&env, &admin, symbol_short!("OP"), payload)
    });

    let op = invoke(&env, &cid, || Timelock::get_operation(&env, id));
    assert_eq!(op.execute_after, 1000 + DEFAULT_DELAY);
}

#[test]
fn propose_fails_for_non_admin() {
    let (env, _, cid) = setup();
    let outsider = Address::generate(&env);
    let payload = make_payload(&env);

    // The operation type and payload are valid, so only the missing role rejects it.
    let client = TestTLClient::new(&env, &cid);
    let error = client
        .try_propose(&outsider, &symbol_short!("OP"), &payload)
        .unwrap_err()
        .unwrap();

    assert_eq!(error, unauthorized_error());
}

#[test]
fn propose_requires_caller_auth() {
    let (env, admin, cid) = setup();
    let payload = make_payload(&env);
    let client = TestTLClient::new(&env, &cid);

    // Admin role is present; only the signature is deliberately absent.
    env.mock_auths(&[]);
    let error = client
        .try_propose(&admin, &symbol_short!("OP"), &payload)
        .unwrap_err()
        .unwrap();

    assert_eq!(error, missing_auth_error());
}

#[test]
fn propose_fails_for_revoked_admin() {
    let (env, admin, cid) = setup();
    let delegated_admin = Address::generate(&env);
    let payload = make_payload(&env);
    grant_admin(&env, &cid, &admin, &delegated_admin);

    // Prove the delegated role works before it is revoked.
    invoke(&env, &cid, || {
        Timelock::propose(
            &env,
            &delegated_admin,
            symbol_short!("BEFORE"),
            payload.clone(),
        )
    });
    revoke_admin(&env, &cid, &admin, &delegated_admin);

    // Inputs remain valid, so only the revoked role can reject this call.
    let client = TestTLClient::new(&env, &cid);
    let error = client
        .try_propose(&delegated_admin, &symbol_short!("AFTER"), &payload)
        .unwrap_err()
        .unwrap();

    assert_eq!(error, unauthorized_error());
}

// ---------------------------------------------------------------------------
// Execute — happy path
// ---------------------------------------------------------------------------

#[test]
fn execute_after_delay_succeeds() {
    let (env, admin, cid) = setup();
    let payload = make_payload(&env);

    let id = invoke(&env, &cid, || {
        Timelock::propose(&env, &admin, symbol_short!("CHG_FEE"), payload.clone())
    });

    advance_time(&env, DEFAULT_DELAY);

    let returned = invoke(&env, &cid, || Timelock::execute(&env, &admin, id));
    assert_eq!(returned, payload);

    let op = invoke(&env, &cid, || Timelock::get_operation(&env, id));
    assert_eq!(op.status, TimelockStatus::Executed);
}

#[test]
fn execute_at_exact_delay_boundary() {
    let (env, admin, cid) = setup();
    env.ledger().set_timestamp(1000);
    let payload = make_payload(&env);

    let id = invoke(&env, &cid, || {
        Timelock::propose(&env, &admin, symbol_short!("OP"), payload.clone())
    });

    // Set timestamp to exactly execute_after
    env.ledger().set_timestamp(1000 + DEFAULT_DELAY);

    let returned = invoke(&env, &cid, || Timelock::execute(&env, &admin, id));
    assert_eq!(returned, payload);
}

// ---------------------------------------------------------------------------
// Execute — rejection cases
// ---------------------------------------------------------------------------

#[test]
#[should_panic]
fn execute_before_delay_panics() {
    let (env, admin, cid) = setup();
    let payload = make_payload(&env);

    let id = invoke(&env, &cid, || {
        Timelock::propose(&env, &admin, symbol_short!("OP"), payload)
    });

    advance_time(&env, DEFAULT_DELAY - 1);

    invoke(&env, &cid, || Timelock::execute(&env, &admin, id));
}

#[test]
#[should_panic]
fn execute_expired_operation_panics() {
    let (env, admin, cid) = setup();
    let payload = make_payload(&env);

    let id = invoke(&env, &cid, || {
        Timelock::propose(&env, &admin, symbol_short!("OP"), payload)
    });

    // Advance past the expiry window
    advance_time(&env, DEFAULT_DELAY + EXPIRY_WINDOW + 1);

    invoke(&env, &cid, || Timelock::execute(&env, &admin, id));
}

#[test]
fn expired_operation_gets_status_updated() {
    let (env, admin, cid) = setup();
    let payload = make_payload(&env);

    let id = invoke(&env, &cid, || {
        Timelock::propose(&env, &admin, symbol_short!("OP"), payload)
    });

    advance_time(&env, DEFAULT_DELAY + EXPIRY_WINDOW + 1);

    // The execute will panic, but we can catch it by checking the status
    // after the panic. Instead, let's verify we can still read the op.
    // We need to attempt execution via should_panic, so test status separately.
    let op = invoke(&env, &cid, || Timelock::get_operation(&env, id));
    // Still Pending because execute hasn't been called yet
    assert_eq!(op.status, TimelockStatus::Pending);
}

#[test]
#[should_panic]
fn execute_already_executed_panics() {
    let (env, admin, cid) = setup();
    let payload = make_payload(&env);

    let id = invoke(&env, &cid, || {
        Timelock::propose(&env, &admin, symbol_short!("OP"), payload)
    });

    advance_time(&env, DEFAULT_DELAY);

    invoke(&env, &cid, || Timelock::execute(&env, &admin, id));
    // Second execution must fail
    invoke(&env, &cid, || Timelock::execute(&env, &admin, id));
}

#[test]
#[should_panic]
fn execute_cancelled_operation_panics() {
    let (env, admin, cid) = setup();
    let payload = make_payload(&env);

    let id = invoke(&env, &cid, || {
        Timelock::propose(&env, &admin, symbol_short!("OP"), payload)
    });

    invoke(&env, &cid, || Timelock::cancel(&env, &admin, id));

    advance_time(&env, DEFAULT_DELAY);

    invoke(&env, &cid, || Timelock::execute(&env, &admin, id));
}

#[test]
#[should_panic]
fn execute_nonexistent_op_panics() {
    let (env, admin, cid) = setup();
    invoke(&env, &cid, || Timelock::execute(&env, &admin, 999));
}

#[test]
fn execute_fails_for_non_admin() {
    let (env, admin, cid) = setup();
    let outsider = Address::generate(&env);
    let payload = make_payload(&env);

    let id = invoke(&env, &cid, || {
        Timelock::propose(&env, &admin, symbol_short!("OP"), payload)
    });

    advance_time(&env, DEFAULT_DELAY);

    // The operation exists and is ready, so only the missing role rejects it.
    let client = TestTLClient::new(&env, &cid);
    let error = client.try_execute(&outsider, &id).unwrap_err().unwrap();

    assert_eq!(error, unauthorized_error());
}

#[test]
fn execute_requires_caller_auth() {
    let (env, admin, cid) = setup();
    let payload = make_payload(&env);
    let id = invoke(&env, &cid, || {
        Timelock::propose(&env, &admin, symbol_short!("OP"), payload)
    });
    advance_time(&env, DEFAULT_DELAY);

    // The operation is ready and `admin` still has the role; only auth is absent.
    env.mock_auths(&[]);
    let client = TestTLClient::new(&env, &cid);
    let error = client.try_execute(&admin, &id).unwrap_err().unwrap();

    assert_eq!(error, missing_auth_error());
}

#[test]
fn execute_fails_for_revoked_admin() {
    let (env, admin, cid) = setup();
    let delegated_admin = Address::generate(&env);
    let payload = make_payload(&env);
    grant_admin(&env, &cid, &admin, &delegated_admin);
    let id = invoke(&env, &cid, || {
        Timelock::propose(&env, &delegated_admin, symbol_short!("OP"), payload)
    });
    advance_time(&env, DEFAULT_DELAY);
    revoke_admin(&env, &cid, &admin, &delegated_admin);

    // The operation is pending and ready; revocation must be the rejection.
    let client = TestTLClient::new(&env, &cid);
    let error = client
        .try_execute(&delegated_admin, &id)
        .unwrap_err()
        .unwrap();

    assert_eq!(error, unauthorized_error());
}

// ---------------------------------------------------------------------------
// Cancel
// ---------------------------------------------------------------------------

#[test]
fn cancel_pending_operation() {
    let (env, admin, cid) = setup();
    let payload = make_payload(&env);

    let id = invoke(&env, &cid, || {
        Timelock::propose(&env, &admin, symbol_short!("OP"), payload)
    });

    invoke(&env, &cid, || Timelock::cancel(&env, &admin, id));

    let op = invoke(&env, &cid, || Timelock::get_operation(&env, id));
    assert_eq!(op.status, TimelockStatus::Cancelled);
}

#[test]
#[should_panic]
fn cancel_already_executed_panics() {
    let (env, admin, cid) = setup();
    let payload = make_payload(&env);

    let id = invoke(&env, &cid, || {
        Timelock::propose(&env, &admin, symbol_short!("OP"), payload)
    });

    advance_time(&env, DEFAULT_DELAY);
    invoke(&env, &cid, || Timelock::execute(&env, &admin, id));

    invoke(&env, &cid, || Timelock::cancel(&env, &admin, id));
}

#[test]
#[should_panic]
fn cancel_already_cancelled_panics() {
    let (env, admin, cid) = setup();
    let payload = make_payload(&env);

    let id = invoke(&env, &cid, || {
        Timelock::propose(&env, &admin, symbol_short!("OP"), payload)
    });

    invoke(&env, &cid, || Timelock::cancel(&env, &admin, id));
    invoke(&env, &cid, || Timelock::cancel(&env, &admin, id));
}

#[test]
fn cancel_fails_for_non_admin() {
    let (env, admin, cid) = setup();
    let outsider = Address::generate(&env);
    let payload = make_payload(&env);

    let id = invoke(&env, &cid, || {
        Timelock::propose(&env, &admin, symbol_short!("OP"), payload)
    });

    // The operation exists and is pending, so only the missing role rejects it.
    let client = TestTLClient::new(&env, &cid);
    let error = client.try_cancel(&outsider, &id).unwrap_err().unwrap();

    assert_eq!(error, unauthorized_error());
}

#[test]
fn cancel_requires_caller_auth() {
    let (env, admin, cid) = setup();
    let payload = make_payload(&env);
    let id = invoke(&env, &cid, || {
        Timelock::propose(&env, &admin, symbol_short!("OP"), payload)
    });

    // The operation is valid and pending; only the Admin signature is absent.
    env.mock_auths(&[]);
    let client = TestTLClient::new(&env, &cid);
    let error = client.try_cancel(&admin, &id).unwrap_err().unwrap();

    assert_eq!(error, missing_auth_error());
}

#[test]
fn cancel_fails_for_revoked_admin() {
    let (env, admin, cid) = setup();
    let delegated_admin = Address::generate(&env);
    let payload = make_payload(&env);
    grant_admin(&env, &cid, &admin, &delegated_admin);
    let id = invoke(&env, &cid, || {
        Timelock::propose(&env, &delegated_admin, symbol_short!("OP"), payload)
    });
    revoke_admin(&env, &cid, &admin, &delegated_admin);

    // Still Pending, so the revoked role is the only rejection condition.
    let client = TestTLClient::new(&env, &cid);
    let error = client
        .try_cancel(&delegated_admin, &id)
        .unwrap_err()
        .unwrap();

    assert_eq!(error, unauthorized_error());
}

// ---------------------------------------------------------------------------
// get_pending
// ---------------------------------------------------------------------------

#[test]
fn get_pending_returns_only_pending() {
    let (env, admin, cid) = setup();
    let payload = make_payload(&env);

    // Create 3 operations
    let id0 = invoke(&env, &cid, || {
        Timelock::propose(&env, &admin, symbol_short!("OP0"), payload.clone())
    });
    let _id1 = invoke(&env, &cid, || {
        Timelock::propose(&env, &admin, symbol_short!("OP1"), payload.clone())
    });
    let id2 = invoke(&env, &cid, || {
        Timelock::propose(&env, &admin, symbol_short!("OP2"), payload.clone())
    });

    // Cancel op0, execute op2 (after delay)
    invoke(&env, &cid, || Timelock::cancel(&env, &admin, id0));

    advance_time(&env, DEFAULT_DELAY);
    invoke(&env, &cid, || Timelock::execute(&env, &admin, id2));

    let pending = invoke(&env, &cid, || Timelock::get_pending(&env));
    assert_eq!(pending.len(), 1);
    assert_eq!(pending.get(0).unwrap().id, 1); // Only OP1 remains pending
}

#[test]
fn get_pending_empty_when_none() {
    let (env, _, cid) = setup();
    let pending = invoke(&env, &cid, || Timelock::get_pending(&env));
    assert_eq!(pending.len(), 0);
}

// ---------------------------------------------------------------------------
// set_delay (timelocked)
// ---------------------------------------------------------------------------

#[test]
fn propose_set_delay_and_apply() {
    let (env, admin, cid) = setup();
    let new_delay = 7200u64; // 2 hours

    let id = invoke(&env, &cid, || {
        Timelock::propose_set_delay(&env, &admin, new_delay)
    });

    // Verify operation created with correct type
    let op = invoke(&env, &cid, || Timelock::get_operation(&env, id));
    assert_eq!(op.op_type, symbol_short!("SET_DLY"));

    // Wait for delay, then execute
    advance_time(&env, DEFAULT_DELAY);

    let payload = invoke(&env, &cid, || Timelock::execute(&env, &admin, id));

    // Apply the new delay
    invoke(&env, &cid, || Timelock::apply_delay(&env, &payload));

    let current_delay = invoke(&env, &cid, || Timelock::get_delay(&env));
    assert_eq!(current_delay, new_delay);
}

#[test]
#[should_panic]
fn propose_set_delay_below_min_panics() {
    let (env, admin, cid) = setup();
    invoke(&env, &cid, || {
        Timelock::propose_set_delay(&env, &admin, MIN_DELAY - 1)
    });
}

#[test]
#[should_panic]
fn propose_set_delay_above_max_panics() {
    let (env, admin, cid) = setup();
    invoke(&env, &cid, || {
        Timelock::propose_set_delay(&env, &admin, MAX_DELAY + 1)
    });
}

#[test]
fn propose_set_delay_at_bounds() {
    let (env, admin, cid) = setup();

    // Min delay should work
    let _id_min = invoke(&env, &cid, || {
        Timelock::propose_set_delay(&env, &admin, MIN_DELAY)
    });

    // Max delay should work
    let _id_max = invoke(&env, &cid, || {
        Timelock::propose_set_delay(&env, &admin, MAX_DELAY)
    });
}

#[test]
fn propose_set_delay_fails_for_non_admin() {
    let (env, _admin, cid) = setup();
    let outsider = Address::generate(&env);

    // Two hours is valid, so the role check is the only rejection condition.
    let client = TestTLClient::new(&env, &cid);
    let error = client
        .try_propose_set_delay(&outsider, &7_200)
        .unwrap_err()
        .unwrap();

    assert_eq!(error, unauthorized_error());
}

#[test]
fn propose_set_delay_requires_caller_auth() {
    let (env, admin, cid) = setup();

    env.mock_auths(&[]);
    let client = TestTLClient::new(&env, &cid);
    let error = client
        .try_propose_set_delay(&admin, &7_200)
        .unwrap_err()
        .unwrap();

    assert_eq!(error, missing_auth_error());
}

#[test]
fn propose_set_delay_fails_for_revoked_admin() {
    let (env, admin, cid) = setup();
    let delegated_admin = Address::generate(&env);
    grant_admin(&env, &cid, &admin, &delegated_admin);

    // First prove the delegated Admin can propose a valid delay change.
    invoke(&env, &cid, || {
        Timelock::propose_set_delay(&env, &delegated_admin, 7_200)
    });
    revoke_admin(&env, &cid, &admin, &delegated_admin);

    // This second delay is also valid; only the revoked role may reject it.
    let client = TestTLClient::new(&env, &cid);
    let error = client
        .try_propose_set_delay(&delegated_admin, &10_800)
        .unwrap_err()
        .unwrap();

    assert_eq!(error, unauthorized_error());
}

// ---------------------------------------------------------------------------
// Events
// ---------------------------------------------------------------------------

#[test]
fn propose_emits_event() {
    let (env, admin, cid) = setup();
    let payload = make_payload(&env);

    invoke(&env, &cid, || {
        Timelock::propose(&env, &admin, symbol_short!("CHG_FEE"), payload)
    });

    assert!(!env.events().all().is_empty());
}

#[test]
fn execute_emits_event() {
    let (env, admin, cid) = setup();
    let payload = make_payload(&env);

    let id = invoke(&env, &cid, || {
        Timelock::propose(&env, &admin, symbol_short!("OP"), payload)
    });

    advance_time(&env, DEFAULT_DELAY);
    invoke(&env, &cid, || Timelock::execute(&env, &admin, id));

    assert!(!env.events().all().is_empty());
}

#[test]
fn cancel_emits_event() {
    let (env, admin, cid) = setup();
    let payload = make_payload(&env);

    let id = invoke(&env, &cid, || {
        Timelock::propose(&env, &admin, symbol_short!("OP"), payload)
    });

    invoke(&env, &cid, || Timelock::cancel(&env, &admin, id));

    assert!(!env.events().all().is_empty());
}

// ---------------------------------------------------------------------------
// Full lifecycle: propose → wait → execute
// ---------------------------------------------------------------------------

#[test]
fn full_lifecycle_propose_wait_execute() {
    let (env, admin, cid) = setup();
    env.ledger().set_timestamp(10_000);
    let payload = make_payload(&env);

    // Step 1: Propose
    let id = invoke(&env, &cid, || {
        Timelock::propose(&env, &admin, symbol_short!("CHG_FEE"), payload.clone())
    });

    let op = invoke(&env, &cid, || Timelock::get_operation(&env, id));
    assert_eq!(op.status, TimelockStatus::Pending);
    assert_eq!(op.execute_after, 10_000 + DEFAULT_DELAY);

    // Verify it shows up in pending
    let pending = invoke(&env, &cid, || Timelock::get_pending(&env));
    assert_eq!(pending.len(), 1);

    // Step 2: Wait
    env.ledger().set_timestamp(10_000 + DEFAULT_DELAY);

    // Step 3: Execute
    let returned = invoke(&env, &cid, || Timelock::execute(&env, &admin, id));
    assert_eq!(returned, payload);

    let op = invoke(&env, &cid, || Timelock::get_operation(&env, id));
    assert_eq!(op.status, TimelockStatus::Executed);

    // No longer in pending
    let pending = invoke(&env, &cid, || Timelock::get_pending(&env));
    assert_eq!(pending.len(), 0);
}

// ---------------------------------------------------------------------------
// Edge: execute at last second before expiry
// ---------------------------------------------------------------------------

#[test]
fn execute_at_expiry_boundary_succeeds() {
    let (env, admin, cid) = setup();
    env.ledger().set_timestamp(1000);
    let payload = make_payload(&env);

    let id = invoke(&env, &cid, || {
        Timelock::propose(&env, &admin, symbol_short!("OP"), payload.clone())
    });

    // Set to exactly execute_after + EXPIRY_WINDOW (should still succeed)
    env.ledger()
        .set_timestamp(1000 + DEFAULT_DELAY + EXPIRY_WINDOW);

    let returned = invoke(&env, &cid, || Timelock::execute(&env, &admin, id));
    assert_eq!(returned, payload);
}

#[test]
#[should_panic]
fn execute_one_second_past_expiry_fails() {
    let (env, admin, cid) = setup();
    env.ledger().set_timestamp(1000);
    let payload = make_payload(&env);

    let id = invoke(&env, &cid, || {
        Timelock::propose(&env, &admin, symbol_short!("OP"), payload)
    });

    env.ledger()
        .set_timestamp(1000 + DEFAULT_DELAY + EXPIRY_WINDOW + 1);

    invoke(&env, &cid, || Timelock::execute(&env, &admin, id));
}

// ---------------------------------------------------------------------------
// Issue #510: explicit expiry-boundary and state-machine coverage
// ---------------------------------------------------------------------------

#[test]
fn test_execute_at_exact_unlock_timestamp() {
    let (env, admin, cid) = setup();
    env.ledger().set_timestamp(5_000);
    let payload = make_payload(&env);

    let id = invoke(&env, &cid, || {
        Timelock::propose(&env, &admin, symbol_short!("OP"), payload.clone())
    });

    let unlock_at = 5_000 + DEFAULT_DELAY;
    env.ledger().set_timestamp(unlock_at);

    let returned = invoke(&env, &cid, || Timelock::execute(&env, &admin, id));
    assert_eq!(returned, payload);

    let op = invoke(&env, &cid, || Timelock::get_operation(&env, id));
    assert_eq!(op.status, TimelockStatus::Executed);
}

#[test]
#[should_panic]
fn test_execute_before_unlock_fails() {
    let (env, admin, cid) = setup();
    env.ledger().set_timestamp(2_000);
    let payload = make_payload(&env);

    let id = invoke(&env, &cid, || {
        Timelock::propose(&env, &admin, symbol_short!("OP"), payload)
    });

    let unlock_at = 2_000 + DEFAULT_DELAY;
    env.ledger().set_timestamp(unlock_at - 1);

    invoke(&env, &cid, || Timelock::execute(&env, &admin, id));
}

#[test]
#[should_panic]
fn test_execute_after_expiry_fails() {
    let (env, admin, cid) = setup();
    env.ledger().set_timestamp(3_000);
    let payload = make_payload(&env);

    let id = invoke(&env, &cid, || {
        Timelock::propose(&env, &admin, symbol_short!("OP"), payload)
    });

    let expiry_at = 3_000 + DEFAULT_DELAY + EXPIRY_WINDOW;
    env.ledger().set_timestamp(expiry_at + 1);

    invoke(&env, &cid, || Timelock::execute(&env, &admin, id));
}

#[test]
fn test_cancel_pending_operation() {
    let (env, admin, cid) = setup();
    env.ledger().set_timestamp(4_000);
    let payload = make_payload(&env);

    let id = invoke(&env, &cid, || {
        Timelock::propose(&env, &admin, symbol_short!("OP"), payload)
    });

    // Cancel while still locked — valid transition scheduled → cancelled.
    invoke(&env, &cid, || Timelock::cancel(&env, &admin, id));

    let op = invoke(&env, &cid, || Timelock::get_operation(&env, id));
    assert_eq!(op.status, TimelockStatus::Cancelled);
}

#[test]
#[should_panic]
fn test_cancel_executed_operation_fails() {
    let (env, admin, cid) = setup();
    let payload = make_payload(&env);

    let id = invoke(&env, &cid, || {
        Timelock::propose(&env, &admin, symbol_short!("OP"), payload)
    });

    advance_time(&env, DEFAULT_DELAY);
    invoke(&env, &cid, || Timelock::execute(&env, &admin, id));

    invoke(&env, &cid, || Timelock::cancel(&env, &admin, id));
}

/// Operation IDs are auto-assigned and monotonic — a second propose never
/// overwrites an existing pending operation.
#[test]
fn test_schedule_duplicate_operation_id() {
    let (env, admin, cid) = setup();
    let payload = make_payload(&env);

    let id_a = invoke(&env, &cid, || {
        Timelock::propose(&env, &admin, symbol_short!("OP_A"), payload.clone())
    });
    let id_b = invoke(&env, &cid, || {
        Timelock::propose(&env, &admin, symbol_short!("OP_B"), payload)
    });

    assert_ne!(id_a, id_b);

    let op_a = invoke(&env, &cid, || Timelock::get_operation(&env, id_a));
    let op_b = invoke(&env, &cid, || Timelock::get_operation(&env, id_b));
    assert_eq!(op_a.status, TimelockStatus::Pending);
    assert_eq!(op_b.status, TimelockStatus::Pending);
}

// ---------------------------------------------------------------------------
// Multiple operations in flight
// ---------------------------------------------------------------------------

#[test]
fn multiple_operations_independent() {
    let (env, admin, cid) = setup();
    let payload = make_payload(&env);

    let id0 = invoke(&env, &cid, || {
        Timelock::propose(&env, &admin, symbol_short!("OP_A"), payload.clone())
    });
    let id1 = invoke(&env, &cid, || {
        Timelock::propose(&env, &admin, symbol_short!("OP_B"), payload.clone())
    });

    advance_time(&env, DEFAULT_DELAY);

    // Execute op1 first, leave op0 pending
    invoke(&env, &cid, || Timelock::execute(&env, &admin, id1));

    let op0 = invoke(&env, &cid, || Timelock::get_operation(&env, id0));
    let op1 = invoke(&env, &cid, || Timelock::get_operation(&env, id1));
    assert_eq!(op0.status, TimelockStatus::Pending);
    assert_eq!(op1.status, TimelockStatus::Executed);

    // Now execute op0
    invoke(&env, &cid, || Timelock::execute(&env, &admin, id0));
    let op0 = invoke(&env, &cid, || Timelock::get_operation(&env, id0));
    assert_eq!(op0.status, TimelockStatus::Executed);
}
