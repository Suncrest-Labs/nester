//! Nester Savings Goal Registry
//!
//! Lightweight on-chain record binding a savings goal to an owner, a target
//! amount, a deadline, and a vault, plus milestone attestations emitted as
//! the goal progresses. This is deliberately not a ledger: deposits still
//! flow through the vault, and the backend remains the system of record for
//! presentation data (emoji, colour, description). This contract records
//! only what needs to be trustless: who owns the goal, what they committed
//! to, how far along they are, and whether they finished.
//!
//! # Custody
//! The registry **never holds funds**. `contribute` only updates accounting
//! and emits attestations; it never moves a token. A registry bug can
//! therefore never lose funds.
//!
//! # Milestone idempotency
//! Each goal stores a bitmask of the 25/50/75/100% thresholds already
//! attested. `contribute` only emits `goal_milestone_reached` for a
//! threshold the first time its bit transitions from unset to set, so
//! retried or repeated calls can never double-attest the same milestone —
//! matching the backend notifier's own `notified_milestones` dedup.
//!
//! # Vault validation
//! `create_goal` cross-calls the deployed [`nester_vault_factory`]'s
//! `is_nester_vault` to reject goals pointing at an arbitrary contract.

#![no_std]

use soroban_sdk::{
    contract, contractimpl, contracttype, panic_with_error, symbol_short, Address, Env, IntoVal,
    Symbol, Vec,
};

use nester_access_control::{AccessControl, Role};
use nester_common::ContractError;

const SAVINGS_GOAL: Symbol = symbol_short!("SAV_GOAL");
const GOAL_CREATED: Symbol = symbol_short!("GOAL_NEW");
const GOAL_MILESTONE: Symbol = symbol_short!("GOAL_MS");
const GOAL_COMPLETED: Symbol = symbol_short!("GOAL_CMP");
const GOAL_EXPIRED: Symbol = symbol_short!("GOAL_EXP");
const GOAL_ABANDONED: Symbol = symbol_short!("GOAL_AB");

/// Percentage thresholds attested as the goal progresses. Index into this
/// array is the bit position set in [`Goal::milestones`].
pub const MILESTONE_THRESHOLDS_PCT: [u32; 4] = [25, 50, 75, 100];

/// Bound on distinct contributors tracked per goal. `contribute` is O(1) and
/// never iterates this list; only settlement/enumeration paths do.
pub const MAX_CONTRIBUTORS_PER_GOAL: u32 = 25;

// ---------------------------------------------------------------------------
// Public types
// ---------------------------------------------------------------------------

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
    /// Bit `i` set means `MILESTONE_THRESHOLDS_PCT[i]` has been attested.
    pub milestones: u32,
    pub status: GoalStatus,
    pub contributors: Vec<Address>,
    pub created_at: u64,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct GoalCreatedEventData {
    pub owner: Address,
    pub vault: Address,
    pub target_amount: i128,
    pub deadline: u64,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct GoalMilestoneEventData {
    pub threshold_pct: u32,
    pub contributed: i128,
    pub timestamp: u64,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct GoalCompletedEventData {
    pub contributed: i128,
    pub timestamp: u64,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct GoalExpiredEventData {
    pub contributed: i128,
    pub timestamp: u64,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct GoalAbandonedEventData {
    pub contributed: i128,
    pub timestamp: u64,
}

// ---------------------------------------------------------------------------
// Storage keys
// ---------------------------------------------------------------------------

#[contracttype]
#[derive(Clone)]
enum DataKey {
    /// goal_id → Goal
    Goal(soroban_sdk::BytesN<32>),
    /// (goal_id, contributor) → cumulative amount contributed by that address.
    ContributorAmount(soroban_sdk::BytesN<32>, Address),
    /// Address of the deployed vault factory, used to validate vaults.
    VaultFactory,
}

// ---------------------------------------------------------------------------
// Contract
// ---------------------------------------------------------------------------

#[contract]
pub struct SavingsGoalContract;

#[contractimpl]
impl SavingsGoalContract {
    /// Initialise the registry, granting `admin` the Admin role and
    /// recording the vault factory used to validate goal vaults.
    pub fn initialize(env: Env, admin: Address, vault_factory: Address) {
        AccessControl::initialize(&env, &admin);
        env.storage()
            .instance()
            .set(&DataKey::VaultFactory, &vault_factory);
    }

    /// Repoint the vault factory used for vault validation. Admin only.
    pub fn set_vault_factory(env: Env, caller: Address, vault_factory: Address) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);
        env.storage()
            .instance()
            .set(&DataKey::VaultFactory, &vault_factory);
    }

    /// Register a new savings goal. `goal_id` is derived off-chain (the
    /// backend's UUID hashed) so the contract never generates identifiers.
    ///
    /// # Panics
    /// * [`ContractError::AlreadyInitialized`] if `goal_id` is already registered.
    /// * [`ContractError::InvalidAmount`] if `target_amount <= 0` or `deadline`
    ///   is not in the future.
    /// * [`ContractError::NotNesterVault`] if `vault` is not a vault recognised
    ///   by the configured vault factory.
    pub fn create_goal(
        env: Env,
        owner: Address,
        goal_id: soroban_sdk::BytesN<32>,
        vault: Address,
        target_amount: i128,
        deadline: u64,
    ) {
        owner.require_auth();

        if env.storage().instance().has(&DataKey::Goal(goal_id.clone())) {
            panic_with_error!(&env, ContractError::AlreadyInitialized);
        }
        if target_amount <= 0 {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }
        if deadline <= env.ledger().timestamp() {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }
        if !is_known_vault(&env, &vault) {
            panic_with_error!(&env, ContractError::NotNesterVault);
        }

        let goal = Goal {
            owner: owner.clone(),
            vault: vault.clone(),
            target_amount,
            deadline,
            contributed: 0,
            milestones: 0,
            status: GoalStatus::Active,
            contributors: Vec::new(&env),
            created_at: env.ledger().timestamp(),
        };
        env.storage()
            .instance()
            .set(&DataKey::Goal(goal_id.clone()), &goal);

        env.events().publish(
            (SAVINGS_GOAL, GOAL_CREATED, goal_id),
            GoalCreatedEventData {
                owner,
                vault,
                target_amount,
                deadline,
            },
        );
    }

    /// Record a contribution earmarked for `goal_id`. Called by the vault
    /// (or a user directly) when a deposit is attributed to a goal.
    ///
    /// Idempotently attests any newly-crossed 25/50/75/100% milestone via
    /// the goal's bitmask, and transitions the goal to `Completed` once
    /// `contributed >= target_amount`.
    ///
    /// # Panics
    /// * [`ContractError::InvalidAmount`] if `amount <= 0`.
    /// * [`ContractError::StrategyNotFound`] if `goal_id` is unknown.
    /// * [`ContractError::InvalidOperation`] if the goal is not `Active`.
    /// * [`ContractError::ExceedsLimit`] if `contributor` is new and the goal
    ///   already tracks [`MAX_CONTRIBUTORS_PER_GOAL`] distinct contributors.
    pub fn contribute(env: Env, contributor: Address, goal_id: soroban_sdk::BytesN<32>, amount: i128) {
        contributor.require_auth();

        if amount <= 0 {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }

        let mut goal = get_goal_or_panic(&env, &goal_id);
        if !matches!(goal.status, GoalStatus::Active) {
            panic_with_error!(&env, ContractError::InvalidOperation);
        }

        record_contribution(&env, &goal_id, &mut goal, &contributor, amount);

        goal.contributed += amount;

        attest_new_milestones(&env, &goal_id, &mut goal);

        if goal.contributed >= goal.target_amount {
            complete_goal(&env, &goal_id, &mut goal);
        }

        save_goal(&env, &goal_id, &goal);
    }

    /// Permissionlessly finalise a goal once its target has been reached.
    /// Lets any party (not just the contributor whose deposit tipped it
    /// over) settle a goal that `contribute` hasn't already completed.
    ///
    /// # Panics
    /// * [`ContractError::StrategyNotFound`] if `goal_id` is unknown.
    /// * [`ContractError::InvalidOperation`] if the goal is not `Active` or
    ///   has not yet reached its target.
    pub fn finalize_goal(env: Env, goal_id: soroban_sdk::BytesN<32>) {
        let mut goal = get_goal_or_panic(&env, &goal_id);
        if !matches!(goal.status, GoalStatus::Active) {
            panic_with_error!(&env, ContractError::InvalidOperation);
        }
        if goal.contributed < goal.target_amount {
            panic_with_error!(&env, ContractError::InvalidOperation);
        }
        complete_goal(&env, &goal_id, &mut goal);
        save_goal(&env, &goal_id, &goal);
    }

    /// Permissionlessly expire a goal whose deadline has passed without
    /// completion, so the backend can drive deadline notifications off
    /// chain state instead of guessing with a cron.
    ///
    /// # Panics
    /// * [`ContractError::StrategyNotFound`] if `goal_id` is unknown.
    /// * [`ContractError::InvalidOperation`] if the goal is not `Active` or
    ///   the deadline has not yet passed.
    pub fn expire_goal(env: Env, goal_id: soroban_sdk::BytesN<32>) {
        let mut goal = get_goal_or_panic(&env, &goal_id);
        if !matches!(goal.status, GoalStatus::Active) {
            panic_with_error!(&env, ContractError::InvalidOperation);
        }
        if env.ledger().timestamp() < goal.deadline {
            panic_with_error!(&env, ContractError::InvalidOperation);
        }

        goal.status = GoalStatus::Expired;
        save_goal(&env, &goal_id, &goal);

        env.events().publish(
            (SAVINGS_GOAL, GOAL_EXPIRED, goal_id),
            GoalExpiredEventData {
                contributed: goal.contributed,
                timestamp: env.ledger().timestamp(),
            },
        );
    }

    /// Abandon a goal. Owner only.
    ///
    /// # Panics
    /// * [`ContractError::StrategyNotFound`] if `goal_id` is unknown.
    /// * [`ContractError::Unauthorized`] if `caller` is not the goal owner.
    /// * [`ContractError::InvalidOperation`] if the goal is not `Active`.
    pub fn abandon_goal(env: Env, caller: Address, goal_id: soroban_sdk::BytesN<32>) {
        caller.require_auth();

        let mut goal = get_goal_or_panic(&env, &goal_id);
        if goal.owner != caller {
            panic_with_error!(&env, ContractError::Unauthorized);
        }
        if !matches!(goal.status, GoalStatus::Active) {
            panic_with_error!(&env, ContractError::InvalidOperation);
        }

        goal.status = GoalStatus::Abandoned;
        save_goal(&env, &goal_id, &goal);

        env.events().publish(
            (SAVINGS_GOAL, GOAL_ABANDONED, goal_id),
            GoalAbandonedEventData {
                contributed: goal.contributed,
                timestamp: env.ledger().timestamp(),
            },
        );
    }

    // -----------------------------------------------------------------------
    // Queries
    // -----------------------------------------------------------------------

    pub fn get_goal(env: Env, goal_id: soroban_sdk::BytesN<32>) -> Goal {
        get_goal_or_panic(&env, &goal_id)
    }

    pub fn has_goal(env: Env, goal_id: soroban_sdk::BytesN<32>) -> bool {
        env.storage().instance().has(&DataKey::Goal(goal_id))
    }

    pub fn get_goal_status(env: Env, goal_id: soroban_sdk::BytesN<32>) -> GoalStatus {
        get_goal_or_panic(&env, &goal_id).status
    }

    /// Cumulative amount contributed by `contributor` to `goal_id` (0 if
    /// they have never contributed).
    pub fn get_contributor_amount(
        env: Env,
        goal_id: soroban_sdk::BytesN<32>,
        contributor: Address,
    ) -> i128 {
        env.storage()
            .instance()
            .get(&DataKey::ContributorAmount(goal_id, contributor))
            .unwrap_or(0)
    }

    /// All distinct addresses that have contributed to `goal_id`, for
    /// fair settlement if the goal is abandoned or expired.
    pub fn get_contributors(env: Env, goal_id: soroban_sdk::BytesN<32>) -> Vec<Address> {
        get_goal_or_panic(&env, &goal_id).contributors
    }

    // -----------------------------------------------------------------------
    // Role management — delegates to nester_access_control
    // -----------------------------------------------------------------------

    pub fn grant_role(env: Env, grantor: Address, grantee: Address, role: Role) {
        AccessControl::grant_role(&env, &grantor, &grantee, role);
    }

    pub fn revoke_role(env: Env, revoker: Address, target: Address, role: Role) {
        AccessControl::revoke_role(&env, &revoker, &target, role);
    }

    pub fn transfer_admin(env: Env, current_admin: Address, new_admin: Address) {
        AccessControl::transfer_admin(&env, &current_admin, &new_admin);
    }

    pub fn accept_admin(env: Env, new_admin: Address) {
        AccessControl::accept_admin(&env, &new_admin);
    }
}

// ---------------------------------------------------------------------------
// Private helpers
// ---------------------------------------------------------------------------

fn get_goal_or_panic(env: &Env, goal_id: &soroban_sdk::BytesN<32>) -> Goal {
    env.storage()
        .instance()
        .get::<DataKey, Goal>(&DataKey::Goal(goal_id.clone()))
        .unwrap_or_else(|| panic_with_error!(env, ContractError::StrategyNotFound))
}

fn save_goal(env: &Env, goal_id: &soroban_sdk::BytesN<32>, goal: &Goal) {
    env.storage()
        .instance()
        .set(&DataKey::Goal(goal_id.clone()), goal);
}

/// Update per-contributor accounting for `contributor`, adding them to the
/// goal's bounded contributor list the first time they contribute.
fn record_contribution(
    env: &Env,
    goal_id: &soroban_sdk::BytesN<32>,
    goal: &mut Goal,
    contributor: &Address,
    amount: i128,
) {
    let key = DataKey::ContributorAmount(goal_id.clone(), contributor.clone());
    let prev: i128 = env.storage().instance().get(&key).unwrap_or(0);

    if prev == 0 {
        if goal.contributors.len() >= MAX_CONTRIBUTORS_PER_GOAL {
            panic_with_error!(env, ContractError::ExceedsLimit);
        }
        goal.contributors.push_back(contributor.clone());
    }

    env.storage().instance().set(&key, &(prev + amount));
}

/// Set the bit for any milestone threshold newly crossed by `goal.contributed`
/// and emit `goal_milestone_reached` for each. A bit can only transition from
/// unset to set once, so this is idempotent across retried/duplicate calls.
fn attest_new_milestones(env: &Env, goal_id: &soroban_sdk::BytesN<32>, goal: &mut Goal) {
    for (bit, threshold_pct) in MILESTONE_THRESHOLDS_PCT.iter().enumerate() {
        let mask = 1u32 << bit;
        if goal.milestones & mask != 0 {
            continue;
        }
        // contributed / target >= threshold / 100, cross-multiplied to avoid
        // floating point: contributed * 100 >= threshold * target.
        if goal.contributed.saturating_mul(100) >= (*threshold_pct as i128) * goal.target_amount {
            goal.milestones |= mask;
            env.events().publish(
                (SAVINGS_GOAL, GOAL_MILESTONE, goal_id.clone()),
                GoalMilestoneEventData {
                    threshold_pct: *threshold_pct,
                    contributed: goal.contributed,
                    timestamp: env.ledger().timestamp(),
                },
            );
        }
    }
}

fn complete_goal(env: &Env, goal_id: &soroban_sdk::BytesN<32>, goal: &mut Goal) {
    goal.status = GoalStatus::Completed;
    env.events().publish(
        (SAVINGS_GOAL, GOAL_COMPLETED, goal_id.clone()),
        GoalCompletedEventData {
            contributed: goal.contributed,
            timestamp: env.ledger().timestamp(),
        },
    );
}

/// Cross-call the configured vault factory's `is_nester_vault` to confirm
/// `vault` was actually deployed through it, rather than trusting an
/// arbitrary caller-supplied address.
fn is_known_vault(env: &Env, vault: &Address) -> bool {
    let factory: Address = env
        .storage()
        .instance()
        .get(&DataKey::VaultFactory)
        .unwrap_or_else(|| panic_with_error!(env, ContractError::NotInitialized));

    env.invoke_contract(
        &factory,
        &Symbol::new(env, "is_nester_vault"),
        soroban_sdk::vec![env, vault.into_val(env)],
    )
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod test;
