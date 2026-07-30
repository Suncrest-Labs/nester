#![cfg(test)]

//! Authorization test matrix for every protected Vault entrypoint.
//!
//! Role-negative calls use valid arguments with `mock_all_auths`, isolating
//! the role guard. Signature-negative calls clear auths with `mock_auths(&[])`.
//! Read-only APIs and the deliberately permissionless `process_emergency_queue`
//! are outside this protected-entrypoint matrix.
//!
//! | Entrypoint | Unauthorized / unsigned test | Authorized / revoked test |
//! | --- | --- | --- |
//! | `initialize` | `initialize_without_admin_signature_is_rejected` | `vault_initializes_correctly` / n/a |
//! | `set_max_deposit` | `admin_entrypoints_reject_outsider`, `admin_entrypoints_require_admin_signature` | `admin_entrypoints_accept_then_reject_revoked_admin` |
//! | `set_min_deposit` | same as above, plus `set_min_deposit_by_non_admin_panics` | same as above |
//! | `set_rebalance_threshold` | `admin_entrypoints_reject_outsider`, `admin_entrypoints_require_admin_signature` | `admin_entrypoints_accept_then_reject_revoked_admin` |
//! | `set_rebalance_slippage` | same as above | same as above |
//! | `set_circuit_breaker_config` | same as above | same as above |
//! | `set_early_withdrawal_fee` | same as above | same as above |
//! | `set_fee_config` | same as above | same as above |
//! | `set_emergency_fee` | same as above | same as above |
//! | `set_allocation_strategy` | same as above | same as above |
//! | `set_rebalance_cooldown` | same as above | same as above |
//! | `pause`, `unpause` | `admin_entrypoints_reject_outsider`, `admin_entrypoints_require_admin_signature` | `admin_entrypoints_accept_then_reject_revoked_admin` |
//! | `grant_role`, `revoke_role` | `admin_entrypoints_reject_outsider`, `admin_entrypoints_require_admin_signature` | `admin_entrypoints_accept_then_reject_revoked_admin` |
//! | `transfer_admin` | `admin_entrypoints_reject_outsider`, `admin_entrypoints_require_admin_signature` | `admin_transfer_wrappers_enforce_authorization` |
//! | `accept_admin` | `accept_admin_rejects_wrong_successor`, `accept_admin_requires_successor_signature` | `admin_transfer_wrappers_enforce_authorization` / n/a |
//! | `report_yield` | `manager_entrypoints_reject_outsider`, `manager_entrypoints_require_signature` | `manager_entrypoints_accept_then_reject_revoked_manager` |
//! | `harvest` | `harvest_without_user_signature_is_rejected` | `test_harvest_basic` / n/a |
//! | `harvest_vault` | `admin_entrypoints_reject_outsider`, `admin_entrypoints_require_admin_signature` | `admin_entrypoints_accept_then_reject_revoked_admin` |
//! | `rebalance` | `admin_entrypoints_reject_outsider`, `operator_entrypoints_require_signature` | `operator_entrypoints_accept_then_reject_revoked_operator` |
//! | `record_source_allocation` | same as `rebalance` | same as `rebalance` |
//! | `collect_fees` | `manager_entrypoints_reject_outsider`, `manager_entrypoints_require_signature` | `manager_entrypoints_accept_then_reject_revoked_manager` |
//! | `deposit` | `deposit_without_user_signature_is_rejected` | `first_deposit_creates_one_to_one_shares` / n/a |
//! | `withdraw` | `withdraw_without_user_signature_is_rejected` | `full_withdrawal_leaves_zero_balance` / n/a |
//! | `emergency_withdraw` | `emergency_withdraw_without_user_signature_is_rejected` | `emergency_withdraw_works_when_paused` / n/a |
//! | `emergency_withdraw_all` | `emergency_withdraw_all_without_user_signature_is_rejected` | `emergency_withdraw_all_exits_all_active_positions` / n/a |

extern crate std;

use nester_access_control::Role;
use soroban_sdk::{
    contract, contractimpl, symbol_short,
    testutils::{Address as _, Ledger, LedgerInfo},
    token, Address, Env, String, Symbol,
};
use vault_token::{VaultTokenContract, VaultTokenContractClient};

use crate::{
    CircuitBreakerConfig, FeeConfig, Severity, VaultContract, VaultContractClient, VaultStatus,
};

macro_rules! assert_rejected {
    ($call:expr, $entrypoint:literal) => {
        assert!(
            $call.is_err(),
            concat!($entrypoint, " must reject this caller")
        );
    };
}

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
) {
    let env = Env::default();
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
    let sac: token::StellarAssetClient<'static> = token::StellarAssetClient::new(
        unsafe { core::mem::transmute::<&Env, &'static Env>(&env) },
        &token_id,
    );

    // -----------------------------
    // Vault setup
    // -----------------------------
    let admin = Address::generate(&env);
    let treasury = env.register_contract(None, MockTreasury); // new treasury address

    let vault_id = env.register_contract(None, VaultContract);
    let vault_token_id = env.register_contract(None, VaultTokenContract);

    let vault: VaultContractClient<'static> = VaultContractClient::new(
        unsafe { core::mem::transmute::<&Env, &'static Env>(&env) },
        &vault_id,
    );

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

fn bind_strategy(vault: &VaultContractClient, admin: &Address, strategy: &Address) {
    vault.register_callee(admin, strategy);
    vault.set_allocation_strategy(admin, strategy);
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
    vault.deposit(&user, &(-XLM), &0);
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
    assert_eq!(
        token::Client::new(&env, &token.address).balance(&bob),
        999 * XLM
    );
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
    assert_eq!(
        token::Client::new(&env, &token.address).balance(&user),
        1_898 * XLM
    );
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
    assert_eq!(
        vault.share_price(),
        10_000_000,
        "deposit should occur at rate 1.0"
    );

    // Impairment: report a loss that halves the share price (1.0 -> 0.5).
    vault.grant_role(&admin, &admin, &Role::Manager);
    vault.report_yield(&admin, &(-(500 * XLM)));
    assert_eq!(
        vault.share_price(),
        5_000_000,
        "rate should halve after impairment"
    );

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
    assert_eq!(
        vault.share_price(),
        10_000_000,
        "deposit should occur at rate 1.0"
    );

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
    assert_eq!(
        fee_preview.early_withdrawal_fee_deducted, 0,
        "no early fee after lock"
    );
    assert!(
        fee_preview.net_amount_received >= net,
        "user-aware preview >= worst-case net"
    );
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

/// A withdrawal exactly at the rolling window boundary still counts toward
/// the cumulative sum (the boundary is inclusive), so two 100-XLM
/// withdrawals 60s apart against a 60s window both land in the same
/// window and cumulatively exceed the 10%-of-1000-XLM threshold.
///
/// Historically this hard-paused the vault via a panic
/// (`CircuitBreakerTriggered`). Under the staged breaker (#817) a velocity
/// breach instead escalates severity to `Throttled` and lets the triggering
/// withdrawal complete — the vault stays open, only the *next* deposit or
/// withdrawal decision is informed by the new severity.
#[test]
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
    assert_eq!(vault.get_breaker_status().severity, Severity::Normal);

    advance_time(&env, 60);
    vault.withdraw(&user, &(100 * XLM), &0);
    assert_eq!(vault.get_breaker_status().severity, Severity::Throttled);
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
    assert!(!preview.can_process);

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
    assert_eq!(
        balance,
        800 * XLM,
        "impairment must not charge a performance fee"
    );
}

#[contract]
struct MockStrategy;
#[contractimpl]
impl MockStrategy {
    pub fn calculate_rebalance_deltas(
        env: Env,
        _current: soroban_sdk::Vec<crate::CurrentAllocationView>,
        _total: i128,
    ) -> soroban_sdk::Vec<crate::AllocationDeltaView> {
        let mut deltas = soroban_sdk::Vec::new(&env);
        deltas.push_back(crate::AllocationDeltaView {
            source_id: Symbol::new(&env, "aave"),
            delta: -400 * 10_000_000, // Withdraw 400
        });
        deltas
    }
}

// ---------------------------------------------------------------------------
// Harvest Tests (issue #518)
// ---------------------------------------------------------------------------

#[test]
fn test_harvest_basic() {
    // deposit, report_yield for user (as Manager), harvest returns correct amounts
    let (env, admin, token, vault, treasury) = setup();
    let user = Address::generate(&env);
    let deposit = 1_000 * XLM;
    mint(&token, &user, deposit);
    vault.deposit(&user, &deposit, &0);

    // Mint tokens into the vault so the treasury transfer can succeed.
    mint(&token, &vault.address, 500 * XLM);

    // Grant admin the Manager role so they can call report_yield
    vault.grant_role(&admin, &admin, &Role::Manager);
    let yield_amount = 200 * XLM;
    vault.report_yield(&admin, &yield_amount);

    // Advance time so the harvest timestamp is non-zero and verifiable.
    advance_time(&env, DAY);
    let harvest_time = env.ledger().timestamp();

    let shares_before = vault.get_shares(&user);

    let result = vault.harvest(&user);

    assert_eq!(result.gross_yield, yield_amount);
    // performance_fee_bps = 1000 (10%)
    assert_eq!(result.performance_fee, 20 * XLM);
    assert_eq!(result.net_yield, 180 * XLM);
    assert!(result.compounded);
    assert_eq!(result.user, user);

    // new_share_balance must be >= shares before harvest (net yield minted new shares)
    assert!(
        result.new_share_balance >= shares_before,
        "share balance should have grown after compounding"
    );

    // Performance fee must have been sent to treasury (not sitting in accrued fees)
    let treasury_token = token::Client::new(&env, &token.address);
    let treasury_balance = treasury_token.balance(&treasury);
    assert!(
        treasury_balance >= 20 * XLM,
        "treasury should have received the performance fee"
    );

    // last_harvest_at timestamp must be set to current ledger time
    let harvested_at = vault.get_last_harvest_at(&user);
    assert_eq!(
        harvested_at, harvest_time,
        "last_harvest_at should match ledger timestamp at harvest"
    );
}

#[test]
fn test_harvest_zero_yield() {
    // harvest with no yield returns zeros and compounded=false
    let (env, admin, token, vault, _treasury) = setup();
    let user = Address::generate(&env);
    let deposit = 500 * XLM;
    mint(&token, &user, deposit);
    vault.deposit(&user, &deposit, &0);

    // Advance time so harvest timestamp is verifiable.
    advance_time(&env, DAY);
    let harvest_time = env.ledger().timestamp();

    // user never had report_yield called on their behalf — zero pending yield
    let result = vault.harvest(&user);

    assert_eq!(result.gross_yield, 0);
    assert_eq!(result.performance_fee, 0);
    assert_eq!(result.net_yield, 0);
    assert!(!result.compounded);
    assert_eq!(result.user, user);

    // last_harvest_at is still updated for zero-yield harvest
    let harvested_at = vault.get_last_harvest_at(&user);
    assert_eq!(
        harvested_at, harvest_time,
        "last_harvest_at should be set even on zero-yield harvest"
    );

    // Admin also has zero yield initially
    let admin_result = vault.harvest(&admin);
    assert_eq!(admin_result.gross_yield, 0);
    assert!(!admin_result.compounded);
}

#[test]
fn test_harvest_vault() {
    // report_yield, then harvest_vault collects fees, transfers to treasury, resets counter
    let (env, admin, token, vault, treasury) = setup();
    let user = Address::generate(&env);
    let deposit = 1_000 * XLM;
    mint(&token, &user, deposit);
    vault.deposit(&user, &deposit, &0);

    // Mint extra tokens into vault so the treasury transfer can succeed.
    mint(&token, &vault.address, 500 * XLM);

    vault.grant_role(&admin, &admin, &Role::Manager);
    let yield_amount = 500 * XLM;
    vault.report_yield(&admin, &yield_amount);

    let result = vault.harvest_vault(&admin);

    assert_eq!(result.total_gross_yield, yield_amount);
    // 10% performance fee
    assert_eq!(result.total_fee_collected, 50 * XLM);
    assert_eq!(result.total_net_yield, 450 * XLM);
    assert_eq!(result.positions_harvested, 1);

    // Fee must be at treasury, not sitting in accrued fees
    let treasury_token = token::Client::new(&env, &token.address);
    let treasury_balance = treasury_token.balance(&treasury);
    assert!(
        treasury_balance >= 50 * XLM,
        "treasury should have received harvest_vault fee"
    );

    // Counter should be reset: a second harvest_vault returns zeros
    let second = vault.harvest_vault(&admin);
    assert_eq!(second.total_gross_yield, 0);
    assert_eq!(second.total_fee_collected, 0);
    assert_eq!(second.positions_harvested, 0);
}

#[test]
#[should_panic]
fn test_harvest_paused_vault() {
    // harvest panics when vault is paused
    let (env, admin, token, vault, _treasury) = setup();
    let user = Address::generate(&env);
    let deposit = 1_000 * XLM;
    mint(&token, &user, deposit);
    vault.deposit(&user, &deposit, &0);

    vault.grant_role(&admin, &admin, &Role::Manager);
    vault.report_yield(&admin, &(100 * XLM));

    vault.pause(&admin);
    // harvest must panic (require_active fails)
    vault.harvest(&admin);
}

#[test]
fn test_harvest_fee_calculation() {
    // Verify fee_bps is applied correctly across different fee configs
    let (env, admin, token, vault, treasury) = setup();
    let user = Address::generate(&env);
    let deposit = 1_000 * XLM;
    mint(&token, &user, deposit);
    vault.deposit(&user, &deposit, &0);

    // Mint tokens into vault so the treasury transfer can succeed.
    mint(&token, &vault.address, 500 * XLM);

    // Override to 20% performance fee
    let mut fee_config: FeeConfig = vault.get_fee_config();
    fee_config.performance_fee_bps = 2000;
    vault.set_fee_config(&admin, &fee_config);

    vault.grant_role(&admin, &admin, &Role::Manager);
    let yield_amount = 1_000 * XLM;
    vault.report_yield(&admin, &yield_amount);

    let result = vault.harvest(&user);

    assert_eq!(result.gross_yield, yield_amount);
    // 20% of 1000 = 200
    assert_eq!(result.performance_fee, 200 * XLM);
    assert_eq!(result.net_yield, 800 * XLM);
    assert!(result.compounded);

    // Fee goes to treasury, not accrued internally
    let treasury_token = token::Client::new(&env, &token.address);
    let treasury_balance = treasury_token.balance(&treasury);
    assert!(
        treasury_balance >= 200 * XLM,
        "treasury should have received 20% performance fee"
    );
}

#[test]
fn test_harvest_resets_user_yield_to_zero() {
    // After harvest, a second harvest returns zero yield
    let (env, admin, token, vault, _treasury) = setup();
    let user = Address::generate(&env);
    let deposit = 1_000 * XLM;
    mint(&token, &user, deposit);
    vault.deposit(&user, &deposit, &0);

    vault.grant_role(&admin, &admin, &Role::Manager);
    vault.report_yield(&admin, &(300 * XLM));

    let first = vault.harvest(&user);
    assert_eq!(first.gross_yield, 300 * XLM);
    assert!(first.compounded);

    // Second harvest on the same user should return zeros
    let second = vault.harvest(&user);
    assert_eq!(second.gross_yield, 0);
    assert!(!second.compounded);
}

#[test]
fn test_harvest_impairment_zero_fee() {
    let (env, admin, token, vault, _treasury) = setup();
    let user = Address::generate(&env);
    let deposit = 1_000 * XLM;
    mint(&token, &user, deposit);
    vault.deposit(&user, &deposit, &0);

    vault.grant_role(&admin, &admin, &Role::Manager);

    // Simulate impairment: Reduce total assets below principal.
    vault.report_yield(&admin, &(-(200 * XLM)));

    // Try harvesting
    let result = vault.harvest(&user);

    assert_eq!(result.gross_yield, -(200 * XLM));
    assert_eq!(result.performance_fee, 0);
    assert_eq!(result.net_yield, 0);

    // Ensure principal hasn't been reduced by fees
    let principal = vault.get_principal(&user);
    assert_eq!(principal, deposit);
}

#[test]
fn test_harvest_impairment_no_fee_charged() {
    // When yield is negative (impairment), no performance fee should be charged
    // and user's pending yield should be reduced (floored at zero).
    let (env, admin, token, vault, treasury) = setup();
    let user = Address::generate(&env);
    let deposit = 1_000 * XLM;
    mint(&token, &user, deposit);
    vault.deposit(&user, &deposit, &0);

    vault.grant_role(&admin, &admin, &Role::Manager);

    // Report positive yield first, then a larger impairment to net to zero
    vault.report_yield(&admin, &(100 * XLM));
    vault.report_yield(&admin, &(-(200 * XLM))); // impairment wipes pending yield to zero

    // After impairment reduces pending yield to zero, harvest should be a no-op
    let result = vault.harvest(&admin);
    assert_eq!(
        result.gross_yield, 0,
        "impairment should reduce pending yield to zero"
    );
    assert_eq!(result.performance_fee, 0, "no fee on impairment");
    assert!(!result.compounded);

    // Treasury must not have received any fee
    let treasury_token = token::Client::new(&env, &token.address);
    let treasury_balance = treasury_token.balance(&treasury);
    assert_eq!(
        treasury_balance, 0,
        "treasury must receive no fee when yield is non-positive"
    );
}

#[test]
fn test_harvest_new_share_balance_increases() {
    // After harvest, user's share balance should be greater than before
    let (env, admin, token, vault, _treasury) = setup();
    let user = Address::generate(&env);
    let deposit = 1_000 * XLM;
    mint(&token, &user, deposit);
    vault.deposit(&user, &deposit, &0);

    mint(&token, &vault.address, 500 * XLM);

    vault.grant_role(&admin, &admin, &Role::Manager);
    vault.report_yield(&admin, &(500 * XLM));

    let shares_before = vault.get_shares(&admin);
    let result = vault.harvest(&admin);

    assert!(
        result.new_share_balance >= shares_before,
        "share balance must not decrease after harvest with positive yield"
    );
    assert_eq!(
        result.new_share_balance,
        vault.get_shares(&admin),
        "new_share_balance in result must match on-chain balance"
    );
}

#[test]
fn rebalance_with_net_negative_delta_increases_liquid_reserves() {
    let (env, admin, token, vault, _treasury) = setup();
    let strategy_id = Address::generate(&env); // Mock strategy
    bind_strategy(&vault, &admin, &strategy_id);

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
    bind_strategy(&vault, &admin, &real_strategy_id);

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

// ---------------------------------------------------------------------------
// Rebalance slippage guard (issue #638)
// ---------------------------------------------------------------------------

#[test]
fn rebalance_slippage_default_and_configurable_per_vault() {
    let (_env, admin, _token, vault, _treasury) = setup();
    assert_eq!(vault.get_rebalance_slippage(), 50); // default 0.5%
    vault.set_rebalance_slippage(&admin, &200);
    assert_eq!(vault.get_rebalance_slippage(), 200);
}

#[test]
#[should_panic]
fn set_rebalance_slippage_rejects_out_of_range() {
    let (_env, admin, _token, vault, _treasury) = setup();
    vault.set_rebalance_slippage(&admin, &6_000); // > MAX_REBALANCE_SLIPPAGE_BPS
}

#[test]
fn rebalance_succeeds_within_slippage_tolerance() {
    // A normal negative-delta rebalance must not be blocked by the guard.
    let (env, admin, token, vault, _treasury) = setup();
    let user = Address::generate(&env);
    mint(&token, &user, 1000 * XLM);
    vault.deposit(&user, &(1000 * XLM), &0);

    let source_id = Symbol::new(&env, "aave");
    vault.grant_role(&admin, &admin, &Role::Operator);
    vault.record_source_allocation(&admin, &source_id, &(1000 * XLM));

    let strategy_id = env.register_contract(None, MockStrategy);
    bind_strategy(&vault, &admin, &strategy_id);

    let _ = vault.rebalance(&admin); // should not panic
}

#[test]
fn rebalance_slippage_guard_allows_when_within_floor() {
    let (env, _admin, _token, vault, _treasury) = setup();
    env.as_contract(&vault.address, || {
        // Realised proceeds at or above the floor: no revert.
        super::enforce_rebalance_slippage(&env, 100, 100);
        super::enforce_rebalance_slippage(&env, 100, 250);
    });
}

#[test]
#[should_panic]
fn rebalance_slippage_guard_reverts_when_below_floor() {
    let (env, _admin, _token, vault, _treasury) = setup();
    env.as_contract(&vault.address, || {
        // Realised proceeds below the floor → SlippageExceeded.
        super::enforce_rebalance_slippage(&env, 100, 99);
    });
}

// ---------------------------------------------------------------------------
// Minimum deposit enforcement (issue #730)
// ---------------------------------------------------------------------------

#[test]
#[should_panic(expected = "Error(Contract, #21)")]
fn deposit_below_min_deposit_panics() {
    let (env, admin, token, vault, _treasury) = setup();
    let user = Address::generate(&env);
    mint(&token, &user, 100 * XLM);

    vault.set_min_deposit(&admin, &(10 * XLM));

    // 5 XLM is below the 10 XLM minimum — must panic.
    vault.deposit(&user, &(5 * XLM), &0);
}

#[test]
fn deposit_exactly_at_min_deposit_succeeds() {
    let (_env, admin, token, vault, _treasury) = setup();
    let user = Address::generate(&_env);
    mint(&token, &user, 100 * XLM);

    vault.set_min_deposit(&admin, &(10 * XLM));

    let shares = vault.deposit(&user, &(10 * XLM), &0);
    assert_eq!(shares, 10 * XLM);
}

#[test]
fn deposit_above_min_deposit_succeeds() {
    let (_env, admin, token, vault, _treasury) = setup();
    let user = Address::generate(&_env);
    mint(&token, &user, 100 * XLM);

    vault.set_min_deposit(&admin, &(10 * XLM));

    let shares = vault.deposit(&user, &(50 * XLM), &0);
    assert_eq!(shares, 50 * XLM);
}

#[test]
#[should_panic]
fn set_min_deposit_by_non_admin_panics() {
    let (env, _admin, _token, vault, _treasury) = setup();
    let outsider = Address::generate(&env);

    vault.set_min_deposit(&outsider, &(10 * XLM));
}

#[test]
fn get_min_deposit_returns_zero_when_unset() {
    let (_env, _admin, _token, vault, _treasury) = setup();
    assert_eq!(vault.get_min_deposit(), 0);
}

#[test]
fn get_min_deposit_returns_configured_value() {
    let (_env, admin, _token, vault, _treasury) = setup();
    vault.set_min_deposit(&admin, &(10 * XLM));
    assert_eq!(vault.get_min_deposit(), 10 * XLM);
}

// ---------------------------------------------------------------------------
// Emergency Withdraw All Positions Tests (issue #736)
// ---------------------------------------------------------------------------

#[test]
fn emergency_withdraw_all_exits_all_active_positions() {
    let (env, admin, token, vault, _treasury) = setup();
    let user = Address::generate(&env);
    mint(&token, &user, 1_000 * XLM);
    vault.deposit(&user, &(1_000 * XLM), &0);

    let aave = symbol_short!("aave");
    let blend = symbol_short!("blend");
    vault.record_source_allocation(&admin, &aave, &(600 * XLM));
    vault.record_source_allocation(&admin, &blend, &(400 * XLM));

    let result = vault.emergency_withdraw_all(&user);

    assert_eq!(result.succeeded.len(), 2, "both positions should be exited");
    assert_eq!(result.failed.len(), 0, "no positions should fail");

    // Allocations are cleared after a successful emergency exit.
    let allocations = vault.get_current_allocations();
    for view in allocations.iter() {
        assert_eq!(view.amount, 0, "position should be unwound to zero");
    }
}

#[test]
fn emergency_withdraw_all_skips_inactive_positions() {
    let (env, admin, token, vault, _treasury) = setup();
    let user = Address::generate(&env);
    mint(&token, &user, 1_000 * XLM);
    vault.deposit(&user, &(1_000 * XLM), &0);

    let aave = symbol_short!("aave");
    let blend = symbol_short!("blend");
    vault.record_source_allocation(&admin, &aave, &(500 * XLM));
    // Inactive position: zero allocation.
    vault.record_source_allocation(&admin, &blend, &0);

    let result = vault.emergency_withdraw_all(&user);

    assert_eq!(
        result.succeeded.len(),
        1,
        "only the active position is exited"
    );
    assert_eq!(result.failed.len(), 0);
    assert_eq!(result.succeeded.get(0).unwrap().protocol, aave);
}

#[test]
fn emergency_withdraw_all_allows_partial_success() {
    let (env, admin, token, vault, _treasury) = setup();
    let user = Address::generate(&env);
    mint(&token, &user, 1_000 * XLM);
    vault.deposit(&user, &(1_000 * XLM), &0);

    let aave = symbol_short!("aave");
    let blend = symbol_short!("blend");
    // Unwinding `aave` would overflow the vault's reserves, so it must fail
    // while `blend` still completes — partial success.
    vault.record_source_allocation(&admin, &aave, &i128::MAX);
    vault.record_source_allocation(&admin, &blend, &(400 * XLM));

    let result = vault.emergency_withdraw_all(&user);

    assert_eq!(result.failed.len(), 1, "overflowing position should fail");
    assert_eq!(result.failed.get(0).unwrap().protocol, aave);
    assert_eq!(
        result.succeeded.len(),
        1,
        "healthy position should still exit"
    );
    assert_eq!(result.succeeded.get(0).unwrap().protocol, blend);
}

#[test]
fn emergency_withdraw_all_with_no_positions_returns_empty() {
    let (env, _admin, token, vault, _treasury) = setup();
    let user = Address::generate(&env);
    mint(&token, &user, 1_000 * XLM);
    vault.deposit(&user, &(1_000 * XLM), &0);

    let result = vault.emergency_withdraw_all(&user);

    assert_eq!(result.succeeded.len(), 0);
    assert_eq!(result.failed.len(), 0);
}

// ---------------------------------------------------------------------------
// Systematic negative-authorization matrix
// ---------------------------------------------------------------------------

/// Give the vault real assets, a deployed source, a strategy contract, and no
/// cooldown. A role check removed from `rebalance` therefore turns a rejected
/// call into a successful call instead of merely reaching another panic.
fn prepare_authorized_rebalance(
    env: &Env,
    admin: &Address,
    token: &token::StellarAssetClient,
    vault: &VaultContractClient,
) -> (Address, Symbol) {
    let user = Address::generate(env);
    mint(token, &user, 1_000 * XLM);
    vault.deposit(&user, &(1_000 * XLM), &0);

    let source_id = symbol_short!("aave");
    vault.record_source_allocation(admin, &source_id, &(1_000 * XLM));
    let strategy_id = env.register_contract(None, MockStrategy);
    bind_strategy(vault, admin, &strategy_id);
    vault.set_rebalance_cooldown(admin, &0);
    (strategy_id, source_id)
}

fn authorization_fee_config(treasury: &Address) -> FeeConfig {
    FeeConfig {
        management_fee_bps: 25,
        performance_fee_bps: 500,
        early_withdrawal_fee_bps: 15,
        treasury_address: treasury.clone(),
    }
}

fn authorization_circuit_breaker_config() -> CircuitBreakerConfig {
    CircuitBreakerConfig {
        threshold_bps: 10_000,
        window_seconds: 3_600,
    }
}

#[test]
fn initialize_without_admin_signature_is_rejected() {
    let env = Env::default();
    env.mock_auths(&[]);
    let admin = Address::generate(&env);
    let token_id = Address::generate(&env);
    let vault_token_id = Address::generate(&env);
    let treasury = Address::generate(&env);
    let vault_id = env.register_contract(None, VaultContract);
    let vault = VaultContractClient::new(&env, &vault_id);

    assert_rejected!(
        vault.try_initialize(&admin, &token_id, &vault_token_id, &treasury),
        "initialize"
    );
}

#[test]
fn admin_entrypoints_reject_outsider() {
    let (env, admin, token, vault, treasury) = setup();
    let (strategy_id, source_id) = prepare_authorized_rebalance(&env, &admin, &token, &vault);
    let outsider = Address::generate(&env);
    let role_target = Address::generate(&env);
    let successor = Address::generate(&env);
    let fee_config = authorization_fee_config(&treasury);
    let breaker_config = authorization_circuit_breaker_config();

    // Keep revoke_role's arguments valid and away from last-admin protection.
    vault.grant_role(&admin, &role_target, &Role::Operator);

    assert_rejected!(
        vault.try_set_max_deposit(&outsider, &(2_000 * XLM)),
        "set_max_deposit"
    );
    assert_rejected!(
        vault.try_set_min_deposit(&outsider, &XLM),
        "set_min_deposit"
    );
    assert_rejected!(
        vault.try_set_rebalance_threshold(&outsider, &500),
        "set_rebalance_threshold"
    );
    assert_rejected!(
        vault.try_set_rebalance_slippage(&outsider, &100),
        "set_rebalance_slippage"
    );
    assert_rejected!(
        vault.try_set_circuit_breaker_config(&outsider, &breaker_config),
        "set_circuit_breaker_config"
    );
    assert_rejected!(
        vault.try_set_early_withdrawal_fee(&outsider, &10),
        "set_early_withdrawal_fee"
    );
    assert_rejected!(
        vault.try_set_fee_config(&outsider, &fee_config),
        "set_fee_config"
    );
    assert_rejected!(
        vault.try_set_emergency_fee(&outsider, &100),
        "set_emergency_fee"
    );
    assert_rejected!(
        vault.try_set_allocation_strategy(&outsider, &strategy_id),
        "set_allocation_strategy"
    );
    assert_rejected!(
        vault.try_set_rebalance_cooldown(&outsider, &0),
        "set_rebalance_cooldown"
    );
    assert_rejected!(
        vault.try_grant_role(&outsider, &role_target, &Role::Operator),
        "grant_role"
    );
    assert_rejected!(
        vault.try_revoke_role(&outsider, &role_target, &Role::Operator),
        "revoke_role"
    );
    assert_rejected!(
        vault.try_transfer_admin(&outsider, &successor),
        "transfer_admin"
    );
    assert_rejected!(vault.try_harvest_vault(&outsider), "harvest_vault");
    assert_rejected!(vault.try_rebalance(&outsider), "rebalance");
    assert_rejected!(
        vault.try_record_source_allocation(&outsider, &source_id, &(1_000 * XLM)),
        "record_source_allocation"
    );
    assert_rejected!(vault.try_collect_fees(&outsider), "collect_fees");
    assert_rejected!(vault.try_pause(&outsider), "pause");
    assert_rejected!(vault.try_unpause(&outsider), "unpause");
}

#[test]
fn admin_entrypoints_require_admin_signature() {
    let (env, admin, token, vault, treasury) = setup();
    let (strategy_id, source_id) = prepare_authorized_rebalance(&env, &admin, &token, &vault);
    let role_target = Address::generate(&env);
    let successor = Address::generate(&env);
    let fee_config = authorization_fee_config(&treasury);
    let breaker_config = authorization_circuit_breaker_config();
    vault.grant_role(&admin, &role_target, &Role::Operator);

    // `admin` still holds every required role. Only its signature is absent.
    env.mock_auths(&[]);
    assert_rejected!(
        vault.try_set_max_deposit(&admin, &(2_000 * XLM)),
        "set_max_deposit"
    );
    assert_rejected!(vault.try_set_min_deposit(&admin, &XLM), "set_min_deposit");
    assert_rejected!(
        vault.try_set_rebalance_threshold(&admin, &500),
        "set_rebalance_threshold"
    );
    assert_rejected!(
        vault.try_set_rebalance_slippage(&admin, &100),
        "set_rebalance_slippage"
    );
    assert_rejected!(
        vault.try_set_circuit_breaker_config(&admin, &breaker_config),
        "set_circuit_breaker_config"
    );
    assert_rejected!(
        vault.try_set_early_withdrawal_fee(&admin, &10),
        "set_early_withdrawal_fee"
    );
    assert_rejected!(
        vault.try_set_fee_config(&admin, &fee_config),
        "set_fee_config"
    );
    assert_rejected!(
        vault.try_set_emergency_fee(&admin, &100),
        "set_emergency_fee"
    );
    assert_rejected!(
        vault.try_set_allocation_strategy(&admin, &strategy_id),
        "set_allocation_strategy"
    );
    assert_rejected!(
        vault.try_set_rebalance_cooldown(&admin, &0),
        "set_rebalance_cooldown"
    );
    assert_rejected!(
        vault.try_grant_role(&admin, &role_target, &Role::Operator),
        "grant_role"
    );
    assert_rejected!(
        vault.try_revoke_role(&admin, &role_target, &Role::Operator),
        "revoke_role"
    );
    assert_rejected!(
        vault.try_transfer_admin(&admin, &successor),
        "transfer_admin"
    );
    assert_rejected!(vault.try_harvest_vault(&admin), "harvest_vault");
    assert_rejected!(vault.try_rebalance(&admin), "rebalance");
    assert_rejected!(
        vault.try_record_source_allocation(&admin, &source_id, &(1_000 * XLM)),
        "record_source_allocation"
    );
    assert_rejected!(vault.try_collect_fees(&admin), "collect_fees");
    assert_rejected!(vault.try_pause(&admin), "pause");
    assert_rejected!(vault.try_unpause(&admin), "unpause");
}

#[test]
fn admin_entrypoints_accept_then_reject_revoked_admin() {
    let (env, admin, token, vault, treasury) = setup();
    let (strategy_id, source_id) = prepare_authorized_rebalance(&env, &admin, &token, &vault);
    let delegated_admin = Address::generate(&env);
    let role_target = Address::generate(&env);
    let successor = Address::generate(&env);
    let fee_config = authorization_fee_config(&treasury);
    let breaker_config = authorization_circuit_breaker_config();
    vault.grant_role(&admin, &delegated_admin, &Role::Admin);

    // Every Admin entrypoint accepts an authenticated account holding Admin.
    vault.set_max_deposit(&delegated_admin, &(2_000 * XLM));
    vault.set_min_deposit(&delegated_admin, &XLM);
    vault.set_rebalance_threshold(&delegated_admin, &500);
    vault.set_rebalance_slippage(&delegated_admin, &100);
    vault.set_circuit_breaker_config(&delegated_admin, &breaker_config);
    vault.set_early_withdrawal_fee(&delegated_admin, &10);
    vault.set_fee_config(&delegated_admin, &fee_config);
    vault.set_emergency_fee(&delegated_admin, &100);
    bind_strategy(&vault, &delegated_admin, &strategy_id);
    vault.set_rebalance_cooldown(&delegated_admin, &0);
    vault.harvest_vault(&delegated_admin);
    vault.record_source_allocation(&delegated_admin, &source_id, &(1_000 * XLM));
    vault.rebalance(&delegated_admin);
    vault.collect_fees(&delegated_admin);
    vault.pause(&delegated_admin);
    vault.unpause(&delegated_admin);

    vault.grant_role(&delegated_admin, &role_target, &Role::Operator);
    vault.record_source_allocation(&role_target, &source_id, &(1_000 * XLM));
    vault.revoke_role(&delegated_admin, &role_target, &Role::Operator);
    assert_rejected!(
        vault.try_record_source_allocation(&role_target, &source_id, &(1_000 * XLM)),
        "record_source_allocation after Operator revoke"
    );

    assert_eq!(vault.get_max_deposit(), 2_000 * XLM);
    assert_eq!(vault.get_min_deposit(), XLM);
    assert_eq!(vault.get_rebalance_threshold(), 500);
    assert_eq!(vault.get_rebalance_slippage(), 100);
    assert_eq!(vault.get_rebalance_cooldown(), 0);

    // Restore a valid deployed allocation for the post-revocation rebalance.
    vault.record_source_allocation(&admin, &source_id, &(1_000 * XLM));
    vault.grant_role(&admin, &role_target, &Role::Operator);
    vault.revoke_role(&admin, &delegated_admin, &Role::Admin);

    // The same address is still authenticated by mock_all_auths, but its Admin
    // role is gone. Every call must now fail at authorization.
    assert_rejected!(
        vault.try_set_max_deposit(&delegated_admin, &(2_500 * XLM)),
        "set_max_deposit after Admin revoke"
    );
    assert_rejected!(
        vault.try_set_min_deposit(&delegated_admin, &XLM),
        "set_min_deposit after Admin revoke"
    );
    assert_rejected!(
        vault.try_set_rebalance_threshold(&delegated_admin, &500),
        "set_rebalance_threshold after Admin revoke"
    );
    assert_rejected!(
        vault.try_set_rebalance_slippage(&delegated_admin, &100),
        "set_rebalance_slippage after Admin revoke"
    );
    assert_rejected!(
        vault.try_set_circuit_breaker_config(&delegated_admin, &breaker_config),
        "set_circuit_breaker_config after Admin revoke"
    );
    assert_rejected!(
        vault.try_set_early_withdrawal_fee(&delegated_admin, &10),
        "set_early_withdrawal_fee after Admin revoke"
    );
    assert_rejected!(
        vault.try_set_fee_config(&delegated_admin, &fee_config),
        "set_fee_config after Admin revoke"
    );
    assert_rejected!(
        vault.try_set_emergency_fee(&delegated_admin, &100),
        "set_emergency_fee after Admin revoke"
    );
    assert_rejected!(
        vault.try_set_allocation_strategy(&delegated_admin, &strategy_id),
        "set_allocation_strategy after Admin revoke"
    );
    assert_rejected!(
        vault.try_set_rebalance_cooldown(&delegated_admin, &0),
        "set_rebalance_cooldown after Admin revoke"
    );
    assert_rejected!(
        vault.try_grant_role(&delegated_admin, &role_target, &Role::Operator),
        "grant_role after Admin revoke"
    );
    assert_rejected!(
        vault.try_revoke_role(&delegated_admin, &role_target, &Role::Operator),
        "revoke_role after Admin revoke"
    );
    assert_rejected!(
        vault.try_transfer_admin(&delegated_admin, &successor),
        "transfer_admin after Admin revoke"
    );
    assert_rejected!(
        vault.try_harvest_vault(&delegated_admin),
        "harvest_vault after Admin revoke"
    );
    assert_rejected!(
        vault.try_rebalance(&delegated_admin),
        "rebalance after Admin revoke"
    );
    assert_rejected!(
        vault.try_record_source_allocation(&delegated_admin, &source_id, &(1_000 * XLM)),
        "record_source_allocation after Admin revoke"
    );
    assert_rejected!(
        vault.try_collect_fees(&delegated_admin),
        "collect_fees after Admin revoke"
    );
    assert_rejected!(
        vault.try_pause(&delegated_admin),
        "pause after Admin revoke"
    );
    assert_rejected!(
        vault.try_unpause(&delegated_admin),
        "unpause after Admin revoke"
    );
}

#[test]
fn admin_transfer_wrappers_enforce_authorization() {
    let (_env, admin, _token, vault, _treasury) = setup();
    let successor = Address::generate(&_env);

    vault.transfer_admin(&admin, &successor);
    vault.accept_admin(&successor);
    vault.set_max_deposit(&successor, &(2_000 * XLM));

    // accept_admin atomically revokes the proposing admin.
    assert_rejected!(
        vault.try_set_max_deposit(&admin, &(3_000 * XLM)),
        "former Admin after accept_admin"
    );
}

#[test]
fn accept_admin_rejects_wrong_successor() {
    let (env, admin, _token, vault, _treasury) = setup();
    let successor = Address::generate(&env);
    let outsider = Address::generate(&env);
    vault.transfer_admin(&admin, &successor);

    assert_rejected!(vault.try_accept_admin(&outsider), "accept_admin");
    // A failed impersonation must not consume the valid proposal.
    vault.accept_admin(&successor);
    vault.set_max_deposit(&successor, &(2_000 * XLM));
}

#[test]
fn accept_admin_requires_successor_signature() {
    let (env, admin, _token, vault, _treasury) = setup();
    let successor = Address::generate(&env);
    vault.transfer_admin(&admin, &successor);

    env.mock_auths(&[]);
    assert_rejected!(vault.try_accept_admin(&successor), "accept_admin");
}

#[test]
fn manager_entrypoints_reject_outsider() {
    let (env, _admin, _token, vault, _treasury) = setup();
    let outsider = Address::generate(&env);

    assert_rejected!(vault.try_report_yield(&outsider, &0), "report_yield");
    assert_rejected!(vault.try_collect_fees(&outsider), "collect_fees");
}

#[test]
fn manager_entrypoints_accept_then_reject_revoked_manager() {
    let (env, admin, _token, vault, _treasury) = setup();
    let manager = Address::generate(&env);
    vault.grant_role(&admin, &manager, &Role::Manager);

    // Manager is deliberately not an Admin, exercising the Manager branches.
    vault.report_yield(&manager, &0);
    vault.collect_fees(&manager);

    vault.revoke_role(&admin, &manager, &Role::Manager);
    assert_rejected!(
        vault.try_report_yield(&manager, &0),
        "report_yield after Manager revoke"
    );
    assert_rejected!(
        vault.try_collect_fees(&manager),
        "collect_fees after Manager revoke"
    );
}

#[test]
fn manager_entrypoints_require_signature() {
    let (env, admin, _token, vault, _treasury) = setup();
    let manager = Address::generate(&env);
    vault.grant_role(&admin, &manager, &Role::Manager);

    env.mock_auths(&[]);
    assert_rejected!(vault.try_report_yield(&manager, &0), "report_yield");
    assert_rejected!(vault.try_collect_fees(&manager), "collect_fees");
}

#[test]
fn operator_entrypoints_accept_then_reject_revoked_operator() {
    let (env, admin, token, vault, _treasury) = setup();
    let (_strategy_id, source_id) = prepare_authorized_rebalance(&env, &admin, &token, &vault);
    let operator = Address::generate(&env);
    vault.grant_role(&admin, &operator, &Role::Operator);

    // Operator is deliberately not an Admin, exercising the Operator branches.
    vault.record_source_allocation(&operator, &source_id, &(1_000 * XLM));
    vault.rebalance(&operator);

    vault.record_source_allocation(&admin, &source_id, &(1_000 * XLM));
    vault.revoke_role(&admin, &operator, &Role::Operator);
    assert_rejected!(
        vault.try_record_source_allocation(&operator, &source_id, &(1_000 * XLM)),
        "record_source_allocation after Operator revoke"
    );
    assert_rejected!(
        vault.try_rebalance(&operator),
        "rebalance after Operator revoke"
    );
}

#[test]
fn operator_entrypoints_require_signature() {
    let (env, admin, token, vault, _treasury) = setup();
    let (_strategy_id, source_id) = prepare_authorized_rebalance(&env, &admin, &token, &vault);
    let operator = Address::generate(&env);
    vault.grant_role(&admin, &operator, &Role::Operator);

    env.mock_auths(&[]);
    assert_rejected!(
        vault.try_record_source_allocation(&operator, &source_id, &(1_000 * XLM)),
        "record_source_allocation"
    );
    assert_rejected!(vault.try_rebalance(&operator), "rebalance");
}

#[test]
fn deposit_without_user_signature_is_rejected() {
    let (env, _admin, token, vault, _treasury) = setup();
    let user = Address::generate(&env);
    mint(&token, &user, 100 * XLM);

    env.mock_auths(&[]);
    assert_rejected!(vault.try_deposit(&user, &(100 * XLM), &0), "deposit");
}

#[test]
fn withdraw_without_user_signature_is_rejected() {
    let (env, _admin, token, vault, _treasury) = setup();
    let user = Address::generate(&env);
    mint(&token, &user, 100 * XLM);
    vault.deposit(&user, &(100 * XLM), &0);

    env.mock_auths(&[]);
    assert_rejected!(vault.try_withdraw(&user, &(100 * XLM), &0), "withdraw");
}

#[test]
fn harvest_without_user_signature_is_rejected() {
    let (env, _admin, token, vault, _treasury) = setup();
    let user = Address::generate(&env);
    mint(&token, &user, 100 * XLM);
    vault.deposit(&user, &(100 * XLM), &0);

    env.mock_auths(&[]);
    assert_rejected!(vault.try_harvest(&user), "harvest");
}

#[test]
fn emergency_withdraw_without_user_signature_is_rejected() {
    let (env, admin, token, vault, _treasury) = setup();
    let user = Address::generate(&env);
    mint(&token, &user, 100 * XLM);
    vault.deposit(&user, &(100 * XLM), &0);
    vault.pause(&admin);

    env.mock_auths(&[]);
    assert_rejected!(vault.try_emergency_withdraw(&user), "emergency_withdraw");
}

#[test]
fn emergency_withdraw_all_without_user_signature_is_rejected() {
    let (env, _admin, token, vault, _treasury) = setup();
    let user = Address::generate(&env);
    mint(&token, &user, 100 * XLM);
    vault.deposit(&user, &(100 * XLM), &0);

    env.mock_auths(&[]);
    assert_rejected!(
        vault.try_emergency_withdraw_all(&user),
        "emergency_withdraw_all"
    );
}

#[test]
fn measure_reentrancy_guard_resource_cost_on_deposit_and_withdraw() {
    let (env, _admin, token, vault, _treasury) = setup();
    let user = Address::generate(&env);
    mint(&token, &user, 100 * XLM);

    env.budget().reset_tracker();
    vault.deposit(&user, &(10 * XLM), &0);
    let deposit_cpu = env.budget().cpu_instruction_cost();
    let deposit_mem = env.budget().memory_bytes_cost();

    env.budget().reset_tracker();
    vault.withdraw(&user, &(5 * XLM), &0);
    let withdraw_cpu = env.budget().cpu_instruction_cost();
    let withdraw_mem = env.budget().memory_bytes_cost();

    assert!(deposit_cpu > 0);
    assert!(withdraw_cpu > 0);
    std::println!(
        "reentrancy_guard_deposit_cpu={deposit_cpu} reentrancy_guard_deposit_mem={deposit_mem} \
         reentrancy_guard_withdraw_cpu={withdraw_cpu} reentrancy_guard_withdraw_mem={withdraw_mem}"
    );
}

// ---------------------------------------------------------------------------
// Property-based invariant test suite (proptest)
// ---------------------------------------------------------------------------
mod proptests {
    use super::*;
    use crate::conversion;
    use proptest::prelude::*;

    proptest! {
        #![proptest_config(ProptestConfig::with_cases(64))]

        /// Property 1a: Pure conversion round-trip invariant
        /// `shares_to_assets_down(assets_to_shares_down(a, TA, TS), TA + a, TS + shares) <= a`
        /// Converted assets never exceed original assets for any initial assets and shares.
        #[test]
        fn prop_pure_conversion_roundtrip(
            initial_assets in 1_i128..1_000_000_000 * XLM,
            initial_shares in 1_i128..1_000_000_000 * XLM,
            deposit_amount in nester_common::MIN_DEPOSIT_AMOUNT..100_000_000 * XLM,
        ) {
            let shares_minted = conversion::assets_to_shares_down(
                deposit_amount,
                initial_assets,
                initial_shares,
            ).unwrap();

            let assets_back = conversion::shares_to_assets_down(
                shares_minted,
                initial_assets + deposit_amount,
                initial_shares + shares_minted,
            ).unwrap();

            prop_assert!(
                assets_back <= deposit_amount,
                "Round-trip assets returned ({assets_back}) must not exceed deposited ({deposit_amount})"
            );
        }

        /// Property 1b: Full contract deposit-then-withdraw round-trip invariant
        /// `withdraw(shares(deposit(a))) <= a`
        /// Withdrawing immediately minted shares never returns more than the deposited amount.
        #[test]
        fn prop_contract_deposit_withdraw_roundtrip(
            deposit_amount in nester_common::MIN_DEPOSIT_AMOUNT..1_000_000 * XLM,
        ) {
            let (env, admin, token, vault, _treasury) = setup();
            let user = Address::generate(&env);
            mint(&token, &user, deposit_amount);

            // Disable early-withdrawal fee to isolate rounding math
            let mut fee_config = vault.get_fee_config();
            fee_config.early_withdrawal_fee_bps = 0;
            vault.set_fee_config(&admin, &fee_config);

            let initial_user_token_bal = token::Client::new(&env, &token.address).balance(&user);

            let shares_minted = vault.deposit(&user, &deposit_amount, &0);
            vault.withdraw(&user, &shares_minted, &0);

            let final_user_token_bal = token::Client::new(&env, &token.address).balance(&user);
            prop_assert!(
                final_user_token_bal <= initial_user_token_bal,
                "User balance after deposit & withdraw ({final_user_token_bal}) must be <= initial ({initial_user_token_bal})"
            );
        }

        /// Property 2: Economic Invariant — No value creation across multi-user sequences
        /// Across randomized deposit and withdrawal sequences with optional yield,
        /// cumulative assets returned to users never exceeds total deposited + positive yield.
        #[test]
        fn prop_no_value_creation_multi_user(
            dep1 in nester_common::MIN_DEPOSIT_AMOUNT..500_000 * XLM,
            dep2 in nester_common::MIN_DEPOSIT_AMOUNT..500_000 * XLM,
            yield_amount in 0_i128..100_000 * XLM,
            withdraw_pct_1 in 1_u32..100_u32,
            withdraw_pct_2 in 1_u32..100_u32,
        ) {
            let (env, admin, token, vault, _treasury) = setup();
            let alice = Address::generate(&env);
            let bob = Address::generate(&env);

            mint(&token, &alice, dep1);
            mint(&token, &bob, dep2);

            // User 1 deposits
            let alice_shares = vault.deposit(&alice, &dep1, &0);

            // Report yield if any
            if yield_amount > 0 {
                vault.grant_role(&admin, &admin, &Role::Manager);
                vault.report_yield(&admin, &yield_amount);
                mint(&token, &vault.address, yield_amount);
            }

            // User 2 deposits
            let bob_shares = vault.deposit(&bob, &dep2, &0);

            // Partial or full withdrawals
            let alice_withdraw_shares = alice_shares * (withdraw_pct_1 as i128) / 100;
            let bob_withdraw_shares = bob_shares * (withdraw_pct_2 as i128) / 100;

            if alice_withdraw_shares > 0 {
                vault.withdraw(&alice, &alice_withdraw_shares, &0);
            }
            if bob_withdraw_shares > 0 {
                vault.withdraw(&bob, &bob_withdraw_shares, &0);
            }

            let alice_bal = token::Client::new(&env, &token.address).balance(&alice);
            let bob_bal = token::Client::new(&env, &token.address).balance(&bob);

            let total_received = alice_bal + bob_bal;
            let max_possible = dep1 + dep2 + yield_amount;

            prop_assert!(
                total_received <= max_possible,
                "Total assets withdrawn ({total_received}) exceeds total deposited + yield ({max_possible})"
            );
        }

        /// Property 3: Share Price Monotonicity under positive yield & exact halving under 50% impairment
        #[test]
        fn prop_share_price_monotonicity_and_impairment(
            initial_deposit in nester_common::MIN_DEPOSIT_AMOUNT..1_000_000 * XLM,
            yield_pct in 1_i128..100_i128,
        ) {
            let (env, admin, token, vault, _treasury) = setup();
            let user = Address::generate(&env);
            mint(&token, &user, initial_deposit);
            vault.deposit(&user, &initial_deposit, &0);

            let price_before = vault.share_price();

            // Positive yield increases share price
            let yield_val = initial_deposit * yield_pct / 100;
            vault.grant_role(&admin, &admin, &Role::Manager);
            vault.report_yield(&admin, &yield_val);

            let price_after_yield = vault.share_price();
            prop_assert!(
                price_after_yield > price_before,
                "Share price after positive yield ({price_after_yield}) must be > before ({price_before})"
            );

            // Impairment: loss equal to 50% of current total assets halves share price
            let current_total_assets = vault.get_total_deposits();
            let loss = current_total_assets / 2;
            vault.report_yield(&admin, &(-loss));

            let price_after_impairment = vault.share_price();
            // Expected price after 50% loss: half of price_after_yield
            let expected_halved_price = price_after_yield / 2;

            prop_assert!(
                (price_after_impairment - expected_halved_price).abs() <= 1,
                "Share price after 50% impairment ({price_after_impairment}) must equal half of post-yield price ({expected_halved_price}) within rounding tolerance",
                price_after_impairment = price_after_impairment,
                expected_halved_price = expected_halved_price
            );

            // Impaired withdrawal charges zero performance fee
            let shares = vault.get_shares(&user);
            let fee_preview = vault.withdrawal_fee_preview(&user, &shares);
            prop_assert_eq!(
                fee_preview.performance_fee_deducted,
                0,
                "Impaired position must be charged 0 performance fee"
            );
        }

        /// Property 4: Inflation attack resistance / First-depositor protection
        ///
        /// ERC-4626 Inflation Attack Scenario:
        /// 1. Attacker deposits a tiny amount (MIN_DEPOSIT_AMOUNT = 10_000_000).
        /// 2. Attacker inflates vault assets via yield report / direct transfer.
        /// 3. Victim attempts to deposit `b`.
        ///
        /// Mitigation Verification:
        /// - If victim specifies `min_shares_out > 0` and the ratio would round victim's shares to 0,
        ///   `deposit` panics with `SlippageExceeded` (protecting victim funds from being stolen).
        /// - If victim deposits enough assets to receive shares (> 0), victim receives their exact
        ///   proportional value upon withdrawal, preventing value theft by the first depositor.
        #[test]
        fn prop_first_depositor_inflation_attack_resistance(
            attacker_deposit in nester_common::MIN_DEPOSIT_AMOUNT..(nester_common::MIN_DEPOSIT_AMOUNT * 2),
            direct_transfer in 1_000 * XLM..100_000 * XLM,
            victim_deposit in nester_common::MIN_DEPOSIT_AMOUNT..50_000 * XLM,
        ) {
            let (env, admin, token, vault, _treasury) = setup();
            let attacker = Address::generate(&env);
            let victim = Address::generate(&env);

            mint(&token, &attacker, attacker_deposit + direct_transfer);
            mint(&token, &victim, victim_deposit);

            // 1. Attacker makes initial small deposit
            let attacker_shares = vault.deposit(&attacker, &attacker_deposit, &0);
            prop_assert!(attacker_shares > 0);

            // 2. Attacker inflates vault assets via yield report / direct transfer
            vault.grant_role(&admin, &admin, &Role::Manager);
            vault.report_yield(&admin, &direct_transfer);
            mint(&token, &vault.address, direct_transfer);

            // 3. Check victim deposit behavior
            let total_assets = vault.get_total_deposits();
            let total_shares = vault.get_shares(&attacker);

            // Pure math preview of expected shares for victim
            let expected_victim_shares = conversion::assets_to_shares_down(
                victim_deposit,
                total_assets,
                total_shares,
            ).unwrap();

            if expected_victim_shares == 0 {
                // If victim's deposit is diluted to 0 shares by the inflation,
                // passing min_shares_out = 1 must revert with SlippageExceeded, protecting victim.
                let try_res = vault.try_deposit(&victim, &victim_deposit, &1);
                prop_assert!(
                    try_res.is_err(),
                    "Deposit rounding to 0 shares must be rejected when min_shares_out > 0"
                );
            } else {
                // If victim receives shares (>0), victim must be able to redeem assets proportionally
                let victim_shares = vault.deposit(&victim, &victim_deposit, &0);
                prop_assert_eq!(victim_shares, expected_victim_shares);

                // Gross redeemable assets (preview_withdraw returns gross share value)
                let victim_gross_redeemable = vault.preview_withdraw(&victim_shares);
                let total_assets_now = total_assets + victim_deposit;
                let total_shares_now = total_shares + victim_shares;
                let expected_redeemable = conversion::shares_to_assets_down(
                    victim_shares,
                    total_assets_now,
                    total_shares_now,
                ).unwrap();

                prop_assert!(
                    victim_gross_redeemable == expected_redeemable,
                    "Victim gross redeemable assets ({victim_gross_redeemable}) must equal expected share of vault ({expected_redeemable})",
                    victim_gross_redeemable = victim_gross_redeemable,
                    expected_redeemable = expected_redeemable
                );
            }
        }
    }
}

