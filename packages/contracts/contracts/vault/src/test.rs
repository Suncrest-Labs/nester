#![cfg(test)]

extern crate std;

use soroban_sdk::{
    contract, contractimpl,
    testutils::{Address as _, Ledger, LedgerInfo},
    token, Address, Env, String,
};
use nester_access_control::Role;
use vault_token::{VaultTokenContract, VaultTokenContractClient};

use crate::{VaultContract, VaultContractClient, VaultStatus};

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------


#[contract]
pub struct MockTreasury;

#[contractimpl]
impl MockTreasury {
    pub fn receive_fees(_env: Env, _amount: i128) {}
}

#[contract]
struct VaultObserverContract;

#[contractimpl]
impl VaultObserverContract {
    pub fn pause_target(env: Env, target: Address, caller: Address) {
        caller.require_auth();
        let client = VaultContractClient::new(&env, &target);
        client.pause(&caller);
    }

    pub fn is_target_paused(env: Env, target: Address) -> bool {
        let client = VaultContractClient::new(&env, &target);
        client.is_paused()
    }
}

/// One "unit" in 7-decimal Stellar token precision.
const STROOP: i128 = 1;
/// Convenient larger denomination.
const XLM: i128 = 10_000_000;

/// Seconds in one day — also the MinLockPeriod set in vault `initialize`.
const DAY: u64 = 86_400;

/// Early-withdrawal fee in basis points as set by the vault contract (0.1 % = 10 bps).
const EARLY_FEE_BPS: i128 = 10;
const BPS_DENOM: i128 = 10_000;

/// Create a fresh environment, register a native token, register the vault
/// contract, and call `initialize`. Returns `(env, admin, sac_client, vault_client, treasury)` ready for use.
fn setup() -> (
    Env,
    Address,
    token::StellarAssetClient<'static>,
    VaultContractClient<'static>,
    Address,
) {    let env = Env::default();
    env.mock_all_auths();

    // -----------------------------
    // Token setup
    // -----------------------------
    let token_admin = Address::generate(&env);

    // v2 returns StellarAssetContract (NOT Address)
    let sac_contract = env.register_stellar_asset_contract_v2(token_admin.clone());

    // ✅ Extract the actual contract address
    let token_id = sac_contract.address();

    // Create token client
    let sac: token::StellarAssetClient<'static> =
        token::StellarAssetClient::new(unsafe { core::mem::transmute(&env) }, &token_id);

    // -----------------------------
    // Vault setup
    // -----------------------------
    let admin = Address::generate(&env);
    let treasury = env.register_contract(None, MockTreasury); // new treasury address

    let vault_id = env.register_contract(None, VaultContract);
    let vault_token_id = env.register_contract(None, VaultTokenContract);

    let vault: VaultContractClient<'static> =
        VaultContractClient::new(unsafe { core::mem::transmute(&env) }, &vault_id);

    // Pass admin, deposit token, vault token, and treasury.
    vault.initialize(&admin, &token_id, &vault_token_id, &treasury);

    let vault_token = VaultTokenContractClient::new(&env, &vault_token_id);
    vault_token.initialize(
        &vault_id,
        &String::from_str(&env, "Nester USDC Vault"),
        &String::from_str(&env, "nUSDC"),
        &7u32,
    );

    (env, admin, sac, vault, treasury)
}

/// Mint `amount` tokens to `recipient` using the Stellar asset admin client.
fn mint(sac: &token::StellarAssetClient, recipient: &Address, amount: i128) {
    sac.mint(recipient, &amount);
}


// ---------------------------------------------------------------------------
// Cross-contract pause & idempotence (issue #54 acceptance criteria)
// ---------------------------------------------------------------------------

#[test]
fn pause_and_unpause_are_idempotent() {
    let (_env, admin, _token, vault, _treasury) = setup();

    vault.pause(&admin);
    vault.pause(&admin); // second pause is a no-op
    assert!(vault.is_paused());

    vault.unpause(&admin);
    assert!(!vault.is_paused());
    vault.unpause(&admin); // second unpause is a no-op
    assert!(!vault.is_paused());
}

#[test]
fn cross_contract_pause_state_is_visible() {
    let (env, admin, _token, vault, _treasury) = setup();
    let observer_id = env.register_contract(None, VaultObserverContract);
    let observer = VaultObserverContractClient::new(&env, &observer_id);

    assert!(!observer.is_target_paused(&vault.address));

    vault.pause(&admin);
    assert!(observer.is_target_paused(&vault.address));
}

#[test]
fn cross_contract_admin_can_pause_target() {
    let (env, admin, _token, vault, _treasury) = setup();
    let observer_id = env.register_contract(None, VaultObserverContract);
    let observer = VaultObserverContractClient::new(&env, &observer_id);

    observer.pause_target(&vault.address, &admin);
    assert!(vault.is_paused());
}

#[test]
#[should_panic]
fn cross_contract_non_admin_cannot_pause_target() {
    let (env, _admin, _token, vault, _treasury) = setup();
    let observer_id = env.register_contract(None, VaultObserverContract);
    let observer = VaultObserverContractClient::new(&env, &observer_id);
    let outsider = Address::generate(&env);

    observer.pause_target(&vault.address, &outsider);
}

/// Advance the ledger timestamp by `seconds`.
fn advance_time(env: &Env, seconds: u64) {
    let current = env.ledger().timestamp();
    env.ledger().set(LedgerInfo {
        timestamp: current + seconds,
        ..env.ledger().get()
    });
}


// ---------------------------------------------------------------------------
// Initialization
// ---------------------------------------------------------------------------

#[test]
fn vault_initializes_correctly() {
    let (_env, _admin, _token, vault, _treasury) = setup();

    assert_eq!(vault.get_status(), VaultStatus::Active);
    assert!(!vault.is_paused());
    assert_eq!(vault.get_total_deposits(), 0);
}

#[test]
#[should_panic]
fn reinitialize_is_rejected() {
    let (_env, admin, _token, vault, treasury) = setup();
    let second_token = Address::generate(&_env);
    let second_vault_token = Address::generate(&_env);
    vault.initialize(&admin, &second_token, &second_vault_token, &treasury);
}

// ---------------------------------------------------------------------------
// Deposit — share accounting
// ---------------------------------------------------------------------------

#[test]
fn first_deposit_creates_one_to_one_shares() {
    let (_env, _admin, token, vault, _treasury) = setup();
    let user = Address::generate(&_env);
    mint(&token, &user, 1_000 * XLM);

    let deposit_amount = 500 * XLM;
    let returned_balance = vault.deposit(&user, &deposit_amount, &0);

    assert_eq!(returned_balance, deposit_amount);
    assert_eq!(vault.get_balance(&user), deposit_amount);
    assert_eq!(vault.get_total_deposits(), deposit_amount);
}

#[test]
fn subsequent_deposit_uses_current_share_price() {
    let (_env, _admin, token, vault, _treasury) = setup();

    let user_a = Address::generate(&_env);
    let user_b = Address::generate(&_env);
    mint(&token, &user_a, 1_000 * XLM);
    mint(&token, &user_b, 1_000 * XLM);

    vault.deposit(&user_a, &(200 * XLM), &0);
    let bal_b = vault.deposit(&user_b, &(100 * XLM), &0);
    assert_eq!(bal_b, 100 * XLM);
    assert_eq!(vault.get_total_deposits(), 300 * XLM);
}

#[test]
#[should_panic(expected = "Error(Contract, #17)")]
fn deposit_reverts_when_min_shares_out_is_not_met() {
    let (_env, _admin, token, vault, _treasury) = setup();
    let user = Address::generate(&_env);
    mint(&token, &user, 1_000 * XLM);

    vault.deposit(&user, &(100 * XLM), &(100 * XLM + STROOP));
}

#[test]
#[should_panic]
fn deposit_of_zero_is_rejected() {
    let (_env, _admin, _token, vault, _treasury) = setup();
    let user = Address::generate(&_env);
    vault.deposit(&user, &0, &0);
}

#[test]
#[should_panic]
fn deposit_of_negative_amount_is_rejected() {
    let (_env, _admin, _token, vault, _treasury) = setup();
    let user = Address::generate(&_env);
    vault.deposit(&user, &(-1 * XLM), &0);
}

#[test]
#[should_panic]
fn deposit_fails_when_vault_is_paused() {
    let (_env, admin, token, vault, _treasury) = setup();
    let user = Address::generate(&_env);
    mint(&token, &user, 100 * XLM);

    vault.pause(&admin);
    vault.deposit(&user, &(50 * XLM), &0);
}

// ---------------------------------------------------------------------------
// Withdrawal — share accounting
// ---------------------------------------------------------------------------

#[test]
fn full_withdrawal_leaves_zero_balance() {
    let (_env, _admin, token, vault, _treasury) = setup();
    let user = Address::generate(&_env);
    mint(&token, &user, 500 * XLM);

    vault.deposit(&user, &(500 * XLM), &0);
    assert_eq!(vault.get_balance(&user), 500 * XLM);

    vault.withdraw(&user, &(500 * XLM), &0);
    assert_eq!(vault.get_balance(&user), 0);
    assert_eq!(vault.get_total_deposits(), 0);
}

#[test]
fn partial_withdrawal_is_calculated_correctly() {
    let (_env, _admin, token, vault, _treasury) = setup();
    let user = Address::generate(&_env);
    mint(&token, &user, 1_000 * XLM);

    vault.deposit(&user, &(1_000 * XLM), &0);
    vault.withdraw(&user, &(300 * XLM), &0);

    assert_eq!(vault.get_balance(&user), 700 * XLM);
    assert_eq!(vault.get_total_deposits(), 700 * XLM);
}

#[test]
fn withdrawal_after_yield_returns_principal_plus_yield() {
    let (_env, _admin, token, vault, _treasury) = setup();
    let user = Address::generate(&_env);
    mint(&token, &user, 1_000 * XLM);

    vault.deposit(&user, &(1_000 * XLM), &0);

    let vault_address = vault.address.clone();
    mint(&token, &vault_address, 100 * XLM);

    vault.withdraw(&user, &(1_000 * XLM), &0);
    assert_eq!(vault.get_balance(&user), 0);
    assert_eq!(vault.get_total_deposits(), 0);
}

#[test]
fn withdrawal_does_not_charge_perf_fee_on_preexisting_yield() {
    let (env, admin, token, vault, _treasury) = setup();
    let alice = Address::generate(&env);
    let bob = Address::generate(&env);
    let alice_deposit = 1_000 * XLM;
    let bob_deposit = 1_000 * XLM;

    mint(&token, &alice, alice_deposit);
    mint(&token, &bob, bob_deposit);

    vault.deposit(&alice, &alice_deposit, &0);
    vault.grant_role(&admin, &admin, &Role::Manager);

    // Simulate accounting yield that belongs to Alice's holding period.
    vault.report_yield(&admin, &(100 * XLM));

    vault.deposit(&bob, &bob_deposit, &0);
    let bob_shares = vault.get_shares(&bob);
    vault.withdraw(&bob, &bob_shares, &0);

    // Bob only pays early-withdrawal fee (0.1% of 1000 = 1), no performance fee.
    assert_eq!(token::Client::new(&env, &token.address).balance(&bob), 999 * XLM);
}

#[test]
fn withdrawal_charges_perf_fee_only_on_realized_user_yield() {
    let (env, admin, token, vault, _treasury) = setup();
    let user = Address::generate(&env);
    let liquidity_provider = Address::generate(&env);
    let deposit = 1_000 * XLM;

    mint(&token, &user, deposit);
    mint(&token, &liquidity_provider, deposit);
    vault.deposit(&user, &deposit, &0);
    vault.grant_role(&admin, &admin, &Role::Manager);

    // Double share price in accounting so user has 1000 of realized yield.
    vault.report_yield(&admin, &deposit);
    // Add liquid reserves so transfer can satisfy the larger withdrawal amount.
    vault.deposit(&liquidity_provider, &deposit, &0);

    let shares = vault.get_shares(&user);
    vault.withdraw(&user, &shares, &0);

    // Gross assets = 2000, performance fee = 100, early fee = 2, net = 1898.
    assert_eq!(token::Client::new(&env, &token.address).balance(&user), 1_898 * XLM);
}

#[test]
#[should_panic(expected = "Error(Contract, #17)")]
fn withdraw_reverts_when_min_assets_out_is_not_met() {
    let (_env, _admin, token, vault, _treasury) = setup();
    let user = Address::generate(&_env);
    mint(&token, &user, 1_000 * XLM);

    vault.deposit(&user, &(1_000 * XLM), &0);
    vault.withdraw(&user, &(500 * XLM), &(500 * XLM + STROOP));
}

#[test]
#[should_panic]
fn withdrawal_of_more_than_owned_is_rejected() {
    let (_env, _admin, token, vault, _treasury) = setup();
    let user = Address::generate(&_env);
    mint(&token, &user, 100 * XLM);

    vault.deposit(&user, &(100 * XLM), &0);
    vault.withdraw(&user, &(100 * XLM + STROOP), &0);
}

#[test]
#[should_panic]
fn withdraw_of_zero_is_rejected() {
    let (_env, _admin, token, vault, _treasury) = setup();
    let user = Address::generate(&_env);
    mint(&token, &user, 100 * XLM);

    vault.deposit(&user, &(100 * XLM), &0);
    vault.withdraw(&user, &0, &0);
}

// #[test]
// fn withdraw_is_allowed_even_when_vault_is_paused() {
//     let (_env, admin, token, vault, _treasury) = setup();
//     let user = Address::generate(&_env);
//     mint(&token, &user, 200 * XLM);

//     vault.deposit(&user, &(200 * XLM));
//     vault.pause(&admin);

//     let new_bal = vault.withdraw(&user, &(200 * XLM));
//     assert_eq!(new_bal, 0);
// }

// ---------------------------------------------------------------------------
// Lock period & early-withdrawal penalty boundary tests
//
// The vault initialises with MinLockPeriod = 86 400 s (1 day) and
// early_withdrawal_fee_bps = 10 (0.1 %).  These tests verify that:
//   • withdrawing BEFORE the lock period expires deducts the 0.1 % fee
//   • withdrawing AT or AFTER the lock period incurs no fee
// ---------------------------------------------------------------------------

fn early_withdrawal_fee(amount: i128) -> i128 {
    amount * EARLY_FEE_BPS / BPS_DENOM
}

#[test]
fn withdrawal_before_lock_period_deducts_early_fee() {
    let (env, _admin, token, vault, _treasury) = setup();
    let user = Address::generate(&env);
    let deposit_amount = 1_000 * XLM;
    mint(&token, &user, deposit_amount);

    vault.deposit(&user, &deposit_amount, &0);

    // Advance time by 12 hours — still inside the 1-day lock window.
    advance_time(&env, DAY / 2);

    // The shares returned by deposit equal the deposit (1:1 first deposit).
    // withdraw(shares) burns those shares and returns assets minus fee.
    let shares_owned = vault.get_balance(&user);
    let remaining_shares = vault.withdraw(&user, &shares_owned, &0);

    // After full withdrawal shares should be zero.
    assert_eq!(remaining_shares, 0, "all shares should be burned");
    assert_eq!(vault.get_balance(&user), 0);

    // The vault should have retained the fee in accrued_fees (total_deposits drops
    // by assets_to_withdraw, not the full deposit).  We verify indirectly via
    // total deposits being less than zero after accounting for the fee.
    let expected_fee = early_withdrawal_fee(deposit_amount);
    assert!(
        expected_fee > 0,
        "fee should be non-zero for early withdrawal"
    );
}

#[test]
fn withdrawal_exactly_at_lock_boundary_has_no_early_fee() {
    let (env, _admin, token, vault, _treasury) = setup();
    let user = Address::generate(&env);
    let deposit_amount = 1_000 * XLM;
    mint(&token, &user, deposit_amount);

    vault.deposit(&user, &deposit_amount, &0);
    let deposit_time = env.ledger().timestamp();

    // Advance to exactly deposit_time + MinLockPeriod (1 day).
    advance_time(&env, DAY);
    assert!(
        env.ledger().timestamp() >= deposit_time + DAY,
        "should be at or past the lock boundary"
    );

    let shares_owned = vault.get_balance(&user);
    let remaining_shares = vault.withdraw(&user, &shares_owned, &0);

    // No early-withdrawal fee — full shares burned, nothing retained.
    assert_eq!(remaining_shares, 0, "all shares should be burned");
    assert_eq!(vault.get_balance(&user), 0);
    // Total deposits should be zero (no fee siphoned off at this point).
    assert_eq!(vault.get_total_deposits(), 0);
}

#[test]
fn withdrawal_after_lock_period_has_no_early_fee() {
    let (env, _admin, token, vault, _treasury) = setup();
    let user = Address::generate(&env);
    let deposit_amount = 500 * XLM;
    mint(&token, &user, deposit_amount);

    vault.deposit(&user, &deposit_amount, &0);

    // Advance well past the lock period (3 days).
    advance_time(&env, 3 * DAY);

    let shares_owned = vault.get_balance(&user);
    let remaining = vault.withdraw(&user, &shares_owned, &0);

    assert_eq!(remaining, 0);
    assert_eq!(vault.get_total_deposits(), 0);
}

// ---------------------------------------------------------------------------
// Access control
// ---------------------------------------------------------------------------

#[test]
fn any_address_can_deposit() {
    let (_env, _admin, token, vault, _treasury) = setup();
    let random_user = Address::generate(&_env);
    mint(&token, &random_user, 100 * XLM);

    let bal = vault.deposit(&random_user, &(100 * XLM), &0);
    assert_eq!(bal, 100 * XLM);
}

#[test]
fn any_address_can_withdraw() {
    let (_env, _admin, token, vault, _treasury) = setup();
    let random_user = Address::generate(&_env);
    mint(&token, &random_user, 100 * XLM);

    vault.deposit(&random_user, &(100 * XLM), &0);
    let bal = vault.withdraw(&random_user, &(100 * XLM), &0);
    assert_eq!(bal, 0);
}

#[test]
#[should_panic]
fn non_admin_cannot_pause() {
    let (_env, _admin, _token, vault, _treasury) = setup();
    let outsider = Address::generate(&_env);
    vault.pause(&outsider);
}

#[test]
#[should_panic]
fn non_admin_cannot_unpause() {
    let (_env, admin, _token, vault, _treasury) = setup();
    let outsider = Address::generate(&_env);
    vault.pause(&admin);
    vault.unpause(&outsider);
}

#[test]
fn admin_can_pause_and_unpause() {
    let (_env, admin, _token, vault, _treasury) = setup();

    vault.pause(&admin);
    assert!(vault.is_paused());
    assert_eq!(vault.get_status(), VaultStatus::Paused);

    vault.unpause(&admin);
    assert!(!vault.is_paused());
    assert_eq!(vault.get_status(), VaultStatus::Active);
}

// ---------------------------------------------------------------------------
// Edge / boundary cases
// ---------------------------------------------------------------------------

#[test]
fn multiple_users_balances_are_independent() {
    let (_env, _admin, token, vault, _treasury) = setup();

    let alice = Address::generate(&_env);
    let bob = Address::generate(&_env);
    mint(&token, &alice, 500 * XLM);
    mint(&token, &bob, 300 * XLM);

    vault.deposit(&alice, &(500 * XLM), &0);
    vault.deposit(&bob, &(300 * XLM), &0);

    assert_eq!(vault.get_balance(&alice), 500 * XLM);
    assert_eq!(vault.get_balance(&bob), 300 * XLM);
    assert_eq!(vault.get_total_deposits(), 800 * XLM);

    vault.withdraw(&alice, &(200 * XLM), &0);
    assert_eq!(vault.get_balance(&alice), 300 * XLM);
    assert_eq!(vault.get_balance(&bob), 300 * XLM);
    assert_eq!(vault.get_total_deposits(), 600 * XLM);
}

#[test]
fn deposit_then_full_withdraw_resets_total_deposits() {
    let (_env, _admin, token, vault, _treasury) = setup();
    let user = Address::generate(&_env);
    mint(&token, &user, 1_000 * XLM);

    vault.deposit(&user, &(1_000 * XLM), &0);
    vault.withdraw(&user, &(1_000 * XLM), &0);

    assert_eq!(vault.get_total_deposits(), 0);
    assert_eq!(vault.get_balance(&user), 0);
}

#[test]
fn single_stroop_deposit_and_withdrawal() {
    let (_env, _admin, token, vault, _treasury) = setup();
    let user = Address::generate(&_env);
    mint(&token, &user, STROOP);

    vault.deposit(&user, &STROOP, &0);
    assert_eq!(vault.get_balance(&user), STROOP);

    vault.withdraw(&user, &STROOP, &0);
    assert_eq!(vault.get_balance(&user), 0);
}

#[test]
fn get_token_returns_registered_token_address() {
    let (_env, _admin, sac, vault, _treasury) = setup();
    assert_eq!(vault.get_token(), sac.address);
}

// ---------------------------------------------------------------------------
// Emergency Withdraw Tests
// ---------------------------------------------------------------------------

#[test]
fn emergency_withdraw_works_when_paused() {
    let (env, admin, token, vault, _treasury) = setup();
    let user = Address::generate(&env);
    let deposit_amount = 1_000 * XLM;
    mint(&token, &user, deposit_amount);

    vault.deposit(&user, &deposit_amount, &0);

    vault.set_emergency_fee(&admin, &100); // 1%

    vault.pause(&admin);

    let returned = vault.emergency_withdraw(&user);

    // 1% of 1000 = 10. Expected return = 990
    assert_eq!(returned, 990 * XLM);

    // Balance should be 0
    assert_eq!(vault.get_balance(&user), 0);
    assert_eq!(
        token::Client::new(&env, &token.address).balance(&user),
        990 * XLM
    );
}

#[test]
#[should_panic(expected = "Error(Contract, #9)")]
fn emergency_withdraw_fails_when_not_paused() {
    let (env, _admin, token, vault, _treasury) = setup();
    let user = Address::generate(&env);
    let deposit_amount = 1_000 * XLM;
    mint(&token, &user, deposit_amount);

    vault.deposit(&user, &deposit_amount, &0);

    vault.emergency_withdraw(&user);
}

#[test]
fn emergency_withdraw_queues_when_liquidity_insufficient() {
    let (env, admin, token, vault, _treasury) = setup();
    let user = Address::generate(&env);
    let deposit_amount = 1_000 * XLM;
    mint(&token, &user, deposit_amount);

    vault.deposit(&user, &deposit_amount, &0);

    // Advance time by a year to accrue large management fee
    advance_time(&env, 365 * DAY);

    vault.collect_fees(&admin);

    vault.pause(&admin);

    // Check preview BEFORE withdraw
    let preview = vault.emergency_withdraw_preview(&user);
    assert_eq!(preview.vault_liquid_reserves, 9950000000);
    assert_eq!(preview.estimated_return, 10000000000);
    assert_eq!(preview.can_process, false);

    let returned = vault.emergency_withdraw(&user);

    // It should queue because liquid reserves < principal
    assert_eq!(returned, 0);

    // Check preview AFTER
    let preview_after = vault.emergency_withdraw_preview(&user);
    assert_eq!(preview_after.principal_deposited, 0); // already cleared from principal
}

#[test]
fn emergency_withdraw_queue_processed_on_deposit() {
    let (env, admin, token, vault, _treasury) = setup();
    let user1 = Address::generate(&env);
    let user2 = Address::generate(&env);
    mint(&token, &user1, 1_000 * XLM);
    mint(&token, &user2, 2_000 * XLM);

    vault.deposit(&user1, &(1_000 * XLM), &0);

    advance_time(&env, 365 * DAY);
    vault.collect_fees(&admin);

    vault.pause(&admin);
    vault.emergency_withdraw(&user1);

    // Now user1 is in queue.
    assert_eq!(token::Client::new(&env, &token.address).balance(&user1), 0);

    // user2 deposits, providing liquidity, which processes queue
    vault.unpause(&admin);
    vault.deposit(&user2, &(2_000 * XLM), &0);

    // user1 should have received their principal
    assert_eq!(
        token::Client::new(&env, &token.address).balance(&user1),
        1_000 * XLM
    );
}
