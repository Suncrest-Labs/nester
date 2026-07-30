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
//!
//! # Negative-authorization matrix
//!
//! | Protected API | Unauthorized / unsigned test | Authorized test | Revoked-role test |
//! | --- | --- | --- | --- |
//! | `initialize` | `initialize_without_admin_signature_panics` | `initialize_grants_admin_role` | n/a |
//! | `grant_role` | `non_admin_cannot_grant_role`, `admin_without_signature_cannot_grant_role` | `admin_can_grant_operator_role` | `revoked_admin_cannot_grant_role` |
//! | `revoke_role` | `non_admin_cannot_revoke_role`, `admin_without_signature_cannot_revoke_role` | `admin_can_revoke_operator_role` | `revoked_admin_cannot_revoke_role` |
//! | `require_role` | `require_role_panics_when_role_is_absent` | `require_role_passes_for_authorised_account` | `require_role_panics_after_role_is_revoked` |
//! | `transfer_admin` | `non_admin_cannot_propose_admin_transfer`, `admin_without_signature_cannot_transfer_admin` | `transfer_admin_two_step_happy_path` | `revoked_admin_cannot_transfer_admin` |
//! | `accept_admin` | `wrong_address_cannot_accept_admin`, `accept_admin_without_signature_panics` | `transfer_admin_two_step_happy_path` | n/a |
//!
//! Role-negative tests run with mocked signatures so they exercise the role
//! guard specifically. Signature-negative tests clear all mocked auths after
//! setup so deleting a `require_auth` call makes the corresponding test fail.

extern crate std;

use nester_common::ContractError;
use soroban_sdk::{
    contract, contractimpl, testutils::Address as _, testutils::Ledger as _, xdr::ScErrorCode,
    xdr::ScErrorType, Address, Env, Error,
};

use crate::{AccessControl, Role};

// ---------------------------------------------------------------------------
// Minimal test contract — provides a stable contract ID for `as_contract`
// setup and test-only entrypoints whose generated `try_*` client methods let
// negative tests inspect the exact Soroban error from the target call.
// ---------------------------------------------------------------------------

#[contract]
struct TestAC;

#[contractimpl]
impl TestAC {
    pub fn initialize(env: Env, admin: Address) {
        AccessControl::initialize(&env, &admin);
    }

    pub fn grant_role(env: Env, grantor: Address, grantee: Address, role: Role) {
        AccessControl::grant_role(&env, &grantor, &grantee, role);
    }

    pub fn revoke_role(env: Env, revoker: Address, target: Address, role: Role) {
        AccessControl::revoke_role(&env, &revoker, &target, role);
    }

    pub fn require_role(env: Env, account: Address, role: Role) {
        AccessControl::require_role(&env, &account, role);
    }

    pub fn transfer_admin(env: Env, current_admin: Address, new_admin: Address) {
        AccessControl::transfer_admin(&env, &current_admin, &new_admin);
    }

    pub fn accept_admin(env: Env, new_admin: Address) {
        AccessControl::accept_admin(&env, &new_admin);
    }

    pub fn grant_role_until(
        env: Env,
        grantor: Address,
        grantee: Address,
        role: Role,
        expires_at: u64,
    ) {
        AccessControl::grant_role_until(&env, &grantor, &grantee, role, expires_at);
    }

    pub fn transfer_role(env: Env, current_holder: Address, role: Role, new_holder: Address) {
        AccessControl::transfer_role(&env, &current_holder, role, &new_holder);
    }

    pub fn accept_role(env: Env, new_holder: Address, role: Role) {
        AccessControl::accept_role(&env, &new_holder, role);
    }

    pub fn cancel_role_transfer(env: Env, current_holder: Address, role: Role) {
        AccessControl::cancel_role_transfer(&env, &current_holder, role);
    }

    pub fn role_expires_at(env: Env, account: Address, role: Role) -> Option<u64> {
        AccessControl::role_expires_at(&env, &account, role)
    }

    pub fn get_role_members(
        env: Env,
        role: Role,
        start: u32,
        limit: u32,
    ) -> soroban_sdk::Vec<Address> {
        AccessControl::get_role_members(&env, role, start, limit)
    }
}

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

macro_rules! assert_soroban_error {
    ($result:expr, $expected:expr $(,)?) => {
        match $result {
            Err(Ok(error)) => assert_eq!(error, $expected),
            Err(Err(_)) => panic!("expected a typed Soroban error"),
            Ok(_) => panic!("expected the contract invocation to fail"),
        }
    };
}

fn missing_signature_error() -> Error {
    Error::from_type_and_code(ScErrorType::Context, ScErrorCode::InvalidAction)
}

fn unauthorized_role_error() -> Error {
    Error::from_contract_error(ContractError::Unauthorized as u32)
}

/// Set up two admins, revoke the delegated admin again, and return both
/// addresses. This is the canonical fixture for post-revocation checks.
fn setup_with_revoked_admin() -> (Env, Address, Address, Address) {
    let (env, active_admin, revoked_admin, cid) = setup();
    invoke(&env, &cid, || {
        AccessControl::grant_role(&env, &active_admin, &revoked_admin, Role::Admin)
    });
    invoke(&env, &cid, || {
        AccessControl::revoke_role(&env, &active_admin, &revoked_admin, Role::Admin)
    });
    (env, active_admin, revoked_admin, cid)
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

#[test]
fn initialize_without_admin_signature_panics() {
    let env = Env::default();
    env.mock_auths(&[]);
    let admin = Address::generate(&env);
    let cid = env.register_contract(None, TestAC);

    let client = TestACClient::new(&env, &cid);
    assert_soroban_error!(client.try_initialize(&admin), missing_signature_error());
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
fn non_admin_cannot_grant_role() {
    let (env, admin, operator, cid) = setup();
    let outsider = Address::generate(&env);
    invoke(&env, &cid, || {
        AccessControl::grant_role(&env, &admin, &operator, Role::Operator)
    });
    assert!(read(&env, &cid, || AccessControl::has_role(
        &env,
        &operator,
        Role::Operator
    )));
    assert!(!read(&env, &cid, || AccessControl::has_role(
        &env,
        &operator,
        Role::Admin
    )));

    let client = TestACClient::new(&env, &cid);
    assert_soroban_error!(
        client.try_grant_role(&operator, &outsider, &Role::Operator),
        unauthorized_role_error(),
    );
}

#[test]
fn stranger_cannot_grant_role() {
    let (env, _, _, cid) = setup();
    let stranger = Address::generate(&env);
    let target = Address::generate(&env);
    assert!(!read(&env, &cid, || AccessControl::has_role(
        &env,
        &stranger,
        Role::Admin
    )));

    let client = TestACClient::new(&env, &cid);
    assert_soroban_error!(
        client.try_grant_role(&stranger, &target, &Role::Operator),
        unauthorized_role_error(),
    );
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
fn non_admin_cannot_revoke_role() {
    let (env, admin, operator, cid) = setup();
    let target = Address::generate(&env);
    invoke(&env, &cid, || {
        AccessControl::grant_role(&env, &admin, &operator, Role::Operator)
    });
    invoke(&env, &cid, || {
        AccessControl::grant_role(&env, &admin, &target, Role::Operator)
    });
    assert!(!read(&env, &cid, || AccessControl::has_role(
        &env,
        &operator,
        Role::Admin
    )));
    assert!(read(&env, &cid, || AccessControl::has_role(
        &env,
        &target,
        Role::Operator
    )));

    let client = TestACClient::new(&env, &cid);
    assert_soroban_error!(
        client.try_revoke_role(&operator, &target, &Role::Operator),
        unauthorized_role_error(),
    );
}

#[test]
fn double_revoke_operator_role_is_idempotent() {
    let (env, admin, operator, cid) = setup();
    invoke(&env, &cid, || {
        AccessControl::grant_role(&env, &admin, &operator, Role::Operator)
    });
    invoke(&env, &cid, || {
        AccessControl::revoke_role(&env, &admin, &operator, Role::Operator)
    });

    // A second revoke must remain a no-op and must not restore the role.
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
fn require_role_panics_when_role_is_absent() {
    let (env, _, other, cid) = setup();
    assert!(!read(&env, &cid, || AccessControl::has_role(
        &env,
        &other,
        Role::Admin
    )));

    let client = TestACClient::new(&env, &cid);
    assert_soroban_error!(
        client.try_require_role(&other, &Role::Admin),
        unauthorized_role_error(),
    );
}

#[test]
fn require_admin_panics_for_operator() {
    let (env, admin, operator, cid) = setup();
    invoke(&env, &cid, || {
        AccessControl::grant_role(&env, &admin, &operator, Role::Operator)
    });
    assert!(read(&env, &cid, || AccessControl::has_role(
        &env,
        &operator,
        Role::Operator
    )));
    assert!(!read(&env, &cid, || AccessControl::has_role(
        &env,
        &operator,
        Role::Admin
    )));

    let client = TestACClient::new(&env, &cid);
    assert_soroban_error!(
        client.try_require_role(&operator, &Role::Admin),
        unauthorized_role_error(),
    );
}

#[test]
fn require_role_panics_after_role_is_revoked() {
    let (env, admin, operator, cid) = setup();
    invoke(&env, &cid, || {
        AccessControl::grant_role(&env, &admin, &operator, Role::Operator)
    });
    invoke(&env, &cid, || {
        AccessControl::revoke_role(&env, &admin, &operator, Role::Operator)
    });
    assert!(!read(&env, &cid, || AccessControl::has_role(
        &env,
        &operator,
        Role::Operator
    )));

    let client = TestACClient::new(&env, &cid);
    assert_soroban_error!(
        client.try_require_role(&operator, &Role::Operator),
        unauthorized_role_error(),
    );
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
fn wrong_address_cannot_accept_admin() {
    let (env, admin, new_admin, cid) = setup();
    let imposter = Address::generate(&env);
    invoke(&env, &cid, || {
        AccessControl::transfer_admin(&env, &admin, &new_admin)
    });
    assert!(read(&env, &cid, || AccessControl::has_role(
        &env,
        &admin,
        Role::Admin
    )));
    assert!(!read(&env, &cid, || AccessControl::has_role(
        &env,
        &imposter,
        Role::Admin
    )));

    let client = TestACClient::new(&env, &cid);
    assert_soroban_error!(
        client.try_accept_admin(&imposter),
        unauthorized_role_error(),
    );
}

#[test]
#[should_panic]
fn accept_admin_without_proposal_panics() {
    let (env, _, other, cid) = setup();
    invoke(&env, &cid, || AccessControl::accept_admin(&env, &other));
}

#[test]
fn non_admin_cannot_propose_admin_transfer() {
    let (env, _, other, cid) = setup();
    let target = Address::generate(&env);
    assert!(!read(&env, &cid, || AccessControl::has_role(
        &env,
        &other,
        Role::Admin
    )));

    let client = TestACClient::new(&env, &cid);
    assert_soroban_error!(
        client.try_transfer_admin(&other, &target),
        unauthorized_role_error(),
    );
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
// Signature and post-revocation enforcement
// ---------------------------------------------------------------------------

#[test]
fn admin_without_signature_cannot_grant_role() {
    let (env, admin, _, cid) = setup();
    let target = Address::generate(&env);
    assert!(read(&env, &cid, || AccessControl::has_role(
        &env,
        &admin,
        Role::Admin
    )));
    env.mock_auths(&[]);

    let client = TestACClient::new(&env, &cid);
    assert_soroban_error!(
        client.try_grant_role(&admin, &target, &Role::Operator),
        missing_signature_error(),
    );
}

#[test]
fn admin_without_signature_cannot_revoke_role() {
    let (env, admin, target, cid) = setup();
    invoke(&env, &cid, || {
        AccessControl::grant_role(&env, &admin, &target, Role::Operator)
    });
    assert!(read(&env, &cid, || AccessControl::has_role(
        &env,
        &target,
        Role::Operator
    )));
    env.mock_auths(&[]);

    let client = TestACClient::new(&env, &cid);
    assert_soroban_error!(
        client.try_revoke_role(&admin, &target, &Role::Operator),
        missing_signature_error(),
    );
}

#[test]
fn admin_without_signature_cannot_transfer_admin() {
    let (env, admin, new_admin, cid) = setup();
    assert!(read(&env, &cid, || AccessControl::has_role(
        &env,
        &admin,
        Role::Admin
    )));
    env.mock_auths(&[]);

    let client = TestACClient::new(&env, &cid);
    assert_soroban_error!(
        client.try_transfer_admin(&admin, &new_admin),
        missing_signature_error(),
    );
}

#[test]
fn accept_admin_without_signature_panics() {
    let (env, admin, new_admin, cid) = setup();
    invoke(&env, &cid, || {
        AccessControl::transfer_admin(&env, &admin, &new_admin)
    });
    env.mock_auths(&[]);

    let client = TestACClient::new(&env, &cid);
    assert_soroban_error!(
        client.try_accept_admin(&new_admin),
        missing_signature_error(),
    );
}

#[test]
fn revoked_admin_cannot_grant_role() {
    let (env, _, revoked_admin, cid) = setup_with_revoked_admin();
    let target = Address::generate(&env);
    assert!(!read(&env, &cid, || AccessControl::has_role(
        &env,
        &revoked_admin,
        Role::Admin
    )));

    let client = TestACClient::new(&env, &cid);
    assert_soroban_error!(
        client.try_grant_role(&revoked_admin, &target, &Role::Operator),
        unauthorized_role_error(),
    );
}

#[test]
fn revoked_admin_cannot_revoke_role() {
    let (env, active_admin, revoked_admin, cid) = setup_with_revoked_admin();
    let target = Address::generate(&env);
    invoke(&env, &cid, || {
        AccessControl::grant_role(&env, &active_admin, &target, Role::Operator)
    });
    assert!(!read(&env, &cid, || AccessControl::has_role(
        &env,
        &revoked_admin,
        Role::Admin
    )));
    assert!(read(&env, &cid, || AccessControl::has_role(
        &env,
        &target,
        Role::Operator
    )));

    // Targeting Operator deliberately avoids the last-admin guard, so this
    // test can only pass because the revoked caller is rejected.
    let client = TestACClient::new(&env, &cid);
    assert_soroban_error!(
        client.try_revoke_role(&revoked_admin, &target, &Role::Operator),
        unauthorized_role_error(),
    );
}

#[test]
fn revoked_admin_cannot_transfer_admin() {
    let (env, _, revoked_admin, cid) = setup_with_revoked_admin();
    let successor = Address::generate(&env);
    assert!(!read(&env, &cid, || AccessControl::has_role(
        &env,
        &revoked_admin,
        Role::Admin
    )));

    let client = TestACClient::new(&env, &cid);
    assert_soroban_error!(
        client.try_transfer_admin(&revoked_admin, &successor),
        unauthorized_role_error(),
    );
}

// ---------------------------------------------------------------------------
// Operator role restrictions
// ---------------------------------------------------------------------------

#[test]
fn operator_cannot_grant_roles() {
    let (env, admin, operator, cid) = setup();
    let target = Address::generate(&env);
    invoke(&env, &cid, || {
        AccessControl::grant_role(&env, &admin, &operator, Role::Operator)
    });
    assert!(read(&env, &cid, || AccessControl::has_role(
        &env,
        &operator,
        Role::Operator
    )));
    assert!(!read(&env, &cid, || AccessControl::has_role(
        &env,
        &operator,
        Role::Admin
    )));

    let client = TestACClient::new(&env, &cid);
    assert_soroban_error!(
        client.try_grant_role(&operator, &target, &Role::Operator),
        unauthorized_role_error(),
    );
}

#[test]
fn operator_cannot_revoke_roles() {
    let (env, admin, operator, cid) = setup();
    let target = Address::generate(&env);
    invoke(&env, &cid, || {
        AccessControl::grant_role(&env, &admin, &operator, Role::Operator)
    });
    invoke(&env, &cid, || {
        AccessControl::grant_role(&env, &admin, &target, Role::Operator)
    });
    assert!(!read(&env, &cid, || AccessControl::has_role(
        &env,
        &operator,
        Role::Admin
    )));
    assert!(read(&env, &cid, || AccessControl::has_role(
        &env,
        &target,
        Role::Operator
    )));

    let client = TestACClient::new(&env, &cid);
    assert_soroban_error!(
        client.try_revoke_role(&operator, &target, &Role::Operator),
        unauthorized_role_error(),
    );
}

// ---------------------------------------------------------------------------
// Granular roles (issue #820): Guardian asymmetry, generalised two-step
// transfer, role expiry, enumeration.
// ---------------------------------------------------------------------------

fn role_required_error() -> Error {
    Error::from_contract_error(ContractError::RoleRequired as u32)
}

fn role_transfer_not_pending_error() -> Error {
    Error::from_contract_error(ContractError::RoleTransferNotPending as u32)
}

#[test]
fn admin_can_grant_and_revoke_guardian_role() {
    let (env, admin, guardian, cid) = setup();
    invoke(&env, &cid, || {
        AccessControl::grant_role(&env, &admin, &guardian, Role::Guardian)
    });
    assert!(read(&env, &cid, || AccessControl::has_role(
        &env,
        &guardian,
        Role::Guardian
    )));

    invoke(&env, &cid, || {
        AccessControl::revoke_role(&env, &admin, &guardian, Role::Guardian)
    });
    assert!(!read(&env, &cid, || AccessControl::has_role(
        &env,
        &guardian,
        Role::Guardian
    )));
}

/// The core Guardian asymmetry guarantee: holding Guardian never implies
/// holding any fund-moving or de-escalating role (Admin, Upgrader,
/// Treasurer). A Guardian-only key can never unpause, upgrade, or withdraw —
/// those checks live in the calling contracts (e.g. the vault only accepts
/// Guardian for `pause`/halt actions, never for `unpause`), but the
/// prerequisite proven here is that granting Guardian grants nothing else.
#[test]
fn guardian_role_does_not_imply_admin_upgrader_or_treasurer() {
    let (env, admin, guardian, cid) = setup();
    invoke(&env, &cid, || {
        AccessControl::grant_role(&env, &admin, &guardian, Role::Guardian)
    });

    assert!(read(&env, &cid, || AccessControl::has_role(
        &env,
        &guardian,
        Role::Guardian
    )));
    assert!(!read(&env, &cid, || AccessControl::has_role(
        &env,
        &guardian,
        Role::Admin
    )));
    assert!(!read(&env, &cid, || AccessControl::has_role(
        &env,
        &guardian,
        Role::Upgrader
    )));
    assert!(!read(&env, &cid, || AccessControl::has_role(
        &env,
        &guardian,
        Role::Treasurer
    )));

    // And a Guardian-only key cannot pass an Admin-gated check.
    let client = TestACClient::new(&env, &cid);
    assert_soroban_error!(
        client.try_require_role(&guardian, &Role::Admin),
        unauthorized_role_error(),
    );
}

#[test]
fn all_new_granular_roles_are_independently_grantable() {
    let (env, admin, cid) = {
        let (env, admin, _, cid) = setup();
        (env, admin, cid)
    };

    for role in [
        Role::Upgrader,
        Role::Attester,
        Role::FeeManager,
        Role::RebalanceKeeper,
        Role::Treasurer,
    ] {
        let holder = Address::generate(&env);
        invoke(&env, &cid, || {
            AccessControl::grant_role(&env, &admin, &holder, role.clone())
        });
        assert!(read(&env, &cid, || AccessControl::has_role(
            &env,
            &holder,
            role.clone()
        )));
    }
}

// --- Generalised two-step role transfer -------------------------------------

#[test]
fn transfer_role_two_step_happy_path_for_guardian() {
    let (env, admin, guardian, cid) = setup();
    invoke(&env, &cid, || {
        AccessControl::grant_role(&env, &admin, &guardian, Role::Guardian)
    });

    let successor = Address::generate(&env);
    invoke(&env, &cid, || {
        AccessControl::transfer_role(&env, &guardian, Role::Guardian, &successor)
    });

    // Pending: original holder keeps the role, successor does not have it yet.
    assert!(read(&env, &cid, || AccessControl::has_role(
        &env,
        &guardian,
        Role::Guardian
    )));
    assert!(!read(&env, &cid, || AccessControl::has_role(
        &env,
        &successor,
        Role::Guardian
    )));

    invoke(&env, &cid, || {
        AccessControl::accept_role(&env, &successor, Role::Guardian)
    });

    assert!(read(&env, &cid, || AccessControl::has_role(
        &env,
        &successor,
        Role::Guardian
    )));
    assert!(!read(&env, &cid, || AccessControl::has_role(
        &env,
        &guardian,
        Role::Guardian
    )));
}

#[test]
fn wrong_party_cannot_accept_role_transfer() {
    let (env, admin, guardian, cid) = setup();
    invoke(&env, &cid, || {
        AccessControl::grant_role(&env, &admin, &guardian, Role::Guardian)
    });
    let successor = Address::generate(&env);
    let imposter = Address::generate(&env);
    invoke(&env, &cid, || {
        AccessControl::transfer_role(&env, &guardian, Role::Guardian, &successor)
    });

    let client = TestACClient::new(&env, &cid);
    assert_soroban_error!(
        client.try_accept_role(&imposter, &Role::Guardian),
        unauthorized_role_error(),
    );

    // Original holder is unaffected by the failed acceptance attempt.
    assert!(read(&env, &cid, || AccessControl::has_role(
        &env,
        &guardian,
        Role::Guardian
    )));
}

#[test]
fn accept_role_without_pending_transfer_fails_typed() {
    let (env, _, other, cid) = setup();
    let client = TestACClient::new(&env, &cid);
    assert_soroban_error!(
        client.try_accept_role(&other, &Role::Guardian),
        role_transfer_not_pending_error(),
    );
}

#[test]
fn proposer_can_cancel_pending_role_transfer() {
    let (env, admin, guardian, cid) = setup();
    invoke(&env, &cid, || {
        AccessControl::grant_role(&env, &admin, &guardian, Role::Guardian)
    });
    let successor = Address::generate(&env);
    invoke(&env, &cid, || {
        AccessControl::transfer_role(&env, &guardian, Role::Guardian, &successor)
    });
    invoke(&env, &cid, || {
        AccessControl::cancel_role_transfer(&env, &guardian, Role::Guardian)
    });

    // Cancelled: successor can no longer accept.
    let client = TestACClient::new(&env, &cid);
    assert_soroban_error!(
        client.try_accept_role(&successor, &Role::Guardian),
        role_transfer_not_pending_error(),
    );
    assert!(read(&env, &cid, || AccessControl::has_role(
        &env,
        &guardian,
        Role::Guardian
    )));
}

#[test]
fn non_holder_cannot_propose_role_transfer() {
    let (env, _, other, cid) = setup();
    let target = Address::generate(&env);
    let client = TestACClient::new(&env, &cid);
    assert_soroban_error!(
        client.try_transfer_role(&other, &Role::Guardian, &target),
        role_required_error(),
    );
}

// --- Role expiry -------------------------------------------------------------

#[test]
fn grant_role_until_expires_and_stops_authorising() {
    let (env, admin, keeper, cid) = setup();
    let now = env.ledger().timestamp();
    invoke(&env, &cid, || {
        AccessControl::grant_role_until(&env, &admin, &keeper, Role::RebalanceKeeper, now + 1000)
    });

    assert!(read(&env, &cid, || AccessControl::has_role(
        &env,
        &keeper,
        Role::RebalanceKeeper
    )));
    assert_eq!(
        read(&env, &cid, || AccessControl::role_expires_at(
            &env,
            &keeper,
            Role::RebalanceKeeper
        )),
        Some(now + 1000)
    );

    // Advance the ledger past expiry.
    env.ledger().with_mut(|l| l.timestamp = now + 1001);

    assert!(!read(&env, &cid, || AccessControl::has_role(
        &env,
        &keeper,
        Role::RebalanceKeeper
    )));

    let client = TestACClient::new(&env, &cid);
    assert_soroban_error!(
        client.try_require_role(&keeper, &Role::RebalanceKeeper),
        unauthorized_role_error(),
    );
}

#[test]
fn permanent_grant_has_no_expiry() {
    let (env, admin, operator, cid) = setup();
    invoke(&env, &cid, || {
        AccessControl::grant_role(&env, &admin, &operator, Role::Operator)
    });
    assert_eq!(
        read(&env, &cid, || AccessControl::role_expires_at(
            &env,
            &operator,
            Role::Operator
        )),
        None
    );
}

// --- Enumeration --------------------------------------------------------------

#[test]
fn get_role_members_lists_every_holder_and_is_bounded() {
    let (env, admin, first, cid) = setup();
    invoke(&env, &cid, || {
        AccessControl::grant_role(&env, &admin, &first, Role::Guardian)
    });
    let second = Address::generate(&env);
    invoke(&env, &cid, || {
        AccessControl::grant_role(&env, &admin, &second, Role::Guardian)
    });

    let members = read(&env, &cid, || {
        AccessControl::get_role_members(&env, Role::Guardian, 0, 10)
    });
    assert_eq!(members.len(), 2);
    assert!(members.contains(first.clone()));
    assert!(members.contains(second.clone()));

    // Requesting more than MAX_MEMBERS_PAGE never panics or over-returns.
    let bounded = read(&env, &cid, || {
        AccessControl::get_role_members(&env, Role::Guardian, 0, 10_000)
    });
    assert_eq!(bounded.len(), 2);
}

#[test]
fn revoked_member_is_removed_from_enumeration() {
    let (env, admin, guardian, cid) = setup();
    invoke(&env, &cid, || {
        AccessControl::grant_role(&env, &admin, &guardian, Role::Guardian)
    });
    invoke(&env, &cid, || {
        AccessControl::revoke_role(&env, &admin, &guardian, Role::Guardian)
    });

    let members = read(&env, &cid, || {
        AccessControl::get_role_members(&env, Role::Guardian, 0, 10)
    });
    assert!(!members.contains(guardian));
}
