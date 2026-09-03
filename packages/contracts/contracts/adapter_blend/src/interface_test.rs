#![cfg(test)]
//! Proof that the generic lending adapter cannot drive a Blend pool.
//!
//! `adapter_lending` passes its own suite because `MockLendingProtocol`
//! implements the `deposit(address, amount)` entry point that adapter invokes.
//! No Blend pool has that function — a pool's only value-moving entry point is
//! `submit`. Without a test that puts the generic adapter in front of a
//! Blend-shaped pool, that mismatch is invisible until a real deposit reverts
//! on-chain, which is exactly what this asserts.

extern crate std;

use soroban_sdk::{
    testutils::Address as _,
    token::StellarAssetClient,
    Address, Env,
};

use nester_test_utils::mocks::{MockBlendPool, MockBlendPoolClient};

#[test]
fn the_generic_lending_adapter_cannot_drive_a_blend_pool() {
    let env = Env::default();
    env.mock_all_auths();

    let vault = Address::generate(&env);
    let token_admin = Address::generate(&env);
    let token_id = env
        .register_stellar_asset_contract_v2(token_admin)
        .address();
    StellarAssetClient::new(&env, &token_id).mint(&vault, &1_000);

    let pool_id = env.register_contract(None, MockBlendPool);
    MockBlendPoolClient::new(&env, &pool_id).initialize(&token_id, &0);

    // The generic adapter is pointed at a Blend-shaped pool, which is what a
    // real deployment would do.
    let adapter_id = env.register_contract(None, adapter_lending::LendingAdapterContract);
    let adapter = adapter_lending::LendingAdapterContractClient::new(&env, &adapter_id);
    adapter.initialize(&vault, &pool_id, &token_id);

    // It invokes `deposit(address, amount)`; the pool exposes only `submit`,
    // so the call cannot resolve and the deposit fails.
    let result = adapter.try_deposit(&vault, &1_000, &0);
    assert!(
        result.is_err(),
        "the generic adapter invokes a function no Blend pool implements, so \
         this deposit must fail rather than appear to succeed",
    );
}
