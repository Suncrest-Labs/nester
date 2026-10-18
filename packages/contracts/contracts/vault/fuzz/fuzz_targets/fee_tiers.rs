#![no_main]

use arbitrary::Arbitrary;
use libfuzzer_sys::fuzz_target;
use nester_common::fees::{
    calculate_management_fee, calculate_performance_fee, calculate_withdrawal_fee, next_boundary,
    rate_at, validate_no_adverse_increase, validate_tiers, FeeTier, BASIS_POINT_SCALE,
    MAX_FEE_TIERS,
};

#[derive(Debug, Arbitrary, Clone, Copy)]
struct RawTier {
    threshold: i64,
    rate_bps: u16,
}

#[derive(Debug, Arbitrary)]
struct FeeFuzzInput {
    tiers: Vec<RawTier>,
    updated_tiers: Vec<RawTier>,
    rate_ceiling: u16,
    max_delta_bps: u16,
    query_x: i64,
    amount: i64,
    elapsed_seconds: u32,
}

fn to_fee_tiers(raw: &[RawTier]) -> Vec<FeeTier> {
    raw.iter()
        .take(MAX_FEE_TIERS + 2)
        .map(|t| FeeTier {
            threshold: t.threshold as i128,
            rate_bps: (t.rate_bps as u32).min(BASIS_POINT_SCALE as u32),
        })
        .collect()
}

fuzz_target!(|input: FeeFuzzInput| {
    let tiers = to_fee_tiers(&input.tiers);
    let updated_tiers = to_fee_tiers(&input.updated_tiers);
    let ceiling = (input.rate_ceiling as u32).min(BASIS_POINT_SCALE as u32);
    let x = input.query_x as i128;

    let valid = validate_tiers(&tiers, ceiling).is_ok();
    if valid && !tiers.is_empty() {
        let r = rate_at(&tiers, x);
        let min_rate = tiers.iter().map(|t| t.rate_bps).min().unwrap_or(0);
        let max_rate = tiers.iter().map(|t| t.rate_bps).max().unwrap_or(0);
        assert!(
            r >= min_rate && r <= max_rate,
            "interpolated rate {r} outside [{min_rate}, {max_rate}]"
        );
        assert!(r <= ceiling, "interpolated rate {r} exceeded ceiling {ceiling}");

        if let Some(nb) = next_boundary(&tiers, x) {
            assert!(nb.threshold > x, "next_boundary threshold <= query x");
        } else {
            let last_threshold = tiers.last().unwrap().threshold;
            assert!(
                x >= last_threshold,
                "next_boundary returned None before last threshold"
            );
        }

        // Fee calculations at the interpolated rate must never exceed the input amount
        let amount = (input.amount as i128).max(0);
        if let Ok(exit_fee) = calculate_withdrawal_fee(amount, r) {
            assert!(
                exit_fee >= 0 && exit_fee <= amount,
                "exit_fee {exit_fee} out of bounds for amount {amount}"
            );
        }
        if let Ok(perf_fee) = calculate_performance_fee(amount, r) {
            assert!(
                perf_fee >= 0 && perf_fee <= amount,
                "perf_fee {perf_fee} out of bounds for amount {amount}"
            );
        }
        if let Ok(mgmt_fee) =
            calculate_management_fee(amount, r, input.elapsed_seconds as u64)
        {
            assert!(mgmt_fee >= 0, "mgmt_fee must be non-negative");
        }

        if validate_tiers(&updated_tiers, ceiling).is_ok() {
            let max_delta = input.max_delta_bps as u32;
            if validate_no_adverse_increase(&tiers, &updated_tiers, max_delta).is_ok() {
                let old_r = rate_at(&tiers, x) as i128;
                let new_r = rate_at(&updated_tiers, x) as i128;
                // Allow at most 1 bps interpolation rounding slack
                assert!(
                    new_r - old_r <= (max_delta as i128) + 1,
                    "adverse increase at x={x}: old={old_r}, new={new_r}, max_delta={max_delta}"
                );
            }
        }
    }
});
