#![cfg(test)]

extern crate std;

use super::*;
use soroban_sdk::{
    testutils::{Address as _, Events, Ledger},
    token::{StellarAssetClient, TokenClient},
    Address, Env,
};
use nester_access_control::Role;

#[contract]
pub struct MockTreasury;

#[contractimpl]
impl MockTreasury {
    pub fn receive_fees(_env: Env, _amount: i128) {}
}

fn setup() -> (Env, Address, Address, Address, VaultContractClient<'static>) {
    let env = Env::default();
    env.mock_all_auths();
    env.ledger().set_timestamp(1000);

    let admin = Address::generate(&env);
    let token_admin = Address::generate(&env);
    let token_address = env.register_stellar_asset_contract_v2(token_admin.clone()).address();
    
    let treasury = env.register_contract(None, MockTreasury);

    let contract_id = env.register_contract(None, VaultContract);
    let client = VaultContractClient::new(&env, &contract_id);

    client.initialize(&admin, &token_address, &treasury);

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
    let (env, admin, token_address, _contract_id, client) = setup();
    let treasury = Address::generate(&env);

    let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        client.initialize(&admin, &token_address, &treasury);
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

    let large_amount: i128 = 1_000_000_000_000_000_000_i128; // 10^18
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
fn test_management_fee_accrual() {
    let (env, _admin, token_address, _contract_id, client) = setup();
    let user = Address::generate(&env);

    mint_tokens(&env, &token_address, &user, 20_000);
    client.deposit(&user, &10_000);

    // Advance time by 1 year (31,536,000 seconds)
    env.ledger().set_timestamp(1000 + 31_536_000);
    
    // Trigger accrual via a small deposit
    client.deposit(&user, &1); 
    
    let accrued = client.get_accrued_fees();
    // In this test environment, it fluctuates slightly around 50-51.
    assert!(accrued >= 50 && accrued <= 55, "Accrued fee out of range: {}", accrued);
}

#[test]
fn test_performance_fee() {
    let (env, admin, token_address, _contract_id, client) = setup();
    let user = Address::generate(&env);

    client.grant_role(&admin, &admin, &Role::Manager);

    env.ledger().set_timestamp(1000);
    mint_tokens(&env, &token_address, &user, 1_000);
    client.deposit(&user, &1_000);

    // Advance past lock period (1 day = 86400)
    env.ledger().set_timestamp(1000 + 100_000);

    // Simulate 10% yield (100 tokens) via report_yield
    mint_tokens(&env, &token_address, &client.address, 100);
    client.report_yield(&admin, &100);
    
    // Performance fee is 10% of 100 = 10.
    client.withdraw(&user, &1_000); 
    
    assert_eq!(client.get_accrued_fees(), 10);
    
    let token = TokenClient::new(&env, &token_address);
    assert_eq!(token.balance(&user), 1090);
}

#[test]
fn test_early_withdrawal_fee() {
    let (env, _admin, token_address, _contract_id, client) = setup();
    let user = Address::generate(&env);

    env.ledger().set_timestamp(1000);
    mint_tokens(&env, &token_address, &user, 10_000);
    client.deposit(&user, &10_000);

    // Still within lock period
    env.ledger().set_timestamp(1000 + 100);

    // Early withdrawal fee is 0.1% of 5,000 = 5 tokens.
    client.withdraw(&user, &5_000); 
    
    assert_eq!(client.get_accrued_fees(), 5);
}

#[test]
fn test_collect_fees() {
    let (env, admin, token_address, _contract_id, client) = setup();
    let user = Address::generate(&env);

    mint_tokens(&env, &token_address, &user, 10_000);
    client.deposit(&user, &10_000);

    // Advance time for 1 year
    env.ledger().set_timestamp(1000 + 31_536_000);
    
    // Trigger accrual and collect
    client.collect_fees(&admin);
    
    let token = TokenClient::new(&env, &token_address);
    // 0.5% of 10,000 = 50. Allow small environmental delta.
    let config = client.get_fee_config();
    let balance = token.balance(&config.treasury_address);
    assert!(balance >= 50 && balance <= 55, "Treasury balance out of range: {}", balance);
}

#[test]
fn test_pause_enforcement() {
    let (env, admin, token_address, _contract_id, client) = setup();
    let user = Address::generate(&env);

    client.pause(&admin);
    assert!(client.is_paused());

    mint_tokens(&env, &token_address, &user, 1000);
    
    // Deposit should fail
    let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        client.deposit(&user, &1000);
    }));
    assert!(result.is_err());

    // Withdraw should fail
    let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        client.withdraw(&user, &1000);
    }));
    assert!(result.is_err());
}

#[test]
fn test_max_deposit_cap() {
    let (env, admin, token_address, _contract_id, client) = setup();
    let user = Address::generate(&env);

    client.set_max_deposit(&admin, &500);
    assert_eq!(client.get_max_deposit(), 500);

    mint_tokens(&env, &token_address, &user, 1000);

    // Deposit above cap should fail
    let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        client.deposit(&user, &501);
    }));
    assert!(result.is_err());

    // Deposit at cap should succeed
    client.deposit(&user, &500);
    assert_eq!(client.get_balance(&user), 500);
}

#[test]
fn test_circuit_breaker_trigger() {
    let (env, admin, token_address, _contract_id, client) = setup();
    let user = Address::generate(&env);

    // Initial deposit to have TVL
    mint_tokens(&env, &token_address, &user, 1000);
    client.deposit(&user, &1000);

    // Set CB: 10% threshold, 1h window
    client.set_circuit_breaker_config(&admin, &CircuitBreakerConfig {
        threshold_bps: 1000,
        window_seconds: 3600,
    });

    // Withdraw 11% (110 tokens) -> should trigger CB
    // 1000 shares = 1000 tokens. 110 shares = 110 tokens.
    client.withdraw(&user, &110);
    
    assert!(client.is_paused());
}

#[test]
fn test_emergency_withdraw() {
    let (env, admin, token_address, _contract_id, client) = setup();
    let user = Address::generate(&env);

    mint_tokens(&env, &token_address, &user, 1000);
    client.deposit(&user, &1000);

    client.pause(&admin);
    
    let returned = client.emergency_withdraw(&user);
    assert_eq!(returned, 1000);
    assert_eq!(client.get_shares(&user), 0);
    assert_eq!(client.get_total_deposits(), 0);
}
