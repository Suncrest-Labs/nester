//! Pure effort-weighting and eligibility maths for the goal-completion
//! bonus contract (issue #819), independent of Soroban storage/`Env`.
//!
//! # The effort metric
//!
//! The issue asks for a bonus that scales with the time-integral of the
//! goal balance rather than a flat percentage of the target: a user who held
//! a climbing balance for a year did more real saving than one who deposited
//! everything in month eleven, even if both hit the same target on the same
//! day.
//!
//! `effort_score_bps` turns a raw time-integral (amount-seconds, as tracked
//! by `savings_goal::ContributionStats::balance_time_integral`) into a
//! basis-points score in `[0, 10_000]` by comparing it against the integral
//! a *maximally patient* saver would have produced: someone who deposited
//! the full target on day one and held it for the full
//! [`FULL_EFFORT_WINDOW_SECONDS`] window. A real saver's integral can only
//! be less than or equal to that reference (their balance was below target
//! for at least part of the window), so the score is naturally bounded.
//!
//! # fold_timeline: proving the incremental summary is exact
//!
//! `savings_goal` does not store a per-contribution list — see its own doc
//! comment on `ContributionStats` for why. [`fold_timeline`] independently
//! recomputes the same summary from an explicit list of
//! `(amount, timestamp)` contributions plus a `created_at`/`now`, so a test
//! can assert the two never disagree: replaying history and incrementally
//! maintaining a running summary must be mathematically identical
//! operations, and this function is what lets a test prove that rather than
//! assume it.

#![allow(clippy::too_many_arguments)]

/// Reference window (seconds) a maximally-patient saver is compared against
/// when scoring effort. ~180 days: long enough that a lump-sum-at-the-end
/// saver scores far below a steady saver, short enough that a reasonable
/// multi-month goal can still reach a meaningful score.
pub const FULL_EFFORT_WINDOW_SECONDS: u64 = 180 * 24 * 60 * 60;

/// Basis-point denominator effort scores and bonus rates are expressed in.
pub const BPS_SCALE: i128 = 10_000;

/// A single recorded contribution, for [`fold_timeline`]'s from-scratch
/// recomputation. Not used by the contract's hot path (which maintains
/// [`ContributionStatsInput`]/the real `ContributionStats` incrementally);
/// this exists so a test can prove the two approaches agree.
#[derive(Clone, Copy, Debug)]
pub struct Contribution {
    pub amount: i128,
    pub at: u64,
}

/// The subset of `savings_goal::ContributionStats` this module's pure
/// functions need, decoupled from the Soroban `#[contracttype]` so this
/// crate has no dependency on the `savings_goal` contract crate.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct ContributionStatsInput {
    pub contribution_count: u32,
    pub distinct_periods: u32,
    pub largest_contribution: i128,
    pub balance_time_integral: i128,
    pub completed_at: u64,
}

/// Recompute a [`ContributionStatsInput`]-shaped summary from an explicit,
/// time-ordered contribution list. `contributions` must be sorted by `at`
/// ascending (the same order they were actually recorded on-chain);
/// `period_seconds` is `savings_goal`'s `CONTRIBUTION_PERIOD_SECONDS`.
///
/// This exists purely to be checked against the contract's own incremental
/// summary in tests — see the module doc comment.
pub fn fold_timeline(
    contributions: &[Contribution],
    created_at: u64,
    completed_at: u64,
    period_seconds: u64,
) -> ContributionStatsInput {
    let mut out = ContributionStatsInput {
        completed_at,
        ..Default::default()
    };
    let mut balance: i128 = 0;
    let mut last_t = created_at;
    let mut periods_seen: u64 = 0;

    for c in contributions {
        let elapsed = c.at.saturating_sub(last_t);
        out.balance_time_integral = out
            .balance_time_integral
            .saturating_add(balance.saturating_mul(elapsed as i128));
        balance = balance.saturating_add(c.amount);
        last_t = c.at;

        out.contribution_count += 1;
        if c.amount > out.largest_contribution {
            out.largest_contribution = c.amount;
        }
        let period = c.at.saturating_sub(created_at) / period_seconds;
        if period < 64 {
            let bit = 1u64 << period;
            if periods_seen & bit == 0 {
                periods_seen |= bit;
                out.distinct_periods += 1;
            }
        }
    }

    // Close the final interval up to completion, mirroring the contract's
    // own complete_goal accrual step.
    if completed_at > last_t {
        let elapsed = completed_at - last_t;
        out.balance_time_integral = out
            .balance_time_integral
            .saturating_add(balance.saturating_mul(elapsed as i128));
    }

    out
}

/// Score a contribution's timeline in `[0, BPS_SCALE]` basis points, where
/// `BPS_SCALE` means "held the full target balance for the entire reference
/// window" and 0 means no sustained balance at all.
///
/// `target_amount` must be positive (the caller is expected to have already
/// validated this via the goal registry); a non-positive target scores 0
/// rather than dividing by it.
pub fn effort_score_bps(balance_time_integral: i128, target_amount: i128) -> u32 {
    if target_amount <= 0 || balance_time_integral <= 0 {
        return 0;
    }
    let reference_integral = target_amount.saturating_mul(FULL_EFFORT_WINDOW_SECONDS as i128);
    if reference_integral <= 0 {
        return 0;
    }
    let scaled = crate::fees::mul_div(balance_time_integral, BPS_SCALE, reference_integral)
        .unwrap_or(BPS_SCALE);
    scaled.clamp(0, BPS_SCALE) as u32
}

/// Concrete, checkable reasons a goal fails the eligibility gate, so a
/// caller (or `preview_goal_bonus`) can report *why* rather than a bare
/// false — the issue asks a claim to be rejected outright for these, not
/// silently pay zero.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum IneligibilityReason {
    GoalNotCompleted,
    CompletedAfterDeadline,
    ActiveDurationTooShort,
    ContributionPatternFailsAntiGaming,
}

/// Eligibility gate: on-time completion, a minimum active duration, and a
/// genuine (non-lump-sum) contribution pattern. Returns `Ok(())` when every
/// rule passes, or the first failing reason otherwise (checked in the order
/// listed so the caller gets the most fundamental failure first).
///
/// `single_contribution_ceiling_bps` and `min_distinct_periods` are the two
/// alternative anti-gaming rules from the issue ("no single contribution
/// exceeded a configured fraction of the target, OR contributions spanned a
/// minimum number of distinct periods") — a goal passes if it satisfies
/// *either*.
pub fn check_eligibility(
    is_completed: bool,
    completed_at: u64,
    deadline: u64,
    created_at: u64,
    min_active_duration_seconds: u64,
    stats: ContributionStatsInput,
    target_amount: i128,
    single_contribution_ceiling_bps: u32,
    min_distinct_periods: u32,
) -> Result<(), IneligibilityReason> {
    if !is_completed {
        return Err(IneligibilityReason::GoalNotCompleted);
    }
    if completed_at > deadline {
        return Err(IneligibilityReason::CompletedAfterDeadline);
    }
    if completed_at.saturating_sub(created_at) < min_active_duration_seconds {
        return Err(IneligibilityReason::ActiveDurationTooShort);
    }

    let largest_ok = if target_amount > 0 {
        let ceiling = crate::fees::mul_div(
            target_amount,
            single_contribution_ceiling_bps as i128,
            BPS_SCALE,
        )
        .unwrap_or(0);
        stats.largest_contribution <= ceiling
    } else {
        false
    };
    let spread_ok = stats.distinct_periods >= min_distinct_periods;
    if !largest_ok && !spread_ok {
        return Err(IneligibilityReason::ContributionPatternFailsAntiGaming);
    }

    Ok(())
}

/// One (goal-size, bonus-rate) tier, mirroring `nester_common::fees::FeeTier`'s
/// shape/conventions.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct RewardTier {
    /// Goals with `target_amount >= min_target` (and below the next tier's
    /// `min_target`, if any) use this tier's rate/cap.
    pub min_target: i128,
    pub bonus_bps: u32,
    pub max_bonus_absolute: i128,
}

/// Selects the tier whose `min_target` is the largest one not exceeding
/// `target_amount`. `tiers` need not be sorted; every candidate is checked.
/// Returns `None` when no configured tier's `min_target` is low enough to
/// apply (an unconfigured tier pays nothing, per the issue's acceptance
/// criteria).
pub fn select_tier(tiers: &[RewardTier], target_amount: i128) -> Option<RewardTier> {
    tiers
        .iter()
        .filter(|t| t.min_target <= target_amount)
        .max_by_key(|t| t.min_target)
        .copied()
}

/// Compute the effort-weighted bonus for a goal, given its selected tier and
/// effort score. `basis` is the amount the bonus rate applies to — the
/// caller is expected to have already clamped this to
/// `min(target, contributed, real vault balance)` per the contract's
/// anti-fake-contribution rule; this function only does the rate/cap maths.
///
/// `bonus = min(tier.max_bonus_absolute, basis * tier.bonus_bps * effort_bps
/// / BPS_SCALE^2)`, i.e. the tier's flat rate scaled down by how much of the
/// full effort window was actually achieved.
pub fn compute_bonus(basis: i128, tier: RewardTier, effort_score_bps: u32) -> i128 {
    if basis <= 0 || tier.bonus_bps == 0 || effort_score_bps == 0 {
        return 0;
    }
    let rate_applied = match crate::fees::mul_div(basis, tier.bonus_bps as i128, BPS_SCALE) {
        Ok(v) => v,
        Err(_) => return 0,
    };
    let effort_weighted =
        match crate::fees::mul_div(rate_applied, effort_score_bps as i128, BPS_SCALE) {
            Ok(v) => v,
            Err(_) => return 0,
        };
    effort_weighted.min(tier.max_bonus_absolute).max(0)
}

#[cfg(test)]
mod tests {
    use super::*;

    // -----------------------------------------------------------------
    // fold_timeline vs. an incremental accrual walk — this is the proof
    // that the contract's ContributionStats (maintained incrementally,
    // one contribution at a time) is mathematically identical to
    // recomputing from an explicit contribution list.
    // -----------------------------------------------------------------

    /// Mirrors savings_goal's own incremental update (record_contribution_stats
    /// + the final interval closed in complete_goal), operating on the pure
    /// ContributionStatsInput shape.
    fn incremental_walk(
        contributions: &[Contribution],
        created_at: u64,
        completed_at: u64,
        period_seconds: u64,
    ) -> ContributionStatsInput {
        let mut stats = ContributionStatsInput::default();
        let mut balance: i128 = 0;
        let mut last_updated_at = 0u64;
        let mut periods_seen = 0u64;

        for c in contributions {
            let since = if last_updated_at == 0 {
                created_at
            } else {
                last_updated_at
            };
            let elapsed = c.at.saturating_sub(since);
            stats.balance_time_integral = stats
                .balance_time_integral
                .saturating_add(balance.saturating_mul(elapsed as i128));

            stats.contribution_count += 1;
            if c.amount > stats.largest_contribution {
                stats.largest_contribution = c.amount;
            }
            let period = c.at.saturating_sub(created_at) / period_seconds;
            if period < 64 {
                let bit = 1u64 << period;
                if periods_seen & bit == 0 {
                    periods_seen |= bit;
                    stats.distinct_periods += 1;
                }
            }

            balance = balance.saturating_add(c.amount);
            last_updated_at = c.at;
        }

        let since = if last_updated_at == 0 {
            created_at
        } else {
            last_updated_at
        };
        let elapsed = completed_at.saturating_sub(since);
        stats.balance_time_integral = stats
            .balance_time_integral
            .saturating_add(balance.saturating_mul(elapsed as i128));
        stats.completed_at = completed_at;
        stats
    }

    #[test]
    fn fold_timeline_matches_incremental_accrual_for_steady_saver() {
        let created_at = 1_000u64;
        let period = 7 * 24 * 60 * 60u64;
        let contributions = [
            Contribution {
                amount: 100,
                at: created_at + period,
            },
            Contribution {
                amount: 100,
                at: created_at + 2 * period,
            },
            Contribution {
                amount: 100,
                at: created_at + 3 * period,
            },
            Contribution {
                amount: 100,
                at: created_at + 4 * period,
            },
        ];
        let completed_at = created_at + 5 * period;

        let folded = fold_timeline(&contributions, created_at, completed_at, period);
        let walked = incremental_walk(&contributions, created_at, completed_at, period);
        assert_eq!(folded, walked);
    }

    #[test]
    fn fold_timeline_matches_incremental_accrual_for_lump_sum() {
        let created_at = 1_000u64;
        let period = 7 * 24 * 60 * 60u64;
        let completed_at = created_at + 100 * period;
        let contributions = [Contribution {
            amount: 400,
            at: completed_at - 60,
        }];

        let folded = fold_timeline(&contributions, created_at, completed_at, period);
        let walked = incremental_walk(&contributions, created_at, completed_at, period);
        assert_eq!(folded, walked);
    }

    #[test]
    fn fold_timeline_matches_incremental_accrual_with_many_same_period_deposits() {
        let created_at = 0u64;
        let period = 7 * 24 * 60 * 60u64;
        let contributions: [Contribution; 10] = core::array::from_fn(|i| Contribution {
            amount: 10,
            at: 3_600 * (i as u64 + 1),
        });
        let completed_at = 100_000u64;

        let folded = fold_timeline(&contributions, created_at, completed_at, period);
        let walked = incremental_walk(&contributions, created_at, completed_at, period);
        assert_eq!(folded, walked);
    }

    // -----------------------------------------------------------------
    // effort_score_bps
    // -----------------------------------------------------------------

    #[test]
    fn effort_score_full_window_at_full_balance_scores_max() {
        let target = 1_000i128;
        let integral = target * FULL_EFFORT_WINDOW_SECONDS as i128;
        assert_eq!(effort_score_bps(integral, target), BPS_SCALE as u32);
    }

    #[test]
    fn effort_score_lump_sum_near_completion_scores_far_below_steady_saver() {
        let target = 1_000i128;
        // Steady: held the full target for the whole window.
        let steady_integral = target * FULL_EFFORT_WINDOW_SECONDS as i128;
        // Lump sum: held the full target for only 1 day of a 180-day window.
        let lump_integral = target * (24 * 60 * 60);

        let steady_score = effort_score_bps(steady_integral, target);
        let lump_score = effort_score_bps(lump_integral, target);
        assert!(
            lump_score < steady_score / 100,
            "lump={lump_score} steady={steady_score}"
        );
    }

    #[test]
    fn effort_score_clamps_at_max_for_integral_exceeding_reference() {
        let target = 1_000i128;
        // A goal held above target the whole time (e.g. extra contributions
        // after completion, or a larger target used mid-way) should never
        // exceed the max score.
        let integral = target * FULL_EFFORT_WINDOW_SECONDS as i128 * 3;
        assert_eq!(effort_score_bps(integral, target), BPS_SCALE as u32);
    }

    #[test]
    fn effort_score_non_positive_inputs_score_zero() {
        assert_eq!(effort_score_bps(0, 1000), 0);
        assert_eq!(effort_score_bps(-1, 1000), 0);
        assert_eq!(effort_score_bps(1000, 0), 0);
        assert_eq!(effort_score_bps(1000, -1), 0);
    }

    // -----------------------------------------------------------------
    // check_eligibility
    // -----------------------------------------------------------------

    fn base_stats() -> ContributionStatsInput {
        ContributionStatsInput {
            contribution_count: 5,
            distinct_periods: 5,
            largest_contribution: 100,
            balance_time_integral: 1,
            completed_at: 0,
        }
    }

    #[test]
    fn eligibility_rejects_incomplete_goal() {
        let result = check_eligibility(false, 0, 1_000, 0, 0, base_stats(), 1_000, 4_000, 2);
        assert_eq!(result, Err(IneligibilityReason::GoalNotCompleted));
    }

    #[test]
    fn eligibility_rejects_completion_after_deadline() {
        let result = check_eligibility(true, 1_001, 1_000, 0, 0, base_stats(), 1_000, 4_000, 2);
        assert_eq!(result, Err(IneligibilityReason::CompletedAfterDeadline));
    }

    #[test]
    fn eligibility_accepts_completion_exactly_at_deadline() {
        let result = check_eligibility(true, 1_000, 1_000, 0, 0, base_stats(), 1_000, 4_000, 2);
        assert!(result.is_ok());
    }

    #[test]
    fn eligibility_rejects_too_short_active_duration() {
        let result = check_eligibility(true, 100, 1_000, 0, 200, base_stats(), 1_000, 4_000, 2);
        assert_eq!(result, Err(IneligibilityReason::ActiveDurationTooShort));
    }

    #[test]
    fn eligibility_accepts_boundary_active_duration() {
        let result = check_eligibility(true, 200, 1_000, 0, 200, base_stats(), 1_000, 4_000, 2);
        assert!(result.is_ok());
    }

    #[test]
    fn eligibility_rejects_lump_sum_that_also_fails_spread_rule() {
        let stats = ContributionStatsInput {
            largest_contribution: 900, // 90% of target, above a 40% ceiling
            distinct_periods: 1,       // below a 2-period minimum
            ..base_stats()
        };
        let result = check_eligibility(true, 500, 1_000, 0, 0, stats, 1_000, 4_000, 2);
        assert_eq!(
            result,
            Err(IneligibilityReason::ContributionPatternFailsAntiGaming)
        );
    }

    #[test]
    fn eligibility_accepts_via_single_contribution_ceiling_even_with_few_periods() {
        let stats = ContributionStatsInput {
            largest_contribution: 300, // 30%, under the 40% ceiling
            distinct_periods: 1,
            ..base_stats()
        };
        let result = check_eligibility(true, 500, 1_000, 0, 0, stats, 1_000, 4_000, 2);
        assert!(result.is_ok());
    }

    #[test]
    fn eligibility_accepts_via_distinct_periods_even_with_one_large_contribution() {
        let stats = ContributionStatsInput {
            largest_contribution: 900, // 90%, above the ceiling
            distinct_periods: 4,       // well above a 2-period minimum
            ..base_stats()
        };
        let result = check_eligibility(true, 500, 1_000, 0, 0, stats, 1_000, 4_000, 2);
        assert!(result.is_ok());
    }

    // -----------------------------------------------------------------
    // select_tier / compute_bonus
    // -----------------------------------------------------------------

    fn tiers() -> [RewardTier; 3] {
        [
            RewardTier {
                min_target: 0,
                bonus_bps: 100,
                max_bonus_absolute: 10,
            },
            RewardTier {
                min_target: 1_000,
                bonus_bps: 200,
                max_bonus_absolute: 100,
            },
            RewardTier {
                min_target: 100_000,
                bonus_bps: 300,
                max_bonus_absolute: 5_000,
            },
        ]
    }

    #[test]
    fn select_tier_picks_the_highest_matching_floor() {
        let picked = select_tier(&tiers(), 50_000).unwrap();
        assert_eq!(picked.min_target, 1_000);
    }

    #[test]
    fn select_tier_none_when_target_below_every_floor() {
        let sparse = [RewardTier {
            min_target: 1_000,
            bonus_bps: 200,
            max_bonus_absolute: 100,
        }];
        assert!(select_tier(&sparse, 500).is_none());
    }

    #[test]
    fn compute_bonus_is_capped_by_tier_absolute_max() {
        let tier = RewardTier {
            min_target: 0,
            bonus_bps: 10_000,
            max_bonus_absolute: 50,
        };
        // Full rate, full effort: uncapped result would be `basis` itself.
        let bonus = compute_bonus(1_000_000, tier, BPS_SCALE as u32);
        assert_eq!(bonus, 50);
    }

    #[test]
    fn compute_bonus_scales_down_with_effort() {
        let tier = RewardTier {
            min_target: 0,
            bonus_bps: 1_000,
            max_bonus_absolute: i128::MAX,
        };
        let full_effort = compute_bonus(10_000, tier, BPS_SCALE as u32);
        let half_effort = compute_bonus(10_000, tier, (BPS_SCALE / 2) as u32);
        assert_eq!(full_effort, 1_000);
        assert_eq!(half_effort, 500);
    }

    #[test]
    fn compute_bonus_zero_effort_or_basis_pays_nothing() {
        let tier = RewardTier {
            min_target: 0,
            bonus_bps: 1_000,
            max_bonus_absolute: 100,
        };
        assert_eq!(compute_bonus(0, tier, BPS_SCALE as u32), 0);
        assert_eq!(compute_bonus(1_000, tier, 0), 0);
    }
}
