//! Goal-completion bonus contract (issue #819).
//!
//! Pays a bounded bonus when a savings goal legitimately reaches its target
//! on time, funded from a dedicated, capped pool that never draws on user
//! funds, with anti-gaming rules so the bonus rewards sustained saving
//! rather than a last-minute deposit-and-claim.
//!
//! # The contribution-timeline gap, and how this is built around it
//! The issue asks for an effort-weighted bonus computed "from the
//! contribution timeline the goal registry already records" — but on
//! inspection, `savings_goal`'s registry only stored a *cumulative total*
//! per contributor, not a timestamped history, so no timeline existed to
//! read.
//!
//! Rather than approximate around that gap, `savings_goal` itself was
//! extended with `ContributionStats`: a fixed-size, exact summary
//! (contribution count, distinct 7-day periods touched, largest single
//! contribution, and the exact time-integral of the contributed balance)
//! maintained incrementally on every `contribute()` call — see that
//! contract's own doc comment on `ContributionStats` for why an unbounded
//! per-contribution list was rejected in favour of this. `goal_effort::fold_timeline`
//! (in `nester_common`) independently proves this incremental summary is
//! mathematically identical to replaying an explicit contribution list.
//!
//! # Payout model
//! `claim_goal_bonus` pays a **direct token transfer to the goal owner**,
//! not a vault deposit — simpler, and it does not entangle this contract
//! with the vault's share-price/fee-tier accounting.
//!
//! # Anti-gaming, beyond the issue's literal eligibility rules
//! The registry itself only records numbers; it never moves tokens, and any
//! signed-in address can call `contribute`. To stop a goal being backed by
//! fabricated contributions, the bonus **basis** (the amount the tier rate
//! applies to) is `min(target_amount, contributed, the owner's real vault
//! balance)` — fake numbers with no real money behind them earn nothing. A
//! second gap this closes: one real balance backing several concurrent
//! "goals" to farm multiple bonuses — an owner's bonus-earning goals for the
//! same vault must not overlap in time (checked at claim, not at goal
//! creation, since the registry has no goal-count cap and the contract has
//! no view into unrelated goals until a claim is attempted). The
//! trade-off, documented rather than hidden: a user with two genuine
//! concurrent goals against the same vault only ever gets one bonus.
//!
//! What this contract still cannot close on its own: a user could record a
//! few small real contributions early (clearing the anti-gaming rule) and
//! then deposit real money into the vault only just before claiming,
//! inflating the effort score computed from `contributed`'s registered
//! timeline versus their *vault* balance's actual history. Closing that
//! fully means the registry should only accept contributions from the vault
//! itself (or another trusted caller), which changes `savings_goal`'s
//! caller model — left as a separate decision, not attempted here.

#![no_std]

use soroban_sdk::{
    contract, contracterror, contractimpl, contracttype, panic_with_error, symbol_short, token,
    vec, Address, BytesN, Env, IntoVal, Symbol, Val, Vec,
};

use nester_access_control::{AccessControl, Role};
use nester_common::goal_effort::{
    self, check_eligibility, compute_bonus, effort_score_bps, ContributionStatsInput,
    IneligibilityReason, RewardTier,
};
use savings_goal::{GoalStatus, SavingsGoalContractClient};

// ---------------------------------------------------------------------------
// Errors — a SEPARATE enum from nester_common::ContractError, which is
// already at Soroban's 50-discriminant #[contracterror] ceiling (see
// libs/common/src/errors.rs). lp_aggregator's AggregatorError is the
// existing precedent for a contract-local error enum in this workspace.
// ---------------------------------------------------------------------------

#[contracterror]
#[derive(Copy, Clone, Debug, Eq, PartialEq)]
#[repr(u32)]
pub enum GoalRewardError {
    GoalNotCompleted = 1,
    GoalExpired = 2,
    BonusAlreadyClaimed = 3,
    IneligibleContributionPattern = 4,
    RewardPoolEmpty = 5,
    GoalDurationTooShort = 6,
    RewardTierNotConfigured = 7,
    GoalBonusOverlap = 8,
    GoalNotFunded = 9,
    Unauthorized = 10,
    InvalidConfig = 11,
    NotInitialized = 12,
    AlreadyInitialized = 13,
}

impl From<IneligibilityReason> for GoalRewardError {
    fn from(reason: IneligibilityReason) -> Self {
        match reason {
            IneligibilityReason::GoalNotCompleted => GoalRewardError::GoalNotCompleted,
            IneligibilityReason::CompletedAfterDeadline => GoalRewardError::GoalExpired,
            IneligibilityReason::ActiveDurationTooShort => GoalRewardError::GoalDurationTooShort,
            IneligibilityReason::ContributionPatternFailsAntiGaming => {
                GoalRewardError::IneligibleContributionPattern
            }
        }
    }
}

// ---------------------------------------------------------------------------
// Compile-time ceilings/floors (issue requirement: "a compile-time ceiling
// so no configuration can promise an unsustainable bonus").
// ---------------------------------------------------------------------------

/// Absolute maximum bonus rate any tier may be configured with: 5% of a
/// goal's basis. `configure_reward` panics if asked to exceed this.
pub const MAX_BONUS_BPS: u32 = 500;
/// Absolute maximum flat cap any tier's `max_bonus_absolute` may specify, in
/// token base units (10^7 = 1000 units at the network's standard 7 decimals).
pub const MAX_BONUS_ABSOLUTE_CEILING: i128 = 1_000 * 10_000_000;
/// Hard cap on the number of configured tiers, so `select_tier`'s linear
/// scan (and the storage vector `configure_reward` writes) stays bounded.
pub const MAX_REWARD_TIERS: u32 = 4;
/// Floor `min_active_duration_seconds` may not be configured below: a goal
/// created and completed with less than this much elapsed time earns
/// nothing, by construction, regardless of admin configuration.
pub const MIN_ACTIVE_DURATION_FLOOR_SECONDS: u64 = 7 * 24 * 60 * 60;
/// Ceiling `single_contribution_ceiling_bps` may not be configured above.
pub const MAX_SINGLE_CONTRIBUTION_CEILING_BPS: u32 = 5_000;
/// Floor `min_distinct_periods` may not be configured below.
pub const MIN_DISTINCT_PERIODS_FLOOR: u32 = 2;

// ---------------------------------------------------------------------------
// Storage
// ---------------------------------------------------------------------------

#[contracttype]
#[derive(Clone)]
enum DataKey {
    GoalRegistry,
    Token,
    RewardPoolBalance,
    Tiers,
    MinActiveDurationSeconds,
    SingleContributionCeilingBps,
    MinDistinctPeriods,
    /// goal_id -> true once claimed. Also the record checked for the churn
    /// test (complete -> claim -> abandon -> re-create -> claim): a
    /// re-created goal gets a fresh goal_id from the registry, so this key
    /// can never collide with the original claim.
    Claimed(BytesN<32>),
    /// (vault, owner) -> the goal_id of that owner's currently
    /// bonus-eligible-or-claimed goal against that vault, if any — the
    /// overlap guard. Cleared implicitly by being overwritten on the next
    /// claim; not cleared on an unclaimed goal's abandonment, since only a
    /// *claimed* goal should block a future one (an abandoned, never-claimed
    /// goal must not permanently lock out its owner from ever claiming
    /// against that vault again).
    ActiveClaimWindow(Address, Address),
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct ClaimRecord {
    pub goal_id: BytesN<32>,
    pub owner: Address,
    pub bonus: i128,
    pub basis: i128,
    pub effort_bps: u32,
    pub claimed_at: u64,
}

// ---------------------------------------------------------------------------
// Events
// ---------------------------------------------------------------------------

const GOAL_RWD: Symbol = symbol_short!("GOAL_RWD");
const GR_CLAIM: Symbol = symbol_short!("GR_CLAIM");
const GR_EMPTY: Symbol = symbol_short!("GR_EMPTY");
const GR_FUND: Symbol = symbol_short!("GR_FUND");
const GR_DEFUND: Symbol = symbol_short!("GR_DEFUND");
const GR_CFG: Symbol = symbol_short!("GR_CFG");
const GR_RULES: Symbol = symbol_short!("GR_RULES");

#[contract]
pub struct GoalRewardsContract;

#[contractimpl]
impl GoalRewardsContract {
    /// `goal_registry` is the `savings_goal` contract this reads goal state
    /// from. `token` is both the pool's currency and the vault's asset
    /// (checked at claim time against the goal's own vault — see
    /// `claim_goal_bonus`'s basis calculation).
    pub fn initialize(env: Env, admin: Address, goal_registry: Address, token: Address) {
        if env.storage().instance().has(&DataKey::GoalRegistry) {
            panic_with_error!(&env, GoalRewardError::AlreadyInitialized);
        }
        AccessControl::initialize(&env, &admin);
        env.storage()
            .instance()
            .set(&DataKey::GoalRegistry, &goal_registry);
        env.storage().instance().set(&DataKey::Token, &token);
        env.storage()
            .instance()
            .set(&DataKey::RewardPoolBalance, &0i128);
        env.storage()
            .instance()
            .set(&DataKey::MinActiveDurationSeconds, &(30 * 24 * 60 * 60u64));
        env.storage()
            .instance()
            .set(&DataKey::SingleContributionCeilingBps, &4_000u32);
        env.storage()
            .instance()
            .set(&DataKey::MinDistinctPeriods, &4u32);
        env.storage()
            .instance()
            .set(&DataKey::Tiers, &Vec::<RewardTierStored>::new(&env));
    }

    // -----------------------------------------------------------------
    // Admin configuration
    // -----------------------------------------------------------------

    /// Set (or replace) the reward tier for goals with `target_amount >=
    /// min_target`. Panics if `bonus_bps` or `max_bonus_absolute` exceed
    /// the compile-time ceilings, or if this would exceed
    /// [`MAX_REWARD_TIERS`] distinct tiers.
    pub fn configure_reward(
        env: Env,
        caller: Address,
        min_target: i128,
        bonus_bps: u32,
        max_bonus_absolute: i128,
    ) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);

        if bonus_bps > MAX_BONUS_BPS
            || max_bonus_absolute > MAX_BONUS_ABSOLUTE_CEILING
            || max_bonus_absolute < 0
            || min_target < 0
        {
            panic_with_error!(&env, GoalRewardError::InvalidConfig);
        }

        let mut tiers: Vec<RewardTierStored> = env
            .storage()
            .instance()
            .get(&DataKey::Tiers)
            .unwrap_or(Vec::new(&env));

        if let Some(idx) = tiers.iter().position(|t| t.min_target == min_target) {
            tiers.set(
                idx as u32,
                RewardTierStored {
                    min_target,
                    bonus_bps,
                    max_bonus_absolute,
                },
            );
        } else {
            if tiers.len() >= MAX_REWARD_TIERS {
                panic_with_error!(&env, GoalRewardError::InvalidConfig);
            }
            tiers.push_back(RewardTierStored {
                min_target,
                bonus_bps,
                max_bonus_absolute,
            });
        }

        env.storage().instance().set(&DataKey::Tiers, &tiers);
        env.events().publish(
            (GOAL_RWD, GR_CFG),
            (min_target, bonus_bps, max_bonus_absolute),
        );
    }

    /// Configure the anti-gaming eligibility rules. Every value is clamped
    /// to its compile-time floor/ceiling server-side (not just validated) —
    /// see the constants above — so a misconfiguration can only ever make
    /// eligibility *stricter* than the floor, never looser.
    pub fn set_eligibility_rules(
        env: Env,
        caller: Address,
        min_active_duration_seconds: u64,
        single_ceiling_bps: u32,
        min_distinct_periods: u32,
    ) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);

        let min_active_duration_seconds =
            min_active_duration_seconds.max(MIN_ACTIVE_DURATION_FLOOR_SECONDS);
        let single_ceiling_bps = single_ceiling_bps.min(MAX_SINGLE_CONTRIBUTION_CEILING_BPS);
        let min_distinct_periods = min_distinct_periods.max(MIN_DISTINCT_PERIODS_FLOOR);

        env.storage().instance().set(
            &DataKey::MinActiveDurationSeconds,
            &min_active_duration_seconds,
        );
        env.storage()
            .instance()
            .set(&DataKey::SingleContributionCeilingBps, &single_ceiling_bps);
        env.storage()
            .instance()
            .set(&DataKey::MinDistinctPeriods, &min_distinct_periods);

        env.events().publish(
            (GOAL_RWD, GR_RULES),
            (
                min_active_duration_seconds,
                single_ceiling_bps,
                min_distinct_periods,
            ),
        );
    }

    // -----------------------------------------------------------------
    // Pool funding
    // -----------------------------------------------------------------

    /// Pull `amount` of the pool's token from `caller` into this contract
    /// and credit the pool balance. Admin or Treasurer — matches
    /// `treasury.withdraw`'s access model for a fund-moving action.
    pub fn fund_pool(env: Env, caller: Address, amount: i128) {
        caller.require_auth();
        require_admin_or_treasurer(&env, &caller);

        if amount <= 0 {
            panic_with_error!(&env, GoalRewardError::InvalidConfig);
        }

        let token: Address = env.storage().instance().get(&DataKey::Token).unwrap();
        token::Client::new(&env, &token).transfer(
            &caller,
            &env.current_contract_address(),
            &amount,
        );

        let balance: i128 = env
            .storage()
            .instance()
            .get(&DataKey::RewardPoolBalance)
            .unwrap_or(0);
        env.storage()
            .instance()
            .set(&DataKey::RewardPoolBalance, &(balance + amount));

        env.events().publish((GOAL_RWD, GR_FUND), amount);
    }

    /// Credit the pool balance for tokens already pushed to this contract's
    /// address by an external `transfer` (e.g. `treasury.withdraw(caller,
    /// this_contract, token, amount)`, which moves tokens without calling
    /// back into this contract). Only credits up to the contract's actual
    /// token balance minus what is already credited, so this can never
    /// over-credit the pool beyond what is really held.
    pub fn sync_pool(env: Env, caller: Address) -> i128 {
        caller.require_auth();
        require_admin_or_treasurer(&env, &caller);

        let token: Address = env.storage().instance().get(&DataKey::Token).unwrap();
        let held = token::Client::new(&env, &token).balance(&env.current_contract_address());
        let credited: i128 = env
            .storage()
            .instance()
            .get(&DataKey::RewardPoolBalance)
            .unwrap_or(0);
        let uncredited = (held - credited).max(0);
        if uncredited > 0 {
            env.storage()
                .instance()
                .set(&DataKey::RewardPoolBalance, &(credited + uncredited));
            env.events().publish((GOAL_RWD, GR_FUND), uncredited);
        }
        uncredited
    }

    /// Withdraw `amount` out of the pool back to `to`. Admin or Treasurer.
    /// Bounded by the pool's own tracked balance, not the contract's raw
    /// token balance, so this can never remove more than the pool believes
    /// it holds.
    pub fn defund_pool(env: Env, caller: Address, to: Address, amount: i128) {
        caller.require_auth();
        require_admin_or_treasurer(&env, &caller);

        if amount <= 0 {
            panic_with_error!(&env, GoalRewardError::InvalidConfig);
        }
        let balance: i128 = env
            .storage()
            .instance()
            .get(&DataKey::RewardPoolBalance)
            .unwrap_or(0);
        if amount > balance {
            panic_with_error!(&env, GoalRewardError::RewardPoolEmpty);
        }

        env.storage()
            .instance()
            .set(&DataKey::RewardPoolBalance, &(balance - amount));
        let token: Address = env.storage().instance().get(&DataKey::Token).unwrap();
        token::Client::new(&env, &token).transfer(&env.current_contract_address(), &to, &amount);

        env.events().publish((GOAL_RWD, GR_DEFUND), amount);
    }

    // -----------------------------------------------------------------
    // Claim
    // -----------------------------------------------------------------

    /// Claim the completion bonus for `goal_id`. Owner only. A goal can be
    /// claimed exactly once — see [`DataKey::Claimed`].
    ///
    /// # Panics
    /// * [`GoalRewardError::Unauthorized`] if `caller` is not the goal owner.
    /// * [`GoalRewardError::BonusAlreadyClaimed`] if already claimed.
    /// * [`GoalRewardError::GoalBonusOverlap`] if the owner already holds a
    ///   claimed bonus for another goal against the same vault, still within
    ///   this goal's own active window (see the module doc comment).
    /// * [`GoalRewardError::GoalNotCompleted`] / `GoalExpired` /
    ///   `GoalDurationTooShort` / `IneligibleContributionPattern` per
    ///   [`nester_common::goal_effort::check_eligibility`].
    /// * [`GoalRewardError::RewardTierNotConfigured`] if no tier's floor is
    ///   at or below the goal's target.
    /// * [`GoalRewardError::RewardPoolEmpty`] if the pool cannot cover the
    ///   computed bonus (not marked claimed; retryable after a top-up).
    pub fn claim_goal_bonus(env: Env, caller: Address, goal_id: BytesN<32>) -> i128 {
        caller.require_auth();

        if env
            .storage()
            .instance()
            .has(&DataKey::Claimed(goal_id.clone()))
        {
            panic_with_error!(&env, GoalRewardError::BonusAlreadyClaimed);
        }

        let registry: Address = env
            .storage()
            .instance()
            .get(&DataKey::GoalRegistry)
            .unwrap();
        let registry_client = SavingsGoalContractClient::new(&env, &registry);
        let goal = registry_client.get_goal(&goal_id);

        if goal.owner != caller {
            panic_with_error!(&env, GoalRewardError::Unauthorized);
        }

        // Overlap guard: an owner may hold at most one *claimed* bonus at a
        // time per vault. A goal completed while an earlier one against the
        // same vault is still active (not yet claimed, or claimed and this
        // is a genuinely later, non-overlapping goal) is allowed; only a
        // second CLAIM against the same live window is blocked, checked by
        // requiring the previous claim (if any) to be for a goal created
        // before this one's completion — i.e. they must not have been
        // concurrently active.
        let overlap_key = DataKey::ActiveClaimWindow(goal.vault.clone(), caller.clone());
        if let Some(prev_goal_id) = env
            .storage()
            .instance()
            .get::<DataKey, BytesN<32>>(&overlap_key)
        {
            if prev_goal_id != goal_id {
                // Concurrent iff this goal was created before the previous
                // claim's goal had completed — i.e. their active windows
                // overlapped in time.
                let prev_stats = registry_client.get_contribution_stats(&prev_goal_id);
                if goal.created_at < prev_stats.completed_at {
                    panic_with_error!(&env, GoalRewardError::GoalBonusOverlap);
                }
            }
        }

        let stats = registry_client.get_contribution_stats(&goal_id);
        let min_active_duration: u64 = env
            .storage()
            .instance()
            .get(&DataKey::MinActiveDurationSeconds)
            .unwrap();
        let single_ceiling_bps: u32 = env
            .storage()
            .instance()
            .get(&DataKey::SingleContributionCeilingBps)
            .unwrap();
        let min_periods: u32 = env
            .storage()
            .instance()
            .get(&DataKey::MinDistinctPeriods)
            .unwrap();

        check_eligibility(
            matches!(goal.status, GoalStatus::Completed),
            stats.completed_at,
            goal.deadline,
            goal.created_at,
            min_active_duration,
            to_pure_stats(&stats),
            goal.target_amount,
            single_ceiling_bps,
            min_periods,
        )
        .map_err(GoalRewardError::from)
        .unwrap_or_else(|e| panic_with_error!(&env, e));

        let tiers = load_tiers(&env);
        let tier = select_tier(&tiers, goal.target_amount)
            .unwrap_or_else(|| panic_with_error!(&env, GoalRewardError::RewardTierNotConfigured));

        // Anti-fake-contribution basis: the smallest of the target, the
        // registry's recorded total, and the owner's real vault balance —
        // see the module doc comment.
        let vault_balance = read_vault_balance(&env, &goal.vault, &caller);
        let basis = goal
            .target_amount
            .min(goal.contributed)
            .min(vault_balance)
            .max(0);

        let effort_bps = effort_score_bps(stats.balance_time_integral, goal.target_amount);
        let bonus = compute_bonus(basis, tier, effort_bps);

        let pool_balance: i128 = env
            .storage()
            .instance()
            .get(&DataKey::RewardPoolBalance)
            .unwrap_or(0);
        if bonus > pool_balance {
            if pool_balance == 0 {
                env.events().publish((GOAL_RWD, GR_EMPTY), goal_id.clone());
            }
            panic_with_error!(&env, GoalRewardError::RewardPoolEmpty);
        }

        env.storage()
            .instance()
            .set(&DataKey::Claimed(goal_id.clone()), &true);
        env.storage()
            .instance()
            .set(&DataKey::RewardPoolBalance, &(pool_balance - bonus));
        env.storage().instance().set(&overlap_key, &goal_id);

        if bonus == pool_balance && bonus > 0 {
            env.events().publish((GOAL_RWD, GR_EMPTY), goal_id.clone());
        }

        if bonus > 0 {
            let token: Address = env.storage().instance().get(&DataKey::Token).unwrap();
            token::Client::new(&env, &token).transfer(
                &env.current_contract_address(),
                &caller,
                &bonus,
            );
        }

        env.events().publish(
            (GOAL_RWD, GR_CLAIM, goal_id.clone()),
            ClaimRecord {
                goal_id,
                owner: caller,
                bonus,
                basis,
                effort_bps,
                claimed_at: env.ledger().timestamp(),
            },
        );

        bonus
    }

    // -----------------------------------------------------------------
    // Views
    // -----------------------------------------------------------------

    /// Preview `goal_id`'s bonus without claiming and without panicking on
    /// an ineligible goal — meant for the goal detail API to show
    /// "claimable: N" or a reason it isn't. Returns `(eligible, bonus,
    /// reason_code)`: `reason_code` is 0 when eligible, otherwise the
    /// [`GoalRewardError`] discriminant that `claim_goal_bonus` would panic
    /// with.
    pub fn preview_goal_bonus(env: Env, goal_id: BytesN<32>) -> (bool, i128, u32) {
        let registry: Address = env
            .storage()
            .instance()
            .get(&DataKey::GoalRegistry)
            .unwrap();
        let registry_client = SavingsGoalContractClient::new(&env, &registry);
        let goal = registry_client.get_goal(&goal_id);

        if env
            .storage()
            .instance()
            .has(&DataKey::Claimed(goal_id.clone()))
        {
            return (false, 0, GoalRewardError::BonusAlreadyClaimed as u32);
        }

        let stats = registry_client.get_contribution_stats(&goal_id);
        let min_active_duration: u64 = env
            .storage()
            .instance()
            .get(&DataKey::MinActiveDurationSeconds)
            .unwrap_or(MIN_ACTIVE_DURATION_FLOOR_SECONDS);
        let single_ceiling_bps: u32 = env
            .storage()
            .instance()
            .get(&DataKey::SingleContributionCeilingBps)
            .unwrap_or(0);
        let min_periods: u32 = env
            .storage()
            .instance()
            .get(&DataKey::MinDistinctPeriods)
            .unwrap_or(MIN_DISTINCT_PERIODS_FLOOR);

        if let Err(reason) = check_eligibility(
            matches!(goal.status, GoalStatus::Completed),
            stats.completed_at,
            goal.deadline,
            goal.created_at,
            min_active_duration,
            to_pure_stats(&stats),
            goal.target_amount,
            single_ceiling_bps,
            min_periods,
        ) {
            return (false, 0, GoalRewardError::from(reason) as u32);
        }

        let tiers = load_tiers(&env);
        let tier = match select_tier(&tiers, goal.target_amount) {
            Some(t) => t,
            None => return (false, 0, GoalRewardError::RewardTierNotConfigured as u32),
        };

        let vault_balance = read_vault_balance(&env, &goal.vault, &goal.owner);
        let basis = goal
            .target_amount
            .min(goal.contributed)
            .min(vault_balance)
            .max(0);
        let effort_bps = effort_score_bps(stats.balance_time_integral, goal.target_amount);
        let bonus = compute_bonus(basis, tier, effort_bps);

        let pool_balance: i128 = env
            .storage()
            .instance()
            .get(&DataKey::RewardPoolBalance)
            .unwrap_or(0);
        if bonus > pool_balance {
            return (false, bonus, GoalRewardError::RewardPoolEmpty as u32);
        }

        (true, bonus, 0)
    }

    pub fn get_reward_pool_balance(env: Env) -> i128 {
        env.storage()
            .instance()
            .get(&DataKey::RewardPoolBalance)
            .unwrap_or(0)
    }

    pub fn get_reward_tiers(env: Env) -> Vec<RewardTierStored> {
        env.storage()
            .instance()
            .get(&DataKey::Tiers)
            .unwrap_or(Vec::new(&env))
    }

    pub fn is_claimed(env: Env, goal_id: BytesN<32>) -> bool {
        env.storage().instance().has(&DataKey::Claimed(goal_id))
    }

    // -----------------------------------------------------------------
    // Role management — delegates to nester_access_control
    // -----------------------------------------------------------------

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
// Storage-friendly tier representation (RewardTier isn't a #[contracttype])
// ---------------------------------------------------------------------------

#[contracttype]
#[derive(Clone, Debug)]
pub struct RewardTierStored {
    pub min_target: i128,
    pub bonus_bps: u32,
    pub max_bonus_absolute: i128,
}

fn load_tiers(env: &Env) -> Vec<RewardTierStored> {
    env.storage()
        .instance()
        .get(&DataKey::Tiers)
        .unwrap_or(Vec::new(env))
}

// nester_common::goal_effort::select_tier operates on &[RewardTier], not the
// Soroban Vec<RewardTierStored> this contract persists — bridge the two with
// a small fixed-size stack buffer (bounded by MAX_REWARD_TIERS) rather than
// pulling in an allocator.
impl core::ops::Deref for TierBuf {
    type Target = [RewardTier];
    fn deref(&self) -> &[RewardTier] {
        &self.buf[..self.len]
    }
}
struct TierBuf {
    buf: [RewardTier; MAX_REWARD_TIERS as usize],
    len: usize,
}

// select_tier takes &[RewardTier]; Vec<RewardTierStored> -> TierBuf -> deref.
impl From<Vec<RewardTierStored>> for TierBuf {
    fn from(v: Vec<RewardTierStored>) -> Self {
        let mut buf = [RewardTier {
            min_target: 0,
            bonus_bps: 0,
            max_bonus_absolute: 0,
        }; MAX_REWARD_TIERS as usize];
        let mut len = 0;
        for t in v.iter() {
            if len >= buf.len() {
                break;
            }
            buf[len] = RewardTier {
                min_target: t.min_target,
                bonus_bps: t.bonus_bps,
                max_bonus_absolute: t.max_bonus_absolute,
            };
            len += 1;
        }
        TierBuf { buf, len }
    }
}

fn to_pure_stats(stats: &savings_goal::ContributionStats) -> ContributionStatsInput {
    ContributionStatsInput {
        contribution_count: stats.contribution_count,
        distinct_periods: stats.distinct_periods,
        largest_contribution: stats.largest_contribution,
        balance_time_integral: stats.balance_time_integral,
        completed_at: stats.completed_at,
    }
}

fn require_admin_or_treasurer(env: &Env, caller: &Address) {
    if !AccessControl::has_role(env, caller, Role::Admin)
        && !AccessControl::has_role(env, caller, Role::Treasurer)
    {
        panic_with_error!(env, GoalRewardError::Unauthorized);
    }
}

/// Read `owner`'s live balance from `vault` via a dynamic cross-contract
/// call to its `get_balance` view — the same anti-fake-contribution basis
/// input the module doc comment describes. Returns 0 rather than panicking
/// if the call fails (a vault that doesn't expose this view, or a bad
/// address), which only ever makes the computed basis (and therefore the
/// bonus) smaller, never larger — the safe failure direction.
fn read_vault_balance(env: &Env, vault: &Address, owner: &Address) -> i128 {
    let args: Vec<Val> = vec![env, owner.into_val(env)];
    env.try_invoke_contract::<i128, soroban_sdk::Error>(
        vault,
        &Symbol::new(env, "get_balance"),
        args,
    )
    .ok()
    .and_then(|r| r.ok())
    .unwrap_or(0)
}

fn select_tier(tiers: &Vec<RewardTierStored>, target_amount: i128) -> Option<RewardTier> {
    let buf: TierBuf = tiers.clone().into();
    goal_effort::select_tier(&buf, target_amount)
}

#[cfg(test)]
mod test;
