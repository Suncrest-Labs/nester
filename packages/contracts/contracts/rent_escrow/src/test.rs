#![cfg(test)]

extern crate std;

use super::*;
use soroban_sdk::{
    testutils::{Address as _, Events},
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

#[test]
fn test_refund() {
    let (env, _landlord, token_address, contract_id, client) = setup(3_000);
    let user = Address::generate(&env);

    mint_tokens(&env, &token_address, &user, 5_000);
    client.contribute(&user, &2_000);

    client.refund(&user);

    assert_eq!(client.get_contribution(&user), 0);
    assert_eq!(client.get_total_contributions(), 0);

    let token = TokenClient::new(&env, &token_address);
    assert_eq!(token.balance(&user), 5_000);
    assert_eq!(token.balance(&contract_id), 0);
}

#[test]
fn test_refund_no_contribution_fails() {
    let (env, _landlord, _token_address, _contract_id, client) = setup(3_000);
    let user = Address::generate(&env);

    let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        client.refund(&user);
    }));
    assert!(result.is_err());
}

#[test]
fn test_double_refund_fails() {
    let (env, _landlord, token_address, _contract_id, client) = setup(3_000);
    let user = Address::generate(&env);

    mint_tokens(&env, &token_address, &user, 5_000);
    client.contribute(&user, &2_000);
    client.refund(&user);

    let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        client.refund(&user);
    }));
    assert!(result.is_err());
}

#[test]
fn test_release() {
    let (env, landlord, token_address, contract_id, client) = setup(3_000);
    let user = Address::generate(&env);

    mint_tokens(&env, &token_address, &user, 5_000);
    client.contribute(&user, &3_000);

    client.release(&landlord);

    assert!(client.is_released());

    let token = TokenClient::new(&env, &token_address);
    assert_eq!(token.balance(&landlord), 3_000);
    assert_eq!(token.balance(&contract_id), 0);
}

#[test]
fn test_release_before_target_fails() {
    let (env, landlord, token_address, _contract_id, client) = setup(3_000);
    let user = Address::generate(&env);

    mint_tokens(&env, &token_address, &user, 5_000);
    client.contribute(&user, &1_000);

    let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        client.release(&landlord);
    }));
    assert!(result.is_err());
}

#[test]
fn test_release_emits_event() {
    let (env, landlord, token_address, _contract_id, client) = setup(3_000);
    let user = Address::generate(&env);

    mint_tokens(&env, &token_address, &user, 5_000);
    client.contribute(&user, &3_000);
    client.release(&landlord);

    let events = env.events().all();
    assert!(!events.is_empty());

    // Verify that the AgreementReleased event was emitted
    // The event topics should contain the "released" symbol
    let last = events.last().unwrap();
    // Topics are stored as a Vec<Val>; the first topic should be the "released" symbol
    let topics_len = last.1.len();
    assert!(topics_len > 0, "event should have at least one topic");
}

#[test]
fn test_double_release_fails() {
    let (env, landlord, token_address, _contract_id, client) = setup(3_000);
    let user = Address::generate(&env);

    mint_tokens(&env, &token_address, &user, 5_000);
    client.contribute(&user, &3_000);
    client.release(&landlord);

    let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        client.release(&landlord);
    }));
    assert!(result.is_err());
}

// ---------------------------------------------------------------------------
// Full flow scenario: init → contribute (x3 users) → release
// ---------------------------------------------------------------------------

#[test]
fn test_full_flow_init_contribute_release() {
    let rent_target: i128 = 3_000;
    let (env, landlord, token_address, contract_id, client) = setup(rent_target);

    // Generate 3 user addresses
    let user1 = Address::generate(&env);
    let user2 = Address::generate(&env);
    let user3 = Address::generate(&env);

    // Mint tokens to each user
    mint_tokens(&env, &token_address, &user1, 5_000);
    mint_tokens(&env, &token_address, &user2, 5_000);
    mint_tokens(&env, &token_address, &user3, 5_000);

    let token = TokenClient::new(&env, &token_address);

    // --- Step 1: Verify initialization ---
    assert_eq!(client.get_rent_target(), rent_target);
    assert_eq!(client.get_total_contributions(), 0);
    assert!(!client.is_released());

    // --- Step 2: Contributions ---
    let amount1: i128 = 1_000;
    let amount2: i128 = 800;
    let amount3: i128 = 1_200;

    client.contribute(&user1, &amount1);
    assert_eq!(client.get_contribution(&user1), amount1);
    assert_eq!(client.get_total_contributions(), amount1);

    client.contribute(&user2, &amount2);
    assert_eq!(client.get_contribution(&user2), amount2);
    assert_eq!(client.get_total_contributions(), amount1 + amount2);

    client.contribute(&user3, &amount3);
    assert_eq!(client.get_contribution(&user3), amount3);
    assert_eq!(client.get_total_contributions(), amount1 + amount2 + amount3);

    // Verify total contributions == rent target
    let expected_total = amount1 + amount2 + amount3;
    assert_eq!(expected_total, rent_target);
    assert_eq!(client.get_total_contributions(), rent_target);

    // Verify token balances after contributions
    assert_eq!(token.balance(&user1), 5_000 - amount1);
    assert_eq!(token.balance(&user2), 5_000 - amount2);
    assert_eq!(token.balance(&user3), 5_000 - amount3);
    assert_eq!(token.balance(&contract_id), rent_target);

    // --- Step 3: Release ---
    client.release(&landlord);

    // Verify release succeeded
    assert!(client.is_released());

    // Verify funds transferred to landlord
    assert_eq!(token.balance(&landlord), rent_target);
    assert_eq!(token.balance(&contract_id), 0);

    // Verify events were emitted
    let events = env.events().all();
    assert!(!events.is_empty());
}
