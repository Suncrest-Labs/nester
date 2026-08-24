//! Vault Yield Accrual Module
//!
//! Implements global yield accumulator logic and per-user yield checkpoints
//! using fixed-point arithmetic (`SCALE = 1e18`).

use soroban_sdk::{contracttype, symbol_short, Address, Env, Symbol};
use nester_common::constants::SCALE;
use nester_common::errors::ContractError;
use nester_common::events::{emit_event, UserYieldAccruedEventData};

const VAULT: Symbol = symbol_short!("VAULT");
const YIELD_ACC: Symbol = symbol_short!("YLD_ACC");

#[contracttype]
#[derive(Clone)]
pub enum AccrualDataKey {
    YieldIndex,
    UserYieldIndex(Address),
    UserAccrued(Address),
}

/// Get the current global yield index.
/// Defaults to `SCALE` (`1e18`) if uninitialized (lazy migration).
pub fn get_yield_index(env: &Env) -> i128 {
    env.storage()
        .instance()
        .get(&AccrualDataKey::YieldIndex)
        .unwrap_or(SCALE)
}

/// Set the global yield index.
pub fn set_yield_index(env: &Env, index: i128) {
    env.storage()
        .instance()
        .set(&AccrualDataKey::YieldIndex, &index);
}

/// Get user's checkpointed yield index.
/// Defaults to `SCALE` (`1e18`) if uninitialized (lazy migration).
pub fn get_user_index(env: &Env, user: &Address) -> i128 {
    env.storage()
        .persistent()
        .get(&AccrualDataKey::UserYieldIndex(user.clone()))
        .unwrap_or(SCALE)
}

/// Set user's checkpointed yield index.
pub fn set_user_index(env: &Env, user: &Address, index: i128) {
    env.storage()
        .persistent()
        .set(&AccrualDataKey::UserYieldIndex(user.clone()), &index);
}

/// Get user's accrued unharvested yield.
pub fn get_user_accrued(env: &Env, user: &Address) -> i128 {
    env.storage()
        .persistent()
        .get(&AccrualDataKey::UserAccrued(user.clone()))
        .unwrap_or(0)
}

/// Set user's accrued unharvested yield.
pub fn set_user_accrued(env: &Env, user: &Address, amount: i128) {
    env.storage()
        .persistent()
        .set(&AccrualDataKey::UserAccrued(user.clone()), &amount);
}

/// Calculate the index delta for a reported yield amount across total shares.
/// Option A: Rejects report_yield when total_shares == 0.
pub fn calculate_delta_index(yield_amount: i128, total_shares: i128) -> Result<i128, ContractError> {
    if yield_amount < 0 {
        return Err(ContractError::InvalidAmount);
    }
    if total_shares <= 0 {
        return Err(ContractError::InvalidAmount);
    }
    let scaled = yield_amount
        .checked_mul(SCALE)
        .ok_or(ContractError::ArithmeticOverflow)?;
    let delta = scaled
        .checked_div(total_shares)
        .ok_or(ContractError::ArithmeticOverflow)?;
    Ok(delta)
}

/// Calculate accrued yield delta for a user based on their share balance and index diff.
pub fn calculate_user_accrual(
    yield_index: i128,
    user_index: i128,
    user_shares: i128,
) -> Result<i128, ContractError> {
    if user_shares <= 0 {
        return Ok(0);
    }
    if yield_index < user_index {
        return Err(ContractError::ArithmeticOverflow);
    }
    let index_diff = yield_index
        .checked_sub(user_index)
        .ok_or(ContractError::ArithmeticOverflow)?;
    let product = index_diff
        .checked_mul(user_shares)
        .ok_or(ContractError::ArithmeticOverflow)?;
    let delta = product
        .checked_div(SCALE)
        .ok_or(ContractError::ArithmeticOverflow)?;
    Ok(delta)
}

/// Update global yield index with reported yield.
pub fn accumulate_yield(
    env: &Env,
    yield_amount: i128,
    total_shares: i128,
) -> Result<i128, ContractError> {
    let current_index = get_yield_index(env);
    let delta_index = calculate_delta_index(yield_amount, total_shares)?;
    let new_index = current_index
        .checked_add(delta_index)
        .ok_or(ContractError::ArithmeticOverflow)?;
    set_yield_index(env, new_index);
    Ok(new_index)
}

/// Synchronize user checkpoint: accrue yield entitlement up to current global index.
pub fn sync_user(env: &Env, user: &Address, user_shares: i128) -> Result<i128, ContractError> {
    let yield_index = get_yield_index(env);
    let u_index = get_user_index(env, user);
    let current_accrued = get_user_accrued(env, user);
    let delta = calculate_user_accrual(yield_index, u_index, user_shares)?;
    let new_accrued = current_accrued
        .checked_add(delta)
        .ok_or(ContractError::ArithmeticOverflow)?;
    set_user_accrued(env, user, new_accrued);
    set_user_index(env, user, yield_index);

    if delta > 0 {
        emit_event(
            env,
            VAULT,
            YIELD_ACC,
            user.clone(),
            UserYieldAccruedEventData {
                user: user.clone(),
                accrued_delta: delta,
                total_accrued: new_accrued,
                user_index: yield_index,
            },
        );
    }

    Ok(new_accrued)
}

/// Pure read of user's total pending yield (existing accrued + un-checkpointed delta).
pub fn pending_yield(
    env: &Env,
    user: &Address,
    user_shares: i128,
) -> Result<i128, ContractError> {
    let yield_index = get_yield_index(env);
    let u_index = get_user_index(env, user);
    let current_accrued = get_user_accrued(env, user);
    let delta = calculate_user_accrual(yield_index, u_index, user_shares)?;
    let total = current_accrued
        .checked_add(delta)
        .ok_or(ContractError::ArithmeticOverflow)?;
    Ok(total)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_calculate_delta_index() {
        // 100 yield across 1000 shares -> delta = 100 * 1e18 / 1000 = 1e17
        let delta = calculate_delta_index(100, 1000).unwrap();
        assert_eq!(delta, 100_000_000_000_000_000);
    }

    #[test]
    fn test_calculate_delta_index_zero_shares_err() {
        assert!(calculate_delta_index(100, 0).is_err());
    }

    #[test]
    fn test_calculate_user_accrual() {
        let global_index = SCALE + 100_000_000_000_000_000; // 1e17 increase
        let user_index = SCALE;
        let user_shares = 1000;
        let accrued = calculate_user_accrual(global_index, user_index, user_shares).unwrap();
        assert_eq!(accrued, 100);
    }

    #[test]
    fn test_rounding_truncates_down() {
        // yield index diff = 1 (tiny)
        let global_index = SCALE + 1;
        let user_index = SCALE;
        let user_shares = 5;
        // 1 * 5 / 1e18 = 0
        let accrued = calculate_user_accrual(global_index, user_index, user_shares).unwrap();
        assert_eq!(accrued, 0);
    }

    #[test]
    fn test_overflow_protection() {
        // MAX shares * large index delta
        let global_index = i128::MAX;
        let user_index = 0;
        let user_shares = i128::MAX;
        assert!(calculate_user_accrual(global_index, user_index, user_shares).is_err());
    }
}
