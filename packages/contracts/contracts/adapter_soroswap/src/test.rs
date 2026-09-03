#![cfg(test)]

extern crate std;

use soroban_sdk::{
    testutils::{Address as _, Ledger},
    token::{StellarAssetClient, TokenClient},
    Address, Env,
};

use nester_common::adapters::ApyConfidence;
use nester_test_utils::mocks::{
    MockSoroswapPair, MockSoroswapPairClient, MockSoroswapRouter, MockSoroswapRouterClient,
};

use crate::{SoroswapAdapterContract, SoroswapAdapterContractClient, MIN_APY_WINDOW_SECS};

struct Setup {
    env: Env,
    vault: Address,
    adapter: SoroswapAdapterContractClient<'static>,
    pair: MockSoroswapPairClient<'static>,
    usdc: TokenClient<'static>,
    xlm: TokenClient<'static>,
    usdc_admin: StellarAssetClient<'static>,
    xlm_admin: StellarAssetClient<'static>,
}

/// Seeds the pair with liquidity so swaps have a price to move against.
fn setup(seed_usdc: i128, seed_xlm: i128) -> Setup {
    let env = Env::default();
    env.mock_all_auths();

    let vault = Address::generate(&env);
    let token_admin = Address::generate(&env);

    let usdc_id = env
        .register_stellar_asset_contract_v2(token_admin.clone())
        .address();
    let xlm_id = env
        .register_stellar_asset_contract_v2(token_admin)
        .address();
    let usdc = TokenClient::new(&env, &usdc_id);
    let xlm = TokenClient::new(&env, &xlm_id);
    let usdc_admin = StellarAssetClient::new(&env, &usdc_id);
    let xlm_admin = StellarAssetClient::new(&env, &xlm_id);

    let pair_id = env.register_contract(None, MockSoroswapPair);
    let pair = MockSoroswapPairClient::new(&env, &pair_id);
    pair.initialize(&usdc_id, &xlm_id);

    let router_id = env.register_contract(None, MockSoroswapRouter);
    MockSoroswapRouterClient::new(&env, &router_id).initialize(&pair_id);

    // Existing depositors, so the adapter is never the first LP.
    usdc_admin.mint(&pair_id, &seed_usdc);
    xlm_admin.mint(&pair_id, &seed_xlm);
    pair.mint_for(&Address::generate(&env), &seed_usdc, &seed_xlm);

    let adapter_id = env.register_contract(None, SoroswapAdapterContract);
    let adapter = SoroswapAdapterContractClient::new(&env, &adapter_id);
    adapter.initialize(&vault, &router_id, &pair_id, &usdc_id, &xlm_id);

    Setup { env, vault, adapter, pair, usdc, xlm, usdc_admin, xlm_admin }
}

#[test]
fn deposit_swaps_half_and_mints_lp_units() {
    let s = setup(1_000_000, 1_000_000);
    s.usdc_admin.mint(&s.vault, &10_000);

    let units = s.adapter.deposit(&s.vault, &10_000, &0);

    assert!(units > 0, "a deposit must mint LP units");
    assert_eq!(s.usdc.balance(&s.vault), 0, "the vault's asset moved in");
    assert_eq!(s.adapter.max_withdraw(), units);
    // Half was swapped into the paired asset, so the pair holds more of both.
    let (r0, r1) = s.pair.get_reserves();
    assert!(r0 > 1_000_000 && r1 < 1_000_000 + 10_000);
}

#[test]
#[should_panic(expected = "Error(Contract, #17)")]
fn deposit_reverts_when_minted_units_fall_short() {
    let s = setup(1_000_000, 1_000_000);
    s.usdc_admin.mint(&s.vault, &10_000);

    // Far more units than a 10k deposit can mint against this pool.
    s.adapter.deposit(&s.vault, &10_000, &1_000_000);
}

#[test]
fn withdraw_returns_the_underlying_asset_only() {
    let s = setup(1_000_000, 1_000_000);
    s.usdc_admin.mint(&s.vault, &10_000);
    let units = s.adapter.deposit(&s.vault, &10_000, &0);

    let assets = s.adapter.withdraw(&s.vault, &units, &0);

    assert!(assets > 0, "withdrawal must return underlying");
    assert_eq!(s.usdc.balance(&s.vault), assets);
    // The paired asset is swapped back, never handed to the vault.
    assert_eq!(s.xlm.balance(&s.vault), 0);
    assert_eq!(s.adapter.max_withdraw(), 0);
}

#[test]
#[should_panic(expected = "Error(Contract, #17)")]
fn withdraw_reverts_when_proceeds_fall_short() {
    let s = setup(1_000_000, 1_000_000);
    s.usdc_admin.mint(&s.vault, &10_000);
    let units = s.adapter.deposit(&s.vault, &10_000, &0);

    s.adapter.withdraw(&s.vault, &units, &1_000_000);
}

#[test]
#[should_panic(expected = "Error(Contract, #4)")]
fn withdraw_cannot_exceed_the_held_position() {
    let s = setup(1_000_000, 1_000_000);
    s.usdc_admin.mint(&s.vault, &10_000);
    let units = s.adapter.deposit(&s.vault, &10_000, &0);

    s.adapter.withdraw(&s.vault, &(units + 1), &0);
}

#[test]
#[should_panic(expected = "Error(Contract, #3)")]
fn only_the_vault_may_deposit() {
    let s = setup(1_000_000, 1_000_000);
    let stranger = Address::generate(&s.env);
    s.usdc_admin.mint(&stranger, &10_000);

    s.adapter.deposit(&stranger, &10_000, &0);
}

#[test]
#[should_panic(expected = "Error(Contract, #5)")]
fn deposit_rejects_a_non_positive_amount() {
    let s = setup(1_000_000, 1_000_000);
    s.adapter.deposit(&s.vault, &0, &0);
}

#[test]
fn position_is_valued_pro_rata_against_reserves() {
    let s = setup(1_000_000, 1_000_000);
    s.usdc_admin.mint(&s.vault, &10_000);
    s.adapter.deposit(&s.vault, &10_000, &0);

    let value = s.adapter.position_value(&s.vault);

    // Valued through reserves rather than a price feed, so it lands near the
    // deposit less swap fees and price impact — not at some oracle figure.
    assert!(value > 8_000 && value < 11_000, "unexpected position value: {value}");
}

#[test]
fn apy_is_unavailable_before_a_checkpoint_exists() {
    let s = setup(1_000_000, 1_000_000);
    assert_eq!(s.adapter.current_apy().confidence, ApyConfidence::Unavailable);
}

#[test]
fn apy_is_withheld_until_the_observation_window_passes() {
    let s = setup(1_000_000, 1_000_000);
    s.usdc_admin.mint(&s.vault, &10_000);
    s.adapter.deposit(&s.vault, &10_000, &0);
    s.adapter.checkpoint();

    // An hour of growth annualizes to noise, so it must not be reported.
    s.env.ledger().with_mut(|l| l.timestamp += 3_600);

    assert_eq!(s.adapter.current_apy().confidence, ApyConfidence::Unavailable);
}

#[test]
fn apy_is_derived_once_the_window_has_passed() {
    let s = setup(1_000_000, 1_000_000);
    s.usdc_admin.mint(&s.vault, &10_000);
    s.adapter.deposit(&s.vault, &10_000, &0);
    s.adapter.checkpoint();

    // Trading fees accrue to the pool, lifting every LP's share.
    s.usdc_admin.mint(&s.pair.address, &50_000);
    s.xlm_admin.mint(&s.pair.address, &50_000);
    s.pair.mint_for(&Address::generate(&s.env), &0, &0);
    s.env.ledger().with_mut(|l| l.timestamp += MIN_APY_WINDOW_SECS + 1);

    let apy = s.adapter.current_apy();
    assert_eq!(apy.confidence, ApyConfidence::Derived);
}

#[test]
fn configuration_is_reported_as_initialized() {
    let s = setup(1_000_000, 1_000_000);
    assert_eq!(s.adapter.underlying(), s.usdc.address);
    assert_eq!(s.adapter.get_paired(), s.xlm.address);
    assert_eq!(s.adapter.get_pair(), s.pair.address);
    assert_eq!(s.adapter.get_vault(), s.vault);
}

#[test]
#[should_panic(expected = "Error(Contract, #1)")]
fn initialize_is_single_use() {
    let s = setup(1_000_000, 1_000_000);
    s.adapter.initialize(
        &s.vault,
        &s.adapter.get_router(),
        &s.pair.address,
        &s.usdc.address,
        &s.xlm.address,
    );
}

#[test]
#[should_panic(expected = "Error(Contract, #5)")]
fn a_pair_cannot_be_the_same_token_twice() {
    let env = Env::default();
    env.mock_all_auths();
    let vault = Address::generate(&env);
    let token_admin = Address::generate(&env);
    let token = env
        .register_stellar_asset_contract_v2(token_admin)
        .address();

    let adapter_id = env.register_contract(None, SoroswapAdapterContract);
    SoroswapAdapterContractClient::new(&env, &adapter_id).initialize(
        &vault,
        &Address::generate(&env),
        &Address::generate(&env),
        &token,
        &token,
    );
}
