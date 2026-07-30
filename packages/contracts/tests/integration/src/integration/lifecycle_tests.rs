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
use nester_common::{ProtocolType, MIN_UPGRADE_DELAY_TREASURY, MIN_UPGRADE_DELAY_VAULT};
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
    h.vault().register_callee(&h.admin, &h.strategy_id);
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
    assert_eq!(
        remaining, 0,
        "all shares should be burned after full withdrawal"
    );
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
// Upgrade Framework Integration Tests
// ---------------------------------------------------------------------------

#[test]
fn test_upgrade_lifecycle_full_flow() {
    let h = NesterHarness::setup();
    let user = h.create_user();
    let upgrader = h.create_user();
    let relayer = h.create_user();

    configure_fees(&h);
    disable_circuit_breaker(&h);

    // 1. User deposits funds and yield is accrued
    h.mint_deposit_tokens(&user, DEPOSIT);
    let shares = h.vault().deposit(&user, &DEPOSIT, &0);
    h.mint_deposit_tokens(&h.vault_id, YIELD_AMOUNT);
    h.vault().grant_role(&h.admin, &h.admin, &Role::Manager);
    h.vault().report_yield(&h.admin, &YIELD_AMOUNT);

    // Initial schema version is 1
    assert_eq!(h.vault().get_schema_version(), 1);

    // 2. Grant Upgrader role
    h.vault().grant_role(&h.admin, &upgrader, &Role::Upgrader);

    let valid_hash = h.env.deployer().upload_contract_wasm(soroban_sdk::Bytes::new(&h.env));
    let now = h.env.ledger().timestamp();
    let eta = now + MIN_UPGRADE_DELAY_VAULT;

    // 3. Propose upgrade
    h.vault().propose_upgrade(&upgrader, &valid_hash, &eta);

    let pending = h.vault().get_pending_upgrade().unwrap();
    assert_eq!(pending.wasm_hash, valid_hash);
    assert_eq!(pending.eta, eta);
    assert_eq!(pending.proposer, upgrader);

    // 4. Execution before ETA fails with UpgradeNotMatured
    let res = h.vault().try_execute_upgrade(&relayer, &valid_hash);
    assert!(res.is_err());

    // 5. Advance timestamp to ETA
    h.env.ledger().with_mut(|l| l.timestamp = eta);

    // 6. Execute succeeds permissionlessly via relayer
    h.vault().execute_upgrade(&relayer, &valid_hash);
    assert!(h.vault().get_pending_upgrade().is_none());

    // 7. Migration succeeds and is idempotent
    let v1 = h.vault().migrate();
    let v2 = h.vault().migrate();
    assert_eq!(v1, v2);
    assert_eq!(v1, 1);

    // 8. Verify balances, shares, and accrued yield preserved
    assert_eq!(h.token().balance(&user), shares);
    assert_eq!(h.token().total_assets(), DEPOSIT + YIELD_AMOUNT);
}

#[test]
fn test_upgrade_cancellation_and_access_control() {
    let h = NesterHarness::setup();
    let upgrader = h.create_user();
    let outsider = h.create_user();

    h.vault().grant_role(&h.admin, &upgrader, &Role::Upgrader);

    let dummy_hash = soroban_sdk::BytesN::from_array(&h.env, &[8u8; 32]);
    let now = h.env.ledger().timestamp();
    let eta = now + MIN_UPGRADE_DELAY_VAULT;

    // Outsider cannot propose
    assert!(h.vault().try_propose_upgrade(&outsider, &dummy_hash, &eta).is_err());

    // Upgrader proposes
    h.vault().propose_upgrade(&upgrader, &dummy_hash, &eta);

    // Outsider cannot cancel
    assert!(h.vault().try_cancel_upgrade(&outsider).is_err());

    // Upgrader cancels
    h.vault().cancel_upgrade(&upgrader);
    assert!(h.vault().get_pending_upgrade().is_none());

    // Advance timestamp past ETA
    h.env.ledger().with_mut(|l| l.timestamp = eta);

    // Cancelled proposal cannot be executed
    assert!(h.vault().try_execute_upgrade(&outsider, &dummy_hash).is_err());
}

#[test]
fn test_emergency_withdrawal_during_pending_upgrade() {
    let h = NesterHarness::setup();
    let user = h.create_user();
    let upgrader = h.create_user();

    h.mint_deposit_tokens(&user, DEPOSIT);
    h.vault().deposit(&user, &DEPOSIT, &0);

    // Pause vault
    h.vault().grant_role(&h.admin, &h.admin, &Role::Guardian);
    h.vault().pause(&h.admin);

    // Propose upgrade while vault is paused
    h.vault().grant_role(&h.admin, &upgrader, &Role::Upgrader);
    let dummy_hash = soroban_sdk::BytesN::from_array(&h.env, &[9u8; 32]);
    let now = h.env.ledger().timestamp();
    let eta = now + MIN_UPGRADE_DELAY_VAULT;
    h.vault().propose_upgrade(&upgrader, &dummy_hash, &eta);

    // Emergency withdrawal succeeds throughout timelock
    let returned = h.vault().emergency_withdraw(&user);
    assert_eq!(returned, DEPOSIT);
}

#[test]
fn test_treasury_upgrade_delay_requirement() {
    let h = NesterHarness::setup();
    let treasury_client = h.treasury();
    let upgrader = h.create_user();

    treasury_client.grant_role(&h.admin, &upgrader, &Role::Upgrader);

    let dummy_hash = soroban_sdk::BytesN::from_array(&h.env, &[10u8; 32]);
    let now = h.env.ledger().timestamp();

    // Delay less than 7 days (e.g. 48 hours) fails for Treasury
    let short_eta = now + MIN_UPGRADE_DELAY_VAULT;
    assert!(treasury_client.try_propose_upgrade(&upgrader, &dummy_hash, &short_eta).is_err());

    // Delay of 7 days succeeds for Treasury
    let valid_eta = now + MIN_UPGRADE_DELAY_TREASURY;
    treasury_client.propose_upgrade(&upgrader, &dummy_hash, &valid_eta);
    assert!(treasury_client.get_pending_upgrade().is_some());
}

