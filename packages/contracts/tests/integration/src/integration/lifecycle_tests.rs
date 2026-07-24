//! End-to-end integration tests covering the full savings lifecycle.
//!
//! Scenarios:
//!   1. Happy path — deposit → record allocation → rebalance → yield → withdraw
//!   2. Early-withdrawal penalty — fee charged within min_lock_period
//!   3. Loss / impairment — no performance fee when vault reports a loss
//!
//! Addresses issue #506.
#![cfg(test)]

extern crate std;

use soroban_sdk::{symbol_short, testutils::Ledger as _, token, vec};

use allocation_strategy_contract::AllocationWeight;
use nester_access_control::Role;
use nester_common::ProtocolType;
use nester_test_utils::NesterHarness;
use vault_contract::{CircuitBreakerConfig, FeeConfig};

/// 1 USDC at 7 decimals (MIN_DEPOSIT_AMOUNT).
const DEPOSIT: i128 = 10_000_000;
/// 10% simulated yield.
const YIELD_AMOUNT: i128 = 1_000_000;

/// Configure the vault's fee settings for deterministic test assertions:
/// - management_fee = 0 (eliminates time-dependent accrual that varies with
///   ledger timestamps, keeping expected amounts predictable)
/// - performance_fee = 10 % (1000 bps)
/// - early_withdrawal_fee = 0.1 % (10 bps)
fn configure_fees(h: &NesterHarness) {
    h.vault().set_fee_config(
        &h.admin,
        &FeeConfig {
            performance_fee_bps: 1_000,
            management_fee_bps: 0,
            early_withdrawal_fee_bps: 10,
            treasury_address: h.treasury_id.clone(),
        },
    );
}

/// Disable the circuit breaker so lifecycle tests can withdraw large amounts
/// without hitting the 20% rolling-window limit.
fn disable_circuit_breaker(h: &NesterHarness) {
    h.vault().set_circuit_breaker_config(
        &h.admin,
        &CircuitBreakerConfig {
            threshold_bps: 10_000, // 100 % — effectively off
            window_seconds: 7_200,
        },
    );
}

// ---------------------------------------------------------------------------
// Test 1: Happy path — full lifecycle deposit → rebalance → yield → withdraw
// ---------------------------------------------------------------------------

/// Exercises every cross-contract boundary in the happy path:
///   vault ↔ vault_token ↔ allocation_strategy ↔ yield_registry
///
/// Asserts:
/// - Shares minted 1:1 on first deposit
/// - Rebalance applies zero-sum deltas (conservation enforced)
/// - Yield accrues correctly to share price
/// - Withdrawal returns deposit + yield - performance fee
/// - All shares burned after full withdrawal
#[test]
fn test_full_lifecycle_deposit_to_withdraw() {
    let h = NesterHarness::setup();
    // Balanced default caps max_weight_bps at 6500; widen so the single 100% weight is valid.
    h.strategy()
        .update_strategy_params(&h.admin, &500u32, &10_000u32, &100u32);
    let user = h.create_user();
    let aave = symbol_short!("aave");

    configure_fees(&h);
    disable_circuit_breaker(&h);

    // 1. Register yield source, configure strategy weights, wire to vault
    h.registry()
        .register_source(&h.admin, &aave, &h.create_user(), &ProtocolType::Lending);
    h.strategy().set_weights(
        &h.admin,
        &vec![
            &h.env,
            AllocationWeight {
                source_id: aave.clone(),
                weight_bps: 10_000,
            },
        ],
    );
    h.vault().set_allocation_strategy(&h.admin, &h.strategy_id);

    // 2. User deposits DEPOSIT USDC → shares minted 1:1 on first deposit
    h.mint_deposit_tokens(&user, DEPOSIT);
    let shares = h.vault().deposit(&user, &DEPOSIT, &0);
    assert_eq!(shares, DEPOSIT, "first deposit should mint shares 1:1");
    assert_eq!(h.token().total_supply(), DEPOSIT);
    assert_eq!(h.token().total_assets(), DEPOSIT);

    // 3. Admin records that all funds are deployed to aave (bookkeeping)
    h.vault()
        .record_source_allocation(&h.admin, &aave, &DEPOSIT);

    // 4. Rebalance — already 100% in aave, delta = 0, no change applied
    let applied = h.vault().rebalance(&h.admin);
    assert!(
        applied.is_empty(),
        "no rebalance needed when already at target"
    );

    // 5. Simulate yield returned from aave: mint USDC to vault, then report
    h.mint_deposit_tokens(&h.vault_id, YIELD_AMOUNT);
    h.vault().grant_role(&h.admin, &h.admin, &Role::Manager);
    h.vault().report_yield(&h.admin, &YIELD_AMOUNT);
    // Update bookkeeping to reflect yield earned in aave
    h.vault()
        .record_source_allocation(&h.admin, &aave, &(DEPOSIT + YIELD_AMOUNT));

    assert_eq!(h.token().total_assets(), DEPOSIT + YIELD_AMOUNT);

    // 6. Advance ledger past min_lock_period (86 400 s) → no early-withdrawal fee
    h.env.ledger().with_mut(|l| l.timestamp = 86_401);

    // 7. User withdraws all shares
    // Performance fee = 10 % of YIELD_AMOUNT = 100_000
    let shares_held = h.token().balance(&user);
    let remaining = h.vault().withdraw(&user, &shares_held, &0);
    assert_eq!(remaining, 0, "all shares should be burned after full withdrawal");
    assert_eq!(h.token().total_supply(), 0);

    let perf_fee = YIELD_AMOUNT * 1_000 / 10_000;
    let expected = DEPOSIT + YIELD_AMOUNT - perf_fee;
    let user_usdc = token::Client::new(&h.env, &h.deposit_token_id).balance(&user);
    assert_eq!(
        user_usdc, expected,
        "user should receive deposit + yield net of performance fee"
    );
}

// ---------------------------------------------------------------------------
// Test 2: Early-withdrawal penalty
// ---------------------------------------------------------------------------

/// Withdrawing within the 86 400-second lock period triggers the early-
/// withdrawal fee (0.1 % by default).
#[test]
fn test_early_withdrawal_fee_charged() {
    let h = NesterHarness::setup();
    let user = h.create_user();
    configure_fees(&h);
    disable_circuit_breaker(&h);

    // Deposit at ledger timestamp 0; no yield; withdraw immediately (t=0 < 86400)
    h.mint_deposit_tokens(&user, DEPOSIT);
    h.vault().deposit(&user, &DEPOSIT, &0);

    let shares = h.token().balance(&user);
    let remaining = h.vault().withdraw(&user, &shares, &0);
    assert_eq!(remaining, 0, "all shares burned");

    // early_withdrawal_fee_bps = 10 (0.1 %)
    let early_fee = DEPOSIT * 10 / 10_000;
    let expected = DEPOSIT - early_fee;
    let user_usdc = token::Client::new(&h.env, &h.deposit_token_id).balance(&user);
    assert_eq!(
        user_usdc, expected,
        "early withdrawal fee should be deducted"
    );
}

// ---------------------------------------------------------------------------
// Test 3: Impairment — no performance fee on a loss
// ---------------------------------------------------------------------------

/// When the vault reports a loss (negative yield), `assets_for_shares` falls
/// below the user's cost basis.  `yield_part` is negative, so no performance
/// fee is charged and the user receives the impaired amount.
#[test]
fn test_no_performance_fee_on_loss() {
    let h = NesterHarness::setup();
    let user = h.create_user();
    configure_fees(&h);
    disable_circuit_breaker(&h);

    h.mint_deposit_tokens(&user, DEPOSIT);
    h.vault().deposit(&user, &DEPOSIT, &0);

    // Simulate 5 % impairment via Manager role
    let loss = DEPOSIT / 20; // 500_000
    h.vault().grant_role(&h.admin, &h.admin, &Role::Manager);
    h.vault().report_yield(&h.admin, &-loss);
    assert_eq!(h.token().total_assets(), DEPOSIT - loss);

    // Advance past lock period — no early-withdrawal fee
    h.env.ledger().with_mut(|l| l.timestamp = 86_401);

    let shares = h.token().balance(&user);
    let remaining = h.vault().withdraw(&user, &shares, &0);
    assert_eq!(remaining, 0, "all shares burned");
    assert_eq!(h.token().total_supply(), 0);

    // yield_part = (DEPOSIT - loss) - DEPOSIT = -loss < 0 → no performance fee
    let user_usdc = token::Client::new(&h.env, &h.deposit_token_id).balance(&user);
    assert_eq!(
        user_usdc,
        DEPOSIT - loss,
        "user receives impaired amount with no performance fee on loss"
    );
}

// ---------------------------------------------------------------------------
// Test 4: Time-locked deposit — full lifecycle
// ---------------------------------------------------------------------------

/// Deposit → lock (30 days) → advance past maturity → unlock → withdraw.
/// Validates the full time-locked savings lifecycle including yield boost.
const DAY: u64 = 86_400;

fn configure_lock_tiers(h: &NesterHarness) {
    use vault_contract::locks::LockTier;
    let tiers = vec![
        &h.env,
        LockTier {
            duration_secs: 30 * DAY,
            boost_multiplier: 12_000,
        },
        LockTier {
            duration_secs: 90 * DAY,
            boost_multiplier: 15_000,
        },
    ];
    h.vault().set_lock_tiers(&h.admin, &tiers);
}

#[test]
fn test_locked_deposit_lifecycle() {
    let h = NesterHarness::setup();
    h.strategy()
        .update_strategy_params(&h.admin, &500u32, &10_000u32, &100u32);
    let user = h.create_user();
    let aave = symbol_short!("aave");

    configure_fees(&h);
    disable_circuit_breaker(&h);
    configure_lock_tiers(&h);

    // Register source, strategy, wire to vault
    h.registry()
        .register_source(&h.admin, &aave, &h.create_user(), &ProtocolType::Lending);
    h.strategy().set_weights(
        &h.admin,
        &vec![
            &h.env,
            AllocationWeight {
                source_id: aave.clone(),
                weight_bps: 10_000,
            },
        ],
    );
    h.vault().set_allocation_strategy(&h.admin, &h.strategy_id);

    // 1. User deposits DEPOSIT into a 30-day lock
    h.mint_deposit_tokens(&user, DEPOSIT);
    let (new_shares, lock_id) = h.vault().deposit_locked(&user, &DEPOSIT, &0, &(30 * DAY));
    assert_eq!(new_shares, DEPOSIT);
    assert_eq!(lock_id, 0);

    // 2. Verify locked position exists
    let positions = h.vault().get_locked_positions(&user);
    assert_eq!(positions.len(), 1);
    assert_eq!(positions.get(0).unwrap().1.shares, DEPOSIT);
    assert_eq!(positions.get(0).unwrap().1.boost_multiplier, 12_000);

    // 3. Cannot withdraw while locked
    let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        h.vault().withdraw(&user, &DEPOSIT, &0);
    }));
    assert!(result.is_err(), "withdraw should fail when all shares are locked");

    // 4. Cannot unlock before maturity
    let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        h.vault().unlock_position(&user, &lock_id);
    }));
    assert!(result.is_err(), "unlock should fail before maturity");

    // 5. Simulate yield
    h.mint_deposit_tokens(&h.vault_id, YIELD_AMOUNT);
    h.vault().grant_role(&h.admin, &h.admin, &Role::Manager);
    h.vault().report_yield(&h.admin, &YIELD_AMOUNT);

    // 6. Advance past lock period
    h.env.ledger().with_mut(|l| l.timestamp = 30 * DAY + 1);

    // 7. Unlock position
    let unlocked = h.vault().unlock_position(&user, &lock_id);
    assert_eq!(unlocked, DEPOSIT);

    // 8. Verify position is gone
    let positions = h.vault().get_locked_positions(&user);
    assert_eq!(positions.len(), 0);

    // 9. Withdraw all — should succeed now
    let total_shares = h.token().balance(&user);
    let remaining = h.vault().withdraw(&user, &total_shares, &0);
    assert_eq!(remaining, 0, "all shares burned after full withdrawal");

    // 10. Verify user receives deposit + yield net of 10% performance fee
    let perf_fee = YIELD_AMOUNT * 1_000 / 10_000;
    let expected = DEPOSIT + YIELD_AMOUNT - perf_fee;
    let user_usdc = token::Client::new(&h.env, &h.deposit_token_id).balance(&user);
    assert_eq!(
        user_usdc, expected,
        "user should receive deposit + yield net of performance fee"
    );
}

// ---------------------------------------------------------------------------
// Test 5: Early break — penalty goes to remaining depositors
// ---------------------------------------------------------------------------

#[test]
fn test_break_lock_early_applies_penalty() {
    let h = NesterHarness::setup();
    let user = h.create_user();

    configure_fees(&h);
    disable_circuit_breaker(&h);
    configure_lock_tiers(&h);

    // User deposits and locks for 30 days
    h.mint_deposit_tokens(&user, DEPOSIT);
    let (_, lock_id) = h.vault().deposit_locked(&user, &DEPOSIT, &0, &(30 * DAY));

    let bal_before = token::Client::new(&h.env, &h.deposit_token_id).balance(&user);

    // Break immediately — max penalty (500 bps = 5%)
    h.vault().break_lock(&user, &lock_id);

    let bal_after = token::Client::new(&h.env, &h.deposit_token_id).balance(&user);
    // 5% penalty on DEPOSIT = 500_000, so user gets 9_500_000
    let expected_penalty = DEPOSIT * 500 / 10_000;
    assert_eq!(
        bal_after,
        bal_before + DEPOSIT - expected_penalty,
        "user receives deposited amount minus exact 5% penalty"
    );

    // Position should be removed
    let positions = h.vault().get_locked_positions(&user);
    assert_eq!(positions.len(), 0);
}

// ---------------------------------------------------------------------------
// Test 6: Withdraw blocked by locked shares, then succeeds after unlock
// ---------------------------------------------------------------------------

#[test]
fn test_withdraw_blocked_by_lock_then_succeeds_after_unlock() {
    let h = NesterHarness::setup();
    let user = h.create_user();

    configure_fees(&h);
    disable_circuit_breaker(&h);
    configure_lock_tiers(&h);

    // Deposit 200, lock 100, keep 100 flexible
    h.mint_deposit_tokens(&user, 2 * DEPOSIT);
    h.vault().deposit(&user, &DEPOSIT, &0);
    h.vault().deposit_locked(&user, &DEPOSIT, &0, &(30 * DAY));

    // Flexible portion (100 DEPOSIT) should be withdrawable
    let flex_shares = DEPOSIT;
    let remaining = h.vault().withdraw(&user, &flex_shares, &0);
    assert_eq!(remaining, DEPOSIT, "locked shares remain after flexible withdrawal");

    // Cannot withdraw the locked portion
    let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        h.vault().withdraw(&user, &1, &0);
    }));
    assert!(result.is_err(), "should reject withdrawal of locked shares");

    // Advance past lock, unlock, then withdraw
    h.env.ledger().with_mut(|l| l.timestamp = 30 * DAY + 1);
    h.vault().unlock_position(&user, &0);

    let locked_shares = h.token().balance(&user);
    assert!(locked_shares > 0, "should have shares to withdraw after unlock");
    let remaining = h.vault().withdraw(&user, &locked_shares, &0);
    assert_eq!(remaining, 0, "all remaining shares burned");
}
