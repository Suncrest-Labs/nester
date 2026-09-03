#![cfg(test)]
//! Proof that the generic pool adapter cannot drive a Soroswap pair.
//!
//! `adapter_pool` passes its own suite because `MockAmmPool` implements
//! `deposit(address, amount)`. A deployed Soroswap pair takes `deposit(to)`
//! with no amount, so that call cannot resolve. Without a test that puts the
//! generic adapter in front of a Soroswap-shaped pair, the mismatch stays
//! invisible until a real deposit reverts on-chain.

extern crate std;

use soroban_sdk::{testutils::Address as _, token::StellarAssetClient, Address, Env};

use nester_test_utils::mocks::{MockSoroswapPair, MockSoroswapPairClient};

#[test]
fn the_generic_pool_adapter_cannot_drive_a_soroswap_pair() {
    let env = Env::default();
    env.mock_all_auths();

    let vault = Address::generate(&env);
    let token_admin = Address::generate(&env);
    let usdc = env
        .register_stellar_asset_contract_v2(token_admin.clone())
        .address();
    let xlm = env
        .register_stellar_asset_contract_v2(token_admin)
        .address();
    StellarAssetClient::new(&env, &usdc).mint(&vault, &10_000);

    let pair_id = env.register_contract(None, MockSoroswapPair);
    MockSoroswapPairClient::new(&env, &pair_id).initialize(&usdc, &xlm);

    let adapter_id = env.register_contract(None, adapter_pool::PoolAdapterContract);
    let adapter = adapter_pool::PoolAdapterContractClient::new(&env, &adapter_id);
    adapter.initialize(&vault, &pair_id, &usdc);

    // It invokes deposit(address, amount); the pair's deposit takes only `to`.
    let result = adapter.try_deposit(&vault, &10_000, &0);
    assert!(
        result.is_err(),
        "the generic adapter calls deposit with two arguments, which no \
         Soroswap pair accepts, so this must fail rather than appear to work",
    );
}
