//! Unit tests for the Nester access-control module.
//!
//! Because this module is a plain Rust library, all storage access must run
//! inside a contract execution context via `env.as_contract(&cid, || { … })`.
//!
//! Soroban also enforces that `require_auth` for the same address may only be
//! called ONCE per contract invocation frame.  Tests that perform multiple
//! mutating operations with the same auth subject therefore use separate
//! `as_contract` blocks — each block is a fresh frame.
//!
//! Read-only calls (`has_role`, `require_role`) do not call `require_auth` and
//! can be freely mixed inside any frame.

extern crate std;

use soroban_sdk::{contract, contractimpl, testutils::Address as _, Address, Env};

use crate::{AccessControl, Role};

// ---------------------------------------------------------------------------
// Minimal dummy contract — provides a stable contract ID so we can enter a
// contract execution context via `env.as_contract`.
// ---------------------------------------------------------------------------

#[contract]
struct TestAC;

#[contractimpl]
impl TestAC {}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/// Create a default env with all auths mocked, register the dummy contract,
/// initialise access control within it, and return
/// `(env, admin, other, contract_id)`.
fn setup() -> (Env, Address, Address, Address) {
    let env = Env::default();
    env.mock_all_auths();
    let admin = Address::generate(&env);
    let other = Address::generate(&env);
    let cid = env.register_contract(None, TestAC);
    env.as_contract(&cid, || AccessControl::initialize(&env, &admin));
    (env, admin, other, cid)
}

/// Run a read-only closure inside the dummy contract context.
fn read<R>(env: &Env, cid: &Address, f: impl FnOnce() -> R) -> R {
    env.as_contract(cid, f)
}

/// Run a single mutating operation in its own contract frame.
/// Each call to this function is an independent invocation, so
/// `require_auth` for any address is fresh.
fn invoke(env: &Env, cid: &Address, f: impl FnOnce()) {
    env.as_contract(cid, f)
}

// ---------------------------------------------------------------------------
// Initialisation
// ---------------------------------------------------------------------------

#[test]
fn initialize_grants_admin_role() {
    let (env, admin, _, cid) = setup();
    assert!(read(&env, &cid, || AccessControl::has_role(
        &env,
        &admin,
        Role::Admin
    )));
}

#[test]
fn initialize_does_not_grant_operator_to_admin() {
    let (env, admin, _, cid) = setup();
    assert!(!read(&env, &cid, || AccessControl::has_role(
        &env,
        &admin,
        Role::Operator
    )));
}

#[test]
#[should_panic]
fn initialize_twice_panics() {
    let (env, admin, _, cid) = setup();
    invoke(&env, &cid, || AccessControl::initialize(&env, &admin));
}

// ---------------------------------------------------------------------------
// has_role — baseline
// ---------------------------------------------------------------------------

#[test]
fn has_role_returns_false_for_uninitialised_address() {
    let env = Env::default();
    env.mock_all_auths();
    let cid = env.register_contract(None, TestAC);
    let stranger = Address::generate(&env);
    assert!(!read(&env, &cid, || AccessControl::has_role(
        &env,
        &stranger,
        Role::Admin
    )));
    assert!(!read(&env, &cid, || AccessControl::has_role(
        &env,
        &stranger,
        Role::Operator
    )));
}

// ---------------------------------------------------------------------------
// grant_role
// ---------------------------------------------------------------------------

#[test]
fn admin_can_grant_operator_role() {
    let (env, admin, operator, cid) = setup();
    invoke(&env, &cid, || {
        AccessControl::grant_role(&env, &admin, &operator, Role::Operator)
    });
    assert!(read(&env, &cid, || AccessControl::has_role(
        &env,
        &operator,
        Role::Operator
    )));
}

#[test]
fn granting_operator_does_not_also_grant_admin() {
    let (env, admin, operator, cid) = setup();
    invoke(&env, &cid, || {
        AccessControl::grant_role(&env, &admin, &operator, Role::Operator)
    });
    assert!(!read(&env, &cid, || AccessControl::has_role(
        &env,
        &operator,
        Role::Admin
    )));
}

#[test]
fn admin_can_grant_admin_role_to_another() {
    let (env, admin, second_admin, cid) = setup();
    invoke(&env, &cid, || {
        AccessControl::grant_role(&env, &admin, &second_admin, Role::Admin)
    });
    assert!(read(&env, &cid, || AccessControl::has_role(
        &env,
        &second_admin,
        Role::Admin
    )));
}

#[test]
#[should_panic]
fn non_admin_cannot_grant_role() {
    let (env, admin, operator, cid) = setup();
    let outsider = Address::generate(&env);
    invoke(&env, &cid, || {
        AccessControl::grant_role(&env, &admin, &operator, Role::Operator)
    });
    // Operator tries to grant roles — must panic (Unauthorized from require_role).
    invoke(&env, &cid, || {
        AccessControl::grant_role(&env, &operator, &outsider, Role::Operator)
    });
}

#[test]
#[should_panic]
fn stranger_cannot_grant_role() {
    let (env, _, _, cid) = setup();
    let stranger = Address::generate(&env);
    let target = Address::generate(&env);
    invoke(&env, &cid, || {
        AccessControl::grant_role(&env, &stranger, &target, Role::Operator)
    });
}

#[test]
fn granting_existing_role_is_idempotent() {
    let (env, admin, operator, cid) = setup();
    invoke(&env, &cid, || {
        AccessControl::grant_role(&env, &admin, &operator, Role::Operator)
    });
    // Second grant must not panic.
    invoke(&env, &cid, || {
        AccessControl::grant_role(&env, &admin, &operator, Role::Operator)
    });
    assert!(read(&env, &cid, || AccessControl::has_role(
        &env,
        &operator,
        Role::Operator
    )));
}

// ---------------------------------------------------------------------------
// revoke_role
// ---------------------------------------------------------------------------

#[test]
fn admin_can_revoke_operator_role() {
    let (env, admin, operator, cid) = setup();
    invoke(&env, &cid, || {
        AccessControl::grant_role(&env, &admin, &operator, Role::Operator)
    });
    assert!(read(&env, &cid, || AccessControl::has_role(
        &env,
        &operator,
        Role::Operator
    )));

    // Separate frame so admin.require_auth() is fresh.
    invoke(&env, &cid, || {
        AccessControl::revoke_role(&env, &admin, &operator, Role::Operator)
    });
    assert!(!read(&env, &cid, || AccessControl::has_role(
        &env,
        &operator,
        Role::Operator
    )));
}

#[test]
fn revoking_absent_role_is_idempotent() {
    let (env, admin, other, cid) = setup();

    invoke(&env, &cid, || {
        AccessControl::revoke_role(&env, &admin, &other, Role::Operator)
    });

    assert!(!read(&env, &cid, || AccessControl::has_role(
        &env,
        &other,
        Role::Operator
    )));
}

#[test]
#[should_panic]
fn non_admin_cannot_revoke_role() {
    let (env, admin, operator, cid) = setup();
    invoke(&env, &cid, || {
        AccessControl::grant_role(&env, &admin, &operator, Role::Operator)
    });
    invoke(&env, &cid, || {
        AccessControl::revoke_role(&env, &operator, &admin, Role::Admin)
    });
}

#[test]
fn admin_can_revoke_another_admin_when_multiple_admins_exist() {
    let (env, admin, second_admin, cid) = setup();
    invoke(&env, &cid, || {
        AccessControl::grant_role(&env, &admin, &second_admin, Role::Admin)
    });

    invoke(&env, &cid, || {
        AccessControl::revoke_role(&env, &admin, &second_admin, Role::Admin)
    });
    assert!(!read(&env, &cid, || AccessControl::has_role(
        &env,
        &second_admin,
        Role::Admin
    )));
    assert!(read(&env, &cid, || AccessControl::has_role(
        &env,
        &admin,
        Role::Admin
    )));
}

#[test]
#[should_panic]
fn revoking_last_admin_panics() {
    let (env, admin, _, cid) = setup();
    invoke(&env, &cid, || {
        AccessControl::revoke_role(&env, &admin, &admin, Role::Admin)
    });
}

// ---------------------------------------------------------------------------
// require_role
// ---------------------------------------------------------------------------

#[test]
fn require_role_passes_for_authorised_account() {
    let (env, admin, operator, cid) = setup();
    invoke(&env, &cid, || {
        AccessControl::grant_role(&env, &admin, &operator, Role::Operator)
    });
    // require_role is read-only — no require_auth — safe in same frame.
    read(&env, &cid, || {
        AccessControl::require_role(&env, &admin, Role::Admin);
        AccessControl::require_role(&env, &operator, Role::Operator);
    });
}

#[test]
#[should_panic]
fn require_role_panics_when_role_is_absent() {
    let (env, _, other, cid) = setup();
    read(&env, &cid, || {
        AccessControl::require_role(&env, &other, Role::Admin)
    });
}

#[test]
#[should_panic]
fn require_admin_panics_for_operator() {
    let (env, admin, operator, cid) = setup();
    invoke(&env, &cid, || {
        AccessControl::grant_role(&env, &admin, &operator, Role::Operator)
    });
    read(&env, &cid, || {
        AccessControl::require_role(&env, &operator, Role::Admin)
    });
}

// ---------------------------------------------------------------------------
// Two-step admin transfer
// ---------------------------------------------------------------------------

#[test]
fn transfer_admin_two_step_happy_path() {
    let (env, admin, new_admin, cid) = setup();

    // Step 1: current admin proposes.  Different auth subjects in the same
    // frame would be fine, but we use separate frames for clarity.
    invoke(&env, &cid, || {
        AccessControl::transfer_admin(&env, &admin, &new_admin)
    });

    // After proposal: old admin still holds Admin, new one does not.
    assert!(read(&env, &cid, || AccessControl::has_role(
        &env,
        &admin,
        Role::Admin
    )));
    assert!(!read(&env, &cid, || AccessControl::has_role(
        &env,
        &new_admin,
        Role::Admin
    )));

    // Step 2: new admin accepts (different auth subject from step 1).
    invoke(&env, &cid, || AccessControl::accept_admin(&env, &new_admin));

    // After acceptance: new admin holds Admin, old does not.
    assert!(read(&env, &cid, || AccessControl::has_role(
        &env,
        &new_admin,
        Role::Admin
    )));
    assert!(!read(&env, &cid, || AccessControl::has_role(
        &env,
        &admin,
        Role::Admin
    )));
}

#[test]
#[should_panic]
fn wrong_address_cannot_accept_admin() {
    let (env, admin, new_admin, cid) = setup();
    let imposter = Address::generate(&env);
    invoke(&env, &cid, || {
        AccessControl::transfer_admin(&env, &admin, &new_admin)
    });
    invoke(&env, &cid, || AccessControl::accept_admin(&env, &imposter));
}

#[test]
#[should_panic]
fn accept_admin_without_proposal_panics() {
    let (env, _, other, cid) = setup();
    invoke(&env, &cid, || AccessControl::accept_admin(&env, &other));
}

#[test]
#[should_panic]
fn non_admin_cannot_propose_admin_transfer() {
    let (env, _, other, cid) = setup();
    let target = Address::generate(&env);
    invoke(&env, &cid, || {
        AccessControl::transfer_admin(&env, &other, &target)
    });
}

#[test]
fn admin_count_is_consistent_after_full_transfer() {
    let (env, admin, new_admin, cid) = setup();
    let third = Address::generate(&env);

    // Two admins: admin + third.
    invoke(&env, &cid, || {
        AccessControl::grant_role(&env, &admin, &third, Role::Admin)
    });

    // Transfer admin → new_admin.
    invoke(&env, &cid, || {
        AccessControl::transfer_admin(&env, &admin, &new_admin)
    });
    invoke(&env, &cid, || AccessControl::accept_admin(&env, &new_admin));

    assert!(read(&env, &cid, || AccessControl::has_role(
        &env,
        &new_admin,
        Role::Admin
    )));
    assert!(read(&env, &cid, || AccessControl::has_role(
        &env,
        &third,
        Role::Admin
    )));
    assert!(!read(&env, &cid, || AccessControl::has_role(
        &env,
        &admin,
        Role::Admin
    )));

    // Revoke third (2 admins → 1, safe).
    invoke(&env, &cid, || {
        AccessControl::revoke_role(&env, &new_admin, &third, Role::Admin)
    });
    assert!(!read(&env, &cid, || AccessControl::has_role(
        &env,
        &third,
        Role::Admin
    )));
}

// ---------------------------------------------------------------------------
// Operator role restrictions
// ---------------------------------------------------------------------------

#[test]
#[should_panic]
fn operator_cannot_grant_roles() {
    let (env, admin, operator, cid) = setup();
    let target = Address::generate(&env);
    invoke(&env, &cid, || {
        AccessControl::grant_role(&env, &admin, &operator, Role::Operator)
    });
    invoke(&env, &cid, || {
        AccessControl::grant_role(&env, &operator, &target, Role::Operator)
    });
}

#[test]
#[should_panic]
fn operator_cannot_revoke_roles() {
    let (env, admin, operator, cid) = setup();
    invoke(&env, &cid, || {
        AccessControl::grant_role(&env, &admin, &operator, Role::Operator)
    });
    invoke(&env, &cid, || {
        AccessControl::revoke_role(&env, &operator, &admin, Role::Admin)
    });
}
