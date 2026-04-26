use crate::ContractError;

pub const BASIS_POINT_SCALE: i128 = 10000;
pub const SECONDS_PER_YEAR: i128 = 31536000;

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

    // fee = total_assets * (bps/10000) * (elapsed / seconds_per_year)
    let bps_term = total_assets
        .checked_mul(management_fee_bps as i128)
        .ok_or(ContractError::ArithmeticOverflow)?;
    let denom = BASIS_POINT_SCALE
        .checked_mul(SECONDS_PER_YEAR)
        .ok_or(ContractError::ArithmeticOverflow)?;
    mul_div(bps_term, elapsed_seconds as i128, denom)
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
        // Should return Err, not panic.
        let result = calculate_management_fee(i128::MAX, u32::MAX, u64::MAX);
        assert!(result.is_err());
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
