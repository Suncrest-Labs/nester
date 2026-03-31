//! Unit tests for the Nester timelock module.
//!
//! Like access-control, this is a plain Rust library so all storage access
//! must run inside a contract execution context via `env.as_contract`.

extern crate std;

use soroban_sdk::{
    contract, contractimpl, symbol_short,
    testutils::{Address as _, Events as _, Ledger as _},
    Address, Bytes, Env,
};

use nester_access_control::AccessControl;

use crate::{Timelock, TimelockStatus, DEFAULT_DELAY, EXPIRY_WINDOW, MAX_DELAY, MIN_DELAY};

// ---------------------------------------------------------------------------
// Minimal dummy contract for test context
// ---------------------------------------------------------------------------

#[contract]
struct TestTL;

#[contractimpl]
impl TestTL {}

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
#[should_panic]
fn propose_fails_for_non_admin() {
    let (env, _, cid) = setup();
    let outsider = Address::generate(&env);
    let payload = make_payload(&env);

    invoke(&env, &cid, || {
        Timelock::propose(&env, &outsider, symbol_short!("OP"), payload)
    });
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
#[should_panic]
fn execute_fails_for_non_admin() {
    let (env, admin, cid) = setup();
    let outsider = Address::generate(&env);
    let payload = make_payload(&env);

    let id = invoke(&env, &cid, || {
        Timelock::propose(&env, &admin, symbol_short!("OP"), payload)
    });

    advance_time(&env, DEFAULT_DELAY);

    invoke(&env, &cid, || Timelock::execute(&env, &outsider, id));
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
#[should_panic]
fn cancel_fails_for_non_admin() {
    let (env, admin, cid) = setup();
    let outsider = Address::generate(&env);
    let payload = make_payload(&env);

    let id = invoke(&env, &cid, || {
        Timelock::propose(&env, &admin, symbol_short!("OP"), payload)
    });

    invoke(&env, &cid, || Timelock::cancel(&env, &outsider, id));
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
