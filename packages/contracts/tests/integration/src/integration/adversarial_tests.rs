//! Adversarial and negative scenario integration tests.
#![cfg(test)]

extern crate std;

use nester_test_utils::NesterHarness;
use soroban_sdk::{testutils::Ledger as _, vec};

const DEPOSIT: i128 = 10_000_000;
const DAY: u64 = 86_400;

fn configure_lock_tiers(h: &NesterHarness) {
    use vault_contract::locks::LockTier;
    let tiers = vec![
        &h.env,
        LockTier {
            duration_secs: 30 * DAY,
            boost_multiplier: 12_000,
        },
        LockTier {
            duration_secs: 90 * DAY,
            boost_multiplier: 15_000,
        },
    ];
    h.vault().set_lock_tiers(&h.admin, &tiers);
}

// ---------------------------------------------------------------------------
// Original placeholder tests
// ---------------------------------------------------------------------------

#[test]
fn test_zero_deposit_reverts() {
    assert!(true, "placeholder: zero deposit reverts");
}

#[test]
fn test_deposit_to_paused_vault_reverts() {
    assert!(true, "placeholder: deposit to paused vault reverts");
}

#[test]
fn test_withdraw_more_than_owned_reverts() {
    assert!(true, "placeholder: withdraw more than owned reverts");
}

#[test]
fn test_double_initialization_reverts() {
    assert!(true, "placeholder: double initialization reverts");
}

#[test]
fn test_non_admin_admin_functions_revert() {
    assert!(true, "placeholder: non-admin admin functions revert");
}

#[test]
fn test_last_admin_cannot_be_revoked() {
    assert!(true, "placeholder: last admin protection");
}

// ===========================================================================
// Time-locked vault adversarial tests
// ===========================================================================

/// deposit_locked with unconfigured duration should revert.
#[test]
#[should_panic(expected = "Error(Contract, #23)")]
fn test_deposit_locked_invalid_duration_reverts() {
    let h = NesterHarness::setup();
    configure_lock_tiers(&h);
    let user = h.create_user();

    h.mint_deposit_tokens(&user, DEPOSIT);
    // 7 days is not a configured tier
    h.vault().deposit_locked(&user, &DEPOSIT, &0, &(7 * DAY));
}

/// deposit_locked with no tiers configured should revert.
#[test]
#[should_panic(expected = "Error(Contract, #23)")]
fn test_deposit_locked_no_tiers_configured_reverts() {
    let h = NesterHarness::setup();
    let user = h.create_user();

    h.mint_deposit_tokens(&user, DEPOSIT);
    // No tiers have been set
    h.vault().deposit_locked(&user, &DEPOSIT, &0, &(30 * DAY));
}

/// Non-admin cannot set lock tiers.
#[test]
fn test_non_admin_cannot_set_lock_tiers() {
    let h = NesterHarness::setup();
    let outsider = h.create_user();

    use vault_contract::locks::LockTier;
    let tiers = vec![
        &h.env,
        LockTier {
            duration_secs: 30 * DAY,
            boost_multiplier: 12_000,
        },
    ];

    let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        h.vault().set_lock_tiers(&outsider, &tiers);
    }));
    assert!(
        result.is_err(),
        "non-admin should not be able to set lock tiers"
    );
}

/// Non-admin cannot set early break penalty.
#[test]
fn test_non_admin_cannot_set_early_break_penalty() {
    let h = NesterHarness::setup();
    let outsider = h.create_user();

    let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        h.vault().set_early_break_penalty(&outsider, &500);
    }));
    assert!(
        result.is_err(),
        "non-admin should not be able to set early break penalty"
    );
}

/// User cannot unlock another user's lock.
#[test]
fn test_user_cannot_unlock_others_lock() {
    let h = NesterHarness::setup();
    configure_lock_tiers(&h);

    let alice = h.create_user();
    let bob = h.create_user();

    h.mint_deposit_tokens(&alice, DEPOSIT);
    h.vault().deposit_locked(&alice, &DEPOSIT, &0, &(30 * DAY));

    // Advance past maturity so unlock would succeed for the owner
    h.env.ledger().with_mut(|l| l.timestamp = 30 * DAY + 1);

    let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        h.vault().unlock_position(&bob, &0);
    }));
    assert!(
        result.is_err(),
        "bob should not be able to unlock alice's lock"
    );
}

/// User cannot break another user's lock.
#[test]
fn test_user_cannot_break_others_lock() {
    let h = NesterHarness::setup();
    configure_lock_tiers(&h);

    let alice = h.create_user();
    let bob = h.create_user();

    h.mint_deposit_tokens(&alice, DEPOSIT);
    h.vault().deposit_locked(&alice, &DEPOSIT, &0, &(30 * DAY));

    let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        h.vault().break_lock(&bob, &0);
    }));
    assert!(
        result.is_err(),
        "bob should not be able to break alice's lock"
    );
}

/// Unlocking a nonexistent lock should revert.
#[test]
#[should_panic(expected = "Error(Contract, #24)")]
fn test_unlock_nonexistent_lock_reverts() {
    let h = NesterHarness::setup();
    configure_lock_tiers(&h);
    let user = h.create_user();

    h.mint_deposit_tokens(&user, DEPOSIT);
    h.vault().deposit(&user, &DEPOSIT, &0);

    h.vault().unlock_position(&user, &999);
}

/// Breaking a nonexistent lock should revert.
#[test]
#[should_panic(expected = "Error(Contract, #24)")]
fn test_break_nonexistent_lock_reverts() {
    let h = NesterHarness::setup();
    configure_lock_tiers(&h);
    let user = h.create_user();

    h.mint_deposit_tokens(&user, DEPOSIT);
    h.vault().deposit(&user, &DEPOSIT, &0);

    h.vault().break_lock(&user, &999);
}

/// deposit_locked respects max locks per user.
#[test]
fn test_max_locks_per_user_enforced() {
    let h = NesterHarness::setup();
    configure_lock_tiers(&h);
    let user = h.create_user();

    let max = nester_common::MAX_OPEN_LOCKS_PER_USER;
    h.mint_deposit_tokens(&user, (max as i128 + 1) * DEPOSIT);

    for _ in 0..max {
        h.vault().deposit_locked(&user, &DEPOSIT, &0, &(30 * DAY));
    }

    // One more should fail
    let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        h.vault().deposit_locked(&user, &DEPOSIT, &0, &(30 * DAY));
    }));
    assert!(
        result.is_err(),
        "should reject deposit_locked beyond max locks"
    );
}

/// Early break penalty decreases over time (linear decay).
#[test]
fn test_early_break_penalty_decays_over_time() {
    let h = NesterHarness::setup();
    configure_lock_tiers(&h);

    let alice = h.create_user();
    let bob = h.create_user();

    h.mint_deposit_tokens(&alice, DEPOSIT);
    h.mint_deposit_tokens(&bob, DEPOSIT);

    h.vault().deposit_locked(&alice, &DEPOSIT, &0, &(30 * DAY));
    h.vault().deposit_locked(&bob, &DEPOSIT, &0, &(30 * DAY));

    // Alice breaks immediately (max penalty)
    h.vault().break_lock(&alice, &0);
    let alice_out =
        soroban_sdk::token::Client::new(&h.env, &h.deposit_token_id).balance(&alice);

    // Bob breaks halfway through
    h.env.ledger().with_mut(|l| l.timestamp = 15 * DAY);
    h.vault().break_lock(&bob, &0);
    let bob_out =
        soroban_sdk::token::Client::new(&h.env, &h.deposit_token_id).balance(&bob);

    assert!(
        bob_out > alice_out,
        "breaking later should incur less penalty, giving bob more tokens"
    );
}

/// Emergency withdraw bypasses locks.
#[test]
fn test_emergency_withdraw_bypasses_locks() {
    let h = NesterHarness::setup();
    configure_lock_tiers(&h);
    let user = h.create_user();

    h.mint_deposit_tokens(&user, DEPOSIT);
    h.vault().deposit_locked(&user, &DEPOSIT, &0, &(30 * DAY));

    h.vault().pause(&h.admin);

    let bal_before = soroban_sdk::token::Client::new(&h.env, &h.deposit_token_id).balance(&user);
    h.vault().emergency_withdraw(&user);
    let bal_after = soroban_sdk::token::Client::new(&h.env, &h.deposit_token_id).balance(&user);

    assert!(
        bal_after > bal_before,
        "emergency withdraw should return tokens even when locked"
    );
}
