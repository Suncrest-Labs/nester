//! Autonomous, staged circuit breaker for the vault (issue #817).
//!
//! Extracted from the fund-movement hot paths so severity transitions and
//! trip evaluation are testable in isolation. This module holds no storage
//! keys of its own — it reads/writes through the vault's own [`crate::DataKey`],
//! which is visible here because `breaker` is a child module of the vault crate.
//!
//! # Design notes
//! * Severity only ever *escalates* automatically. De-escalation is only
//!   possible through [`recover_next_stage`], which is staged, cooled-down,
//!   and gated by a role the caller (the vault contract) must check before
//!   invoking it — Guardian must never be able to call it.
//! * `check_yield_sanity` does not panic. A transaction that panics reverts
//!   every storage write made during it — including the escalation itself —
//!   so an implausible `report_yield` amount is instead silently *not
//!   applied* (rejected) while the trip is still recorded and the severity
//!   still escalates. The caller sees a normal, successful call; the anomaly
//!   just has no effect on vault accounting.
//! * The withdrawal-velocity condition measures value, not transaction
//!   count, and requires exceeding the configured threshold by
//!   `margin_bps` before firing, so a single dust withdrawal can never grief
//!   the vault into a halt for free.

use soroban_sdk::{contracttype, symbol_short, Env, Symbol};

use nester_common::{emit_event, fees::mul_div};

use crate::DataKey;

pub const BREAKER: Symbol = symbol_short!("BREAKER");
const BRK_TRIP: Symbol = symbol_short!("BRK_TRIP");
const BRK_RCVR: Symbol = symbol_short!("BRK_RCVR");
const SEV_CHG: Symbol = symbol_short!("SEV_CHG");

/// Graded vault severity. Ordering matters: `derive(Ord)` follows
/// declaration order, so `Normal < Throttled < DepositsHalted < FullHalt`.
#[contracttype]
#[derive(Clone, Copy, Debug, Eq, PartialEq, PartialOrd, Ord)]
pub enum Severity {
    /// Unrestricted.
    Normal,
    /// Per-tx/per-window limits reduced; vault stays open.
    Throttled,
    /// New deposits blocked; withdrawals still work.
    DepositsHalted,
    /// Everything blocked except the emergency withdrawal queue.
    FullHalt,
}

#[contracttype]
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum TripReason {
    None,
    SharePriceMove,
    YieldSanity,
    WithdrawVelocity,
    SourceFailure,
    GuardianManual,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct BreakerConfig {
    pub price_move_enabled: bool,
    pub max_price_move_bps: u32,
    pub price_move_window_seconds: u64,

    pub yield_sanity_enabled: bool,
    pub max_single_yield_bps: u32,

    pub withdraw_velocity_enabled: bool,

    pub source_failure_enabled: bool,
    pub max_source_failures: u32,

    /// Extra margin (bps) a condition must exceed its threshold by before
    /// firing — the anti-griefing guard.
    pub margin_bps: u32,
    /// Minimum time between staged recovery steps.
    pub recovery_cooldown_seconds: u64,
}

impl BreakerConfig {
    /// Conservative out-of-the-box defaults. Per issue #817, "a threshold
    /// that is right for a mature large vault is wrong for a new small
    /// one" — so the value-sensitive conditions (share-price move, yield
    /// sanity) ship *disabled* until an operator tunes and enables them for
    /// this specific vault's expected scale via [`crate::VaultContract::set_breaker_config`].
    /// Withdrawal velocity stays enabled by default since it reuses the
    /// vault's existing, already-configured [`crate::CircuitBreakerConfig`].
    pub fn defaults() -> Self {
        BreakerConfig {
            price_move_enabled: false,
            max_price_move_bps: nester_common::DEFAULT_MAX_PRICE_MOVE_BPS,
            price_move_window_seconds: nester_common::DEFAULT_PRICE_MOVE_WINDOW_SECONDS,
            yield_sanity_enabled: false,
            max_single_yield_bps: nester_common::DEFAULT_MAX_SINGLE_YIELD_BPS,
            withdraw_velocity_enabled: true,
            source_failure_enabled: true,
            max_source_failures: nester_common::DEFAULT_MAX_SOURCE_FAILURES,
            margin_bps: nester_common::DEFAULT_BREAKER_MARGIN_BPS,
            recovery_cooldown_seconds: nester_common::DEFAULT_RECOVERY_COOLDOWN_SECONDS,
        }
    }
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct BreakerStatus {
    pub severity: Severity,
    pub last_trip_reason: TripReason,
    pub last_observed_value: i128,
    pub last_threshold: i128,
    pub next_recovery_allowed_at: u64,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct TripEventData {
    pub reason: TripReason,
    pub observed: i128,
    pub threshold: i128,
    pub severity: Severity,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct RecoverEventData {
    pub from: Severity,
    pub to: Severity,
    pub authorised_by: soroban_sdk::Address,
}

// ---------------------------------------------------------------------------
// Config / severity accessors
// ---------------------------------------------------------------------------

pub fn get_config(env: &Env) -> BreakerConfig {
    env.storage()
        .instance()
        .get(&DataKey::BreakerConfigV2)
        .unwrap_or_else(BreakerConfig::defaults)
}

pub fn set_config(env: &Env, config: &BreakerConfig) {
    env.storage()
        .instance()
        .set(&DataKey::BreakerConfigV2, config);
}

pub fn severity(env: &Env) -> Severity {
    env.storage()
        .instance()
        .get(&DataKey::Severity)
        .unwrap_or(Severity::Normal)
}

fn set_severity(env: &Env, sev: Severity) {
    env.storage().instance().set(&DataKey::Severity, &sev);
}

pub fn status(env: &Env) -> BreakerStatus {
    BreakerStatus {
        severity: severity(env),
        last_trip_reason: env
            .storage()
            .instance()
            .get(&DataKey::LastTripReason)
            .unwrap_or(TripReason::None),
        last_observed_value: env
            .storage()
            .instance()
            .get(&DataKey::LastObservedValue)
            .unwrap_or(0),
        last_threshold: env
            .storage()
            .instance()
            .get(&DataKey::LastThreshold)
            .unwrap_or(0),
        next_recovery_allowed_at: env
            .storage()
            .instance()
            .get(&DataKey::NextRecoveryAllowedAt)
            .unwrap_or(0),
    }
}

// ---------------------------------------------------------------------------
// Gating (called from deposit/withdraw hot paths)
// ---------------------------------------------------------------------------

pub fn require_deposits_allowed(env: &Env) {
    if severity(env) >= Severity::DepositsHalted {
        soroban_sdk::panic_with_error!(env, nester_common::ContractError::CircuitBreakerTriggered);
    }
}

pub fn require_withdrawals_allowed(env: &Env) {
    if severity(env) >= Severity::FullHalt {
        soroban_sdk::panic_with_error!(env, nester_common::ContractError::CircuitBreakerTriggered);
    }
}

pub fn require_not_full_halt(env: &Env) {
    if severity(env) >= Severity::FullHalt {
        soroban_sdk::panic_with_error!(env, nester_common::ContractError::CircuitBreakerTriggered);
    }
}

// ---------------------------------------------------------------------------
// Escalation
// ---------------------------------------------------------------------------

/// Escalate severity to at least `target` (never de-escalates). Always
/// records the firing condition, even when the current severity is already
/// at or above `target`, so `get_breaker_status` reflects the most recent
/// signal.
pub fn escalate(env: &Env, target: Severity, reason: TripReason, observed: i128, threshold: i128) {
    let current = severity(env);
    if target > current {
        set_severity(env, target);
        let cooldown = get_config(env).recovery_cooldown_seconds;
        env.storage().instance().set(
            &DataKey::NextRecoveryAllowedAt,
            &(env.ledger().timestamp() + cooldown),
        );
        emit_event(
            env,
            BREAKER,
            SEV_CHG,
            env.current_contract_address(),
            (current, target),
        );
    }

    env.storage()
        .instance()
        .set(&DataKey::LastTripReason, &reason);
    env.storage()
        .instance()
        .set(&DataKey::LastObservedValue, &observed);
    env.storage()
        .instance()
        .set(&DataKey::LastThreshold, &threshold);

    emit_event(
        env,
        BREAKER,
        BRK_TRIP,
        env.current_contract_address(),
        TripEventData {
            reason,
            observed,
            threshold,
            severity: severity(env),
        },
    );
}

// ---------------------------------------------------------------------------
// Trip conditions
// ---------------------------------------------------------------------------

/// Share-price movement within the configured rolling window. Resets its
/// baseline once the window elapses so it measures *rate* of movement, not
/// lifetime drift.
pub fn check_share_price_move(env: &Env, current_price: i128) {
    let cfg = get_config(env);
    if !cfg.price_move_enabled || current_price <= 0 {
        return;
    }

    let now = env.ledger().timestamp();
    let has_baseline = env.storage().instance().has(&DataKey::SharePriceBaselineAt);
    let baseline_at: u64 = env
        .storage()
        .instance()
        .get(&DataKey::SharePriceBaselineAt)
        .unwrap_or(0);
    let baseline_price: i128 = env
        .storage()
        .instance()
        .get(&DataKey::SharePriceBaseline)
        .unwrap_or(current_price);

    // `baseline_at == 0` is not a safe "never set" sentinel — the ledger
    // timestamp can genuinely be 0 (e.g. in tests that never advance it) —
    // so an explicit `has()` check distinguishes "no baseline yet" from
    // "baseline legitimately recorded at time 0".
    if !has_baseline || now.saturating_sub(baseline_at) >= cfg.price_move_window_seconds {
        env.storage()
            .instance()
            .set(&DataKey::SharePriceBaseline, &current_price);
        env.storage()
            .instance()
            .set(&DataKey::SharePriceBaselineAt, &now);
        return;
    }

    if baseline_price <= 0 {
        return;
    }

    let diff = (current_price - baseline_price).abs();
    let move_bps = match mul_div(diff, 10_000, baseline_price) {
        Ok(v) => v,
        Err(_) => return,
    };
    let threshold_bps = (cfg.max_price_move_bps + cfg.margin_bps) as i128;

    if move_bps > threshold_bps {
        escalate(
            env,
            Severity::DepositsHalted,
            TripReason::SharePriceMove,
            move_bps,
            threshold_bps,
        );
    }
}

/// Returns `true` when `amount` is implausible relative to `total_assets`
/// and must be rejected (not applied) rather than accepted. Also escalates
/// severity when it fires.
pub fn check_yield_sanity(env: &Env, amount: i128, total_assets: i128) -> bool {
    let cfg = get_config(env);
    if !cfg.yield_sanity_enabled || amount <= 0 || total_assets <= 0 {
        return false;
    }

    let threshold = match mul_div(
        total_assets,
        (cfg.max_single_yield_bps + cfg.margin_bps) as i128,
        10_000,
    ) {
        Ok(v) => v,
        Err(_) => return false,
    };

    if amount > threshold {
        escalate(
            env,
            Severity::DepositsHalted,
            TripReason::YieldSanity,
            amount,
            threshold,
        );
        return true;
    }
    false
}

/// Withdrawal velocity measured over value (not transaction count) within a
/// rolling window. `window_sum` is the cumulative withdrawal value already
/// including the current withdrawal; `raw_threshold` is `total_assets *
/// max_withdraw_velocity_bps / 10_000` computed by the caller (which already
/// tracks the rolling window history for the legacy config API). Firing
/// requires exceeding `raw_threshold` by `margin_bps` — a single small
/// withdrawal cannot tip the vault into Throttled on its own unless the
/// window is already near the limit.
pub fn check_withdraw_velocity(env: &Env, window_sum: i128, raw_threshold: i128) {
    let cfg = get_config(env);
    if !cfg.withdraw_velocity_enabled || raw_threshold <= 0 {
        return;
    }
    let threshold_with_margin =
        match mul_div(raw_threshold, (10_000 + cfg.margin_bps) as i128, 10_000) {
            Ok(v) => v,
            Err(_) => return,
        };

    if window_sum > threshold_with_margin {
        escalate(
            env,
            Severity::Throttled,
            TripReason::WithdrawVelocity,
            window_sum,
            threshold_with_margin,
        );
    }
}

/// Records a yield-source adapter failure. Trips to `DepositsHalted` once
/// consecutive failures cross the configured threshold.
pub fn note_source_failure(env: &Env) {
    let cfg = get_config(env);
    if !cfg.source_failure_enabled {
        return;
    }
    let count: u32 = env
        .storage()
        .instance()
        .get(&DataKey::SourceFailureCount)
        .unwrap_or(0)
        + 1;
    env.storage()
        .instance()
        .set(&DataKey::SourceFailureCount, &count);

    if count >= cfg.max_source_failures {
        escalate(
            env,
            Severity::DepositsHalted,
            TripReason::SourceFailure,
            count as i128,
            cfg.max_source_failures as i128,
        );
    }
}

pub fn reset_source_failures(env: &Env) {
    env.storage()
        .instance()
        .set(&DataKey::SourceFailureCount, &0u32);
}

// ---------------------------------------------------------------------------
// Manual (Guardian) escalation and staged recovery
// ---------------------------------------------------------------------------

pub fn guardian_escalate(env: &Env, target: Severity) {
    escalate(env, target, TripReason::GuardianManual, 0, 0);
}

/// Move severity exactly one stage down, enforcing the recovery cooldown and
/// refusing to skip stages. The caller (the vault contract) is responsible
/// for checking that the invoking address holds a role higher than Guardian
/// — this function has no role awareness of its own.
pub fn recover_next_stage(env: &Env, authorised_by: &soroban_sdk::Address) -> Severity {
    let current = severity(env);
    if current == Severity::Normal {
        soroban_sdk::panic_with_error!(env, nester_common::ContractError::RecoveryStageInvalid);
    }

    let unlock_at: u64 = env
        .storage()
        .instance()
        .get(&DataKey::NextRecoveryAllowedAt)
        .unwrap_or(0);
    if env.ledger().timestamp() < unlock_at {
        soroban_sdk::panic_with_error!(env, nester_common::ContractError::RecoveryCooldownActive);
    }

    let next = match current {
        Severity::FullHalt => Severity::DepositsHalted,
        Severity::DepositsHalted => Severity::Throttled,
        Severity::Throttled => Severity::Normal,
        Severity::Normal => Severity::Normal,
    };
    set_severity(env, next);

    let cooldown = get_config(env).recovery_cooldown_seconds;
    env.storage().instance().set(
        &DataKey::NextRecoveryAllowedAt,
        &(env.ledger().timestamp() + cooldown),
    );

    emit_event(
        env,
        BREAKER,
        BRK_RCVR,
        authorised_by.clone(),
        RecoverEventData {
            from: current,
            to: next,
            authorised_by: authorised_by.clone(),
        },
    );

    next
}
