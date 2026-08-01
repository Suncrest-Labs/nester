#![cfg(test)]

extern crate std;

use soroban_sdk::{
    testutils::{Address as _, Ledger},
    token::{StellarAssetClient, TokenClient},
    Address, Env,
};

use nester_common::adapters::ApyConfidence;
use nester_test_utils::mocks::{MockAmmPool, MockAmmPoolClient};

use crate::{PoolAdapterContract, PoolAdapterContractClient, MIN_APY_WINDOW_SECS};

struct Setup {
    env: Env,
    vault: Address,
    adapter: PoolAdapterContractClient<'static>,
    pool: MockAmmPoolClient<'static>,
    token: TokenClient<'static>,
    token_admin_client: StellarAssetClient<'static>,
}

const INITIAL_RESERVE: i128 = 10_000_000;

fn setup() -> Setup {
    let env = Env::default();
    env.mock_all_auths();

    let vault = Address::generate(&env);
    let token_admin = Address::generate(&env);

    let token_id = env
        .register_stellar_asset_contract_v2(token_admin)
        .address();
    let token = TokenClient::new(&env, &token_id);
    let token_admin_client = StellarAssetClient::new(&env, &token_id);

    let pool_id = env.register_contract(None, MockAmmPool);
    let pool = MockAmmPoolClient::new(&env, &pool_id);
    pool.initialize(&token_id, &INITIAL_RESERVE, &INITIAL_RESERVE);
    // Seed the pool with real token liquidity backing its reserve figure.
    token_admin_client.mint(&pool_id, &INITIAL_RESERVE);

    let adapter_id = env.register_contract(None, PoolAdapterContract);
    let adapter = PoolAdapterContractClient::new(&env, &adapter_id);
    adapter.initialize(&vault, &pool_id, &token_id);

    Setup {
        env,
        vault,
        adapter,
        pool,
        token,
        token_admin_client,
    }
}

#[test]
fn deposit_mints_lp_units_and_values_pro_rata() {
    let s = setup();
    s.token_admin_client.mint(&s.vault, &1_000_000);

    let units = s.adapter.deposit(&s.vault, &1_000_000, &1);
    assert!(units > 0);
    assert_eq!(s.adapter.max_withdraw(), units);

    // Pro-rata valuation must equal what a burn would pay (± rounding dust).
    let value = s.adapter.position_value(&s.vault);
    assert!((999_999..=1_000_001).contains(&value), "value = {value}");
}

#[test]
#[should_panic(expected = "Error(Contract, #17)")] // SlippageExceeded
fn deposit_enforces_min_units_out() {
    let s = setup();
    s.token_admin_client.mint(&s.vault, &1_000_000);
    s.adapter.deposit(&s.vault, &1_000_000, &i128::MAX);
}

#[test]
#[should_panic(expected = "Error(Contract, #3)")] // Unauthorized
fn deposit_rejects_non_vault_caller() {
    let s = setup();
    let attacker = Address::generate(&s.env);
    s.token_admin_client.mint(&attacker, &1_000);
    s.adapter.deposit(&attacker, &1_000, &0);
}

#[test]
fn withdraw_pays_out_with_min_out_guard() {
    let s = setup();
    s.token_admin_client.mint(&s.vault, &1_000_000);
    let units = s.adapter.deposit(&s.vault, &1_000_000, &0);

    let out = s.adapter.withdraw(&s.vault, &units, &999_000);
    assert!(out >= 999_000);
    assert_eq!(s.token.balance(&s.vault), out);
    assert_eq!(s.adapter.max_withdraw(), 0);
}

#[test]
#[should_panic(expected = "Error(Contract, #17)")] // SlippageExceeded
fn withdraw_enforces_min_out() {
    let s = setup();
    s.token_admin_client.mint(&s.vault, &1_000_000);
    let units = s.adapter.deposit(&s.vault, &1_000_000, &0);
    s.adapter.withdraw(&s.vault, &units, &2_000_000);
}

#[test]
fn position_value_grows_with_pool_fees() {
    let s = setup();
    s.token_admin_client.mint(&s.vault, &1_000_000);
    s.adapter.deposit(&s.vault, &1_000_000, &0);
    let before = s.adapter.position_value(&s.vault);

    // Simulate fee growth: +10% reserves without share dilution.
    s.pool.simulate_fee_growth(&1_100_000);
    let after = s.adapter.position_value(&s.vault);
    assert!(after > before, "LP value should grow with reserves");
}

#[test]
fn derived_apy_unavailable_when_window_too_short() {
    let s = setup();
    s.token_admin_client.mint(&s.vault, &1_000_000);
    s.adapter.deposit(&s.vault, &1_000_000, &0);

    // Growth happened, but the observation window is too short.
    s.pool.simulate_fee_growth(&1_000_000);
    let apy = s.adapter.current_apy();
    assert_eq!(apy.confidence, ApyConfidence::Unavailable);
    assert_eq!(apy.apy_bps, 0);
}

#[test]
fn derived_apy_after_sufficient_window() {
    let s = setup();
    s.token_admin_client.mint(&s.vault, &1_000_000);
    s.adapter.deposit(&s.vault, &1_000_000, &0);

    // ~+1% growth over 30 days.
    s.pool.simulate_fee_growth(&110_000);
    s.env.ledger().with_mut(|l| l.timestamp += 30 * 86_400);

    let apy = s.adapter.current_apy();
    assert_eq!(apy.confidence, ApyConfidence::Derived);
    assert!(apy.apy_bps > 0, "positive growth must derive positive APY");
    assert!(apy.apy_bps <= 10_000, "APY must respect the bps cap");
}

#[test]
fn derived_apy_zero_growth_is_zero_not_unknown() {
    let s = setup();
    s.token_admin_client.mint(&s.vault, &1_000_000);
    s.adapter.deposit(&s.vault, &1_000_000, &0);

    s.env
        .ledger()
        .with_mut(|l| l.timestamp += MIN_APY_WINDOW_SECS + 1);

    let apy = s.adapter.current_apy();
    assert_eq!(apy.confidence, ApyConfidence::Derived);
    assert_eq!(apy.apy_bps, 0);
}

#[test]
fn apy_unavailable_with_no_position() {
    let s = setup();
    let apy = s.adapter.current_apy();
    assert_eq!(apy.confidence, ApyConfidence::Unavailable);
}

#[test]
fn deposit_resets_apy_checkpoint() {
    let s = setup();
    s.token_admin_client.mint(&s.vault, &2_000_000);
    s.adapter.deposit(&s.vault, &1_000_000, &0);
    s.env.ledger().with_mut(|l| l.timestamp += 10 * 86_400);

    // Second deposit re-anchors the checkpoint — deposits must never be
    // mistaken for yield.
    s.adapter.deposit(&s.vault, &1_000_000, &0);
    let cp = s.adapter.get_checkpoint().unwrap();
    assert_eq!(cp.timestamp, s.env.ledger().timestamp());

    let apy = s.adapter.current_apy();
    assert_eq!(apy.confidence, ApyConfidence::Unavailable);
}

#[test]
fn limits_report_capacity() {
    let s = setup();
    assert_eq!(s.adapter.max_deposit(), i128::MAX);
    assert_eq!(s.adapter.max_withdraw(), 0);
    assert_eq!(s.adapter.underlying(), s.token.address);
}
