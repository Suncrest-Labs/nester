//! Time-locked savings positions (issue #802).
//!
//! A locked position is a deposit contractually immobilised until
//! `unlock_at`, in exchange for a yield boost proportional to the tier the
//! depositor chose. It reuses the vault's existing share-minting math
//! (`deposit_internal`), so share price stays a single global quantity;
//! the lock itself is separate metadata keyed by `(user, lock_id)`.
//!
//! # The yield-boost derivation
//!
//! The vault has exactly one global share price (`TotalAssets / TotalSupply`
//! on `vault_token`), and every share, flexible or locked, appreciates
//! identically when that price rises. There is no way to give a locked
//! position a *bigger* share of a yield report without minting it *extra*
//! shares at the moment the price moves — this module computes exactly how
//! many, via [`nester_common::fees::boosted_shares_after_yield`].
//!
//! Concretely: for a locked position with `s` shares and boost `b` (in
//! basis points, 10_000 = 1x), letting `TA`/`TS` be the vault's totals
//! *before* a yield report of `Y`, and `W` = the vault-wide total weight
//! (every open lock's `shares * boost_bps`, plus every flexible share
//! counted at `1x` i.e. `10_000`), the position's new share count after the
//! report is
//!
//! ```text
//! new_shares = s * (TA*W + TS*Y*b) / (TA*W + TS*Y*BASIS_POINT_SCALE)
//! ```
//!
//! This is derived by solving for the one new global share price `P_new`
//! that keeps the *flexible* pool's value conservation equation exactly
//! satisfied (flexible shares are never minted, so their value must equal
//! their unchanged share count times `P_new`); `P_new = TA/TS + Y/W` falls
//! out of that equation directly, and every locked position's new share
//! count follows from wanting its own value to land on its old value plus
//! its own weight-proportional slice of `Y`. See
//! `libs/common/src/fees.rs`'s `boosted_shares_after_yield` doc comment and
//! its `lock_tests` module for the full algebra and a conservation-of-value
//! test that sums every pool's post-report value against `TA + Y`.
//!
//! At `boost_bps == BASIS_POINT_SCALE` (exactly 1x — no boost) this
//! degenerates to `new_shares == s`: a lock with no boost is minted
//! nothing, identical to how a flexible share already appreciates correctly
//! through the share-price rise alone. This is what makes the flexible
//! pool's own shares safe to leave untouched by this whole mechanism: they
//! are, in effect, "1x locks" that were never minted anything to begin
//! with, and the formula agrees.
//!
//! # Why this must run eagerly, for every open lock, in the same call
//!
//! The formula above is only exact when every open lock's share count is
//! updated *simultaneously*, using the same `TA`/`TS`/`W` snapshot — `W`
//! itself depends on every lock's current share count, so updating one
//! lock without updating the others in the same instant leaves the
//! now-stale ones under- or over-counted relative to what the next
//! calculation will assume. A lazily-deferred, per-lock "settle whenever
//! it's next touched" design was considered and rejected during review: it
//! either requires an unsound pool-average approximation (verified,
//! working through concrete numbers, to misallocate value between locks at
//! *different* boost tiers even within a single round) or a genuinely
//! circular per-round accumulator (each round's common factor depends on
//! the vault's current locked weight, which itself depends on unsettled
//! prior rounds' unrealized gains). Settling every open lock in the same
//! transaction that applies the yield report sidesteps both problems by
//! construction, at the cost of bounding `report_yield`'s per-call work by
//! [`nester_common::constants::MAX_TOTAL_OPEN_LOCKS`] rather than making it
//! O(1) regardless of lock count.

use nester_common::{
    constants::{
        DEFAULT_LOCK_BREAK_PENALTY_BPS, DEFAULT_LOCK_TIERS_SECONDS, DEFAULT_LOCK_TIER_BOOST_BPS,
        MAX_LOCK_BOOST_BPS, MAX_LOCK_BREAK_PENALTY_BPS, MAX_OPEN_LOCKS_PER_USER,
        MAX_TOTAL_OPEN_LOCKS,
    },
    fees::{boosted_shares_after_yield, lock_break_penalty_bps, mul_div},
    ContractError,
};
use soroban_sdk::{contracttype, Address, Env, Vec};

/// One admin-configured lock duration and its boost multiplier.
#[contracttype]
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct LockTier {
    pub duration_secs: u64,
    /// Basis points; 10_000 = 1x (no boost).
    pub boost_bps: u32,
}

/// A single open (or matured-but-not-yet-claimed) locked position.
#[contracttype]
#[derive(Clone, Debug)]
pub struct LockedPosition {
    pub lock_id: u64,
    pub owner: Address,
    /// Shares committed to this lock at creation. Frozen at creation time
    /// except when `report_yield`'s boost split mints additional shares
    /// directly into this field — see the module doc.
    pub shares: i128,
    pub created_at: u64,
    pub unlock_at: u64,
    pub tier: LockTier,
    /// The boost multiplier captured at creation time (copy of
    /// `tier.boost_bps`), so a later admin change to the tier table never
    /// retroactively changes an already-open lock's economics.
    pub boost_bps: u32,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct LockedPositionsView {
    pub positions: Vec<LockedPosition>,
    pub total_locked_shares: i128,
}

// ---------------------------------------------------------------------------
// Storage keys local to this module's concerns. `DataKey` itself lives in
// vault/src/lib.rs (this workspace's convention: each contract's storage
// enum is local to it, not shared via nester-common — see e.g. vault_token's
// own separate DataKey), so these are constructed from a shared key space
// via the same enum; see the vault's DataKey variants prefixed `Lock`.
// ---------------------------------------------------------------------------

/// Admin-configured lock tiers. Falls back to
/// [`DEFAULT_LOCK_TIERS_SECONDS`]/[`DEFAULT_LOCK_TIER_BOOST_BPS`] paired up
/// when never explicitly set.
pub fn default_tiers(env: &Env) -> Vec<LockTier> {
    let mut tiers = Vec::new(env);
    for i in 0..DEFAULT_LOCK_TIERS_SECONDS.len() {
        tiers.push_back(LockTier {
            duration_secs: DEFAULT_LOCK_TIERS_SECONDS[i],
            boost_bps: DEFAULT_LOCK_TIER_BOOST_BPS[i],
        });
    }
    tiers
}

/// Validates a proposed tier table: every boost within
/// `[BASIS_POINT_SCALE, MAX_LOCK_BOOST_BPS]` (never below 1x — a "boost"
/// under 1x would just be a penalty for locking, contradicting the whole
/// feature), and every duration strictly positive.
pub fn validate_tiers(tiers: &Vec<LockTier>) -> Result<(), ContractError> {
    if tiers.is_empty() {
        return Err(ContractError::ConfigOutOfRange);
    }
    for tier in tiers.iter() {
        if tier.duration_secs == 0 {
            return Err(ContractError::ConfigOutOfRange);
        }
        if tier.boost_bps < nester_common::fees::BASIS_POINT_SCALE as u32
            || tier.boost_bps > MAX_LOCK_BOOST_BPS
        {
            return Err(ContractError::ConfigOutOfRange);
        }
    }
    Ok(())
}

/// Finds the tier matching `duration_secs` exactly. Arbitrary durations are
/// rejected (`InvalidLockDuration`, mapped to `ConfigOutOfRange` — see
/// `errors.rs`'s reuse table): unbounded tier variety would make the boost
/// curve impossible to reason about and bloat storage with one-off entries.
pub fn find_tier(tiers: &Vec<LockTier>, duration_secs: u64) -> Result<LockTier, ContractError> {
    for tier in tiers.iter() {
        if tier.duration_secs == duration_secs {
            return Ok(tier);
        }
    }
    Err(ContractError::ConfigOutOfRange)
}

/// Every open lock's `shares * boost_bps`, plus every flexible share
/// (`total_supply - total_locked_shares`) counted at `1x`
/// (`BASIS_POINT_SCALE`) — the `W` in the module doc's derivation.
pub fn total_weight(
    total_supply: i128,
    total_locked_shares: i128,
    total_locked_weight: i128,
) -> Result<i128, ContractError> {
    let flexible_shares = total_supply
        .checked_sub(total_locked_shares)
        .ok_or(ContractError::ArithmeticOverflow)?;
    let flexible_weight = flexible_shares
        .checked_mul(nester_common::fees::BASIS_POINT_SCALE)
        .ok_or(ContractError::ArithmeticOverflow)?;
    flexible_weight
        .checked_add(total_locked_weight)
        .ok_or(ContractError::ArithmeticOverflow)
}

/// Applies one yield report's boost split to every currently open lock,
/// eagerly and in this same call (see the module doc for why). Returns the
/// total number of boost shares minted across all locks (already applied to
/// each `LockedPosition.shares` and to `vault_token`'s total supply via
/// `mint_boost_shares`; the caller is responsible for actually invoking
/// that mint).
///
/// Skipped (returns `Ok(0)` immediately) when there are no open locks —
/// the overwhelmingly common case for a vault with no locked depositors —
/// so a vault that never uses this feature pays zero extra cost on every
/// `report_yield` call.
pub struct BoostSettlement {
    pub minted_total: i128,
    pub updated_positions: Vec<LockedPosition>,
}

pub fn settle_boost_for_all_open_locks(
    open_locks: &Vec<LockedPosition>,
    total_assets: i128,
    total_supply: i128,
    total_locked_shares: i128,
    total_locked_weight: i128,
    yield_amount: i128,
) -> Result<BoostSettlement, ContractError> {
    if open_locks.is_empty() || yield_amount <= 0 {
        return Ok(BoostSettlement {
            minted_total: 0,
            updated_positions: open_locks.clone(),
        });
    }

    let w = total_weight(total_supply, total_locked_shares, total_locked_weight)?;

    let mut updated = Vec::new(open_locks.env());
    let mut minted_total: i128 = 0;
    for position in open_locks.iter() {
        let new_shares = boosted_shares_after_yield(
            position.shares,
            position.boost_bps,
            total_assets,
            total_supply,
            w,
            yield_amount,
        )?;
        let minted = new_shares
            .checked_sub(position.shares)
            .ok_or(ContractError::ArithmeticOverflow)?;
        minted_total = minted_total
            .checked_add(minted)
            .ok_or(ContractError::ArithmeticOverflow)?;

        let mut updated_position = position.clone();
        updated_position.shares = new_shares;
        updated.push_back(updated_position);
    }

    Ok(BoostSettlement {
        minted_total,
        updated_positions: updated,
    })
}

/// Validates a new lock against the per-user and vault-wide open-lock caps.
pub fn check_lock_limits(user_open_count: u32, total_open_count: u32) -> Result<(), ContractError> {
    if user_open_count >= MAX_OPEN_LOCKS_PER_USER {
        return Err(ContractError::ExceedsLimit);
    }
    if total_open_count >= MAX_TOTAL_OPEN_LOCKS {
        return Err(ContractError::ExceedsLimit);
    }
    Ok(())
}

/// Early-break penalty in asset terms for a lock being broken at `now`,
/// derived from [`lock_break_penalty_bps`] applied to the lock's current
/// asset value (`amount_for_shares`, computed by the caller from the
/// lock's current — possibly boost-inflated — share count).
pub fn break_penalty_amount(
    asset_value: i128,
    full_penalty_bps: u32,
    created_at: u64,
    unlock_at: u64,
    now: u64,
) -> Result<i128, ContractError> {
    if unlock_at <= created_at {
        return Ok(0);
    }
    let total_duration = unlock_at - created_at;
    let elapsed = now.saturating_sub(created_at);
    let bps = lock_break_penalty_bps(full_penalty_bps, elapsed, total_duration);
    mul_div(asset_value, bps as i128, 10_000)
}

/// Validates a proposed full lock-break penalty rate against
/// [`MAX_LOCK_BREAK_PENALTY_BPS`].
pub fn validate_break_penalty_bps(bps: u32) -> Result<(), ContractError> {
    if bps > MAX_LOCK_BREAK_PENALTY_BPS {
        return Err(ContractError::ConfigOutOfRange);
    }
    Ok(())
}

/// The default full lock-break penalty rate, for `initialize`/first-read
/// call sites that have not yet had an admin override configured.
pub fn default_break_penalty_bps() -> u32 {
    DEFAULT_LOCK_BREAK_PENALTY_BPS
}

#[cfg(test)]
mod tests {
    use super::*;
    use soroban_sdk::{testutils::Address as _, Env};

    fn tier(duration_secs: u64, boost_bps: u32) -> LockTier {
        LockTier {
            duration_secs,
            boost_bps,
        }
    }

    #[test]
    fn default_tiers_match_the_constants_pairwise() {
        let env = Env::default();
        let tiers = default_tiers(&env);
        assert_eq!(tiers.len(), 4);
        assert_eq!(tiers.get(0).unwrap().duration_secs, 30 * 86_400);
        assert_eq!(tiers.get(3).unwrap().boost_bps, 20_000);
    }

    #[test]
    fn validate_tiers_rejects_empty() {
        let env = Env::default();
        let tiers: Vec<LockTier> = Vec::new(&env);
        assert_eq!(validate_tiers(&tiers), Err(ContractError::ConfigOutOfRange));
    }

    #[test]
    fn validate_tiers_rejects_sub_1x_boost() {
        let env = Env::default();
        let mut tiers = Vec::new(&env);
        tiers.push_back(tier(86_400, 9_999));
        assert_eq!(validate_tiers(&tiers), Err(ContractError::ConfigOutOfRange));
    }

    #[test]
    fn validate_tiers_rejects_above_max_boost() {
        let env = Env::default();
        let mut tiers = Vec::new(&env);
        tiers.push_back(tier(86_400, MAX_LOCK_BOOST_BPS + 1));
        assert_eq!(validate_tiers(&tiers), Err(ContractError::ConfigOutOfRange));
    }

    #[test]
    fn validate_tiers_rejects_zero_duration() {
        let env = Env::default();
        let mut tiers = Vec::new(&env);
        tiers.push_back(tier(0, 11_000));
        assert_eq!(validate_tiers(&tiers), Err(ContractError::ConfigOutOfRange));
    }

    #[test]
    fn validate_tiers_accepts_the_defaults() {
        let env = Env::default();
        let tiers = default_tiers(&env);
        assert!(validate_tiers(&tiers).is_ok());
    }

    #[test]
    fn find_tier_matches_exact_duration_only() {
        let env = Env::default();
        let tiers = default_tiers(&env);
        assert_eq!(find_tier(&tiers, 30 * 86_400).unwrap().boost_bps, 11_000);
        assert_eq!(
            find_tier(&tiers, 30 * 86_400 + 1),
            Err(ContractError::ConfigOutOfRange)
        );
    }

    #[test]
    fn total_weight_counts_flexible_at_1x() {
        // No locks at all: total_weight should equal total_supply * BASIS_POINT_SCALE.
        let w = total_weight(1000, 0, 0).unwrap();
        assert_eq!(w, 1000 * 10_000);
    }

    #[test]
    fn total_weight_adds_locked_weight_on_top_of_remaining_flexible() {
        // 1000 total supply, 500 of it locked (weight already computed as
        // 500*20_000=10_000_000 by the caller); flexible remainder is 500.
        let w = total_weight(1000, 500, 500 * 20_000).unwrap();
        assert_eq!(w, 500 * 10_000 + 10_000_000);
    }

    #[test]
    fn check_lock_limits_rejects_at_the_per_user_cap() {
        assert_eq!(
            check_lock_limits(MAX_OPEN_LOCKS_PER_USER, 1),
            Err(ContractError::ExceedsLimit)
        );
        assert!(check_lock_limits(MAX_OPEN_LOCKS_PER_USER - 1, 1).is_ok());
    }

    #[test]
    fn check_lock_limits_rejects_at_the_vault_wide_cap() {
        assert_eq!(
            check_lock_limits(1, MAX_TOTAL_OPEN_LOCKS),
            Err(ContractError::ExceedsLimit)
        );
        assert!(check_lock_limits(1, MAX_TOTAL_OPEN_LOCKS - 1).is_ok());
    }

    #[test]
    fn break_penalty_amount_is_full_at_creation() {
        let amount = break_penalty_amount(10_000, 1_000, 0, 90 * 86_400, 0).unwrap();
        assert_eq!(amount, 1_000); // 10% of 10_000
    }

    #[test]
    fn break_penalty_amount_decays_at_the_midpoint() {
        let amount = break_penalty_amount(10_000, 1_000, 0, 90 * 86_400, 45 * 86_400).unwrap();
        assert_eq!(amount, 500); // 5% of 10_000 (half the 10% full rate)
    }

    #[test]
    fn break_penalty_amount_is_zero_at_maturity() {
        let amount = break_penalty_amount(10_000, 1_000, 0, 90 * 86_400, 90 * 86_400).unwrap();
        assert_eq!(amount, 0);
    }

    #[test]
    fn validate_break_penalty_bps_rejects_above_the_cap() {
        assert_eq!(
            validate_break_penalty_bps(MAX_LOCK_BREAK_PENALTY_BPS + 1),
            Err(ContractError::ConfigOutOfRange)
        );
        assert!(validate_break_penalty_bps(MAX_LOCK_BREAK_PENALTY_BPS).is_ok());
    }

    #[test]
    fn settle_boost_is_a_noop_with_no_open_locks() {
        let env = Env::default();
        let empty: Vec<LockedPosition> = Vec::new(&env);
        let result = settle_boost_for_all_open_locks(&empty, 1000, 1000, 0, 0, 100).unwrap();
        assert_eq!(result.minted_total, 0);
        assert_eq!(result.updated_positions.len(), 0);
    }

    #[test]
    fn settle_boost_matches_the_worked_two_pool_example() {
        let env = Env::default();
        let owner = Address::generate(&env);
        let mut locks = Vec::new(&env);
        locks.push_back(LockedPosition {
            lock_id: 1,
            owner: owner.clone(),
            shares: 500,
            created_at: 0,
            unlock_at: 90 * 86_400,
            tier: tier(90 * 86_400, 20_000),
            boost_bps: 20_000,
        });

        // Matches fees::lock_tests::boosted_shares_matches_the_worked_two_pool_example:
        // TA=1000, TS=1000, flexible=500 (so total_locked_shares=500 too,
        // meaning flexible remainder = 1000-500=500), total_locked_weight = 500*20_000.
        let result =
            settle_boost_for_all_open_locks(&locks, 1000, 1000, 500, 500 * 20_000, 100).unwrap();
        assert_eq!(result.updated_positions.get(0).unwrap().shares, 531);
        assert_eq!(result.minted_total, 31);
    }

    #[test]
    fn settle_boost_settles_every_open_lock_in_one_call() {
        let env = Env::default();
        let owner = Address::generate(&env);
        let mut locks = Vec::new(&env);
        locks.push_back(LockedPosition {
            lock_id: 1,
            owner: owner.clone(),
            shares: 750,
            created_at: 0,
            unlock_at: 90 * 86_400,
            tier: tier(90 * 86_400, 15_000),
            boost_bps: 15_000,
        });
        locks.push_back(LockedPosition {
            lock_id: 2,
            owner: owner.clone(),
            shares: 750,
            created_at: 0,
            unlock_at: 365 * 86_400,
            tier: tier(365 * 86_400, 30_000),
            boost_bps: 30_000,
        });

        // Matches the mixed-tier verification done during design: TA=2000,
        // TS=2000, flexible=500, lockA=750@1.5x, lockB=750@3x, Y=200.
        let total_locked_shares = 1500;
        let total_locked_weight = 750 * 15_000 + 750 * 30_000;
        let result = settle_boost_for_all_open_locks(
            &locks,
            2000,
            2000,
            total_locked_shares,
            total_locked_weight,
            200,
        )
        .unwrap();

        let a = result.updated_positions.get(0).unwrap();
        let b = result.updated_positions.get(1).unwrap();
        assert_eq!(a.shares, 768); // 750 + 18 (18.40 rounds down)
        assert_eq!(b.shares, 823); // 750 + 73 (73.62 rounds down)
    }
}
