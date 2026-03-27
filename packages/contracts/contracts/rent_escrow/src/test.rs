#![cfg(test)]

extern crate std;

use super::*;
use soroban_sdk::{
    testutils::Address as _,
    token::{StellarAssetClient, TokenClient},
    Address, Env,
};

fn setup(rent_target: i128) -> (Env, Address, Address, Address, RentEscrowContractClient<'static>) {
    let env = Env::default();
    env.mock_all_auths();

    let landlord = Address::generate(&env);
    let token_admin = Address::generate(&env);
    let token_address = env.register_stellar_asset_contract_v2(token_admin.clone()).address();

    let contract_id = env.register_contract(None, RentEscrowContract);
    let client = RentEscrowContractClient::new(&env, &contract_id);

    client.initialize(&landlord, &token_address, &rent_target);

    (env, landlord, token_address, contract_id, client)
}

fn mint_tokens(env: &Env, token_address: &Address, to: &Address, amount: i128) {
    StellarAssetClient::new(env, token_address).mint(to, &amount);
}

#[test]
fn test_initialize() {
    let (_env, _landlord, _token, _contract_id, client) = setup(3_000);

    assert_eq!(client.get_rent_target(), 3_000);
    assert_eq!(client.get_total_contributions(), 0);
    assert!(!client.is_released());
}

#[test]
fn test_contribute() {
    let (env, _landlord, token_address, contract_id, client) = setup(3_000);
    let user = Address::generate(&env);

    mint_tokens(&env, &token_address, &user, 5_000);
    client.contribute(&user, &1_000);

    assert_eq!(client.get_contribution(&user), 1_000);
    assert_eq!(client.get_total_contributions(), 1_000);

    let token = TokenClient::new(&env, &token_address);
    assert_eq!(token.balance(&user), 4_000);
    assert_eq!(token.balance(&contract_id), 1_000);
}

#[test]
fn test_contribute_multiple_users() {
    let (env, _landlord, token_address, _contract_id, client) = setup(3_000);
    let user1 = Address::generate(&env);
    let user2 = Address::generate(&env);

    mint_tokens(&env, &token_address, &user1, 5_000);
    mint_tokens(&env, &token_address, &user2, 5_000);

    client.contribute(&user1, &1_000);
    client.contribute(&user2, &2_000);

    assert_eq!(client.get_contribution(&user1), 1_000);
    assert_eq!(client.get_contribution(&user2), 2_000);
    assert_eq!(client.get_total_contributions(), 3_000);
}

#[test]
fn test_contribute_zero_fails() {
    let (env, _landlord, token_address, _contract_id, client) = setup(3_000);
    let user = Address::generate(&env);
    mint_tokens(&env, &token_address, &user, 5_000);

    let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        client.contribute(&user, &0);
    }));
    assert!(result.is_err());
}

#[test]
fn test_contribute_negative_fails() {
    let (env, _landlord, _token_address, _contract_id, client) = setup(3_000);
    let user = Address::generate(&env);

    let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        client.contribute(&user, &-100);
    }));
    assert!(result.is_err());
}
