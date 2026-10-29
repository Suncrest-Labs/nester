//! Client for the `savings_goal` registry this contract reads goal state from.
//!
//! Declared here rather than taken from the `savings_goal` crate: depending on
//! that crate links its `#[contractimpl]` exports into this WASM, and the
//! entrypoints both contracts share (`grant_role`, `version`, ...) collide at
//! link time. Only the two calls this contract makes are declared.
//!
//! The types mirror `savings_goal`'s field for field. Soroban encodes struct
//! fields and enum variants by name, so they decode the registry's return
//! values exactly; the tests run against the real `savings_goal` contract, so
//! any drift between the two definitions fails them.

use soroban_sdk::{contractclient, contracttype, Address, BytesN, Env, Vec};

#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum GoalStatus {
    Active,
    Completed,
    Abandoned,
    Expired,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct Goal {
    pub owner: Address,
    pub vault: Address,
    pub target_amount: i128,
    pub deadline: u64,
    pub contributed: i128,
    pub milestones: u32,
    pub status: GoalStatus,
    pub contributors: Vec<Address>,
    pub created_at: u64,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct ContributionStats {
    pub contribution_count: u32,
    pub distinct_periods: u32,
    pub periods_seen_mask: u64,
    pub largest_contribution: i128,
    pub balance_time_integral: i128,
    pub last_updated_at: u64,
    pub completed_at: u64,
}

#[allow(dead_code)]
#[contractclient(name = "SavingsGoalContractClient")]
pub trait SavingsGoalRegistry {
    fn get_goal(env: Env, goal_id: BytesN<32>) -> Goal;
    fn get_contribution_stats(env: Env, goal_id: BytesN<32>) -> ContributionStats;
}
