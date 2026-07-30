//! Slippage-safe multi-hop rebalance planning (issue #810).
//!
//! A rebalance is a sequence of legs (withdrawals from over-weighted
//! sources, deposits into under-weighted ones). Applying one aggregate
//! slippage tolerance to that sequence lets an adversary manipulate a single
//! leg's price, extract value there, and still land inside the aggregate
//! tolerance absorbed by the other legs' good execution. This module builds
//! a plan where **every leg** carries its own `min_out`, and provides the
//! pure comparison used to detect a stale plan (prices moved between
//! `plan_rebalance` and `execute_rebalance`) without any cross-contract
//! calls — so it can be unit-tested against hand-constructed delta lists.

use nester_common::fees::mul_div;
use soroban_sdk::{contracttype, Env, Symbol, Vec};

/// Hard ceiling on the per-leg slippage tolerance. Stored configuration
/// (`rebalance_slippage` combined with an admin-set `max_leg_slippage_bps`)
/// can only ever *tighten* this, never loosen it — an admin key compromise
/// cannot authorise a leg to lose more than this many bps.
pub const MAX_LEG_SLIPPAGE_BPS_CEILING: u32 = 1_000; // 10%

/// Default cap on the fraction of vault TVL a single rebalance call may
/// move, bounding both legitimate churn and the blast radius of a
/// compromised keeper.
pub const DEFAULT_MAX_REBALANCE_VALUE_BPS: u32 = 2_000; // 20%

/// How far a submitted plan's per-leg amounts may drift from what the
/// contract would compute right now before `execute_rebalance` rejects it
/// as stale. This is intentionally a tolerance, not exact-hash equality:
/// legitimate price movement between `plan_rebalance` and `execute_rebalance`
/// must not always be treated as an attack, but a large deviation should be.
pub const PLAN_STALENESS_TOLERANCE_BPS: u32 = 200; // 2%

/// One rebalance leg: move `delta` into (positive) or out of (negative)
/// `source_id`. `min_out` is only meaningful for withdrawal legs
/// (`delta < 0`) — the minimum assets that must be realised or the whole
/// call reverts.
#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct RebalanceLeg {
    pub source_id: Symbol,
    pub delta: i128,
    pub min_out: i128,
}

/// A raw (source, delta) pair as returned by the allocation strategy's
/// `calculate_rebalance_deltas` — this module's input shape, kept separate
/// from the vault's own `AllocationDeltaView` so `rebalance.rs` has no
/// dependency on the surrounding vault contract's types.
#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct DeltaInput {
    pub source_id: Symbol,
    pub delta: i128,
}

/// Build a slippage-safe execution plan from raw strategy deltas.
///
/// `min_assets_out_for_withdrawal` converts a gross withdrawal amount into
/// its slippage-adjusted floor (injected so this function has no vault-token
/// contract dependency — the real vault passes a closure around
/// `rebalance_min_assets_out`, tests pass a simple percentage function).
///
/// Returns the plan plus the total absolute value moved across all legs, so
/// the caller can enforce the per-rebalance value cap.
pub fn build_plan(
    env: &Env,
    deltas: &Vec<DeltaInput>,
    min_assets_out_for_withdrawal: impl Fn(i128) -> i128,
) -> (Vec<RebalanceLeg>, i128) {
    let mut plan = Vec::new(env);
    let mut total_moved: i128 = 0;

    for d in deltas.iter() {
        if d.delta == 0 {
            continue;
        }
        total_moved = total_moved.saturating_add(d.delta.abs());

        let min_out = if d.delta < 0 {
            min_assets_out_for_withdrawal(-d.delta)
        } else {
            0
        };

        plan.push_back(RebalanceLeg {
            source_id: d.source_id.clone(),
            delta: d.delta,
            min_out,
        });
    }

    (plan, total_moved)
}

/// Cheap integrity/commitment checksum over a plan's amounts and ordering.
/// Not a cryptographic hash of the `Symbol` contents (Soroban's `no_std`
/// environment has no convenient byte-serialisation for arbitrary
/// contract types without an `Env`-bound allocation) — order and amount are
/// the security-relevant fields here, since [`plan_matches_fresh`] separately
/// re-validates every `source_id` by direct equality against a freshly
/// computed plan.
pub fn checksum(legs: &Vec<RebalanceLeg>) -> u64 {
    let mut h: u64 = 1_469_598_103_934_665_603; // FNV-1a offset basis
    const PRIME: u64 = 1_099_511_628_211;

    for (i, leg) in legs.iter().enumerate() {
        for b in leg.delta.to_be_bytes() {
            h = (h ^ b as u64).wrapping_mul(PRIME);
        }
        for b in leg.min_out.to_be_bytes() {
            h = (h ^ b as u64).wrapping_mul(PRIME);
        }
        h = (h ^ i as u64).wrapping_mul(PRIME);
    }
    h
}

/// True when `submitted` matches `fresh` — same legs, same order, same
/// sign, and each leg's `delta` within `tolerance_bps` of the freshly
/// computed value. Used by `execute_rebalance` to reject a plan built
/// against prices that have since moved more than the allowed tolerance
/// (`PlanStale`).
pub fn plan_matches_fresh(
    fresh: &Vec<RebalanceLeg>,
    submitted: &Vec<RebalanceLeg>,
    tolerance_bps: u32,
) -> bool {
    if fresh.len() != submitted.len() {
        return false;
    }
    for i in 0..fresh.len() {
        let f = fresh.get(i).unwrap();
        let s = submitted.get(i).unwrap();
        if f.source_id != s.source_id {
            return false;
        }
        if (f.delta < 0) != (s.delta < 0) {
            return false;
        }
        let diff = (f.delta - s.delta).abs();
        let base = f.delta.abs().max(1);
        let allowed = mul_div(base, tolerance_bps as i128, 10_000).unwrap_or(0);
        if diff > allowed {
            return false;
        }
    }
    true
}

#[cfg(test)]
mod tests {
    use super::*;

    fn deltas(env: &Env, pairs: &[(&str, i128)]) -> Vec<DeltaInput> {
        let mut v = Vec::new(env);
        for (sym, delta) in pairs {
            v.push_back(DeltaInput {
                source_id: Symbol::new(env, sym),
                delta: *delta,
            });
        }
        v
    }

    #[test]
    fn every_withdrawal_leg_carries_a_min_out() {
        let env = Env::default();
        let d = deltas(&env, &[("aave", -1_000_000), ("blend", 1_000_000)]);
        let (plan, total_moved) = build_plan(&env, &d, |gross| gross * 99 / 100);

        assert_eq!(total_moved, 2_000_000);
        let withdraw_leg = plan.get(0).unwrap();
        assert_eq!(withdraw_leg.delta, -1_000_000);
        assert_eq!(withdraw_leg.min_out, 990_000);

        let deposit_leg = plan.get(1).unwrap();
        assert_eq!(deposit_leg.min_out, 0, "deposit legs have no min_out floor");
    }

    #[test]
    fn zero_delta_legs_are_skipped() {
        let env = Env::default();
        let d = deltas(&env, &[("aave", 0), ("blend", 500)]);
        let (plan, _) = build_plan(&env, &d, |g| g);
        assert_eq!(plan.len(), 1);
    }

    #[test]
    fn checksum_is_stable_for_identical_plans() {
        let env = Env::default();
        let d = deltas(&env, &[("aave", -1_000_000), ("blend", 1_000_000)]);
        let (plan_a, _) = build_plan(&env, &d, |g| g * 99 / 100);
        let (plan_b, _) = build_plan(&env, &d, |g| g * 99 / 100);
        assert_eq!(checksum(&plan_a), checksum(&plan_b));
    }

    #[test]
    fn checksum_changes_when_amount_changes() {
        let env = Env::default();
        let d1 = deltas(&env, &[("aave", -1_000_000)]);
        let d2 = deltas(&env, &[("aave", -1_000_001)]);
        let (plan1, _) = build_plan(&env, &d1, |g| g);
        let (plan2, _) = build_plan(&env, &d2, |g| g);
        assert_ne!(checksum(&plan1), checksum(&plan2));
    }

    #[test]
    fn plan_within_tolerance_is_accepted() {
        let env = Env::default();
        let fresh_d = deltas(&env, &[("aave", -1_000_000)]);
        let (fresh, _) = build_plan(&env, &fresh_d, |g| g * 99 / 100);

        // Submitted plan drifted by 1%, well inside the 2% tolerance.
        let submitted_d = deltas(&env, &[("aave", -990_000)]);
        let (submitted, _) = build_plan(&env, &submitted_d, |g| g * 99 / 100);

        assert!(plan_matches_fresh(&fresh, &submitted, 200));
    }

    #[test]
    fn plan_beyond_tolerance_is_stale() {
        let env = Env::default();
        let fresh_d = deltas(&env, &[("aave", -1_000_000)]);
        let (fresh, _) = build_plan(&env, &fresh_d, |g| g * 99 / 100);

        // 10% drift — well beyond the 2% tolerance.
        let submitted_d = deltas(&env, &[("aave", -900_000)]);
        let (submitted, _) = build_plan(&env, &submitted_d, |g| g * 99 / 100);

        assert!(!plan_matches_fresh(&fresh, &submitted, 200));
    }

    #[test]
    fn plan_with_different_source_is_stale() {
        let env = Env::default();
        let fresh_d = deltas(&env, &[("aave", -1_000_000)]);
        let (fresh, _) = build_plan(&env, &fresh_d, |g| g);
        let submitted_d = deltas(&env, &[("blend", -1_000_000)]);
        let (submitted, _) = build_plan(&env, &submitted_d, |g| g);
        assert!(!plan_matches_fresh(&fresh, &submitted, 10_000));
    }

    #[test]
    fn plan_with_different_leg_count_is_stale() {
        let env = Env::default();
        let fresh_d = deltas(&env, &[("aave", -1_000_000), ("blend", 1_000_000)]);
        let (fresh, _) = build_plan(&env, &fresh_d, |g| g);
        let submitted_d = deltas(&env, &[("aave", -1_000_000)]);
        let (submitted, _) = build_plan(&env, &submitted_d, |g| g);
        assert!(!plan_matches_fresh(&fresh, &submitted, 10_000));
    }
}
