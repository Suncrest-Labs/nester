//! On-chain referral / deposit-incentive program (issue #818).
//!
//! A standalone contract rather than vault-embedded logic: the vault is
//! already the most safety-critical contract in the system, so growth-program
//! complexity — Sybil gating, budget accounting, reward math — is kept out
//! of its hot path. The trust boundary this creates is explicit: the vault
//! is the only address trusted to call [`ReferralContract::accrue_reward`]
//! (mirroring the existing `treasury.receive_fees` pattern, where the vault
//! is likewise the sole trusted caller), and the vault supplies facts it
//! already tracks (a referred user's principal and first-deposit time) that
//! this contract independently re-checks against the configured minimums
//! before crediting anything — it does not blindly trust a pre-computed
//! eligibility flag.
//!
//! # Reward source
//! Rewards accrue from the protocol's performance-fee slice on a referred
//! user's yield, never from the user's own yield — the vault passes the
//! *fee* amount already destined for the treasury, not the user's net
//! proceeds, so a referrer's earnings can never reduce what the referred
//! user receives.
//!
//! # Anti-Sybil / anti-drain
//! * A referral relationship is permanent, one-time, and forms a forest (one
//!   referrer per user, no self-referral, no direct cycles) — set at
//!   [`ReferralContract::register_referral`].
//! * A referred user must clear a minimum principal and minimum tenure
//!   before any reward accrues, so dust accounts farm nothing.
//! * Both a per-referrer count of *rewarded* distinct referees and a
//!   lifetime reward cap bound any single referrer's take.
//! * A global budget halts new accrual once exhausted, without clawing back
//!   anything already earned.

#![no_std]

use soroban_sdk::{
    contract, contractimpl, contracttype, panic_with_error, symbol_short, token, Address, Env,
    Symbol,
};

use nester_access_control::{AccessControl, Role};
use nester_common::{emit_event, fees::mul_div, ContractError};

const REFERRAL: Symbol = symbol_short!("REFERRAL");
const REF_REG: Symbol = symbol_short!("REF_REG");
const REF_ACCR: Symbol = symbol_short!("REF_ACCR");
const REF_CLAIM: Symbol = symbol_short!("REF_CLAIM");
const REF_BGT_X: Symbol = symbol_short!("REF_BGT_X");

#[contracttype]
#[derive(Clone)]
enum DataKey {
    Vault,
    Token,
    Referrer(Address),
    /// Has this referred user already counted toward its referrer's
    /// rewarded-referral-count cap? Set the first time any reward accrues
    /// for them, regardless of how many times they generate yield after.
    CountedTowardCap(Address),
    ReferredCount(Address),
    RewardBalance(Address),
    LifetimeReward(Address),
    BudgetRemaining,
    ShareBps,
    MinReferredDeposit,
    MinRefereeTenureSeconds,
    MaxRewardedReferrals,
    MaxRewardPerReferrer,
    MinClaim,
}

#[contract]
pub struct ReferralContract;

#[contractimpl]
impl ReferralContract {
    /// `budget` is the total lifetime reward pool this contract will ever
    /// pay out; it must already hold (or later receive) at least that much
    /// of `token` for claims to succeed. Defaults for the share/caps/minimums
    /// come from `nester_common`'s referral constants and can be re-tuned
    /// later via the setters below.
    pub fn initialize(env: Env, admin: Address, vault: Address, token: Address, budget: i128) {
        AccessControl::initialize(&env, &admin);
        env.storage().instance().set(&DataKey::Vault, &vault);
        env.storage().instance().set(&DataKey::Token, &token);
        env.storage()
            .instance()
            .set(&DataKey::BudgetRemaining, &budget);
        env.storage().instance().set(
            &DataKey::ShareBps,
            &nester_common::DEFAULT_REFERRAL_SHARE_BPS,
        );
        env.storage().instance().set(
            &DataKey::MinReferredDeposit,
            &nester_common::DEFAULT_MIN_REFERRED_DEPOSIT,
        );
        env.storage().instance().set(
            &DataKey::MinRefereeTenureSeconds,
            &nester_common::DEFAULT_MIN_REFEREE_TENURE_SECONDS,
        );
        env.storage().instance().set(
            &DataKey::MaxRewardedReferrals,
            &nester_common::DEFAULT_MAX_REWARDED_REFERRALS,
        );
        env.storage().instance().set(
            &DataKey::MaxRewardPerReferrer,
            &nester_common::DEFAULT_MAX_REWARD_PER_REFERRER,
        );
        env.storage().instance().set(
            &DataKey::MinClaim,
            &nester_common::DEFAULT_MIN_REFERRAL_CLAIM,
        );
    }

    /// Bind `referrer` to `user`, permanently. Callable once per user (the
    /// vault/frontend should call this before the user's first deposit —
    /// this contract doesn't itself know about deposits, so it enforces
    /// only the parts of "one-time, pre-first-deposit" that are provable
    /// here: permanence and no self/direct-cycle).
    ///
    /// # Panics
    /// * [`ContractError::SelfReferral`] if `user == referrer`.
    /// * [`ContractError::AlreadyReferred`] if `user` already has a referrer.
    /// * [`ContractError::ReferralCycle`] if `referrer` was themselves
    ///   referred by `user` (a direct 2-cycle). Relationships form a forest —
    ///   each user has at most one referrer — so deeper cycles cannot occur.
    pub fn register_referral(env: Env, user: Address, referrer: Address) {
        user.require_auth();

        if user == referrer {
            panic_with_error!(&env, ContractError::SelfReferral);
        }
        if env
            .storage()
            .instance()
            .has(&DataKey::Referrer(user.clone()))
        {
            panic_with_error!(&env, ContractError::AlreadyReferred);
        }
        if let Some(referrers_referrer) = env
            .storage()
            .instance()
            .get::<DataKey, Address>(&DataKey::Referrer(referrer.clone()))
        {
            if referrers_referrer == user {
                panic_with_error!(&env, ContractError::ReferralCycle);
            }
        }

        env.storage()
            .instance()
            .set(&DataKey::Referrer(user.clone()), &referrer);

        emit_event(&env, REFERRAL, REF_REG, user, referrer);
    }

    /// Called by the vault after it collects a performance fee on a
    /// referred user's harvested yield. Independently re-checks eligibility
    /// (minimum deposit, minimum tenure) rather than trusting the caller's
    /// framing of `principal`/`first_deposit_time` blindly — those are
    /// facts the vault already tracks and simply forwards.
    ///
    /// A no-op (not a panic) whenever the referred user has no referrer,
    /// is not yet eligible, the referrer has exhausted its caps, or the
    /// global budget is exhausted — none of these are the vault's problem
    /// to handle, and a harvest must never fail because of the referral
    /// program.
    ///
    /// # Authorization
    /// Only the registered vault address may call this.
    pub fn accrue_reward(
        env: Env,
        referred_user: Address,
        performance_fee_amount: i128,
        principal: i128,
        first_deposit_time: u64,
    ) {
        let vault: Address = env.storage().instance().get(&DataKey::Vault).unwrap();
        vault.require_auth();

        if performance_fee_amount <= 0 {
            return;
        }

        let referrer: Address = match env
            .storage()
            .instance()
            .get(&DataKey::Referrer(referred_user.clone()))
        {
            Some(r) => r,
            None => return,
        };

        let min_deposit: i128 = env
            .storage()
            .instance()
            .get(&DataKey::MinReferredDeposit)
            .unwrap_or(0);
        if principal < min_deposit {
            return;
        }

        let min_tenure: u64 = env
            .storage()
            .instance()
            .get(&DataKey::MinRefereeTenureSeconds)
            .unwrap_or(0);
        if env.ledger().timestamp().saturating_sub(first_deposit_time) < min_tenure {
            return;
        }

        let budget: i128 = env
            .storage()
            .instance()
            .get(&DataKey::BudgetRemaining)
            .unwrap_or(0);
        if budget <= 0 {
            return;
        }

        let share_bps: u32 = env
            .storage()
            .instance()
            .get(&DataKey::ShareBps)
            .unwrap_or(0);
        let raw_reward = match mul_div(performance_fee_amount, share_bps as i128, 10_000) {
            Ok(v) => v,
            Err(_) => return,
        };
        if raw_reward <= 0 {
            return;
        }

        // Per-referrer distinct-referee count cap — only ever consumed the
        // first time a given referred user earns anything.
        let already_counted = env
            .storage()
            .instance()
            .get(&DataKey::CountedTowardCap(referred_user.clone()))
            .unwrap_or(false);
        if !already_counted {
            let max_referrals: u32 = env
                .storage()
                .instance()
                .get(&DataKey::MaxRewardedReferrals)
                .unwrap_or(0);
            let count: u32 = env
                .storage()
                .instance()
                .get(&DataKey::ReferredCount(referrer.clone()))
                .unwrap_or(0);
            if count >= max_referrals {
                return;
            }
            env.storage()
                .instance()
                .set(&DataKey::ReferredCount(referrer.clone()), &(count + 1));
            env.storage()
                .instance()
                .set(&DataKey::CountedTowardCap(referred_user.clone()), &true);
        }

        // Per-referrer lifetime cap, then global budget — whichever binds
        // tighter wins; never overshoot either.
        let lifetime: i128 = env
            .storage()
            .instance()
            .get(&DataKey::LifetimeReward(referrer.clone()))
            .unwrap_or(0);
        let max_lifetime: i128 = env
            .storage()
            .instance()
            .get(&DataKey::MaxRewardPerReferrer)
            .unwrap_or(0);
        let lifetime_headroom = (max_lifetime - lifetime).max(0);
        let reward = raw_reward.min(lifetime_headroom).min(budget);

        if reward <= 0 {
            return;
        }

        env.storage().instance().set(
            &DataKey::LifetimeReward(referrer.clone()),
            &(lifetime + reward),
        );

        let balance: i128 = env
            .storage()
            .instance()
            .get(&DataKey::RewardBalance(referrer.clone()))
            .unwrap_or(0);
        env.storage().instance().set(
            &DataKey::RewardBalance(referrer.clone()),
            &(balance + reward),
        );

        let new_budget = budget - reward;
        env.storage()
            .instance()
            .set(&DataKey::BudgetRemaining, &new_budget);

        emit_event(
            &env,
            REFERRAL,
            REF_ACCR,
            referrer.clone(),
            (referred_user, reward),
        );

        if new_budget == 0 {
            emit_event(&env, REFERRAL, REF_BGT_X, referrer, new_budget);
        }
    }

    /// Pull-based claim: transfers the referrer's full accrued balance and
    /// zeroes it. Gated by a minimum-claim floor to prevent dust-claim spam.
    ///
    /// # Panics
    /// * [`ContractError::BelowClaimMinimum`] if the accrued balance is below
    ///   the configured minimum.
    pub fn claim_referral_rewards(env: Env, referrer: Address) -> i128 {
        referrer.require_auth();

        let balance: i128 = env
            .storage()
            .instance()
            .get(&DataKey::RewardBalance(referrer.clone()))
            .unwrap_or(0);
        let min_claim: i128 = env
            .storage()
            .instance()
            .get(&DataKey::MinClaim)
            .unwrap_or(0);
        if balance < min_claim {
            panic_with_error!(&env, ContractError::BelowClaimMinimum);
        }

        env.storage()
            .instance()
            .set(&DataKey::RewardBalance(referrer.clone()), &0i128);

        let token: Address = env.storage().instance().get(&DataKey::Token).unwrap();
        token::Client::new(&env, &token).transfer(
            &env.current_contract_address(),
            &referrer,
            &balance,
        );

        emit_event(&env, REFERRAL, REF_CLAIM, referrer, balance);

        balance
    }

    // -----------------------------------------------------------------------
    // Admin configuration
    // -----------------------------------------------------------------------

    pub fn set_share_bps(env: Env, caller: Address, bps: u32) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);
        if bps > nester_common::BASIS_POINT_SCALE {
            panic_with_error!(&env, ContractError::ConfigOutOfRange);
        }
        env.storage().instance().set(&DataKey::ShareBps, &bps);
    }

    pub fn set_eligibility(
        env: Env,
        caller: Address,
        min_referred_deposit: i128,
        min_referee_tenure_seconds: u64,
    ) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);
        env.storage()
            .instance()
            .set(&DataKey::MinReferredDeposit, &min_referred_deposit);
        env.storage().instance().set(
            &DataKey::MinRefereeTenureSeconds,
            &min_referee_tenure_seconds,
        );
    }

    pub fn set_caps(
        env: Env,
        caller: Address,
        max_rewarded_referrals: u32,
        max_reward_per_referrer: i128,
    ) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);
        env.storage()
            .instance()
            .set(&DataKey::MaxRewardedReferrals, &max_rewarded_referrals);
        env.storage()
            .instance()
            .set(&DataKey::MaxRewardPerReferrer, &max_reward_per_referrer);
    }

    pub fn set_min_claim(env: Env, caller: Address, min_claim: i128) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);
        env.storage().instance().set(&DataKey::MinClaim, &min_claim);
    }

    /// Top up the global reward budget. Pulls `amount` of the configured
    /// token from `caller` (typically the treasury) into this contract.
    pub fn fund_budget(env: Env, caller: Address, amount: i128) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);
        if amount <= 0 {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }
        let token: Address = env.storage().instance().get(&DataKey::Token).unwrap();
        token::Client::new(&env, &token).transfer(
            &caller,
            &env.current_contract_address(),
            &amount,
        );

        let budget: i128 = env
            .storage()
            .instance()
            .get(&DataKey::BudgetRemaining)
            .unwrap_or(0);
        env.storage()
            .instance()
            .set(&DataKey::BudgetRemaining, &(budget + amount));
    }

    // -----------------------------------------------------------------------
    // Views
    // -----------------------------------------------------------------------

    pub fn get_referrer(env: Env, user: Address) -> Option<Address> {
        env.storage().instance().get(&DataKey::Referrer(user))
    }

    pub fn get_reward_balance(env: Env, referrer: Address) -> i128 {
        env.storage()
            .instance()
            .get(&DataKey::RewardBalance(referrer))
            .unwrap_or(0)
    }

    pub fn get_lifetime_reward(env: Env, referrer: Address) -> i128 {
        env.storage()
            .instance()
            .get(&DataKey::LifetimeReward(referrer))
            .unwrap_or(0)
    }

    pub fn get_referred_count(env: Env, referrer: Address) -> u32 {
        env.storage()
            .instance()
            .get(&DataKey::ReferredCount(referrer))
            .unwrap_or(0)
    }

    pub fn get_budget_remaining(env: Env) -> i128 {
        env.storage()
            .instance()
            .get(&DataKey::BudgetRemaining)
            .unwrap_or(0)
    }

    // -----------------------------------------------------------------------
    // Role management passthroughs
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

#[cfg(test)]
mod test;
