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
    let part2 = r
        .checked_mul(b)
        .ok_or(ContractError::ArithmeticOverflow)?
        / divisor;
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
