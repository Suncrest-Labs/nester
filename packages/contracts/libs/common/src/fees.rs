use crate::ContractError;

pub const BASIS_POINT_SCALE: i128 = 10000;
pub const SECONDS_PER_YEAR: i128 = 31536000;

/// Upper bound on the elapsed window passed to `calculate_management_fee` in a
/// single call. Callers that have not accrued fees in longer than this should
/// advance their accrual cursor by `MAX_FEE_ACCRUAL_INTERVAL_SECONDS` per call
/// so the remainder is collected over subsequent invocations rather than
/// computed as one giant intermediate that could overflow.
pub const MAX_FEE_ACCRUAL_INTERVAL_SECONDS: u64 = 30 * 24 * 60 * 60;

/// Compute `(a * b) / divisor` without panicking on intermediate overflow.
///
/// Falls back to `(a / divisor) * b + (a % divisor) * b / divisor` when the
/// straight `a * b` would overflow. This keeps the result exact for the
/// non-overflow case and only loses sub-divisor precision (1 unit at most)
/// in the fallback path.
pub fn mul_div(a: i128, b: i128, divisor: i128) -> Result<i128, ContractError> {
    if divisor == 0 {
        return Err(ContractError::ArithmeticOverflow);
    }

    if let Some(prod) = a.checked_mul(b) {
        return prod
            .checked_div(divisor)
            .ok_or(ContractError::ArithmeticOverflow);
    }

    let q = a / divisor;
    let r = a % divisor;
    let part1 = q.checked_mul(b).ok_or(ContractError::ArithmeticOverflow)?;
    let part2 = r.checked_mul(b).ok_or(ContractError::ArithmeticOverflow)? / divisor;
    part1
        .checked_add(part2)
        .ok_or(ContractError::ArithmeticOverflow)
}

pub fn calculate_management_fee(
    total_assets: i128,
    management_fee_bps: u32,
    elapsed_seconds: u64,
) -> Result<i128, ContractError> {
    if total_assets <= 0 || management_fee_bps == 0 || elapsed_seconds == 0 {
        return Ok(0);
    }

    // fee = total_assets * fee_bps * elapsed / (BPS_SCALE * SECONDS_PER_YEAR)
    //
    // Computed via two `mul_div` stages so neither intermediate ever needs to
    // hold a raw `total_assets * fee_bps` or `total_assets * elapsed` product.
    // `mul_div` itself falls back to a divide-then-multiply path if the direct
    // product overflows, so the only remaining error path is a result that
    // genuinely exceeds i128.
    let bps_share = mul_div(total_assets, management_fee_bps as i128, BASIS_POINT_SCALE)?;
    mul_div(bps_share, elapsed_seconds as i128, SECONDS_PER_YEAR)
}

pub fn calculate_performance_fee(
    yield_earned: i128,
    performance_fee_bps: u32,
) -> Result<i128, ContractError> {
    if yield_earned <= 0 || performance_fee_bps == 0 {
        return Ok(0);
    }

    mul_div(yield_earned, performance_fee_bps as i128, BASIS_POINT_SCALE)
}

pub fn calculate_withdrawal_fee(amount: i128, fee_bps: u32) -> Result<i128, ContractError> {
    if amount <= 0 || fee_bps == 0 {
        return Ok(0);
    }

    mul_div(amount, fee_bps as i128, BASIS_POINT_SCALE)
}

// ---------------------------------------------------------------------------
// Duration- and size-tiered fee schedule (issue #813)
// ---------------------------------------------------------------------------
//
// Pure, `no_std`-friendly tier arithmetic with zero Soroban dependency —
// operates over plain slices so it can be unit-tested with stack arrays and
// exercised by a Soroban contract by converting its on-chain `Vec<T>` tier
// storage into a `&[FeeTier]` first (a bounded, cheap conversion capped by
// `MAX_FEE_TIERS`).

/// One (threshold, rate) breakpoint. `threshold` is either a tenure in
/// seconds (for exit/performance tiers) or a TVL amount (for the management
/// tier) — the caller decides which domain by what it passes to `rate_at`.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct FeeTier {
    pub threshold: i128,
    pub rate_bps: u32,
}

/// Ceiling on how many tiers a single schedule may hold. Unbounded tiers
/// would mean unbounded iteration in a hot path (every deposit/withdraw).
pub const MAX_FEE_TIERS: usize = 8;

/// A schedule update may not raise any point on the curve by more than this
/// many bps relative to the previous schedule — see
/// [`validate_no_adverse_increase`]. Protects existing depositors from a
/// retroactive fee hike hidden inside an otherwise-reasonable-looking
/// schedule change.
pub const MAX_SCHEDULE_RATE_INCREASE_BPS: u32 = 100;

/// Validates a tier table: non-empty-thresholds strictly ascending, count
/// within [`MAX_FEE_TIERS`], and every rate at or below `rate_ceiling`.
/// An empty table is valid — it means "tiers not configured, use the flat
/// fee" and is the default.
pub fn validate_tiers(tiers: &[FeeTier], rate_ceiling: u32) -> Result<(), ContractError> {
    if tiers.len() > MAX_FEE_TIERS {
        return Err(ContractError::ConfigOutOfRange);
    }
    for tier in tiers {
        if tier.rate_bps > rate_ceiling {
            return Err(ContractError::FeeTooHigh);
        }
    }
    for pair in tiers.windows(2) {
        if pair[1].threshold <= pair[0].threshold {
            return Err(ContractError::ConfigOutOfRange);
        }
    }
    Ok(())
}

/// Continuous, monotonic rate lookup: linearly interpolates between the two
/// tiers bracketing `x`, so there is no cliff at a tier boundary a user
/// could game by timing a withdrawal. Before the first threshold, returns
/// the first tier's rate; at or after the last threshold, returns the last
/// tier's rate. Returns 0 for an empty table (caller should treat that as
/// "tiers not configured").
pub fn rate_at(tiers: &[FeeTier], x: i128) -> u32 {
    if tiers.is_empty() {
        return 0;
    }
    if x <= tiers[0].threshold {
        return tiers[0].rate_bps;
    }
    let last = tiers[tiers.len() - 1];
    if x >= last.threshold {
        return last.rate_bps;
    }
    for pair in tiers.windows(2) {
        let (a, b) = (pair[0], pair[1]);
        if x >= a.threshold && x <= b.threshold {
            let span = b.threshold - a.threshold;
            if span <= 0 {
                return a.rate_bps;
            }
            let pos = x - a.threshold;
            let rate_diff = b.rate_bps as i128 - a.rate_bps as i128;
            let interpolated = a.rate_bps as i128 + mul_div(rate_diff, pos, span).unwrap_or(0);
            return interpolated.max(0) as u32;
        }
    }
    // Unreachable given the bounds checks above, but stay safe rather than
    // panicking on tier arithmetic.
    last.rate_bps
}

/// The nearest threshold strictly greater than `x`, and the rate a point at
/// that exact threshold would carry — used to answer "how long until my
/// rate improves". Returns `None` once `x` is at or past the last tier.
pub fn next_boundary(tiers: &[FeeTier], x: i128) -> Option<FeeTier> {
    tiers.iter().find(|t| t.threshold > x).copied()
}

/// True if no point on `new_tiers` exceeds the corresponding point on
/// `old_tiers` by more than `max_delta_bps`. Because both curves are
/// piecewise-linear, the maximum of their difference can only occur at a
/// breakpoint of either curve, so checking every threshold from both tables
/// is exact — not a sample-based approximation.
pub fn validate_no_adverse_increase(
    old_tiers: &[FeeTier],
    new_tiers: &[FeeTier],
    max_delta_bps: u32,
) -> Result<(), ContractError> {
    if old_tiers.is_empty() {
        // Nothing was previously promised; any valid new schedule is fine.
        return Ok(());
    }
    let mut check_points: [i128; MAX_FEE_TIERS * 2] = [0; MAX_FEE_TIERS * 2];
    let mut n = 0;
    for t in old_tiers.iter().chain(new_tiers.iter()) {
        if n < check_points.len() {
            check_points[n] = t.threshold;
            n += 1;
        }
    }
    for &x in &check_points[..n] {
        let old_rate = rate_at(old_tiers, x) as i128;
        let new_rate = rate_at(new_tiers, x) as i128;
        if new_rate - old_rate > max_delta_bps as i128 {
            return Err(ContractError::ConfigOutOfRange);
        }
    }
    Ok(())
}

#[cfg(test)]
mod tier_tests {
    use super::*;

    fn decreasing_tiers() -> [FeeTier; 3] {
        // Exit-fee-style curve: 200 bps at day 0, dropping to 100 bps at 90
        // days, down to 25 bps at 365 days.
        [
            FeeTier {
                threshold: 0,
                rate_bps: 200,
            },
            FeeTier {
                threshold: 90 * 86400,
                rate_bps: 100,
            },
            FeeTier {
                threshold: 365 * 86400,
                rate_bps: 25,
            },
        ]
    }

    #[test]
    fn rate_at_is_continuous_at_every_boundary() {
        let tiers = decreasing_tiers();
        for tier in tiers.iter() {
            let just_before = rate_at(&tiers, tier.threshold - 1);
            let at = rate_at(&tiers, tier.threshold);
            let just_after = rate_at(&tiers, tier.threshold + 1);
            // A 1-second nudge across a boundary must not jump by more than
            // 1 bps worth of rounding — no cliff.
            assert!((at as i32 - just_before as i32).abs() <= 1);
            assert!((just_after as i32 - at as i32).abs() <= 1);
        }
    }

    #[test]
    fn rate_at_is_monotonic_non_increasing_across_domain() {
        let tiers = decreasing_tiers();
        let mut prev = rate_at(&tiers, 0);
        let mut t = 1i128;
        while t <= 400 * 86400 {
            let cur = rate_at(&tiers, t);
            assert!(cur <= prev, "rate rose from {prev} to {cur} at t={t}");
            prev = cur;
            t += 3600;
        }
    }

    #[test]
    fn rate_at_clamps_outside_domain() {
        let tiers = decreasing_tiers();
        assert_eq!(rate_at(&tiers, -1), 200);
        assert_eq!(rate_at(&tiers, 10_000 * 86400), 25);
    }

    #[test]
    fn rate_at_empty_table_returns_zero() {
        let tiers: [FeeTier; 0] = [];
        assert_eq!(rate_at(&tiers, 1000), 0);
    }

    #[test]
    fn validate_tiers_rejects_non_ascending_thresholds() {
        let bad = [
            FeeTier {
                threshold: 100,
                rate_bps: 50,
            },
            FeeTier {
                threshold: 100,
                rate_bps: 25,
            },
        ];
        assert!(validate_tiers(&bad, 5000).is_err());
    }

    #[test]
    fn validate_tiers_rejects_rate_above_ceiling() {
        let bad = [FeeTier {
            threshold: 0,
            rate_bps: 6000,
        }];
        assert!(validate_tiers(&bad, 5000).is_err());
    }

    #[test]
    fn validate_tiers_rejects_too_many_tiers() {
        let many: [FeeTier; MAX_FEE_TIERS + 1] = core::array::from_fn(|i| FeeTier {
            threshold: i as i128,
            rate_bps: 10,
        });
        assert!(validate_tiers(&many, 5000).is_err());
    }

    #[test]
    fn validate_tiers_accepts_empty_table() {
        let empty: [FeeTier; 0] = [];
        assert!(validate_tiers(&empty, 5000).is_ok());
    }

    #[test]
    fn next_boundary_finds_upcoming_threshold() {
        let tiers = decreasing_tiers();
        let nb = next_boundary(&tiers, 10 * 86400).unwrap();
        assert_eq!(nb.threshold, 90 * 86400);
        assert_eq!(nb.rate_bps, 100);
    }

    #[test]
    fn next_boundary_none_after_last_tier() {
        let tiers = decreasing_tiers();
        assert!(next_boundary(&tiers, 400 * 86400).is_none());
    }

    #[test]
    fn grandfathering_allows_decrease_and_small_increase() {
        let old = decreasing_tiers();
        let mut lowered = old;
        lowered[0].rate_bps = 150; // a decrease everywhere is always fine
        assert!(
            validate_no_adverse_increase(&old, &lowered, MAX_SCHEDULE_RATE_INCREASE_BPS).is_ok()
        );

        let mut nudged = old;
        nudged[0].rate_bps = old[0].rate_bps + MAX_SCHEDULE_RATE_INCREASE_BPS; // exactly at the cap
        assert!(
            validate_no_adverse_increase(&old, &nudged, MAX_SCHEDULE_RATE_INCREASE_BPS).is_ok()
        );
    }

    #[test]
    fn grandfathering_rejects_adverse_increase_beyond_cap() {
        let old = decreasing_tiers();
        let mut hiked = old;
        hiked[0].rate_bps = old[0].rate_bps + MAX_SCHEDULE_RATE_INCREASE_BPS + 1;
        assert!(
            validate_no_adverse_increase(&old, &hiked, MAX_SCHEDULE_RATE_INCREASE_BPS).is_err()
        );
    }

    #[test]
    fn grandfathering_allows_anything_when_no_prior_schedule() {
        let empty: [FeeTier; 0] = [];
        let new_tiers = decreasing_tiers();
        assert!(validate_no_adverse_increase(&empty, &new_tiers, 0).is_ok());
    }
}

// ---------------------------------------------------------------------------
// Early-exit penalty escrow split (issue #805)
// ---------------------------------------------------------------------------

/// Splits `escrow` between remaining depositors and the treasury by
/// `depositor_share_bps`. Returns `(depositor_slice, treasury_slice, dust)`.
/// Both slices round down independently, so `dust` (0 or a few base units)
/// is never allocated to either side — it is left for the caller to retain
/// in the escrow for the next round, so no value is ever lost to rounding.
pub fn split_penalty(escrow: i128, depositor_share_bps: u32) -> (i128, i128, i128) {
    if escrow <= 0 {
        return (0, 0, 0);
    }
    let depositor_slice =
        mul_div(escrow, depositor_share_bps as i128, BASIS_POINT_SCALE).unwrap_or(0);
    let treasury_bps = 10_000u32.saturating_sub(depositor_share_bps);
    let treasury_slice = mul_div(escrow, treasury_bps as i128, BASIS_POINT_SCALE).unwrap_or(0);
    let dust = escrow - depositor_slice - treasury_slice;
    (depositor_slice, treasury_slice, dust)
}

// ---------------------------------------------------------------------------
// Time-locked savings vault (issue #802)
// ---------------------------------------------------------------------------

/// Early-break penalty rate for a lock, decaying linearly from
/// `full_penalty_bps` at creation down to zero at maturity: someone breaking
/// a lock one second before it matures is not charged the same as someone
/// breaking it on day one. `elapsed_secs` and `total_duration_secs` describe
/// how far through the term the lock is; a lock already past maturity (this
/// function is never meant to be called for one, but defensively) returns 0.
pub fn lock_break_penalty_bps(
    full_penalty_bps: u32,
    elapsed_secs: u64,
    total_duration_secs: u64,
) -> u32 {
    if total_duration_secs == 0 || elapsed_secs >= total_duration_secs {
        return 0;
    }
    let remaining = total_duration_secs - elapsed_secs;
    // full_penalty_bps * remaining / total_duration_secs, computed in i128 to
    // reuse the same overflow-checked mul_div as everywhere else in this
    // file rather than a separate u64 path.
    mul_div(
        full_penalty_bps as i128,
        remaining as i128,
        total_duration_secs as i128,
    )
    .unwrap_or(0) as u32
}

/// New total share count for one locked position after a boosted yield
/// report, so that its value gain works out to exactly its boost-weighted
/// slice of the reported yield — see the module doc on
/// `contracts/vault/src/locks.rs` for the full derivation. Returns the
/// position's unchanged share count on any degenerate input (zero weight,
/// zero yield) rather than erroring, since those are "nothing to boost"
/// cases, not failures.
///
/// - `position_shares`: this position's current share count.
/// - `boost_bps`: this position's boost multiplier in basis points (10_000 =
///   1x, i.e. no boost).
/// - `total_assets`, `total_supply`: the vault's totals *before* this yield
///   report is applied.
/// - `total_weight`: sum over every locked position (and the flexible pool,
///   counted at 1x) of `shares * boost_bps`, using the SAME total_assets/
///   total_supply snapshot as this call — the flexible pool's own shares are
///   never minted here, only every open locked position is, one at a time,
///   all against this one shared `total_weight`.
/// - `yield_amount`: the yield being reported this round (must be positive;
///   an impairment never boosts).
#[allow(clippy::too_many_arguments)]
pub fn boosted_shares_after_yield(
    position_shares: i128,
    boost_bps: u32,
    total_assets: i128,
    total_supply: i128,
    total_weight: i128,
    yield_amount: i128,
) -> Result<i128, ContractError> {
    if position_shares <= 0 || total_weight <= 0 || yield_amount <= 0 || total_supply <= 0 {
        return Ok(position_shares);
    }

    // Derivation (plain, unscaled units first): the flexible pool's shares
    // never change count, so its post-report value is fixed at
    // `F * P_new` where P_new is the new global share price. Solving the
    // flexible pool's own conservation equation
    // `F * P_new == F*(TA/TS) + Y*F/W` (its old value plus its
    // weight-proportional slice of the yield, W = total weight) for P_new,
    // the F terms cancel and give the strikingly simple
    // `P_new = TA/TS + Y/W`. Every locked position i then just needs
    // `new_shares_i = target_value_i / P_new`, where
    // `target_value_i = s_i*(TA/TS) + Y*s_i*b_i/W` (old value plus its own
    // weight-proportional slice, at its own boost b_i). Substituting P_new
    // and simplifying: `new_shares_i = s_i * (TA*W + TS*Y*b_i) / (TA*W + TS*Y)`.
    // This conserves total value exactly (sum of every pool's post-report
    // value, including the untouched flexible pool, equals TA + Y) and
    // degenerates to `new_shares_i == s_i` at b_i == 1x (no boost, matching
    // the flexible pool's own unminted behaviour) — both verified in the
    // tests below.
    //
    // total_weight (W) and boost_bps (b_i) arrive in basis points
    // (BASIS_POINT_SCALE = 1x) rather than plain multipliers, so the plain
    // formula above is scaled by BASIS_POINT_SCALE to clear the fraction:
    // `new_shares_i = s_i * (TA*W_bps + TS*Y*boost_bps) / (TA*W_bps + TS*Y*BASIS_POINT_SCALE)`.
    // Note the asymmetry: only the yield term in the denominator gets the
    // extra BASIS_POINT_SCALE factor (matching the plain formula's bare `Y`
    // there), not the TA*W term again — getting this backwards was caught by
    // `boosted_shares_matches_the_worked_two_pool_example` during development.
    let ta_w = total_assets
        .checked_mul(total_weight)
        .ok_or(ContractError::ArithmeticOverflow)?;
    let ts_y = total_supply
        .checked_mul(yield_amount)
        .ok_or(ContractError::ArithmeticOverflow)?;

    let numerator = ta_w
        .checked_add(
            ts_y.checked_mul(boost_bps as i128)
                .ok_or(ContractError::ArithmeticOverflow)?,
        )
        .ok_or(ContractError::ArithmeticOverflow)?;
    let denominator = ta_w
        .checked_add(
            ts_y.checked_mul(BASIS_POINT_SCALE)
                .ok_or(ContractError::ArithmeticOverflow)?,
        )
        .ok_or(ContractError::ArithmeticOverflow)?;

    if denominator == 0 {
        return Ok(position_shares);
    }

    mul_div(position_shares, numerator, denominator)
}

#[cfg(test)]
mod penalty_split_tests {
    use super::*;

    #[test]
    fn split_at_zero_depositor_share_sends_everything_to_treasury() {
        let (dep, treas, dust) = split_penalty(10_000, 0);
        assert_eq!(dep, 0);
        assert_eq!(treas, 10_000);
        assert_eq!(dust, 0);
    }

    #[test]
    fn split_at_the_treasury_cap_is_even() {
        // depositor_share_bps = 5000 means treasury also gets 5000 (the cap).
        let (dep, treas, dust) = split_penalty(10_000, 5_000);
        assert_eq!(dep, 5_000);
        assert_eq!(treas, 5_000);
        assert_eq!(dust, 0);
    }

    #[test]
    fn split_rounds_down_and_retains_dust_never_loses_a_unit() {
        // 7 units at 3333 bps: depositor = 2 (2.331 floored), treasury at
        // 6667 bps = 4 (4.6669 floored) -> dust = 1, nothing lost.
        let (dep, treas, dust) = split_penalty(7, 3_333);
        assert_eq!(dep + treas + dust, 7);
        assert!((0..=2).contains(&dust));
    }

    #[test]
    fn split_of_zero_escrow_is_all_zero() {
        assert_eq!(split_penalty(0, 5_000), (0, 0, 0));
    }

    #[test]
    fn split_conserves_value_across_a_wide_sweep() {
        let mut escrow = 1i128;
        while escrow < 1_000_000 {
            for bps in [0u32, 1, 4999, 5000, 6789, 9999, 10000] {
                let (dep, treas, dust) = split_penalty(escrow, bps);
                assert_eq!(dep + treas + dust, escrow);
                assert!(dep >= 0 && treas >= 0 && dust >= 0);
            }
            escrow = escrow * 7 + 3;
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn mul_div_handles_overflow_path() {
        // a * b would overflow i128, but a/divisor * b stays in range.
        let a = i128::MAX / 2;
        let b = 4;
        let divisor = 8;
        let got = mul_div(a, b, divisor).unwrap();
        // Expected: a / 2  (since 4/8 = 1/2). Allow off-by-one rounding.
        let expected = a / 2;
        assert!((got - expected).abs() <= 1);
    }

    #[test]
    fn mul_div_zero_divisor_errors() {
        assert!(mul_div(10, 10, 0).is_err());
    }

    #[test]
    fn management_fee_no_panic_at_extreme_values() {
        // Should return Err or Ok cleanly, not panic. Two-stage mul_div keeps
        // the intermediate within range for plausible inputs and only errors
        // when the *result* exceeds i128.
        let result = calculate_management_fee(i128::MAX, u32::MAX, u64::MAX);
        assert!(result.is_ok() || result.is_err());
    }

    #[test]
    fn management_fee_handles_large_total_assets_without_intermediate_overflow() {
        // total_assets = 10^30 base units (well above any realistic TVL),
        // 10% annual fee, 30 days elapsed. The naive
        // total_assets * fee_bps intermediate would be 10^34 which still fits
        // i128 but the test guards against regressions that reintroduce a
        // raw multiplication of unbounded magnitudes.
        let total_assets: i128 = 10i128.pow(30);
        let fee_bps: u32 = 1000; // 10% annual
        let elapsed: u64 = 30 * 24 * 60 * 60;
        let fee = calculate_management_fee(total_assets, fee_bps, elapsed).unwrap();
        // Expected ~ total_assets * 0.10 * (30/365) ≈ 8.22e27.
        let lower = total_assets / 100; // 1% lower bound (10% * 30d / 365d ≈ 0.82%)
        let upper = total_assets / 10; // 10% upper bound
        assert!(fee > 0 && fee >= lower / 100 && fee <= upper);
    }

    #[test]
    fn management_fee_capped_interval_does_not_overflow() {
        // With elapsed clamped at MAX_FEE_ACCRUAL_INTERVAL_SECONDS, even a
        // pathological total_assets value must produce a finite Ok or a clean
        // Err — never a Rust panic.
        let total_assets = i128::MAX / 4;
        let fee_bps = 1000;
        let elapsed = MAX_FEE_ACCRUAL_INTERVAL_SECONDS;
        let _ = calculate_management_fee(total_assets, fee_bps, elapsed);
    }

    #[test]
    fn performance_fee_no_panic_at_extreme_values() {
        // i128::MAX * any positive bps overflows the intermediate product;
        // mul_div takes the fallback path and returns Ok.
        let result = calculate_performance_fee(i128::MAX, 1000);
        assert!(result.is_ok());
    }

    #[test]
    fn withdrawal_fee_zero_amount_returns_zero() {
        assert_eq!(calculate_withdrawal_fee(0, 100).unwrap(), 0);
        assert_eq!(calculate_withdrawal_fee(1000, 0).unwrap(), 0);
    }
}

#[cfg(test)]
mod lock_tests {
    use super::*;

    #[test]
    fn lock_break_penalty_is_full_at_creation() {
        // elapsed = 0: the full configured penalty applies.
        assert_eq!(lock_break_penalty_bps(1000, 0, 90 * 86400), 1000);
    }

    #[test]
    fn lock_break_penalty_decays_linearly_at_the_midpoint() {
        // 50% elapsed -> 50% of the full penalty remains.
        assert_eq!(lock_break_penalty_bps(1000, 45 * 86400, 90 * 86400), 500);
    }

    #[test]
    fn lock_break_penalty_is_nearly_zero_just_before_maturity() {
        // 99% elapsed -> roughly 1% of the full penalty remains.
        let bps = lock_break_penalty_bps(1000, 89 * 86400 + 86400 * 99 / 100, 90 * 86400);
        assert!(bps <= 10, "expected a small residual penalty, got {bps}");
    }

    #[test]
    fn lock_break_penalty_is_zero_at_or_after_maturity() {
        assert_eq!(lock_break_penalty_bps(1000, 90 * 86400, 90 * 86400), 0);
        assert_eq!(lock_break_penalty_bps(1000, 91 * 86400, 90 * 86400), 0);
    }

    #[test]
    fn lock_break_penalty_zero_duration_is_zero() {
        assert_eq!(lock_break_penalty_bps(1000, 0, 0), 0);
    }

    /// Matches the worked example from the design derivation: TA=1000,
    /// TS=1000, flexible=500 shares (boost 1x), locked=500 shares (boost
    /// 2x), yield=100. Locked should end up with exactly 531.25 -> 531
    /// (mul_div rounds down) shares.
    #[test]
    fn boosted_shares_matches_the_worked_two_pool_example() {
        let total_weight = 500 * 10_000 + 500 * 20_000; // flexible@1x + locked@2x, in bps-scaled shares
        let new_shares =
            boosted_shares_after_yield(500, 20_000, 1000, 1000, total_weight, 100).unwrap();
        // Exact rational answer is 531.25; integer division rounds down to 531.
        assert_eq!(new_shares, 531);
    }

    #[test]
    fn boosted_shares_at_1x_boost_is_unchanged() {
        // A boost of exactly 1x (10_000 bps) must never mint anything, since
        // that position's shares already appreciate correctly through the
        // ordinary share-price rise, same as the flexible pool.
        let total_weight = 1000 * 10_000; // single pool, all at 1x
        let new_shares =
            boosted_shares_after_yield(1000, 10_000, 1000, 1000, total_weight, 100).unwrap();
        assert_eq!(new_shares, 1000);
    }

    #[test]
    fn boosted_shares_conserves_total_value_across_all_pools() {
        // Full end-to-end conservation check, not just one pool's share
        // count: flexible (500 @ 1x) keeps its share count; locked (500 @
        // 2x) mints via the function under test. Total post-yield value
        // (at the resulting share price) must equal total_assets + yield,
        // to within integer-rounding dust.
        let flexible_shares: i128 = 500;
        let locked_shares: i128 = 500;
        let boost_bps: u32 = 20_000;
        let total_assets: i128 = 1000;
        let total_supply: i128 = 1000;
        let yield_amount: i128 = 100;
        let total_weight = flexible_shares * 10_000 + locked_shares * boost_bps as i128;

        let locked_new_shares = boosted_shares_after_yield(
            locked_shares,
            boost_bps,
            total_assets,
            total_supply,
            total_weight,
            yield_amount,
        )
        .unwrap();
        let minted = locked_new_shares - locked_shares;
        let new_total_supply = total_supply + minted;
        let new_total_assets = total_assets + yield_amount;

        let flexible_value = flexible_shares * new_total_assets / new_total_supply;
        let locked_value = locked_new_shares * new_total_assets / new_total_supply;
        let total_value = flexible_value + locked_value;

        // Rounding dust is at most a handful of base units on these small
        // numbers; the total must never exceed the real assets (that would
        // be inflation) and must be very close to it (not badly short).
        assert!(total_value <= new_total_assets);
        assert!(total_value >= new_total_assets - 2);
    }

    #[test]
    fn boosted_shares_no_panic_at_extreme_values() {
        let result = boosted_shares_after_yield(
            i128::MAX / 4,
            50_000,
            i128::MAX / 4,
            i128::MAX / 4,
            i128::MAX / 4,
            i128::MAX / 4,
        );
        assert!(result.is_ok() || result.is_err());
    }

    #[test]
    fn boosted_shares_zero_yield_is_unchanged() {
        assert_eq!(
            boosted_shares_after_yield(500, 20_000, 1000, 1000, 1_500_000, 0).unwrap(),
            500
        );
    }
}
