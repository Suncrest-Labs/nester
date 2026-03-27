#![cfg(test)]

extern crate std;

use super::*;
use soroban_sdk::{
    testutils::{Address as _, Events},
    token::{StellarAssetClient, TokenClient},
    Address, Env,
};

fn setup() -> (Env, Address, Address, Address, VaultContractClient<'static>) {
    let env = Env::default();
    env.mock_all_auths();

    let admin = Address::generate(&env);
    let token_admin = Address::generate(&env);
    let token_address = env.register_stellar_asset_contract_v2(token_admin.clone()).address();

    let contract_id = env.register_contract(None, VaultContract);
    let client = VaultContractClient::new(&env, &contract_id);

    client.initialize(&admin, &token_address);

    (env, admin, token_address, contract_id, client)
}

fn mint_tokens(env: &Env, token_address: &Address, to: &Address, amount: i128) {
    StellarAssetClient::new(env, token_address).mint(to, &amount);
}

#[test]
fn test_initialize() {
    let (_env, _admin, token_address, _contract_id, client) = setup();

    assert_eq!(client.get_status(), VaultStatus::Active);
    assert_eq!(client.get_token(), token_address);
    assert_eq!(client.get_total_deposits(), 0);
}

#[test]
fn test_initialize_twice_fails() {
    let (_env, admin, token_address, _contract_id, client) = setup();

    let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        client.initialize(&admin, &token_address);
    }));
    assert!(result.is_err());
}

#[test]
fn test_deposit() {
    let (env, _admin, token_address, contract_id, client) = setup();
    let user = Address::generate(&env);

    mint_tokens(&env, &token_address, &user, 1_000);

    let balance = client.deposit(&user, &500);
    assert_eq!(balance, 500);
    assert_eq!(client.get_balance(&user), 500);
    assert_eq!(client.get_total_deposits(), 500);

    let token = TokenClient::new(&env, &token_address);
    assert_eq!(token.balance(&user), 500);
    assert_eq!(token.balance(&contract_id), 500);
}

#[test]
fn test_multiple_deposits() {
    let (env, _admin, token_address, _contract_id, client) = setup();
    let user = Address::generate(&env);

    mint_tokens(&env, &token_address, &user, 5_000);

    client.deposit(&user, &1_000);
    client.deposit(&user, &2_000);
    let balance = client.deposit(&user, &500);

    assert_eq!(balance, 3_500);
    assert_eq!(client.get_balance(&user), 3_500);
    assert_eq!(client.get_total_deposits(), 3_500);
}

#[test]
fn test_multiple_users_deposit() {
    let (env, _admin, token_address, _contract_id, client) = setup();
    let user_a = Address::generate(&env);
    let user_b = Address::generate(&env);

    mint_tokens(&env, &token_address, &user_a, 5_000);
    mint_tokens(&env, &token_address, &user_b, 3_000);

    client.deposit(&user_a, &2_000);
    client.deposit(&user_b, &1_500);

    assert_eq!(client.get_balance(&user_a), 2_000);
    assert_eq!(client.get_balance(&user_b), 1_500);
    assert_eq!(client.get_total_deposits(), 3_500);
}

#[test]
fn test_withdraw() {
    let (env, _admin, token_address, contract_id, client) = setup();
    let user = Address::generate(&env);

    mint_tokens(&env, &token_address, &user, 1_000);
    client.deposit(&user, &1_000);

    let balance = client.withdraw(&user, &400);
    assert_eq!(balance, 600);
    assert_eq!(client.get_balance(&user), 600);
    assert_eq!(client.get_total_deposits(), 600);

    let token = TokenClient::new(&env, &token_address);
    assert_eq!(token.balance(&user), 400);
    assert_eq!(token.balance(&contract_id), 600);
}

#[test]
fn test_withdraw_full_balance() {
    let (env, _admin, token_address, _contract_id, client) = setup();
    let user = Address::generate(&env);

    mint_tokens(&env, &token_address, &user, 1_000);
    client.deposit(&user, &1_000);

    let balance = client.withdraw(&user, &1_000);
    assert_eq!(balance, 0);
    assert_eq!(client.get_balance(&user), 0);
    assert_eq!(client.get_total_deposits(), 0);
}

#[test]
fn test_withdraw_exceeds_balance_fails() {
    let (env, _admin, token_address, _contract_id, client) = setup();
    let user = Address::generate(&env);

    mint_tokens(&env, &token_address, &user, 1_000);
    client.deposit(&user, &500);

    let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        client.withdraw(&user, &600);
    }));
    assert!(result.is_err());
}

#[test]
fn test_deposit_zero_fails() {
    let (env, _admin, token_address, _contract_id, client) = setup();
    let user = Address::generate(&env);

    mint_tokens(&env, &token_address, &user, 1_000);

    let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        client.deposit(&user, &0);
    }));
    assert!(result.is_err());
}

#[test]
fn test_deposit_negative_fails() {
    let (env, _admin, _token_address, _contract_id, client) = setup();
    let user = Address::generate(&env);

    let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        client.deposit(&user, &-100);
    }));
    assert!(result.is_err());
}

#[test]
fn test_withdraw_zero_fails() {
    let (env, _admin, token_address, _contract_id, client) = setup();
    let user = Address::generate(&env);

    mint_tokens(&env, &token_address, &user, 1_000);
    client.deposit(&user, &500);

    let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        client.withdraw(&user, &0);
    }));
    assert!(result.is_err());
}

#[test]
fn test_pause_blocks_deposits() {
    let (env, admin, token_address, _contract_id, client) = setup();
    let user = Address::generate(&env);

    mint_tokens(&env, &token_address, &user, 1_000);

    client.pause(&admin);
    assert_eq!(client.get_status(), VaultStatus::Paused);

    let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        client.deposit(&user, &500);
    }));
    assert!(result.is_err());
}

#[test]
fn test_pause_allows_withdrawals() {
    let (env, admin, token_address, _contract_id, client) = setup();
    let user = Address::generate(&env);

    mint_tokens(&env, &token_address, &user, 1_000);
    client.deposit(&user, &1_000);

    client.pause(&admin);

    let balance = client.withdraw(&user, &500);
    assert_eq!(balance, 500);
}

#[test]
fn test_unpause_resumes_deposits() {
    let (env, admin, token_address, _contract_id, client) = setup();
    let user = Address::generate(&env);

    mint_tokens(&env, &token_address, &user, 1_000);

    client.pause(&admin);
    client.unpause(&admin);
    assert_eq!(client.get_status(), VaultStatus::Active);

    let balance = client.deposit(&user, &500);
    assert_eq!(balance, 500);
}

#[test]
fn test_only_admin_can_pause() {
    let (env, _admin, _token_address, _contract_id, client) = setup();
    let outsider = Address::generate(&env);

    let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        client.pause(&outsider);
    }));
    assert!(result.is_err());
}

#[test]
fn test_only_admin_can_unpause() {
    let (env, admin, _token_address, _contract_id, client) = setup();
    let outsider = Address::generate(&env);

    client.pause(&admin);

    let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        client.unpause(&outsider);
    }));
    assert!(result.is_err());
}

#[test]
fn test_get_balance_unregistered_user() {
    let (env, _admin, _token_address, _contract_id, client) = setup();
    let unknown = Address::generate(&env);

    assert_eq!(client.get_balance(&unknown), 0);
}

#[test]
fn test_deposit_emits_event() {
    let (env, _admin, token_address, _contract_id, client) = setup();
    let user = Address::generate(&env);

    mint_tokens(&env, &token_address, &user, 1_000);
    client.deposit(&user, &500);

    assert!(!env.events().all().is_empty());
}

#[test]
fn test_withdraw_emits_event() {
    let (env, _admin, token_address, _contract_id, client) = setup();
    let user = Address::generate(&env);

    mint_tokens(&env, &token_address, &user, 1_000);
    client.deposit(&user, &1_000);
    client.withdraw(&user, &300);

    assert!(!env.events().all().is_empty());
}

#[test]
fn test_large_deposit_and_withdraw() {
    let (env, _admin, token_address, contract_id, client) = setup();
    let user = Address::generate(&env);

    let large_amount: i128 = i128::MAX / 2;
    mint_tokens(&env, &token_address, &user, large_amount);

    let balance = client.deposit(&user, &large_amount);
    assert_eq!(balance, large_amount);
    assert_eq!(client.get_balance(&user), large_amount);
    assert_eq!(client.get_total_deposits(), large_amount);

    let token = TokenClient::new(&env, &token_address);
    assert_eq!(token.balance(&contract_id), large_amount);

    let balance = client.withdraw(&user, &large_amount);
    assert_eq!(balance, 0);
    assert_eq!(client.get_balance(&user), 0);
    assert_eq!(client.get_total_deposits(), 0);
}

#[test]
fn test_multiple_large_deposits_overflow_protection() {
    let (env, _admin, token_address, _contract_id, client) = setup();
    let user_a = Address::generate(&env);
    let user_b = Address::generate(&env);

    let large_amount: i128 = i128::MAX / 2;
    mint_tokens(&env, &token_address, &user_a, large_amount);
    mint_tokens(&env, &token_address, &user_b, large_amount);

    client.deposit(&user_a, &large_amount);

    // Second large deposit should panic due to overflow in total tracking
    let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        client.deposit(&user_b, &large_amount);
    }));
    assert!(result.is_err());
}
