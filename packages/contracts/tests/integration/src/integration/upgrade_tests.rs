//! End-to-end upgrade integration tests — issue #1499.
//!
//! Covers:
//! 1. `propose → execute` path: user balances and `total_assets` are fully
//!    preserved after the WASM swap and schema migration.
//! 2. `propose → cancel` path: state is completely unchanged.
//! 3. Schema migration is idempotent (calling `migrate` twice returns the same
//!    version and doesn't corrupt storage).
//! 4. Schema version is set correctly after migration.
//!
//! These tests run in CI.  `test_upgrade_wasm_swap_preserves_all_state`
//! is the primary regression guard against an upgrade that silently clears
//! or misreads on-chain storage.
#![cfg(test)]

extern crate std;

use soroban_sdk::{testutils::Ledger as _, BytesN};

use nester_access_control::Role;
use nester_common::{MIN_DEPOSIT_AMOUNT, MIN_UPGRADE_DELAY_VAULT};
use nester_test_utils::NesterHarness;
use vault_contract::FeeConfig;

const DEPOSIT: i128 = MIN_DEPOSIT_AMOUNT * 10; // 10 USDC
const YIELD: i128 = MIN_DEPOSIT_AMOUNT; // 10% simulated yield

fn no_fees(h: &NesterHarness) {
    h.vault().set_fee_config(
        &h.admin,
        &FeeConfig {
            performance_fee_bps: 0,
            management_fee_bps: 0,
            early_withdrawal_fee_bps: 0,
            treasury_address: h.treasury_id.clone(),
        },
    );
}

// ── Upgrade-execute path ──────────────────────────────────────────────────────

/// Full propose → execute path:
/// deposits and yield are seeded BEFORE the upgrade, and every balance
/// assertion runs AFTER.  Proves the WASM swap does not touch storage.
#[test]
fn test_upgrade_wasm_swap_preserves_all_state() {
    let h = NesterHarness::setup();
    no_fees(&h);

    let alice = h.create_user();
    let bob = h.create_user();
    h.mint_deposit_tokens(&alice, DEPOSIT);
    h.mint_deposit_tokens(&bob, DEPOSIT);

    let alice_shares = h.vault().deposit(&alice, &DEPOSIT, &0);
    let bob_shares = h.vault().deposit(&bob, &DEPOSIT, &0);

    h.vault().grant_role(&h.admin, &h.admin, &Role::Manager);
    h.mint_deposit_tokens(&h.vault_id, YIELD);
    h.vault().report_yield(&h.admin, &YIELD);

    let pre_total = h.token().total_assets();
    let pre_alice = h.token().balance(&alice);
    let pre_bob = h.token().balance(&bob);

    assert_eq!(pre_alice, alice_shares);
    assert_eq!(pre_bob, bob_shares);

    // Propose and execute upgrade using empty-WASM target (valid in test env).
    let upgrader = h.create_user();
    h.vault().grant_role(&h.admin, &upgrader, &Role::Upgrader);

    let new_wasm_hash = h
        .env
        .deployer()
        .upload_contract_wasm(soroban_sdk::Bytes::new(&h.env));

    let now = h.env.ledger().timestamp();
    let eta = now + MIN_UPGRADE_DELAY_VAULT;

    h.vault().propose_upgrade(&upgrader, &new_wasm_hash, &eta);

    // Advance time to ETA.
    h.env.ledger().set_timestamp(eta);

    let relayer = h.create_user();
    h.vault().execute_upgrade(&relayer, &new_wasm_hash);

    // Run migration and confirm schema version advances (or stays at current).
    let schema_after = h.vault().migrate();
    assert!(
        schema_after >= 1,
        "schema version must be ≥ 1 after migration"
    );
    let idempotent = h.vault().migrate();
    assert_eq!(schema_after, idempotent, "migrate must be idempotent");

    // Release vested yield then check all balances are intact.
    h.env
        .ledger()
        .set_timestamp(eta + h.vault().get_yield_vesting_period() + 1);
    h.vault().report_yield(&h.admin, &0);

    assert_eq!(
        h.token().balance(&alice),
        pre_alice,
        "Alice's share balance must not change across upgrade"
    );
    assert_eq!(
        h.token().balance(&bob),
        pre_bob,
        "Bob's share balance must not change across upgrade"
    );
    assert_eq!(
        h.token().total_assets(),
        pre_total,
        "total_assets must not change across upgrade"
    );
}

// ── Cancel path ───────────────────────────────────────────────────────────────

/// propose → cancel: the vault returns to its pre-proposal state.
/// No pending upgrade remains, and balances are completely unchanged.
#[test]
fn test_upgrade_cancel_leaves_state_unchanged() {
    let h = NesterHarness::setup();
    no_fees(&h);

    let user = h.create_user();
    h.mint_deposit_tokens(&user, DEPOSIT);
    h.vault().deposit(&user, &DEPOSIT, &0);

    let shares_before = h.token().balance(&user);
    let assets_before = h.token().total_assets();

    let upgrader = h.create_user();
    h.vault().grant_role(&h.admin, &upgrader, &Role::Upgrader);

    let dummy_hash = BytesN::from_array(&h.env, &[0xABu8; 32]);
    let eta = h.env.ledger().timestamp() + MIN_UPGRADE_DELAY_VAULT;
    h.vault().propose_upgrade(&upgrader, &dummy_hash, &eta);

    // Pending upgrade should now exist.
    assert!(
        h.vault().get_pending_upgrade().is_some(),
        "pending upgrade must be set after propose"
    );

    h.vault().cancel_upgrade(&upgrader);

    // After cancel: no pending upgrade, balances unchanged.
    assert!(
        h.vault().get_pending_upgrade().is_none(),
        "no pending upgrade after cancel"
    );
    assert_eq!(
        h.token().balance(&user),
        shares_before,
        "share balance unchanged after cancel"
    );
    assert_eq!(
        h.token().total_assets(),
        assets_before,
        "total_assets unchanged after cancel"
    );
}

// ── Access control ────────────────────────────────────────────────────────────

/// Only `Role::Upgrader` may propose; non-upgrader must be rejected.
#[test]
fn test_propose_upgrade_requires_upgrader_role() {
    let h = NesterHarness::setup();
    let non_upgrader = h.create_user();

    let dummy_hash = BytesN::from_array(&h.env, &[1u8; 32]);
    let eta = h.env.ledger().timestamp() + MIN_UPGRADE_DELAY_VAULT;

    assert!(
        h.vault()
            .try_propose_upgrade(&non_upgrader, &dummy_hash, &eta)
            .is_err(),
        "non-upgrader must not be able to propose"
    );
}

/// `execute_upgrade` before ETA must be rejected regardless of caller.
#[test]
fn test_execute_upgrade_before_eta_rejected() {
    let h = NesterHarness::setup();
    let upgrader = h.create_user();
    h.vault().grant_role(&h.admin, &upgrader, &Role::Upgrader);

    let dummy_hash = BytesN::from_array(&h.env, &[2u8; 32]);
    let eta = h.env.ledger().timestamp() + MIN_UPGRADE_DELAY_VAULT;
    h.vault().propose_upgrade(&upgrader, &dummy_hash, &eta);

    assert!(
        h.vault()
            .try_execute_upgrade(&upgrader, &dummy_hash)
            .is_err(),
        "execute_upgrade before ETA must fail"
    );
}
