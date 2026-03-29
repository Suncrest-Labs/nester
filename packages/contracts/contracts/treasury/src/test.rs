#![cfg(test)]

use super::*;
use soroban_sdk::{
    testutils::Address as _,
    Address, Env,
};

#[contract]
pub struct MockVault;

#[contractimpl]
impl MockVault {}

fn setup() -> (Env, Address, Address, TreasuryContractClient<'static>) {
    let env = Env::default();
    env.mock_all_auths();

    let admin = Address::generate(&env);
    let vault = env.register_contract(None, MockVault);

    let contract_id = env.register_contract(None, TreasuryContract);
    let client = TreasuryContractClient::new(&env, &contract_id);

    client.initialize(&admin, &vault);

    (env, admin, vault, client)
}

#[test]
fn test_initialize() {
    let (_env, _admin, vault, client) = setup();
    assert_eq!(client.get_vault(), vault);
    assert_eq!(client.get_total_received(), 0);
}

#[test]
fn test_receive_fees() {
    let (env, _admin, vault, client) = setup();
    
    env.as_contract(&vault, || {
        client.receive_fees(&1000);
    });

    assert_eq!(client.get_total_received(), 1000);
}

#[test]
fn test_withdraw() {
    let (env, admin, _vault, client) = setup();
    let to = Address::generate(&env);
    let token_admin = Address::generate(&env);
    let token_address = env.register_stellar_asset_contract_v2(token_admin.clone()).address();
    let token_client = soroban_sdk::token::StellarAssetClient::new(&env, &token_address);
    
    token_client.mint(&client.address, &5000);

    client.withdraw(&admin, &to, &token_address, &2000);

    let token = soroban_sdk::token::TokenClient::new(&env, &token_address);
    assert_eq!(token.balance(&to), 2000);
    assert_eq!(token.balance(&client.address), 3000);
}
