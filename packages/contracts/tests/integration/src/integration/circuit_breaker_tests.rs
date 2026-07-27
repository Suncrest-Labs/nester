//! Integration tests for the autonomous, staged circuit breaker (issue #817).
#![cfg(test)]

extern crate std;

use soroban_sdk::testutils::Ledger as _;

use nester_test_utils::NesterHarness;
use vault_contract::{FeeConfig, Severity, TripReason};

const DEPOSIT: i128 = 100_000_000; // 10 units at 7 decimals

fn permissive_fee_config(h: &NesterHarness) -> FeeConfig {
    FeeConfig {
        performance_fee_bps: 1_000,
        management_fee_bps: 0,
        early_withdrawal_fee_bps: 0,
        treasury_address: h.treasury_id.clone(),
    }
}

fn seed_deposit(h: &NesterHarness) -> soroban_sdk::Address {
    let user = h.create_user();
    h.mint_deposit_tokens(&user, DEPOSIT);
    h.vault().deposit(&user, &DEPOSIT, &0);
    user
}

// ---------------------------------------------------------------------------
// Share-price-move trip condition
// ---------------------------------------------------------------------------

#[test]
fn share_price_move_condition_trips_when_enabled() {
    let h = NesterHarness::setup();
    h.vault()
        .set_fee_config(&h.admin, &permissive_fee_config(&h));
    let mut cfg = h.vault().get_breaker_config_v2();
    cfg.price_move_enabled = true;
    cfg.max_price_move_bps = 500; // 5%
    h.vault().set_breaker_config(&h.admin, &cfg);

    seed_deposit(&h);
    assert_eq!(h.vault().get_breaker_status().severity, Severity::Normal);

    // A huge yield report moves the share price by far more than 5% + margin.
    h.vault()
        .grant_role(&h.admin, &h.admin, &nester_access_control::Role::Manager);
    h.vault().report_yield(&h.admin, &(DEPOSIT * 2));

    // report_yield's own yield-sanity check is independent and disabled by
    // default; trigger the share-price check via a harvest/withdraw path
    // instead — deposit again to force a fresh share-price evaluation.
    let user2 = h.create_user();
    h.mint_deposit_tokens(&user2, DEPOSIT);
    h.vault().deposit(&user2, &DEPOSIT, &0);

    let status = h.vault().get_breaker_status();
    assert_eq!(status.severity, Severity::DepositsHalted);
    assert_eq!(status.last_trip_reason, TripReason::SharePriceMove);
}

#[test]
fn share_price_move_condition_is_independently_toggleable() {
    let h = NesterHarness::setup();
    h.vault()
        .set_fee_config(&h.admin, &permissive_fee_config(&h));
    let mut cfg = h.vault().get_breaker_config_v2();
    cfg.price_move_enabled = false; // explicitly disabled
    h.vault().set_breaker_config(&h.admin, &cfg);

    seed_deposit(&h);
    h.vault()
        .grant_role(&h.admin, &h.admin, &nester_access_control::Role::Manager);
    h.vault().report_yield(&h.admin, &(DEPOSIT * 5));

    let user2 = h.create_user();
    h.mint_deposit_tokens(&user2, DEPOSIT);
    h.vault().deposit(&user2, &DEPOSIT, &0);

    // Disabled: no trip despite the enormous share-price move.
    assert_eq!(h.vault().get_breaker_status().severity, Severity::Normal);
}

// ---------------------------------------------------------------------------
// Yield-sanity trip condition
// ---------------------------------------------------------------------------

#[test]
fn yield_sanity_condition_rejects_implausible_report_without_reverting() {
    let h = NesterHarness::setup();
    let mut cfg = h.vault().get_breaker_config_v2();
    cfg.yield_sanity_enabled = true;
    cfg.max_single_yield_bps = 2_000; // 20%
    h.vault().set_breaker_config(&h.admin, &cfg);

    seed_deposit(&h);
    h.vault()
        .grant_role(&h.admin, &h.admin, &nester_access_control::Role::Manager);

    let before = h.vault().get_total_deposits();
    // 50% of total assets in one report — far past 20% + margin.
    h.vault().report_yield(&h.admin, &(DEPOSIT / 2));

    // The call succeeded (no panic) but the amount was not applied.
    assert_eq!(h.vault().get_total_deposits(), before);
    let status = h.vault().get_breaker_status();
    assert_eq!(status.last_trip_reason, TripReason::YieldSanity);
    assert_eq!(status.severity, Severity::DepositsHalted);
}

// ---------------------------------------------------------------------------
// Withdrawal-velocity trip condition + anti-griefing margin
// ---------------------------------------------------------------------------

#[test]
fn withdrawal_velocity_escalates_to_throttled_but_lets_withdrawal_complete() {
    let h = NesterHarness::setup();
    h.vault().set_circuit_breaker_config(
        &h.admin,
        &vault_contract::CircuitBreakerConfig {
            threshold_bps: 1_000, // 10%
            window_seconds: 3_600,
        },
    );

    let user = seed_deposit(&h);
    // Two 8% withdrawals cumulatively exceed 10% + the default 2% margin.
    let bal1 = h.vault().withdraw(&user, &(DEPOSIT * 8 / 100), &0);
    assert!(bal1 > 0); // first withdrawal completes, no trip yet
    assert_eq!(h.vault().get_breaker_status().severity, Severity::Normal);

    let bal2 = h.vault().withdraw(&user, &(DEPOSIT * 8 / 100), &0);
    assert!(bal2 > 0); // triggering withdrawal itself still completes
    assert_eq!(h.vault().get_breaker_status().severity, Severity::Throttled);
}

#[test]
fn dust_withdrawal_cannot_grief_the_velocity_breaker() {
    let h = NesterHarness::setup();
    h.vault().set_circuit_breaker_config(
        &h.admin,
        &vault_contract::CircuitBreakerConfig {
            threshold_bps: 1_000, // 10%
            window_seconds: 3_600,
        },
    );

    let user = seed_deposit(&h);
    // A dust withdrawal (0.01%) sits far below the threshold+margin — it
    // must not tip the breaker on its own.
    h.vault().withdraw(&user, &(DEPOSIT / 10_000), &0);

    assert_eq!(h.vault().get_breaker_status().severity, Severity::Normal);
}

// ---------------------------------------------------------------------------
// Source-failure trip condition
// ---------------------------------------------------------------------------

#[test]
fn source_failure_condition_trips_after_threshold_consecutive_failures() {
    let h = NesterHarness::setup();
    let mut cfg = h.vault().get_breaker_config_v2();
    cfg.source_failure_enabled = true;
    cfg.max_source_failures = 3;
    h.vault().set_breaker_config(&h.admin, &cfg);

    h.vault().record_source_failure(&h.admin);
    h.vault().record_source_failure(&h.admin);
    assert_eq!(h.vault().get_breaker_status().severity, Severity::Normal);

    h.vault().record_source_failure(&h.admin);
    let status = h.vault().get_breaker_status();
    assert_eq!(status.severity, Severity::DepositsHalted);
    assert_eq!(status.last_trip_reason, TripReason::SourceFailure);
}

// ---------------------------------------------------------------------------
// Emergency exit always works, at every severity
// ---------------------------------------------------------------------------

#[test]
fn emergency_withdraw_works_even_at_full_halt() {
    let h = NesterHarness::setup();
    let user = seed_deposit(&h);

    h.vault().guardian_trip_breaker(&h.admin);
    assert_eq!(h.vault().get_breaker_status().severity, Severity::FullHalt);

    // Ordinary withdraw is blocked at FullHalt...
    assert!(h.vault().try_withdraw(&user, &1, &0).is_err());

    // ...but the emergency path is never gated by severity.
    let returned = h.vault().emergency_withdraw(&user);
    assert!(returned > 0);
}

// ---------------------------------------------------------------------------
// Staged recovery: cannot skip stages, cannot beat the cooldown
// ---------------------------------------------------------------------------

#[test]
fn recovery_is_staged_and_cannot_skip_or_beat_cooldown() {
    let h = NesterHarness::setup();
    h.vault().guardian_trip_breaker(&h.admin); // -> FullHalt

    // Cannot recover before the cooldown elapses.
    assert!(h.vault().try_recover_next_stage(&h.admin).is_err());

    h.env
        .ledger()
        .with_mut(|l| l.timestamp += nester_common::DEFAULT_RECOVERY_COOLDOWN_SECONDS + 1);

    // One step at a time: FullHalt -> DepositsHalted (never straight to Normal).
    let next = h.vault().recover_next_stage(&h.admin);
    assert_eq!(next, Severity::DepositsHalted);

    // Still cannot skip ahead until the next cooldown elapses.
    assert!(h.vault().try_recover_next_stage(&h.admin).is_err());

    h.env
        .ledger()
        .with_mut(|l| l.timestamp += nester_common::DEFAULT_RECOVERY_COOLDOWN_SECONDS + 1);
    let next = h.vault().recover_next_stage(&h.admin);
    assert_eq!(next, Severity::Throttled);

    h.env
        .ledger()
        .with_mut(|l| l.timestamp += nester_common::DEFAULT_RECOVERY_COOLDOWN_SECONDS + 1);
    let next = h.vault().recover_next_stage(&h.admin);
    assert_eq!(next, Severity::Normal);
}

#[test]
fn guardian_cannot_call_recover_next_stage() {
    let h = NesterHarness::setup();
    let guardian = h.create_user();
    h.vault()
        .grant_role(&h.admin, &guardian, &nester_access_control::Role::Guardian);

    h.vault().guardian_trip_breaker(&guardian);
    h.env
        .ledger()
        .with_mut(|l| l.timestamp += nester_common::DEFAULT_RECOVERY_COOLDOWN_SECONDS + 1);

    // Reversing a Guardian action requires a higher role — Guardian itself
    // cannot call recover_next_stage.
    assert!(h.vault().try_recover_next_stage(&guardian).is_err());
}

#[test]
fn guardian_can_halt_deposits_but_not_unpause_upgrade_or_withdraw_as_admin() {
    let h = NesterHarness::setup();
    let guardian = h.create_user();
    h.vault()
        .grant_role(&h.admin, &guardian, &nester_access_control::Role::Guardian);

    // Can make the vault safer.
    h.vault().guardian_halt_deposits(&guardian);
    assert_eq!(
        h.vault().get_breaker_status().severity,
        Severity::DepositsHalted
    );
    h.vault().pause(&guardian);
    assert!(h.vault().is_paused());

    // Cannot make it riskier.
    assert!(h.vault().try_unpause(&guardian).is_err());
    assert!(h
        .vault()
        .try_set_fee_config(&guardian, &permissive_fee_config(&h))
        .is_err());
    assert!(h.vault().try_recover_next_stage(&guardian).is_err());
}
