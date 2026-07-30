pub const BASIS_POINT_SCALE: u32 = 10_000;
pub const DECIMALS: u8 = 7;
pub const ADMIN_KEY: &str = "admin";
pub const INITIALIZED_KEY: &str = "init";

pub const MAX_MANAGEMENT_FEE_BPS: u32 = 1000;
pub const MAX_PERFORMANCE_FEE_BPS: u32 = 5000;
pub const MAX_EARLY_WITHDRAWAL_FEE_BPS: u32 = 500;
pub const MAX_EMERGENCY_FEE_BPS: u32 = 500;
pub const MIN_DEPOSIT_AMOUNT: i128 = 10_000_000; // 1 unit in 7 decimals

/// Hard ceiling on the treasury's slice of the early-exit penalty escrow
/// (issue #805). `depositor_share_bps` is admin-configurable but must always
/// leave the treasury with no more than this — a compile-time constant, not
/// storage, so an admin key compromise cannot redirect the whole escrow to
/// the treasury.
pub const MAX_TREASURY_SHARE_BPS: u32 = 5_000;

// ---------------------------------------------------------------------------
// Circuit breaker defaults (issue #817)
// ---------------------------------------------------------------------------
/// Share-price move that trips the breaker within a single update window (bps).
pub const DEFAULT_MAX_PRICE_MOVE_BPS: u32 = 1_000; // 10%
/// A single `report_yield` exceeding this fraction of total assets is rejected (bps).
pub const DEFAULT_MAX_SINGLE_YIELD_BPS: u32 = 2_000; // 20%
/// Withdrawal value within the rolling window that escalates severity (bps of total assets).
pub const DEFAULT_MAX_WITHDRAW_VELOCITY_BPS: u32 = 2_000; // 20%
/// A trip condition must exceed its threshold by this extra margin to fire,
/// so noise/dust cannot grief the breaker into tripping right at the boundary.
pub const DEFAULT_BREAKER_MARGIN_BPS: u32 = 200; // 2%
/// Consecutive adapter failures before the source-failure condition trips.
pub const DEFAULT_MAX_SOURCE_FAILURES: u32 = 3;
/// Minimum time between staged recovery steps.
pub const DEFAULT_RECOVERY_COOLDOWN_SECONDS: u64 = 3_600; // 1h
/// Rolling window used to evaluate share-price movement.
pub const DEFAULT_PRICE_MOVE_WINDOW_SECONDS: u64 = 3_600; // 1h

// ---------------------------------------------------------------------------
// Referral program defaults (issue #818)
// ---------------------------------------------------------------------------
/// Share of the protocol's performance fee routed to a referrer (bps of the fee, not the yield).
pub const DEFAULT_REFERRAL_SHARE_BPS: u32 = 1_000; // 10% of the performance fee slice
/// Minimum principal a referred user must hold before rewards accrue (anti-Sybil).
pub const DEFAULT_MIN_REFERRED_DEPOSIT: i128 = 100_000_000; // 10 units at 7 decimals
/// Minimum tenure (seconds) a referred user must have before rewards accrue.
pub const DEFAULT_MIN_REFEREE_TENURE_SECONDS: u64 = 604_800; // 7 days
/// Max number of distinct referred users that earn a reward per referrer.
pub const DEFAULT_MAX_REWARDED_REFERRALS: u32 = 50;
/// Lifetime reward cap per referrer, in token units.
pub const DEFAULT_MAX_REWARD_PER_REFERRER: i128 = 5_000_000_000; // 500 units
/// Minimum claimable balance before `claim_referral_rewards` will pay out.
pub const DEFAULT_MIN_REFERRAL_CLAIM: i128 = 1_000_000; // 0.1 unit

// ---------------------------------------------------------------------------
// Upgrade delay constants
// ---------------------------------------------------------------------------
/// Mandatory timelock delay for Vault upgrades (48 hours).
pub const MIN_UPGRADE_DELAY_VAULT: u64 = 172_800;
/// Mandatory timelock delay for Yield Registry upgrades (48 hours).
pub const MIN_UPGRADE_DELAY_YIELD_REGISTRY: u64 = 172_800;
/// Mandatory timelock delay for Allocation Strategy upgrades (48 hours).
pub const MIN_UPGRADE_DELAY_ALLOCATION_STRATEGY: u64 = 172_800;
/// Mandatory timelock delay for Treasury upgrades (7 days).
pub const MIN_UPGRADE_DELAY_TREASURY: u64 = 604_800;

