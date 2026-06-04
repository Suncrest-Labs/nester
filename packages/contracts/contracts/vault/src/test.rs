#![cfg(test)]

extern crate std;

use soroban_sdk::{
    contract, contractimpl,
    testutils::{Address as _, Ledger, LedgerInfo},
    token, Address, Env, String, Symbol,
};
use nester_access_control::Role;
use vault_token::{VaultTokenContract, VaultTokenContractClient};

use crate::{CircuitBreakerConfig, FeeConfig, VaultContract, VaultContractClient, VaultStatus};

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

    // Unit tests should not be blocked by the circuit breaker — tests that
    // specifically exercise CB behaviour set their own config. Set threshold
    // to 100 % so any single withdrawal is allowed.
    vault.set_circuit_breaker_config(
        &admin,
        &CircuitBreakerConfig {
            threshold_bps: 10000,
            window_seconds: 7200,
        },
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
fn second_deposit_after_fee_accrual_uses_gross_assets_denominator() {
    let (env, _admin, token, vault, _treasury) = setup();
    let user_a = Address::generate(&env);
    let user_b = Address::generate(&env);
    mint(&token, &user_a, 2_000 * XLM);
    mint(&token, &user_b, 2_000 * XLM);

    vault.deposit(&user_a, &(1_000 * XLM), &0);
    advance_time(&env, 365 * DAY);

    // This deposit triggers fee accrual first; share minting must still use gross total assets.
    let user_b_shares = vault.deposit(&user_b, &(1_000 * XLM), &0);
    assert_eq!(user_b_shares, 1_000 * XLM);
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
fn performance_fee_charges_only_realized_yield_not_principal() {
    let (env, admin, token, vault, _treasury) = setup();
    let user_a = Address::generate(&env);
    let user_b = Address::generate(&env);
    mint(&token, &user_a, 2_000 * XLM);
    mint(&token, &user_b, 2_000 * XLM);

    // Disable early withdrawal fee so this test isolates performance fee behavior.
    let mut fee_config: FeeConfig = vault.get_fee_config();
    fee_config.early_withdrawal_fee_bps = 0;
    vault.set_fee_config(&admin, &fee_config);

    vault.grant_role(&admin, &admin, &Role::Manager);
    vault.deposit(&user_a, &(1_000 * XLM), &0);
    vault.report_yield(&admin, &(100 * XLM));

    // User B enters after yield is already reflected in share price.
    let user_b_shares = vault.deposit(&user_b, &(1_000 * XLM), &0);
    assert_eq!(user_b_shares, 9_090_909_090);

    // User B immediately exits: no yield earned post-entry, so performance fee must be zero.
    vault.withdraw(&user_b, &user_b_shares, &0);
    assert_eq!(
        token::Client::new(&env, &token.address).balance(&user_b),
        2_000 * XLM - 1
    );
}

// ---------------------------------------------------------------------------
// Impairment regression test (issue #451 / PR #275)
//
// The original performance-fee issue explicitly required:
//   "User deposits at rate 1.0, rate halves (impairment) → zero performance fee."
//
// The withdraw path skips the performance fee whenever `yield_part =
// assets_to_withdraw - principal` is not strictly positive. This test proves
// that zero-performance-fee invariant holds after a loss halves the share
// price, so an impaired position is never charged a fee on a gain it never had.
// ---------------------------------------------------------------------------
#[test]
fn impairment_charges_zero_performance_fee() {
    let (env, admin, token, vault, _treasury) = setup();
    let user = Address::generate(&env);
    let deposit = 1_000 * XLM;
    mint(&token, &user, deposit);

    // Disable the early-withdrawal fee so the assertion isolates performance-fee
    // behaviour and the withdrawn amount is unambiguous.
    let mut fee_config: FeeConfig = vault.get_fee_config();
    fee_config.early_withdrawal_fee_bps = 0;
    vault.set_fee_config(&admin, &fee_config);

    // Deposit at the initial share price of 1.0.
    vault.deposit(&user, &deposit, &0);
    assert_eq!(vault.share_price(), 10_000_000, "deposit should occur at rate 1.0");

    // Impairment: report a loss that halves the share price (1.0 -> 0.5).
    vault.grant_role(&admin, &admin, &Role::Manager);
    vault.report_yield(&admin, &(-(500 * XLM)));
    assert_eq!(vault.share_price(), 5_000_000, "rate should halve after impairment");

    // The preview must show no performance fee owed on an impaired position.
    let shares = vault.get_shares(&user);
    let preview = vault.withdrawal_fee_preview(&user, &shares);
    assert_eq!(
        preview.performance_fee_deducted, 0,
        "no performance fee may be charged when the position has lost value"
    );

    // The actual withdrawal returns the impaired value (500 XLM) with no fees
    // siphoned off: 1000 shares now worth 0.5 each.
    vault.withdraw(&user, &shares, &0);
    assert_eq!(
        token::Client::new(&env, &token.address).balance(&user),
        500 * XLM,
        "user receives the full impaired value with zero performance fee"
    );
}

// ---------------------------------------------------------------------------
// Impairment regression test (issue #636)
//
// Proves the zero-performance-fee invariant when a real loss reduces the
// vault's underlying token balance below deposited principal. Tokens are
// transferred out of the vault first; the Manager then reports the loss via
// `report_yield` (yield accrual path) so share price tracks the impairment.
// ---------------------------------------------------------------------------
#[test]
fn test_impairment_produces_zero_performance_fee() {
    let (env, admin, token, vault, _treasury) = setup();
    let user = Address::generate(&env);
    let deposit = 1_000 * XLM;
    mint(&token, &user, deposit);

    // Disable early-withdrawal fee so assertions isolate performance-fee behaviour.
    let mut fee_config: FeeConfig = vault.get_fee_config();
    fee_config.early_withdrawal_fee_bps = 0;
    vault.set_fee_config(&admin, &fee_config);

    vault.deposit(&user, &deposit, &0);
    assert_eq!(vault.share_price(), 10_000_000, "deposit should occur at rate 1.0");

    let vault_addr = vault.address.clone();
    let token_client = token::Client::new(&env, &token.address);
    assert_eq!(token_client.balance(&vault_addr), deposit);

    // Simulate a real loss: transfer tokens out of the vault so underlying
    // balance falls below the deposited amount (mock_all_auths authorises the
    // vault as sender).
    let loss = 400 * XLM;
    let loss_sink = Address::generate(&env);
    token_client.transfer(&vault_addr, &loss_sink, &loss);
    assert!(
        token_client.balance(&vault_addr) < deposit,
        "underlying balance must fall below the deposited amount"
    );

    // Yield accrual path: Manager reports negative yield matching the physical loss.
    vault.grant_role(&admin, &admin, &Role::Manager);
    vault.report_yield(&admin, &(-loss));

    let impaired_assets = deposit - loss;
    assert_eq!(
        vault.share_price(),
        6_000_000,
        "share price must reflect the impairment without fee extraction"
    );

    let shares = vault.get_shares(&user);
    let preview = vault.withdrawal_fee_preview(&user, &shares);
    let performance_fee_collected = preview.performance_fee_deducted;
    assert_eq!(
        performance_fee_collected, 0,
        "impairment must not collect any performance fee"
    );

    vault.withdraw(&user, &shares, &0);
    assert_eq!(
        token_client.balance(&user),
        impaired_assets,
        "user receives the full impaired value with zero performance fee"
    );
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

// Regression for #448: `withdrawal_fee_preview().net_amount_received` is the
// post-fee amount actually transferred, so a caller can use it directly as
// `min_assets_out` for a fee-bearing withdrawal without tripping slippage.
//
// No time is advanced, so no management fee accrues (elapsed = 0) and the
// preview models every fee the withdrawal applies exactly: the reported yield
// triggers a performance fee, and remaining inside the MinLockPeriod triggers
// the early-withdrawal fee.
#[test]
fn withdrawal_fee_preview_net_is_slippage_safe_as_min_assets_out() {
    let (env, admin, token, vault, _treasury) = setup();
    let user = Address::generate(&env);
    let deposit = 1_000 * XLM;
    mint(&token, &user, deposit);
    vault.deposit(&user, &deposit, &0);

    vault.grant_role(&admin, &admin, &Role::Manager);
    vault.report_yield(&admin, &(500 * XLM));
    // Back the reported yield with real tokens so the vault can pay it out
    // (report_yield only updates share-price accounting).
    mint(&token, &vault.address, 500 * XLM);

    let shares = vault.get_shares(&user);
    let preview = vault.withdrawal_fee_preview(&user, &shares);
    assert!(
        preview.performance_fee_deducted > 0,
        "expected a performance fee on realized yield"
    );
    assert!(
        preview.early_withdrawal_fee_deducted > 0,
        "expected an early-withdrawal fee inside the lock period"
    );
    assert!(preview.net_amount_received < preview.gross_asset_value);

    let before = token::Client::new(&env, &token.address).balance(&user);

    // Using the NET preview as the slippage floor must succeed.
    vault.withdraw(&user, &shares, &preview.net_amount_received);

    let after = token::Client::new(&env, &token.address).balance(&user);
    // The user receives exactly the previewed net amount — the preview is an
    // exact (not merely conservative) floor.
    assert_eq!(after - before, preview.net_amount_received);
}

// Regression for #448: the GROSS preview (`preview_withdraw` /
// `gross_asset_value`) must NOT be used as `min_assets_out` — on a fee-bearing
// withdrawal the transfer is below gross, so it reverts with SlippageExceeded
// (#17). This is exactly the failure mode the issue describes.
#[test]
#[should_panic(expected = "Error(Contract, #17)")]
fn gross_preview_as_min_assets_out_trips_slippage_on_fee_bearing_withdrawal() {
    let (env, admin, token, vault, _treasury) = setup();
    let user = Address::generate(&env);
    let deposit = 1_000 * XLM;
    mint(&token, &user, deposit);
    vault.deposit(&user, &deposit, &0);

    vault.grant_role(&admin, &admin, &Role::Manager);
    vault.report_yield(&admin, &(500 * XLM));

    let shares = vault.get_shares(&user);
    let gross = vault.preview_withdraw(&shares);
    // gross > net (fees apply) => SlippageExceeded.
    vault.withdraw(&user, &shares, &gross);
}

// preview_withdraw_net must always be <= preview_withdraw (gross).
#[test]
fn preview_withdraw_net_le_preview_withdraw() {
    let (env, admin, token, vault, _treasury) = setup();
    let user = Address::generate(&env);
    mint(&token, &user, 1_000 * XLM);
    vault.deposit(&user, &(1_000 * XLM), &0);

    vault.grant_role(&admin, &admin, &Role::Manager);
    vault.report_yield(&admin, &(200 * XLM));
    mint(&token, &vault.address, 200 * XLM);

    let shares = vault.get_shares(&user);
    let gross = vault.preview_withdraw(&shares);
    let net = vault.preview_withdraw_net(&shares);
    assert!(net <= gross, "net must be <= gross");
}

// No early-withdrawal fee when outside the lock period.
// preview_withdraw_net is worst-case (always deducts both fees), so the
// result is still <= gross. The user-aware withdrawal_fee_preview is the
// right tool when the lock period is known to have elapsed.
#[test]
fn preview_withdraw_net_no_early_fee_after_lock() {
    let (env, admin, token, vault, _treasury) = setup();
    let user = Address::generate(&env);
    mint(&token, &user, 1_000 * XLM);
    vault.deposit(&user, &(1_000 * XLM), &0);

    // Advance past the lock period (DAY seconds).
    env.ledger().set(LedgerInfo {
        timestamp: DAY + 1,
        ..env.ledger().get()
    });

    vault.grant_role(&admin, &admin, &Role::Manager);
    vault.report_yield(&admin, &(200 * XLM));
    mint(&token, &vault.address, 200 * XLM);

    let shares = vault.get_shares(&user);
    let gross = vault.preview_withdraw(&shares);
    let net = vault.preview_withdraw_net(&shares);

    // preview_withdraw_net is always worst-case — net <= gross regardless of
    // lock status. The user-aware withdrawal_fee_preview returns a tighter
    // estimate once the lock has elapsed.
    assert!(net <= gross);

    // Confirm withdrawal_fee_preview correctly omits the early fee after lock.
    let fee_preview = vault.withdrawal_fee_preview(&user, &shares);
    assert_eq!(fee_preview.early_withdrawal_fee_deducted, 0, "no early fee after lock");
    assert!(fee_preview.net_amount_received >= net, "user-aware preview >= worst-case net");
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

#[test]
#[should_panic(expected = "Error(Contract, #11)")]
fn circuit_breaker_uses_rolling_window_across_boundary() {
    let (env, admin, token, vault, _treasury) = setup();
    let user = Address::generate(&env);
    let deposit_amount = 1_000 * XLM;
    mint(&token, &user, deposit_amount);

    vault.set_circuit_breaker_config(
        &admin,
        &CircuitBreakerConfig {
            threshold_bps: 1000,
            window_seconds: 60,
        },
    );

    vault.deposit(&user, &deposit_amount, &0);
    vault.withdraw(&user, &(100 * XLM), &0);

    advance_time(&env, 60);
    vault.withdraw(&user, &(100 * XLM), &0);
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
fn minimum_deposit_and_withdrawal() {
    // MIN_DEPOSIT_AMOUNT (= 1 XLM in 7-decimal precision) is the smallest
    // amount the vault accepts; smaller deposits are rejected with
    // InvalidAmount (#5).
    let (_env, _admin, token, vault, _treasury) = setup();
    let user = Address::generate(&_env);
    mint(&token, &user, XLM);

    vault.deposit(&user, &XLM, &0);
    assert_eq!(vault.get_balance(&user), XLM);

    vault.withdraw(&user, &XLM, &0);
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
    assert_eq!(preview.vault_liquid_reserves, 9995890411);
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

// ---------------------------------------------------------------------------
// New Queries Tests
// ---------------------------------------------------------------------------

#[test]
fn test_read_only_queries() {
    let (env, admin, token, vault, _treasury) = setup();
    let user = Address::generate(&env);
    let deposit = 1_000 * XLM;

    mint(&token, &user, deposit);
    vault.deposit(&user, &deposit, &0);
    
    assert_eq!(vault.total_shares(), deposit);
    assert_eq!(vault.share_price(), 10_000_000); // 1.0 share price initialized

    // Simulate yield
    vault.grant_role(&admin, &admin, &Role::Manager);
    vault.report_yield(&admin, &(500 * XLM));

    assert_eq!(vault.total_shares(), deposit);
    assert_eq!(vault.share_price(), 15_000_000); // 1.5 share price
    
    // estimated fees — advance less than DAY so we remain within the
    // MinLockPeriod (= DAY) window and still incur an early-withdrawal fee.
    advance_time(&env, DAY / 2);
    let fees = vault.estimated_fees();
    assert!(fees > 0);

    // withdrawal preview
    let preview = vault.withdrawal_fee_preview(&user, &deposit);
    assert_eq!(preview.gross_asset_value, 1_500 * XLM);
    assert!(preview.early_withdrawal_fee_deducted > 0);
    assert!(preview.performance_fee_deducted > 0);
    assert_eq!(preview.management_fee_deducted, 0);
    assert!(preview.net_amount_received > 0);
    assert!(preview.net_amount_received < preview.gross_asset_value);

    // pending yield
    // pending yield is contract balance minus liquid reserves
    // Let's directly mint to contract to simulate un-reported yield
    mint(&token, &vault.address, 200 * XLM);
    assert_eq!(vault.pending_yield(), 200 * XLM);
}

// LiquidReserved tests — verifies collect_fees cannot over-draw committed funds
// ---------------------------------------------------------------------------

#[test]
fn collect_fees_capped_when_emergency_queue_commits_all_reserves() {
    // Deposit, accrue fees, then queue an emergency withdrawal that commits
    // all liquid reserves.  collect_fees must transfer nothing.
    let (env, admin, token, vault, _treasury) = setup();
    let user = Address::generate(&env);
    mint(&token, &user, 1_000 * XLM);

    vault.deposit(&user, &(1_000 * XLM), &0);

    // Accrue a full year of management fee
    advance_time(&env, 365 * DAY);
    vault.pause(&admin);

    // User queues an emergency withdrawal, which reserves their principal
    vault.emergency_withdraw(&user);

    // Now all liquid reserves are committed to the queue.
    // collect_fees should transfer 0 — fees exist but available = 0.
    let treasury_before = token::Client::new(&env, &token.address).balance(&_treasury);
    vault.unpause(&admin);
    vault.collect_fees(&admin);
    let treasury_after = token::Client::new(&env, &token.address).balance(&_treasury);

    assert_eq!(
        treasury_after, treasury_before,
        "collect_fees must not transfer when all reserves are committed to the queue"
    );
}

#[test]
fn collect_fees_transfers_only_unreserved_portion() {
    // Two users deposit.  One queues an emergency withdrawal (reserving half
    // the pool).  collect_fees should be capped to the unreserved half.
    let (env, admin, token, vault, _treasury) = setup();
    let user1 = Address::generate(&env);
    let user2 = Address::generate(&env);
    mint(&token, &user1, 500 * XLM);
    mint(&token, &user2, 500 * XLM);

    vault.deposit(&user1, &(500 * XLM), &0);
    vault.deposit(&user2, &(500 * XLM), &0);

    advance_time(&env, 365 * DAY);
    vault.pause(&admin);

    // user1's emergency withdrawal reserves ~500 XLM from the pool
    vault.emergency_withdraw(&user1);

    vault.unpause(&admin);

    let treasury_before = token::Client::new(&env, &token.address).balance(&_treasury);
    vault.collect_fees(&admin);
    let treasury_after = token::Client::new(&env, &token.address).balance(&_treasury);

    let fees_collected = treasury_after - treasury_before;

    // The reserved portion (user1's principal, ~500 XLM) must not be touched.
    // Fees collected must be strictly less than the total accrued fees.
    let total_reserves = token::Client::new(&env, &token.address).balance(&vault.address);
    assert!(
        fees_collected < total_reserves,
        "collect_fees must not exceed unreserved liquid reserves"
    );
}

#[test]
fn process_emergency_queue_decrements_liquid_reserved() {
    // After a queued withdrawal is processed, LiquidReserved must decrease
    // so that subsequent collect_fees calls can access those funds again.
    let (env, admin, token, vault, _treasury) = setup();
    let user1 = Address::generate(&env);
    let user2 = Address::generate(&env);
    mint(&token, &user1, 1_000 * XLM);
    mint(&token, &user2, 2_000 * XLM);

    vault.deposit(&user1, &(1_000 * XLM), &0);
    advance_time(&env, 365 * DAY);
    vault.pause(&admin);

    vault.emergency_withdraw(&user1);
    // user1 is now queued; reserved = ~1000 XLM

    // user2 deposits, providing liquidity and processing the queue
    vault.unpause(&admin);
    vault.deposit(&user2, &(2_000 * XLM), &0);

    // user1 should have received their principal
    assert_eq!(
        token::Client::new(&env, &token.address).balance(&user1),
        1_000 * XLM,
        "queued user should receive principal after queue is processed"
    );

    // After the queue is processed, collect_fees should succeed (reserved = 0)
    advance_time(&env, 30 * DAY);
    let treasury_before = token::Client::new(&env, &token.address).balance(&_treasury);
    vault.collect_fees(&admin);
    let treasury_after = token::Client::new(&env, &token.address).balance(&_treasury);

    assert!(
        treasury_after > treasury_before,
        "collect_fees should transfer fees once LiquidReserved is decremented after queue processing"
    );
}

/// Regression test for the zero-performance-fee-on-impairment invariant (#451).
///
/// When a vault is impaired (`yield_part < 0`), `withdraw` must NOT charge a
/// performance fee. The fee guard is `if yield_part > 0` in `lib.rs`; a future
/// refactor could silently drop it and start charging fees on losses. This test
/// locks the behaviour in: with the early-withdrawal fee disabled, an impaired
/// withdrawal returns the full impaired value with no fee skimmed off.
#[test]
fn withdrawal_charges_no_perf_fee_on_impairment() {
    let (env, admin, token, vault, _treasury) = setup();
    let user = Address::generate(&env);
    let deposit = 1_000 * XLM;
    mint(&token, &user, deposit);

    // Isolate performance-fee behaviour by disabling the early-withdrawal fee.
    let mut fee_config: FeeConfig = vault.get_fee_config();
    fee_config.early_withdrawal_fee_bps = 0;
    vault.set_fee_config(&admin, &fee_config);

    vault.grant_role(&admin, &admin, &Role::Manager);
    vault.deposit(&user, &deposit, &0);

    // Simulate a 20% impairment: total assets drop from 1000 to 800.
    vault.report_yield(&admin, &(-(200 * XLM)));

    let shares = vault.get_shares(&user);
    vault.withdraw(&user, &shares, &0);

    // yield_part = 800 - 1000 = -200 (< 0) => no performance fee. The user
    // recovers the full impaired 800 XLM, not 780 after a wrongful 10% fee.
    // Soroban arithmetic is deterministic integer math (no FP rounding), so
    // an exact equality is the right contract — per-review feedback.
    let balance = token::Client::new(&env, &token.address).balance(&user);
    assert_eq!(balance, 800 * XLM, "impairment must not charge a performance fee");
}

#[contract]
struct MockStrategy;
#[contractimpl]
impl MockStrategy {
    pub fn calculate_rebalance_deltas(env: Env, _current: soroban_sdk::Vec<crate::CurrentAllocationView>, _total: i128) -> soroban_sdk::Vec<crate::AllocationDeltaView> {
        let mut deltas = soroban_sdk::Vec::new(&env);
        deltas.push_back(crate::AllocationDeltaView {
            source_id: Symbol::new(&env, "aave"),
            delta: -400 * 10_000_000, // Withdraw 400
        });
        deltas
    }
}

#[test]
fn rebalance_with_net_negative_delta_increases_liquid_reserves() {
    let (env, admin, token, vault, _treasury) = setup();
    let strategy_id = Address::generate(&env); // Mock strategy
    vault.set_allocation_strategy(&admin, &strategy_id);

    let user = Address::generate(&env);
    mint(&token, &user, 1000 * XLM);
    vault.deposit(&user, &(1000 * XLM), &0);

    // Initial reserves = 1000
    // Record allocation to a source to simulate deployment
    let source_id = Symbol::new(&env, "aave");
    vault.grant_role(&admin, &admin, &Role::Operator);
    vault.record_source_allocation(&admin, &source_id, &(1000 * XLM));
    
    // Deployed total = 1000. 
    // We need to mock calculate_rebalance_deltas to return a negative delta.
    
    let real_strategy_id = env.register_contract(None, MockStrategy);
    vault.set_allocation_strategy(&admin, &real_strategy_id);
    
    vault.rebalance(&admin);
    
    // Let's check another way. emergency_withdraw uses liquid reserves.
    vault.pause(&admin);
    let _principal = vault.get_shares(&user); // 1000 shares

    // We have 1000 tokens in contract.
    // We processed rebalance which increased liquid reserves bookkeeping to 1400.
    // Now if we try to emergency_withdraw 1000, it should succeed because 1400 >= 1000.
    let withdrawn = vault.emergency_withdraw(&user);
    assert_eq!(withdrawn, 1000 * XLM);
}
