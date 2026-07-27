#![no_std]

use soroban_sdk::{
    contract, contractimpl, contracttype, panic_with_error, symbol_short,
    Address, Env, Symbol, Vec, token,
};
use nester_common::{ContractError};
use nester_access_control::{AccessControl, Role};

const REWARD: Symbol = symbol_short!("REWARD");
const CLAIM: Symbol = symbol_short!("CLAIM");
const CONFIG: Symbol = symbol_short!("CONFIG");
const TOPUP: Symbol = symbol_short!("TOPUP");

const BASIS_POINTS: u32 = 10_000;
const MIN_ACTIVE_DURATION_SECONDS: u64 = 86_400;
const MAX_BONUS_BPS: u32 = 5_000;
const MAX_BONUS_ABSOLUTE: i128 = 1_000_000_000_000;

#[contracttype]
#[derive(Clone)]
enum DataKey {
    RewardPoolBalance,
    RewardConfig(u32),
    Claimed(u64),
    TotalRewardsClaimed,
    TotalRewardsPaid,
}

#[contracttype]
#[derive(Clone)]
pub struct RewardConfig {
    pub bonus_bps: u32,
    pub max_bonus_absolute: i128,
    pub min_amount: i128,
}

#[contracttype]
#[derive(Clone)]
pub struct Contribution {
    pub amount: i128,
    pub timestamp: u64,
}

#[contracttype]
#[derive(Clone)]
pub struct GoalState {
    pub id: u64,
    pub owner: Address,
    pub target_amount: i128,
    pub current_balance: i128,
    pub created_at: u64,
    pub completed_at: Option<u64>,
    pub deadline: u64,
    pub is_completed: bool,
    pub contributions: Vec<Contribution>,
    pub vault_id: Address,
}

#[contracttype]
#[derive(Clone)]
pub struct RewardClaim {
    pub goal_id: u64,
    pub recipient: Address,
    pub bonus_amount: i128,
    pub effort_score: i128,
    pub claimed_at: u64,
}

#[contract]
pub struct GoalRewardsContract;

#[contractimpl]
impl GoalRewardsContract {
    pub fn initialize(env: Env, admin: Address) {
        if env.storage().instance().has(&DataKey::RewardPoolBalance) {
            panic_with_error!(&env, ContractError::AlreadyInitialized);
        }

        AccessControl::initialize(&env, &admin);

        env.storage().instance().set(&DataKey::RewardPoolBalance, &0_i128);
        env.storage().instance().set(&DataKey::TotalRewardsClaimed, &0_i128);
        env.storage().instance().set(&DataKey::TotalRewardsPaid, &0_i128);
    }

    pub fn configure_reward(
        env: Env,
        admin: Address,
        goal_tier: u32,
        bonus_bps: u32,
        max_bonus_absolute: i128,
        min_amount: i128,
    ) {
        admin.require_auth();
        AccessControl::require_role(&env, &admin, Role::Admin);

        if bonus_bps > MAX_BONUS_BPS {
            panic_with_error!(&env, ContractError::ExceedsLimit);
        }

        if max_bonus_absolute <= 0 || max_bonus_absolute > MAX_BONUS_ABSOLUTE {
            panic_with_error!(&env, ContractError::ExceedsLimit);
        }

        if min_amount < 0 {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }

        let config = RewardConfig {
            bonus_bps,
            max_bonus_absolute,
            min_amount,
        };

        env.storage().instance().set(&DataKey::RewardConfig(goal_tier), &config);

        env.events().publish(
            (REWARD, CONFIG),
            (goal_tier, bonus_bps, max_bonus_absolute, min_amount),
        );
    }

    pub fn top_up_reward_pool(
        env: Env,
        admin: Address,
        token: Address,
        amount: i128,
    ) {
        admin.require_auth();
        AccessControl::require_role(&env, &admin, Role::Admin);

        if amount <= 0 {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }

        let token_client = token::Client::new(&env, &token);
        let contract_addr = env.current_contract_address();

        token_client.transfer_from(&contract_addr, &admin, &contract_addr, &amount);

        let current: i128 = env.storage().instance().get(&DataKey::RewardPoolBalance).unwrap_or(0);
        let new_balance = current.checked_add(amount).ok_or(ContractError::ArithmeticOverflow).unwrap();

        env.storage().instance().set(&DataKey::RewardPoolBalance, &new_balance);

        env.events().publish((REWARD, TOPUP), (amount, new_balance));
    }

    pub fn get_reward_pool_balance(env: Env) -> i128 {
        env.storage().instance().get(&DataKey::RewardPoolBalance).unwrap_or(0)
    }

    pub fn calculate_bonus(env: Env, goal: GoalState, config: RewardConfig) -> i128 {
        let effort_score = Self::compute_effort_score(&env, &goal);

        let base_bonus = effort_score
            .checked_mul(config.bonus_bps as i128)
            .unwrap_or(0)
            / BASIS_POINTS as i128;

        if base_bonus > config.max_bonus_absolute {
            return config.max_bonus_absolute;
        }

        base_bonus
    }

    fn compute_effort_score(env: &Env, goal: &GoalState) -> i128 {
        if goal.contributions.is_empty() {
            return 0;
        }

        let mut score: i128 = 0;
        let mut prev_time = goal.created_at;
        let mut prev_balance: i128 = 0;

        for i in 0..goal.contributions.len() {
            let contrib = goal.contributions.get(i).unwrap();
            let time_delta = contrib.timestamp.saturating_sub(prev_time) as i128;
            
            score = score.checked_add(prev_balance.checked_mul(time_delta).unwrap_or(0)).unwrap_or(0);

            prev_time = contrib.timestamp;
            prev_balance = prev_balance.checked_add(contrib.amount).unwrap_or(prev_balance);
        }

        let end_time = goal.completed_at.unwrap_or(env.ledger().timestamp());
        let final_time_delta = end_time.saturating_sub(prev_time) as i128;
        score = score.checked_add(prev_balance.checked_mul(final_time_delta).unwrap_or(0)).unwrap_or(0);

        score
    }

    pub fn check_eligibility(
        env: Env,
        goal: GoalState,
        config: RewardConfig,
        max_single_contribution_pct: u32,
        min_contribution_periods: u32,
    ) -> bool {
        if !goal.is_completed || goal.completed_at.is_none() {
            return false;
        }

        let completed_at = goal.completed_at.unwrap();
        if completed_at > goal.deadline {
            return false;
        }

        let active_duration = completed_at.saturating_sub(goal.created_at);
        if active_duration < MIN_ACTIVE_DURATION_SECONDS {
            return false;
        }

        if !Self::check_contribution_pattern(&goal, max_single_contribution_pct) {
            return false;
        }

        if !Self::check_distinct_periods(&goal, min_contribution_periods) {
            return false;
        }

        let bonus = Self::calculate_bonus(env, goal, config);
        if bonus <= 0 {
            return false;
        }

        true
    }

    fn check_contribution_pattern(goal: &GoalState, max_single_pct: u32) -> bool {
        let target = goal.target_amount;
        if target <= 0 {
            return false;
        }

        let max_allowed = target
            .checked_mul(max_single_pct as i128)
            .unwrap_or(0)
            / BASIS_POINTS as i128;

        for i in 0..goal.contributions.len() {
            let contrib = goal.contributions.get(i).unwrap();
            if contrib.amount > max_allowed {
                return false;
            }
        }

        true
    }

    fn check_distinct_periods(goal: &GoalState, min_periods: u32) -> bool {
        if goal.contributions.len() < min_periods as usize {
            return false;
        }

        let mut periods = Vec::new(goal.contributions.env());
        let mut last_day = 0u64;

        for i in 0..goal.contributions.len() {
            let contrib = goal.contributions.get(i).unwrap();
            let day = contrib.timestamp / 86_400;

            if day != last_day {
                periods.push_back(day);
                last_day = day;
            }
        }

        periods.len() >= min_periods as usize
    }

    pub fn claim_goal_bonus(
        env: Env,
        user: Address,
        goal_id: u64,
        goal_data: GoalState,
        tier: u32,
        token: Address,
        max_single_contribution_pct: u32,
        min_contribution_periods: u32,
    ) -> i128 {
        user.require_auth();

        if goal_data.owner != user {
            panic_with_error!(&env, ContractError::Unauthorized);
        }

        let claimed_key = DataKey::Claimed(goal_id);
        if env.storage().instance().has(&claimed_key) {
            panic_with_error!(&env, ContractError::AlreadyClaimed);
        }

        let config: RewardConfig = env
            .storage()
            .instance()
            .get(&DataKey::RewardConfig(tier))
            .unwrap_or_else(|| panic_with_error!(&env, ContractError::StrategyNotFound));

        if !Self::check_eligibility(
            env.clone(),
            goal_data.clone(),
            config.clone(),
            max_single_contribution_pct,
            min_contribution_periods,
        ) {
            panic_with_error!(&env, ContractError::InvalidOperation);
        }

        let bonus_amount = Self::calculate_bonus(env.clone(), goal_data, config);

        if bonus_amount <= 0 {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }

        let pool_balance: i128 = env.storage().instance().get(&DataKey::RewardPoolBalance).unwrap_or(0);

        if pool_balance < bonus_amount {
            panic_with_error!(&env, ContractError::InsufficientBalance);
        }

        env.storage().instance().set(&DataKey::Claimed(goal_id), &true);

        let new_pool_balance = pool_balance
            .checked_sub(bonus_amount)
            .ok_or(ContractError::ArithmeticOverflow)
            .unwrap();

        env.storage().instance().set(&DataKey::RewardPoolBalance, &new_pool_balance);

        let total_claimed: i128 = env.storage().instance().get(&DataKey::TotalRewardsClaimed).unwrap_or(0);
        env.storage().instance().set(&DataKey::TotalRewardsClaimed, &(total_claimed + 1));

        let total_paid: i128 = env.storage().instance().get(&DataKey::TotalRewardsPaid).unwrap_or(0);
        env.storage().instance().set(&DataKey::TotalRewardsPaid, &(total_paid + bonus_amount));

        let token_client = token::Client::new(&env, &token);
        let contract_addr = env.current_contract_address();
        token_client.transfer(&contract_addr, &user, &bonus_amount);

        env.events().publish(
            (REWARD, CLAIM),
            RewardClaim {
                goal_id,
                recipient: user,
                bonus_amount,
                effort_score: Self::compute_effort_score(&env, &goal_data),
                claimed_at: env.ledger().timestamp(),
            },
        );

        bonus_amount
    }

    pub fn is_claimed(env: Env, goal_id: u64) -> bool {
        env.storage().instance().has(&DataKey::Claimed(goal_id))
    }

    pub fn get_reward_config(env: Env, tier: u32) -> Option<RewardConfig> {
        env.storage().instance().get(&DataKey::RewardConfig(tier))
    }

    pub fn get_total_rewards_claimed(env: Env) -> i128 {
        env.storage().instance().get(&DataKey::TotalRewardsClaimed).unwrap_or(0)
    }

    pub fn get_total_rewards_paid(env: Env) -> i128 {
        env.storage().instance().get(&DataKey::TotalRewardsPaid).unwrap_or(0)
    }
}

#[cfg(test)]
mod test;
