//! Integration tests for the circuit breaker, and the trip-and-drain flow
//! through the fair-ordering emergency withdrawal queue that follows it
//! (issue #814), plus the autonomous, staged circuit breaker (issue #817).
#![cfg(test)]

extern crate std;

use soroban_sdk::testutils::Ledger as _;

use nester_test_utils::NesterHarness;
use vault_contract::{CircuitBreakerConfig, FeeConfig, Severity, TripReason};

const DEPOSIT: i128 = 10_000_000;

/// A single withdrawal whose amount exceeds the circuit breaker's threshold
/// (default 20% of total assets) escalates severity to `Throttled` under the
/// staged breaker (issue #817) — it no longer reverts the triggering call.
#[test]
fn test_large_withdrawal_triggers_circuit_breaker() {
    let h = NesterHarness::setup();
    let user = h.create_user();
    h.mint_deposit_tokens(&user, DEPOSIT * 2);
    h.vault().deposit(&user, &DEPOSIT, &0);

    // 30% in one go, above the 20% default threshold.
    h.vault().withdraw(&user, &(DEPOSIT * 3 / 10), &0);
    assert_eq!(h.vault().get_breaker_status().severity, Severity::Throttled);
}

/// Several small withdrawals within the same rolling window whose sum
/// exceeds the threshold also trip the breaker, even though no single
/// withdrawal would have.
#[test]
fn test_cumulative_withdrawals_trigger_circuit_breaker() {
    let h = NesterHarness::setup();
    let user = h.create_user();
    h.mint_deposit_tokens(&user, DEPOSIT * 2);
    h.vault().deposit(&user, &DEPOSIT, &0);

    // Three 8% withdrawals in quick succession sum to 24%, above the 20%
    // threshold, all inside the default 2h window.
    h.vault().withdraw(&user, &(DEPOSIT * 8 / 100), &0);
    h.vault().withdraw(&user, &(DEPOSIT * 8 / 100), &0);
    h.vault().withdraw(&user, &(DEPOSIT * 8 / 100), &0);
    assert_eq!(h.vault().get_breaker_status().severity, Severity::Throttled);
}

/// Once the rolling window has fully elapsed, prior withdrawals drop out of
/// the sum and a withdrawal that would have tripped the breaker earlier
/// succeeds.
#[test]
fn test_circuit_breaker_window_resets() {
    let h = NesterHarness::setup();
    h.vault().set_circuit_breaker_config(
        &h.admin,
        &CircuitBreakerConfig {
            threshold_bps: 2_000,
            window_seconds: 3_600,
        },
    );

    let user = h.create_user();
    h.mint_deposit_tokens(&user, DEPOSIT * 2);
    h.vault().deposit(&user, &DEPOSIT, &0);

    h.vault().withdraw(&user, &(DEPOSIT * 15 / 100), &0);

    // Advance past the window so the first withdrawal is no longer counted.
    h.env.ledger().with_mut(|l| l.timestamp += 3_601);

    // This alone is under the 20% threshold — must succeed even though the
    // combined total across both calls would have exceeded it.
    h.vault().withdraw(&user, &(DEPOSIT * 15 / 100), &0);
}

/// Deposits are unaffected by a withdrawal that tripped the circuit breaker
/// — under the staged breaker (issue #817) the triggering withdrawal itself
/// completes, and the vault stays active for normal operations.
#[test]
fn test_deposits_succeed_after_a_reverted_circuit_breaker_trip() {
    let h = NesterHarness::setup();
    let user = h.create_user();
    h.mint_deposit_tokens(&user, DEPOSIT * 3);
    h.vault().deposit(&user, &DEPOSIT, &0);

    let result = h.vault().try_withdraw(&user, &(DEPOSIT * 3 / 10), &0);
    assert!(result.is_ok(), "30% withdrawal should complete and only escalate severity");
    assert_eq!(h.vault().get_breaker_status().severity, Severity::Throttled);

    assert!(!h.vault().is_paused());
    let balance_after_withdraw = h.token().balance(&user);
    h.vault().deposit(&user, &DEPOSIT, &0);
    assert!(h.token().balance(&user) > balance_after_withdraw);
}

// ---------------------------------------------------------------------------
// Fair emergency-queue trip-and-drain flow (issue #814)
// ---------------------------------------------------------------------------

/// A request queues via the fair emergency queue, and a completely
/// unrelated caller (not the requester, not the admin) can permissionlessly
/// drive `process_emergency_queue` to fill it — exits do not depend on
/// operator liveness.
#[test]
fn queue_drains_permissionlessly_once_liquidity_is_available() {
    let h = NesterHarness::setup();
    let whale = h.create_user();
    let bystander = h.create_user();

    h.mint_deposit_tokens(&whale, DEPOSIT * 2);
    h.vault().deposit(&whale, &DEPOSIT, &0);

    // No per-round fill cap for this scenario — it demonstrates a single
    // permissionless call fully draining a queue that already has enough
    // liquidity behind it, not the incremental-fill behaviour (covered by
    // the dedicated test below).
    h.vault().set_max_fill_share_bps(&h.admin, &10_000u32);

    let shares = h.token().balance(&whale);
    h.vault().request_emergency_withdrawal(&whale, &shares);

    let stats = h.vault().get_queue_stats();
    assert_eq!(stats.open_entry_count, 1);
    assert_eq!(stats.total_open_shares, shares);

    let processed = h.vault().process_emergency_queue(&bystander, &10);
    assert_eq!(
        processed, 1,
        "bystander should be able to fully drain a queue backed by sufficient liquidity"
    );

    let stats_after = h.vault().get_queue_stats();
    assert_eq!(stats_after.open_entry_count, 0);
    assert_eq!(h.token().balance(&whale), 0, "shares burned on fill");
}

/// A request larger than a single round's liquidity is filled
/// incrementally across multiple `process_emergency_queue` calls — each
/// call makes forward progress, and the request is never blocked
/// indefinitely.
#[test]
fn large_request_fills_incrementally_across_multiple_process_calls() {
    let h = NesterHarness::setup();
    let whale = h.create_user();
    let caller = h.create_user();
    let liquidity_provider = h.create_user();

    h.mint_deposit_tokens(&whale, DEPOSIT * 2);
    h.vault().deposit(&whale, &DEPOSIT, &0);

    let shares = h.token().balance(&whale);
    h.vault().request_emergency_withdrawal(&whale, &shares);

    // Default max_fill_share_bps is 50%, so the first call against the
    // vault's initial liquidity fills at most half the request — it does
    // not block, but it also does not finish in one round.
    let processed = h.vault().process_emergency_queue(&caller, &10);
    assert_eq!(processed, 1);

    let position_mid = h.vault().get_queue_position(&whale);
    assert!(
        position_mid.shares_filled > 0
            && position_mid.shares_filled < position_mid.shares_requested,
        "first round should partially fill a request capped by max_fill_share_bps"
    );

    // More capital returns to the vault (the scenario this queue exists
    // for), comfortably enough to finish the request even under the
    // per-round cap.
    h.mint_deposit_tokens(&liquidity_provider, DEPOSIT * 10);
    h.vault().deposit(&liquidity_provider, &(DEPOSIT * 10), &0);

    h.vault().process_emergency_queue(&caller, &10);
    assert_eq!(
        h.vault().get_queue_stats().open_entry_count,
        0,
        "queue fully drains once more liquidity returns to the vault"
    );
}

/// Two requesters make concurrent progress in the same round thanks to the
/// per-entry `max_fill_share_bps` cap — a large head-of-line request cannot
/// starve the smaller request behind it.
#[test]
fn two_requesters_progress_in_the_same_round() {
    let h = NesterHarness::setup();
    let whale = h.create_user();
    let minnow = h.create_user();
    let caller = h.create_user();

    h.mint_deposit_tokens(&whale, DEPOSIT * 10);
    h.mint_deposit_tokens(&minnow, DEPOSIT * 2);
    h.vault().deposit(&whale, &(DEPOSIT * 10), &0);
    h.vault().deposit(&minnow, &DEPOSIT, &0);

    let whale_shares = h.token().balance(&whale);
    let minnow_shares = h.token().balance(&minnow);
    h.vault()
        .request_emergency_withdrawal(&whale, &whale_shares);
    h.vault()
        .request_emergency_withdrawal(&minnow, &minnow_shares);

    let processed = h.vault().process_emergency_queue(&caller, &10);
    assert_eq!(
        processed, 2,
        "both entries should be touched in the same round"
    );
    assert_eq!(
        h.token().balance(&minnow),
        0,
        "minnow's small request fully satisfied in this round"
    );
}

/// Cancelling an open request returns the unfilled shares immediately
/// (they were never burned) and does not disturb another user's position.
#[test]
fn cancel_returns_unfilled_shares_without_disturbing_others() {
    let h = NesterHarness::setup();
    let u1 = h.create_user();
    let u2 = h.create_user();

    h.mint_deposit_tokens(&u1, DEPOSIT * 2);
    h.mint_deposit_tokens(&u2, DEPOSIT * 2);
    h.vault().deposit(&u1, &DEPOSIT, &0);
    h.vault().deposit(&u2, &DEPOSIT, &0);

    let u1_shares = h.token().balance(&u1);
    let u2_shares = h.token().balance(&u2);
    h.vault().request_emergency_withdrawal(&u1, &u1_shares);
    h.vault().request_emergency_withdrawal(&u2, &u2_shares);

    let returned = h.vault().cancel_emergency_request(&u1);
    assert_eq!(returned, u1_shares);
    assert_eq!(
        h.token().balance(&u1),
        u1_shares,
        "shares were never burned, so cancellation needs no transfer"
    );

    let pos2 = h.vault().get_queue_position(&u2);
    assert_eq!(
        pos2.entries_ahead, 0,
        "u1's cancellation must not disturb u2's position"
    );
}

/// A request below the minimum size is rejected outright — the queue
/// cannot be spammed with dust entries.
#[test]
#[should_panic]
fn dust_request_is_rejected() {
    let h = NesterHarness::setup();
    let user = h.create_user();
    h.mint_deposit_tokens(&user, DEPOSIT);
    h.vault().deposit(&user, &DEPOSIT, &0);
    h.vault().request_emergency_withdrawal(&user, &1);
}

/// A second `request_emergency_withdrawal` call extends the existing queue
/// position rather than creating a second one — a user cannot occupy the
/// front of the queue multiple times over.
#[test]
fn repeat_request_extends_the_same_position() {
    let h = NesterHarness::setup();
    let user = h.create_user();
    h.mint_deposit_tokens(&user, DEPOSIT * 2);
    h.vault().deposit(&user, &DEPOSIT, &0);

    let shares = h.token().balance(&user);
    let half = shares / 2;
    let e1 = h.vault().request_emergency_withdrawal(&user, &half);
    let e2 = h.vault().request_emergency_withdrawal(&user, &half);

    assert_eq!(e1.seq, e2.seq);
    assert_eq!(h.vault().get_queue_stats().open_entry_count, 1);
}

const STAGED_DEPOSIT: i128 = 100_000_000; // 10 units at 7 decimals

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
    h.mint_deposit_tokens(&user, STAGED_DEPOSIT);
    h.vault().deposit(&user, &STAGED_DEPOSIT, &0);
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
    h.vault().report_yield(&h.admin, &(STAGED_DEPOSIT * 2));

    // report_yield's own yield-sanity check is independent and disabled by
    // default; trigger the share-price check via a harvest/withdraw path
    // instead — deposit again to force a fresh share-price evaluation.
    let user2 = h.create_user();
    h.mint_deposit_tokens(&user2, STAGED_DEPOSIT);
    h.vault().deposit(&user2, &STAGED_DEPOSIT, &0);

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
    h.vault().report_yield(&h.admin, &(STAGED_DEPOSIT * 5));

    let user2 = h.create_user();
    h.mint_deposit_tokens(&user2, STAGED_DEPOSIT);
    h.vault().deposit(&user2, &STAGED_DEPOSIT, &0);

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
    h.vault().report_yield(&h.admin, &(STAGED_DEPOSIT / 2));

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
    let bal1 = h.vault().withdraw(&user, &(STAGED_DEPOSIT * 8 / 100), &0);
    assert!(bal1 > 0); // first withdrawal completes, no trip yet
    assert_eq!(h.vault().get_breaker_status().severity, Severity::Normal);

    let bal2 = h.vault().withdraw(&user, &(STAGED_DEPOSIT * 8 / 100), &0);
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
    h.vault().withdraw(&user, &(STAGED_DEPOSIT / 10_000), &0);

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
