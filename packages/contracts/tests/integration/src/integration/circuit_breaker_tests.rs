//! Integration tests for the circuit breaker, and the trip-and-drain flow
//! through the fair-ordering emergency withdrawal queue that follows it
//! (issue #814).
#![cfg(test)]

extern crate std;

use soroban_sdk::testutils::Ledger as _;

use nester_test_utils::NesterHarness;
use vault_contract::CircuitBreakerConfig;

const DEPOSIT: i128 = 10_000_000;

/// A single withdrawal whose amount exceeds the circuit breaker's threshold
/// (default 20% of total assets) reverts.
#[test]
#[should_panic]
fn test_large_withdrawal_triggers_circuit_breaker() {
    let h = NesterHarness::setup();
    let user = h.create_user();
    h.mint_deposit_tokens(&user, DEPOSIT * 2);
    h.vault().deposit(&user, &DEPOSIT, &0);

    // 30% in one go, above the 20% default threshold.
    h.vault().withdraw(&user, &(DEPOSIT * 3 / 10), &0);
}

/// Several small withdrawals within the same rolling window whose sum
/// exceeds the threshold also trip the breaker, even though no single
/// withdrawal would have.
#[test]
#[should_panic]
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

/// Deposits are unaffected by a withdrawal that reverted on the circuit
/// breaker — the vault stays active and normal operations continue.
#[test]
fn test_deposits_succeed_after_a_reverted_circuit_breaker_trip() {
    let h = NesterHarness::setup();
    let user = h.create_user();
    h.mint_deposit_tokens(&user, DEPOSIT * 3);
    h.vault().deposit(&user, &DEPOSIT, &0);

    let result = h.vault().try_withdraw(&user, &(DEPOSIT * 3 / 10), &0);
    assert!(result.is_err(), "30% withdrawal should trip the breaker");

    assert!(!h.vault().is_paused());
    h.vault().deposit(&user, &DEPOSIT, &0);
    assert_eq!(h.token().balance(&user), DEPOSIT * 2);
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
