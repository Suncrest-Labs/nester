#![cfg(test)]

use super::*;
use soroban_sdk::{testutils::Address as _, vec, Env, String};

#[test]
fn test_initialize() {
    let env = Env::default();
    let admin = Address::generate(&env);

    GoalRewardsContract::initialize(env.clone(), admin.clone());

    assert_eq!(
        GoalRewardsContract::get_reward_pool_balance(env),
        0
    );
}

#[test]
fn test_configure_reward() {
    let env = Env::default();
    let admin = Address::generate(&env);

    GoalRewardsContract::initialize(env.clone(), admin.clone());

    GoalRewardsContract::configure_reward(
        env.clone(),
        admin.clone(),
        1,
        500,  // 5%
        100_000_000, // 100 USDC
        10_000_000,  // 10 USDC
    );

    let config = GoalRewardsContract::get_reward_config(env.clone(), 1).unwrap();
    assert_eq!(config.bonus_bps, 500);
    assert_eq!(config.max_bonus_absolute, 100_000_000);
    assert_eq!(config.min_amount, 10_000_000);
}

#[test]
fn test_top_up_reward_pool() {
    let env = Env::default();
    env.mock_all_auths();
    let admin = Address::generate(&env);
    let token = Address::generate(&env);

    GoalRewardsContract::initialize(env.clone(), admin.clone());

    // Mock token transfer would need to be set up
    // This is a placeholder test structure
    GoalRewardsContract::top_up_reward_pool(
        env.clone(),
        admin.clone(),
        token.clone(),
        1_000_000_000,
    );

    assert_eq!(
        GoalRewardsContract::get_reward_pool_balance(env),
        1_000_000_000
    );
}

#[test]
fn test_compute_effort_score() {
    let env = Env::default();
    
    let contributions = vec![
        &env,
        Contribution { amount: 100, timestamp: 1000 },
        Contribution { amount: 200, timestamp: 2000 },
        Contribution { amount: 300, timestamp: 3000 },
    ];

    let goal = GoalState {
        id: 1,
        owner: Address::generate(&env),
        target_amount: 1000,
        current_balance: 600,
        created_at: 0,
        completed_at: Some(4000),
        deadline: 5000,
        is_completed: true,
        contributions,
        vault_id: Address::generate(&env),
    };

    let score = GoalRewardsContract::compute_effort_score(&env, &goal);
    // Area: 100*1000 + 300*1000 + 600*1000 = 1,000,000
    assert_eq!(score, 1_000_000);
}