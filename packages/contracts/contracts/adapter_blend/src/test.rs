extern crate std;

use soroban_sdk::{
    testutils::Address as _,
    token::{StellarAssetClient, TokenClient},
    Address, Env,
};

use nester_common::adapters::ApyConfidence;
use nester_test_utils::mocks::{MockBlendPool, MockBlendPoolClient};

use crate::{BlendAdapterContract, BlendAdapterContractClient};

const RESERVE_INDEX: u32 = 3;

struct Setup {
    env: Env,
    vault: Address,
    adapter: BlendAdapterContractClient<'static>,
    pool: MockBlendPoolClient<'static>,
    token: TokenClient<'static>,
    token_admin_client: StellarAssetClient<'static>,
}

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

    let pool_id = env.register_contract(None, MockBlendPool);
    let pool = MockBlendPoolClient::new(&env, &pool_id);
    pool.initialize(&token_id, &RESERVE_INDEX);

    let adapter_id = env.register_contract(None, BlendAdapterContract);
    let adapter = BlendAdapterContractClient::new(&env, &adapter_id);
    adapter.initialize(&vault, &pool_id, &token_id, &RESERVE_INDEX);

    Setup {
        env,
        vault,
        adapter,
        pool,
        token,
        token_admin_client,
    }
}

fn fund_vault(s: &Setup, amount: i128) {
    s.token_admin_client.mint(&s.vault, &amount);
}

#[test]
fn deposit_supplies_through_submit_and_returns_units() {
    let s = setup();
    fund_vault(&s, 1_000);

    let units = s.adapter.deposit(&s.vault, &1_000, &0);

    // At the opening index of 1.0, a supply of 1000 mints 1000 bTokens.
    assert_eq!(units, 1_000);
    // The pool holds the underlying, the vault no longer does.
    assert_eq!(s.token.balance(&s.vault), 0);
    assert_eq!(s.token.balance(&s.pool.address), 1_000);
    // The adapter's view agrees with the pool's own positions map.
    let positions = s.pool.get_positions(&s.adapter.address);
    assert_eq!(positions.supply.get(RESERVE_INDEX).unwrap_or(0), 1_000);
}

#[test]
fn units_track_the_pools_exchange_rate_not_a_local_estimate() {
    let s = setup();
    fund_vault(&s, 2_000);

    s.adapter.deposit(&s.vault, &1_000, &0);
    // The position appreciates: the same underlying now buys fewer bTokens.
    s.pool.set_index_bps(&20_000);
    let units = s.adapter.deposit(&s.vault, &1_000, &0);

    assert_eq!(units, 500, "second deposit must mint at the new index");
    assert_eq!(s.adapter.max_withdraw(), 1_500);
}

#[test]
#[should_panic(expected = "Error(Contract, #17)")]
fn deposit_reverts_when_units_fall_short_of_the_minimum() {
    let s = setup();
    fund_vault(&s, 1_000);
    s.pool.set_index_bps(&20_000);

    // 1000 underlying mints 500 units at this index; demanding 1000 must fail
    // with SlippageExceeded rather than silently accepting half.
    s.adapter.deposit(&s.vault, &1_000, &1_000);
}

#[test]
fn withdraw_returns_underlying_and_reduces_the_position() {
    let s = setup();
    fund_vault(&s, 1_000);
    s.adapter.deposit(&s.vault, &1_000, &0);

    let assets = s.adapter.withdraw(&s.vault, &400, &0);

    assert_eq!(assets, 400);
    assert_eq!(s.token.balance(&s.vault), 400);
    assert_eq!(s.adapter.max_withdraw(), 600);
}

#[test]
fn withdraw_pays_out_accrued_interest() {
    let s = setup();
    fund_vault(&s, 1_000);
    s.adapter.deposit(&s.vault, &1_000, &0);

    // Double the index: the position is now worth twice the deposit. The pool
    // needs the extra underlying on hand to pay it out.
    s.pool.set_index_bps(&20_000);
    s.token_admin_client.mint(&s.pool.address, &1_000);

    let assets = s.adapter.withdraw(&s.vault, &1_000, &0);

    assert_eq!(assets, 2_000, "withdrawal must include accrued interest");
    assert_eq!(s.adapter.max_withdraw(), 0);
}

#[test]
#[should_panic(expected = "Error(Contract, #17)")]
fn withdraw_reverts_when_assets_fall_short_of_the_minimum() {
    let s = setup();
    fund_vault(&s, 1_000);
    s.adapter.deposit(&s.vault, &1_000, &0);

    s.adapter.withdraw(&s.vault, &400, &1_000);
}

#[test]
#[should_panic(expected = "Error(Contract, #4)")]
fn withdraw_cannot_exceed_the_held_position() {
    let s = setup();
    fund_vault(&s, 1_000);
    s.adapter.deposit(&s.vault, &1_000, &0);

    s.adapter.withdraw(&s.vault, &1_500, &0);
}

#[test]
#[should_panic(expected = "Error(Contract, #3)")]
fn only_the_vault_may_deposit() {
    let s = setup();
    let stranger = Address::generate(&s.env);
    s.token_admin_client.mint(&stranger, &1_000);

    s.adapter.deposit(&stranger, &1_000, &0);
}

#[test]
#[should_panic(expected = "Error(Contract, #5)")]
fn deposit_rejects_a_non_positive_amount() {
    let s = setup();
    s.adapter.deposit(&s.vault, &0, &0);
}

#[test]
fn apy_is_reported_unavailable_rather_than_zero() {
    let s = setup();
    let apy = s.adapter.current_apy();

    // Blend publishes rates through reserve data, which this adapter does not
    // read yet. The interface requires consumers to ignore apy_bps when
    // confidence is Unavailable, so an honest unknown cannot be mistaken for a
    // real zero and churn the rebalancer.
    assert_eq!(apy.confidence, ApyConfidence::Unavailable);
}

#[test]
fn underlying_and_pool_are_reported_as_configured() {
    let s = setup();
    assert_eq!(s.adapter.underlying(), s.token.address);
    assert_eq!(s.adapter.get_pool(), s.pool.address);
    assert_eq!(s.adapter.get_reserve_index(), RESERVE_INDEX);
    assert_eq!(s.adapter.get_vault(), s.vault);
}

#[test]
#[should_panic(expected = "Error(Contract, #1)")]
fn initialize_is_single_use() {
    let s = setup();
    s.adapter
        .initialize(&s.vault, &s.pool.address, &s.token.address, &RESERVE_INDEX);
}

#[test]
fn test_position_value_in_underlying_after_rate_growth() {
    let s = setup();
    fund_vault(&s, 100);
    s.adapter.deposit(&s.vault, &100, &0);

    // b_rate 1.05e12: each bToken is now worth 1.05 underlying.
    s.pool.set_index_bps(&10_500);

    assert_eq!(s.adapter.max_withdraw(), 100, "max_withdraw stays in units");
    assert_eq!(s.adapter.position_value(&s.vault), 105);
}

#[test]
fn position_value_rounds_down() {
    let s = setup();
    fund_vault(&s, 3);
    s.adapter.deposit(&s.vault, &3, &0);
    s.pool.set_index_bps(&10_001);

    // 3 * 1.0001 = 3.0003 -> 3, never rounded up.
    assert_eq!(s.adapter.position_value(&s.vault), 3);
}

#[test]
fn test_withdraw_pays_at_least_requested() {
    let s = setup();
    fund_vault(&s, 1_000_000_007);
    s.adapter.deposit(&s.vault, &1_000_000_007, &0);

    // An awkward rate so every conversion rounds.
    s.pool.set_index_bps(&13_337);
    s.token_admin_client
        .mint(&s.pool.address, &1_000_000_000_000);

    for requested in [1i128, 7, 333_333, 123_456_789, 400_000_003] {
        // Convert exactly as savings_core does: units = ceil(held * X / value).
        let held = s.adapter.max_withdraw();
        let value = s.adapter.position_value(&s.vault);
        let units = ((held * requested + value - 1) / value).min(held);

        let before = s.token.balance(&s.vault);
        let paid = s.adapter.withdraw(&s.vault, &units, &requested);

        assert!(paid >= requested, "asked {requested}, paid {paid}");
        assert_eq!(s.token.balance(&s.vault) - before, paid);
        assert_eq!(
            s.adapter.max_withdraw(),
            s.pool
                .get_positions(&s.adapter.address)
                .supply
                .get(RESERVE_INDEX)
                .unwrap_or(0)
        );
    }
}

#[test]
fn full_withdrawal_closes_the_position_and_pays_its_value() {
    let s = setup();
    fund_vault(&s, 1_000_003);
    s.adapter.deposit(&s.vault, &1_000_003, &0);
    s.pool.set_index_bps(&13_337);
    s.token_admin_client.mint(&s.pool.address, &1_000_000);

    let held = s.adapter.max_withdraw();
    let value = s.adapter.position_value(&s.vault);
    let paid = s.adapter.withdraw(&s.vault, &held, &value);

    assert_eq!(paid, value);
    assert_eq!(s.adapter.max_withdraw(), 0);
    assert_eq!(
        s.pool
            .get_positions(&s.adapter.address)
            .supply
            .get(RESERVE_INDEX)
            .unwrap_or(0),
        0,
        "no bToken dust may be left behind"
    );
}

#[test]
fn initialize_needs_no_auth_from_the_vault() {
    let env = Env::default();
    let vault = Address::generate(&env);
    let pool = Address::generate(&env);
    let underlying = Address::generate(&env);
    let adapter =
        BlendAdapterContractClient::new(&env, &env.register_contract(None, BlendAdapterContract));

    adapter.initialize(&vault, &pool, &underlying, &RESERVE_INDEX);

    assert_eq!(adapter.core(), vault);
    assert_eq!(adapter.get_vault(), vault);
    assert!(adapter
        .try_initialize(&vault, &pool, &underlying, &RESERVE_INDEX)
        .is_err());
}
