pub const BASIS_POINT_SCALE: u32 = 10_000;
pub const DECIMALS: u8 = 7;
pub const ADMIN_KEY: &str = "admin";
pub const INITIALIZED_KEY: &str = "init";

pub const MAX_MANAGEMENT_FEE_BPS: u32 = 1000;
pub const MAX_PERFORMANCE_FEE_BPS: u32 = 5000;
pub const MAX_EARLY_WITHDRAWAL_FEE_BPS: u32 = 500;
pub const MAX_EMERGENCY_FEE_BPS: u32 = 500;
pub const MIN_DEPOSIT_AMOUNT: i128 = 10_000_000; // 1 unit in 7 decimals

// Time-locked savings vault constants (issue #802)
/// Maximum number of concurrent open locks a single user may hold.
pub const MAX_OPEN_LOCKS_PER_USER: u32 = 10;
/// Maximum number of distinct lock tiers an admin may configure.
pub const MAX_LOCK_TIERS: u32 = 8;
/// Hard upper bound on the treasury's share of an early-break penalty (50%).
/// Prevents the admin from setting the treasury cut so high that break penalties
/// no longer meaningfully benefit remaining depositors.
pub const MAX_TREASURY_PENALTY_SHARE_BPS: u32 = 5_000;
