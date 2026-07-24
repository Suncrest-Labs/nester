use soroban_sdk::{contracttype, symbol_short, Address, Env, Symbol, Vec};

use nester_common::{emit_event, ContractError, MAX_OPEN_LOCKS_PER_USER};

use crate::VAULT;

const LOCK_CREATED: Symbol = symbol_short!("LOCK_CRT");
const LOCK_UNLOCKED: Symbol = symbol_short!("LOCK_ULCK");
const LOCK_BROKEN: Symbol = symbol_short!("LOCK_BRK");

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

/// An admin-configured lock tier defining a permitted lock duration and its
/// associated yield boost multiplier.
#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct LockTier {
    /// Allowed lock duration in seconds (e.g. 30 * 86400 for 30 days).
    pub duration_secs: u64,
    /// Yield boost multiplier in basis points (e.g. 15000 = 1.5×, 20000 = 2×).
    pub boost_multiplier: u32,
}

/// Metadata for a single time-locked deposit position, stored per
/// `(user, lock_id)` pair in persistent storage.
#[contracttype]
#[derive(Clone, Debug)]
pub struct LockedPosition {
    /// Number of vault-token shares locked.
    pub shares: i128,
    /// Timestamp (ledger close time) after which the lock can be unlocked.
    pub unlock_at: u64,
    /// Lock duration in seconds captured at creation time. Used by
    /// `break_lock_early` so that admin tier changes cannot corrupt penalty
    /// math for existing locks.
    pub lock_duration_secs: u64,
    /// Index into the admin-configured lock tiers vector.
    pub tier_index: u32,
    /// Boost multiplier captured at creation time (bps, e.g. 15000 = 1.5×).
    pub boost_multiplier: u32,
}

/// Event data emitted when a locked deposit is created.
#[contracttype]
#[derive(Clone, Debug)]
pub struct LockedDepositEventData {
    /// Underlying token amount deposited (not vault shares).
    pub amount: i128,
    /// Number of vault-token shares minted for this lock.
    pub shares_minted: i128,
    pub lock_id: u32,
    pub unlock_at: u64,
    pub tier_index: u32,
    pub boost_multiplier: u32,
}

/// Event data emitted when a matured lock is unlocked into flexible shares.
#[contracttype]
#[derive(Clone, Debug)]
pub struct LockUnlockedEventData {
    pub lock_id: u32,
    pub shares: i128,
}

/// Event data emitted when a lock is broken early.
#[contracttype]
#[derive(Clone, Debug)]
pub struct LockBrokenEventData {
    pub lock_id: u32,
    pub shares_burned: i128,
    pub assets_returned: i128,
    pub penalty: i128,
}

// ---------------------------------------------------------------------------
// Storage helpers
// ---------------------------------------------------------------------------

pub fn get_lock_tiers(env: &Env) -> Vec<LockTier> {
    env.storage()
        .instance()
        .get(&crate::DataKey::LockTiers)
        .unwrap_or(Vec::new(env))
}

pub fn set_lock_tiers(env: &Env, tiers: &Vec<LockTier>) {
    env.storage().instance().set(&crate::DataKey::LockTiers, tiers);
}

pub fn get_user_lock_count(env: &Env, user: &Address) -> u32 {
    env.storage()
        .persistent()
        .get(&crate::DataKey::UserLockCount(user.clone()))
        .unwrap_or(0)
}

pub fn set_user_lock_count(env: &Env, user: &Address, count: u32) {
    env.storage()
        .persistent()
        .set(&crate::DataKey::UserLockCount(user.clone()), &count);
}

pub fn get_locked_position(env: &Env, user: &Address, lock_id: u32) -> Option<LockedPosition> {
    env.storage()
        .persistent()
        .get(&crate::DataKey::LockedPositions(user.clone(), lock_id))
}

pub fn set_locked_position(env: &Env, user: &Address, lock_id: u32, pos: &LockedPosition) {
    env.storage()
        .persistent()
        .set(&crate::DataKey::LockedPositions(user.clone(), lock_id), pos);
}

pub fn delete_locked_position(env: &Env, user: &Address, lock_id: u32) {
    env.storage()
        .persistent()
        .remove(&crate::DataKey::LockedPositions(user.clone(), lock_id));
}

pub fn get_early_break_penalty_bps(env: &Env) -> u32 {
    env.storage()
        .instance()
        .get(&crate::DataKey::EarlyBreakPenaltyBps)
        .unwrap_or(500) // default 5%
}

pub fn get_treasury_penalty_share_bps(env: &Env) -> u32 {
    env.storage()
        .instance()
        .get(&crate::DataKey::TreasuryPenaltyShareBps)
        .unwrap_or(0) // default: all penalty goes to vault redistribution
}

// ---------------------------------------------------------------------------
// Core lock operations
// ---------------------------------------------------------------------------

/// Validate that `lock_duration_secs` matches one of the configured tiers and
/// return `(tier_index, boost_multiplier)`.
pub fn validate_tier(env: &Env, lock_duration_secs: u64) -> Result<(u32, u32), ContractError> {
    let tiers = get_lock_tiers(env);
    if tiers.is_empty() {
        return Err(ContractError::InvalidLockDuration);
    }
    for i in 0..tiers.len() {
        let tier = tiers.get(i).unwrap();
        if tier.duration_secs == lock_duration_secs {
            return Ok((i, tier.boost_multiplier));
        }
    }
    Err(ContractError::InvalidLockDuration)
}

/// Create a new time-locked position. The caller must have already transferred
/// tokens to the vault and minted shares; this function records the lock
/// metadata and emits the event.
pub fn create_lock(
    env: &Env,
    user: &Address,
    shares: i128,
    token_amount: i128,
    lock_duration_secs: u64,
    now: u64,
) -> Result<u32, ContractError> {
    if shares <= 0 {
        return Err(ContractError::InvalidAmount);
    }

    let (tier_index, boost_multiplier) = validate_tier(env, lock_duration_secs)?;

    // Count actual active (non-deleted) positions for the per-user cap.
    // `lock_count` is a monotonically increasing ID generator and must NOT be
    // used for the cap check — unlocked/broken slots do not free up their ID.
    let lock_count = get_user_lock_count(env, user);
    let active_count = {
        let mut n: u32 = 0;
        for i in 0..lock_count {
            if get_locked_position(env, user, i).is_some() {
                n += 1;
            }
        }
        n
    };
    if active_count >= MAX_OPEN_LOCKS_PER_USER {
        return Err(ContractError::MaxLocksReached);
    }

    let lock_id = lock_count;
    let unlock_at = now
        .checked_add(lock_duration_secs)
        .ok_or(ContractError::ArithmeticOverflow)?;

    let pos = LockedPosition {
        shares,
        unlock_at,
        lock_duration_secs,
        tier_index,
        boost_multiplier,
    };
    set_locked_position(env, user, lock_id, &pos);
    set_user_lock_count(env, user, lock_count + 1);

    emit_event(
        env,
        VAULT,
        LOCK_CREATED,
        user.clone(),
        LockedDepositEventData {
            amount: token_amount,
            shares_minted: shares,
            lock_id,
            unlock_at,
            tier_index,
            boost_multiplier,
        },
    );

    Ok(lock_id)
}

/// Unlock a matured lock: move its shares from locked into the user's flexible
/// balance. Returns the number of shares unlocked.
pub fn unlock_matured(env: &Env, user: &Address, lock_id: u32, now: u64) -> Result<i128, ContractError> {
    let pos = get_locked_position(env, user, lock_id)
        .ok_or(ContractError::LockNotFound)?;

    if now < pos.unlock_at {
        return Err(ContractError::LockNotMatured);
    }

    let shares = pos.shares;
    delete_locked_position(env, user, lock_id);

    emit_event(
        env,
        VAULT,
        LOCK_UNLOCKED,
        user.clone(),
        LockUnlockedEventData {
            lock_id,
            shares,
        },
    );

    Ok(shares)
}

/// Break a lock early: burn the locked shares, return assets minus a
/// time-decaying penalty. The penalty stays in the vault, mechanically
/// lifting share price for remaining depositors.
///
/// This function must NOT be called on a matured lock — use `unlock_matured`
/// instead. If the lock has already matured, this function returns
/// `LockAlreadyMatured` to prevent bypassing the normal unlock flow.
pub fn break_lock_early(
    env: &Env,
    user: &Address,
    lock_id: u32,
    now: u64,
    gross_assets: i128,
) -> Result<(i128, i128), ContractError> {
    let pos = get_locked_position(env, user, lock_id)
        .ok_or(ContractError::LockNotFound)?;

    // Prevent breaking a lock that has already matured — the user should call
    // `unlock_position` instead.
    if now >= pos.unlock_at {
        return Err(ContractError::LockAlreadyMatured);
    }

    // Use the duration captured at creation time (not the current tier config)
    // so that admin tier changes cannot corrupt penalty math for existing locks.
    let total_term = pos.lock_duration_secs;
    let created_at = pos.unlock_at.saturating_sub(total_term);
    let elapsed_secs = now.saturating_sub(created_at);

    let penalty_bps = get_early_break_penalty_bps(env);
    let penalty = nester_common::fees::calculate_early_break_penalty(
        gross_assets,
        penalty_bps,
        elapsed_secs,
        total_term,
    )?;

    let assets_after_penalty = gross_assets - penalty;
    delete_locked_position(env, user, lock_id);

    emit_event(
        env,
        VAULT,
        LOCK_BROKEN,
        user.clone(),
        LockBrokenEventData {
            lock_id,
            shares_burned: pos.shares,
            assets_returned: assets_after_penalty,
            penalty,
        },
    );

    Ok((pos.shares, assets_after_penalty))
}

/// Get all locked positions for a user. Returns a Vec of `(lock_id, LockedPosition)`.
pub fn get_user_locked_positions(env: &Env, user: &Address) -> Vec<(u32, LockedPosition)> {
    let count = get_user_lock_count(env, user);
    let mut result = Vec::new(env);
    for i in 0..count {
        if let Some(pos) = get_locked_position(env, user, i) {
            result.push_back((i, pos));
        }
    }
    result
}

/// Calculate the total boost-weighted shares across all locked positions for a
/// user. Used by `harvest` to split yield between flexible and locked pools.
///
/// Returns `(flexible_shares, total_locked_weighted_shares)`.
pub fn compute_weighted_shares(
    env: &Env,
    user: &Address,
    flexible_shares: i128,
) -> Result<(i128, i128), ContractError> {
    let locked = get_user_locked_positions(env, user);
    let mut total_locked_weighted: i128 = 0;
    for (_, pos) in locked.iter() {
        let weighted = nester_common::fees::mul_div(
            pos.shares,
            pos.boost_multiplier as i128,
            10_000,
        )?;
        total_locked_weighted = total_locked_weighted.saturating_add(weighted);
    }
    Ok((flexible_shares, total_locked_weighted))
}

/// Sum all locked shares (unweighted) for a user.
pub fn total_locked_shares(env: &Env, user: &Address) -> i128 {
    let locked = get_user_locked_positions(env, user);
    let mut total: i128 = 0;
    for (_, pos) in locked.iter() {
        total = total.saturating_add(pos.shares);
    }
    total
}

/// Delete all locked positions for a user and reset their lock count to zero.
/// Called by `emergency_withdraw` to prevent orphaned storage entries.
pub fn clear_user_locks(env: &Env, user: &Address) {
    let count = get_user_lock_count(env, user);
    for i in 0..count {
        delete_locked_position(env, user, i);
    }
    set_user_lock_count(env, user, 0);
}
