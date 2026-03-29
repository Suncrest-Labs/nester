

pub const BASIS_POINT_SCALE: i128 = 10000;
pub const SECONDS_PER_YEAR: i128 = 31536000;

pub fn calculate_management_fee(
    total_assets: i128,
    management_fee_bps: u32,
    elapsed_seconds: u64,
) -> i128 {
    if total_assets <= 0 || management_fee_bps == 0 || elapsed_seconds == 0 {
        return 0;
    }

    // fee = total_assets * (bps/10000) * (elapsed / seconds_per_year)
    total_assets
        .checked_mul(management_fee_bps as i128)
        .unwrap()
        .checked_mul(elapsed_seconds as i128)
        .unwrap()
        / (BASIS_POINT_SCALE * SECONDS_PER_YEAR)
}

pub fn calculate_performance_fee(yield_earned: i128, performance_fee_bps: u32) -> i128 {
    if yield_earned <= 0 || performance_fee_bps == 0 {
        return 0;
    }

    yield_earned
        .checked_mul(performance_fee_bps as i128)
        .unwrap()
        / BASIS_POINT_SCALE
}

pub fn calculate_withdrawal_fee(amount: i128, fee_bps: u32) -> i128 {
    if amount <= 0 || fee_bps == 0 {
        return 0;
    }

    amount.checked_mul(fee_bps as i128).unwrap() / BASIS_POINT_SCALE
}
