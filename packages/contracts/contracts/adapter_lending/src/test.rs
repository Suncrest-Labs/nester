#![cfg(test)]

extern crate std;

use soroban_sdk::{
    testutils::Address as _,
    token::{StellarAssetClient, TokenClient},
    Address, Env,
};

use nester_common::adapters::ApyConfidence;
use nester_test_utils::mocks::{MockLendingProtocol, MockLendingProtocolClient};

use crate::{LendingAdapterContract, LendingAdapterContractClient};

struct Setup {
    env: Env,
    vault: Address,
    adapter: LendingAdapterContractClient<'static>,
    protocol: MockLendingProtocolClient<'static>,
    token: TokenClient<'static>,
    token_admin_client: StellarAssetClient<'static>,
}

fn setup(rate_bps: u32) -> Setup {
    let env = Env::default();
    env.mock_all_auths();

    let vault = Address::generate(&env);
    let token_admin = Address::generate(&env);

    let token_id = env
        .register_stellar_asset_contract_v2(token_admin)
        .address();
    let token = TokenClient::new(&env, &token_id);
    let token_admin_client = StellarAssetClient::new(&env, &token_id);

    let protocol_id = env.register_contract(None, MockLendingProtocol);
    let protocol = MockLendingProtocolClient::new(&env, &protocol_id);
    protocol.initialize(&token_id, &rate_bps);

    let adapter_id = env.register_contract(None, LendingAdapterContract);
    let adapter = LendingAdapterContractClient::new(&env, &adapter_id);
    adapter.initialize(&vault, &protocol_id, &token_id);

    Setup {
        env,
        vault,
        adapter,
        protocol,
        token,
        token_admin_client,
    }
}

#[test]
fn deposit_moves_assets_and_returns_units() {
    let s = setup(500);
    s.token_admin_client.mint(&s.vault, &1_000_000);

    let units = s.adapter.deposit(&s.vault, &1_000_000, &1_000_000);
    assert_eq!(units, 1_000_000, "index starts at 1.0 → units == amount");
    assert_eq!(s.token.balance(&s.vault), 0);
    assert_eq!(s.adapter.max_withdraw(), 1_000_000);
}

#[test]
#[should_panic(expected = "Error(Contract, #17)")] // SlippageExceeded
fn deposit_enforces_min_units_out() {
    let s = setup(500);
    s.token_admin_client.mint(&s.vault, &1_000_000);
    // Demand more units than the deposit can mint.
    s.adapter.deposit(&s.vault, &1_000_000, &1_000_001);
}

#[test]
#[should_panic(expected = "Error(Contract, #3)")] // Unauthorized
fn deposit_rejects_non_vault_caller() {
    let s = setup(500);
    let attacker = Address::generate(&s.env);
    s.token_admin_client.mint(&attacker, &1_000);
    s.adapter.deposit(&attacker, &1_000, &0);
}

#[test]
fn withdraw_returns_assets_with_accrual() {
    let s = setup(500);
    s.token_admin_client.mint(&s.vault, &1_000_000);
    s.adapter.deposit(&s.vault, &1_000_000, &0);

    // 10% accrual; protocol needs matching liquidity for payout.
    s.protocol.accrue(&1_000);
    s.token_admin_client
        .mint(&s.protocol.address, &100_000);

    let out = s.adapter.withdraw(&s.vault, &1_000_000, &1_100_000);
    assert_eq!(out, 1_100_000);
    assert_eq!(s.token.balance(&s.vault), 1_100_000);
    assert_eq!(s.adapter.max_withdraw(), 0);
}

#[test]
#[should_panic(expected = "Error(Contract, #17)")] // SlippageExceeded
fn withdraw_enforces_min_out() {
    let s = setup(500);
    s.token_admin_client.mint(&s.vault, &1_000_000);
    s.adapter.deposit(&s.vault, &1_000_000, &0);
    // No accrual, so 1_000_000 units pay 1_000_000 — demanding more must revert.
    s.adapter.withdraw(&s.vault, &1_000_000, &1_000_001);
}

#[test]
#[should_panic(expected = "Error(Contract, #4)")] // InsufficientBalance
fn withdraw_rejects_more_units_than_held() {
    let s = setup(500);
    s.token_admin_client.mint(&s.vault, &1_000);
    s.adapter.deposit(&s.vault, &1_000, &0);
    s.adapter.withdraw(&s.vault, &2_000, &0);
}

#[test]
fn position_value_tracks_accrual() {
    let s = setup(500);
    s.token_admin_client.mint(&s.vault, &1_000_000);
    s.adapter.deposit(&s.vault, &1_000_000, &0);
    assert_eq!(s.adapter.position_value(&s.vault), 1_000_000);

    s.protocol.accrue(&2_000); // +20%
    assert_eq!(s.adapter.position_value(&s.vault), 1_200_000);
}

#[test]
fn current_apy_is_protocol_reported() {
    let s = setup(750);
    let apy = s.adapter.current_apy();
    assert_eq!(apy.apy_bps, 750);
    assert_eq!(apy.confidence, ApyConfidence::ProtocolReported);

    s.protocol.set_supply_rate(&425);
    assert_eq!(s.adapter.current_apy().apy_bps, 425);
}

#[test]
fn max_deposit_reflects_protocol_cap() {
    let s = setup(500);
    assert_eq!(s.adapter.max_deposit(), i128::MAX, "uncapped by default");
    s.protocol.set_deposit_cap(&50_000);
    assert_eq!(s.adapter.max_deposit(), 50_000);
}

#[test]
fn underlying_returns_configured_asset() {
    let s = setup(500);
    assert_eq!(s.adapter.underlying(), s.token.address);
}
