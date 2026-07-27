#![cfg(test)]
extern crate std;

use soroban_sdk::{
    testutils::{Address as _, Ledger as _},
    Address, BytesN, Env, IntoVal, Val, Vec,
};

use crate::{VaultFactoryContract, VaultFactoryContractClient, VaultRegistryStatus};

mod dummy_vault {
    soroban_sdk::contractimport!(file = "fixtures/dummy_vault.wasm");
}

fn setup() -> (
    Env,
    Address,
    BytesN<32>,
    VaultFactoryContractClient<'static>,
) {
    let env = Env::default();
    env.mock_all_auths();
    let admin = Address::generate(&env);

    let wasm_hash = env.deployer().upload_contract_wasm(dummy_vault::WASM);

    let cid = env.register_contract(None, VaultFactoryContract);
    let client = VaultFactoryContractClient::new(&env, &cid);
    client.initialize(&admin, &wasm_hash);

    (env, admin, wasm_hash, client)
}

fn init_args(env: &Env, admin: &Address, should_fail: bool) -> Vec<Val> {
    soroban_sdk::vec![env, admin.into_val(env), should_fail.into_val(env)]
}

#[test]
fn predict_vault_address_matches_create_vault() {
    let (env, admin, _hash, client) = setup();
    let salt = BytesN::from_array(&env, &[7u8; 32]);
    let predicted = client.predict_vault_address(&salt);

    let asset = Address::generate(&env);
    let args = init_args(&env, &admin, false);
    let created = client.create_vault(&admin, &salt, &asset, &args);

    assert_eq!(predicted, created);
}

#[test]
fn create_vault_registers_and_is_nester_vault_returns_true() {
    let (env, admin, hash, client) = setup();
    let salt = BytesN::from_array(&env, &[1u8; 32]);
    let asset = Address::generate(&env);
    let args = init_args(&env, &admin, false);

    let created = client.create_vault(&admin, &salt, &asset, &args);

    assert!(client.is_nester_vault(&created));
    assert_eq!(client.vault_count(), 1);
    let record = client.get_vault(&salt).unwrap();
    assert_eq!(record.address, created);
    assert_eq!(record.underlying_asset, asset);
    assert_eq!(record.wasm_hash, hash);
    assert_eq!(record.status, VaultRegistryStatus::Active);
}

#[test]
fn independently_deployed_lookalike_is_not_a_nester_vault() {
    let (env, _admin, _hash, client) = setup();
    // Same code, deployed completely outside the factory — never went
    // through create_vault, so the registry must not recognise it.
    let lookalike = env.register_contract_wasm(None, dummy_vault::WASM);
    assert!(!client.is_nester_vault(&lookalike));
}

#[test]
fn failing_initialize_leaves_nothing_deployed_or_registered() {
    let (env, admin, _hash, client) = setup();
    let salt = BytesN::from_array(&env, &[2u8; 32]);
    let asset = Address::generate(&env);
    let args = init_args(&env, &admin, true); // should_fail = true

    let result = client.try_create_vault(&admin, &salt, &asset, &args);
    assert!(result.is_err());

    assert_eq!(client.vault_count(), 0);
    assert!(client.get_vault(&salt).is_none());
    let predicted = client.predict_vault_address(&salt);
    assert!(!client.is_nester_vault(&predicted));
}

#[test]
fn only_admin_or_vault_creator_can_create_vaults() {
    let (env, admin, _hash, client) = setup();
    let outsider = Address::generate(&env);
    let salt = BytesN::from_array(&env, &[3u8; 32]);
    let asset = Address::generate(&env);
    let args = init_args(&env, &admin, false);

    let result = client.try_create_vault(&outsider, &salt, &asset, &args);
    assert!(result.is_err());
    assert_eq!(client.vault_count(), 0);
}

#[test]
fn vault_creator_role_can_create_without_admin() {
    let (env, admin, _hash, client) = setup();
    let creator = Address::generate(&env);
    client.grant_role(&admin, &creator, &nester_access_control::Role::VaultCreator);

    let salt = BytesN::from_array(&env, &[4u8; 32]);
    let asset = Address::generate(&env);
    let args = init_args(&env, &admin, false);
    let created = client.create_vault(&creator, &salt, &asset, &args);

    assert!(client.is_nester_vault(&created));
}

#[test]
fn deprecate_vault_marks_status_without_removing_it() {
    let (env, admin, _hash, client) = setup();
    let salt = BytesN::from_array(&env, &[5u8; 32]);
    let asset = Address::generate(&env);
    let args = init_args(&env, &admin, false);
    let created = client.create_vault(&admin, &salt, &asset, &args);

    client.deprecate_vault(&admin, &created);

    assert!(client.is_nester_vault(&created)); // still recognised
    let record = client.get_vault(&salt).unwrap();
    assert_eq!(record.status, VaultRegistryStatus::Deprecated);
}

#[test]
fn list_vaults_is_bounded_and_paginated() {
    let (env, admin, _hash, client) = setup();
    for i in 0..5u8 {
        let salt = BytesN::from_array(&env, &[10 + i; 32]);
        let asset = Address::generate(&env);
        let args = init_args(&env, &admin, false);
        client.create_vault(&admin, &salt, &asset, &args);
    }
    assert_eq!(client.vault_count(), 5);

    let page = client.list_vaults(&0, &2);
    assert_eq!(page.len(), 2);

    // A huge limit never over-returns beyond the actual count.
    let bounded = client.list_vaults(&0, &10_000);
    assert_eq!(bounded.len(), 5);
}

#[test]
fn changing_wasm_hash_requires_timelock() {
    let (env, admin, _hash, client) = setup();
    let new_wasm_hash = env.deployer().upload_contract_wasm(dummy_vault::WASM);

    let op_id = client.propose_wasm_hash(&admin, &new_wasm_hash);

    // Cannot apply before the delay elapses.
    assert!(client.try_apply_wasm_hash(&admin, &op_id).is_err());

    env.ledger()
        .with_mut(|l| l.timestamp += nester_timelock::DEFAULT_DELAY + 1);
    client.apply_wasm_hash(&admin, &op_id);

    assert_eq!(client.get_vault_wasm_hash(), new_wasm_hash);
}
