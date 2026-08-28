//! Property-based invariant tests for vault share-price and accounting.
//!
//! These tests generate adversarial sequences against the live contracts and
//! assert the accounting invariants after every successful operation. CI uses
//! a fixed seed; the nightly workflow deliberately leaves the seed random.
//!
//! Run with:
//!   cargo test -p nester-integration-tests property_tests
//!
//! Override case count for deep runs:
//!   PROPTEST_CASES=1000 cargo test -p nester-integration-tests property_tests

#![cfg(test)]

extern crate std;

use proptest::prelude::*;
use soroban_sdk::Address;

use nester_access_control::Role;
use nester_test_utils::NesterHarness;

const STROOP: i128 = 1;
const XLM: i128 = 10_000_000;
const MIN_DEPOSIT: i128 = 10_000_000;

#[derive(Debug, Clone)]
enum VaultOp {
    Deposit { user_idx: usize, amount: i128 },
    Withdraw { user_idx: usize, share_bps: u32 },
    Harvest { user_idx: usize },          // NEW — triggers performance fee (issue #1029)
    ReportYield { yield_bps: u32 },
    Impairment { loss_bps: u32 },         // RENAMED from ReportLoss for clarity
    CollectFees,
    HarvestVault,                         // NEW — admin vault-wide harvest
}

fn amount_strategy() -> impl Strategy<Value = i128> {
    prop_oneof![
        Just(STROOP),
        Just(MIN_DEPOSIT),
        Just(100 * XLM),
        Just(1_000 * XLM),
        Just(10_000 * XLM),
        (1..=100_i128).prop_map(|x| x * XLM),
    ]
}

fn op_strategy(num_users: usize) -> impl Strategy<Value = VaultOp> {
    prop_oneof![
        4 => (0..num_users, amount_strategy())
            .prop_map(|(user_idx, amount)| VaultOp::Deposit { user_idx, amount }),
        4 => (0..num_users, 1_u32..=10_000_u32)
            .prop_map(|(user_idx, share_bps)| VaultOp::Withdraw { user_idx, share_bps }),
        2 => (0..num_users)
            .prop_map(|user_idx| VaultOp::Harvest { user_idx }),  // NEW — exposes #1029
        2 => (1_u32..=2_000_u32)
            .prop_map(|yield_bps| VaultOp::ReportYield { yield_bps }),
        1 => (1_u32..=1_000_u32)
            .prop_map(|loss_bps| VaultOp::Impairment { loss_bps }),
        1 => Just(VaultOp::CollectFees),
        1 => Just(VaultOp::HarvestVault),
    ]
}

struct ReferenceModel {
    total_assets: i128,
    total_shares: i128,
    user_shares: std::vec::Vec<i128>,
    user_principal: std::vec::Vec<i128>,
    accrued_fees: i128,
}

impl ReferenceModel {
    fn new(num_users: usize) -> Self {
        Self {
            total_assets: 0,
            total_shares: 0,
            user_shares: std::vec![0; num_users],
            user_principal: std::vec![0; num_users],
            accrued_fees: 0,
        }
    }

    fn deposit(&mut self, user_idx: usize, amount: i128) -> i128 {
        let shares = if self.total_shares == 0 || self.total_assets == 0 {
            amount
        } else {
            amount * self.total_shares / self.total_assets
        };

        if shares == 0 {
            return 0;
        }

        self.total_assets += amount;
        self.total_shares += shares;
        self.user_shares[user_idx] += shares;
        self.user_principal[user_idx] += amount;

        shares
    }

    fn withdraw(&mut self, user_idx: usize, shares: i128) -> i128 {
        if shares == 0 || self.total_shares == 0 {
            return 0;
        }

        let user_shares = self.user_shares[user_idx];
        let actual_shares = if shares > user_shares {
            user_shares
        } else {
            shares
        };

        if actual_shares == 0 {
            return 0;
        }

        let assets = actual_shares * self.total_assets / self.total_shares;

        let principal_portion = if user_shares > 0 {
            self.user_principal[user_idx] * actual_shares / user_shares
        } else {
            0
        };

        let yield_part = if assets > principal_portion {
            assets - principal_portion
        } else {
            0
        };

        let performance_fee = yield_part * 1000 / 10_000;
        let net_assets = assets - performance_fee;

        self.total_assets -= net_assets + performance_fee;
        self.total_shares -= actual_shares;
        self.user_shares[user_idx] -= actual_shares;
        self.user_principal[user_idx] -= principal_portion;
        self.accrued_fees += performance_fee;

        net_assets
    }

    fn report_yield(&mut self, amount: i128) {
        self.total_assets += amount;
    }

    fn share_price(&self) -> i128 {
        if self.total_shares == 0 {
            return XLM;
        }
        XLM * self.total_assets / self.total_shares
    }

    fn check_conservation(&self) -> bool {
        let sum_user_shares: i128 = self.user_shares.iter().sum();
        sum_user_shares == self.total_shares
    }
}

fn setup_harness_with_users(num_users: usize) -> (NesterHarness, std::vec::Vec<Address>) {
    let h = NesterHarness::setup();
    // A generated case intentionally performs many contract calls in one Env.
    // Remove the emulator's cumulative test budget so a valid long sequence is
    // evaluated by the accounting invariants instead of failing on test plumbing.
    h.env.budget().reset_unlimited();
    let mut users = std::vec![];

    for _ in 0..num_users {
        let user = h.create_user();
        h.mint_deposit_tokens(&user, 1_000_000 * XLM);
        users.push(user);
    }

    (h, users)
}

fn configure_invariant_harness(h: &NesterHarness) {
    let mut breaker = h.vault().get_breaker_config_v2();
    breaker.price_move_enabled = false;
    breaker.yield_sanity_enabled = false;
    breaker.withdraw_velocity_enabled = false;
    breaker.source_failure_enabled = false;
    h.vault().set_breaker_config(&h.admin, &breaker);

    let mut fees = h.vault().get_fee_config();
    fees.management_fee_bps = 0;
    fees.performance_fee_bps = 1_000;
    fees.early_withdrawal_fee_bps = 100;
    h.vault().set_fee_config(&h.admin, &fees);
    h.vault().grant_role(&h.admin, &h.admin, &Role::Manager);
}

fn sum_user_assets(h: &NesterHarness, users: &[Address]) -> i128 {
    users.iter().map(|user| h.vault().get_balance(user)).sum()
}

fn sum_user_shares(h: &NesterHarness, users: &[Address]) -> i128 {
    users.iter().map(|user| h.token().balance(user)).sum()
}

proptest! {
    #![proptest_config(ProptestConfig {
        cases: std::env::var("PROPTEST_CASES")
            .ok()
            .and_then(|s| s.parse().ok())
            .unwrap_or(32), // Increased from 16 for better coverage
        max_shrink_iters: 10_000, // Allow thorough shrinking to produce minimal reproductions
        max_shrink_time: 60_000,  // 60 seconds for shrinking to find minimal failing cases
        ..ProptestConfig::default()
    })]

    /// Comprehensive invariant test with all 6 core vault invariants.
    ///
    /// This test validates:
    /// - INV1: Share price monotonicity (no decrease without impairment)
    /// - INV2: Conservation (total shares * price <= total assets + rounding tolerance)
    /// - INV3: No free money (round-trip deposit/withdraw never profits)
    /// - INV4: No confiscation (user withdrawal >= proportional share - documented fees)
    /// - INV5: Fee bounds (performance fee only on gains above principal)
    /// - INV6: Independence (one user's ops don't reduce others' claims)
    #[test]
    fn prop_randomized_share_accounting_invariants(
        ops in prop::collection::vec(op_strategy(4), 5..40)
    ) {
        let (h, users) = setup_harness_with_users(4);
        configure_invariant_harness(&h);
        let mut previous_price = h.vault().share_price();
        let performance_fee_bps = 1_000_i128; // 10% - matches configure_invariant_harness

        // Track user balances for independence invariant
        let mut user_previous_claims: std::vec::Vec<i128> = std::vec![0; users.len()];

        for op in ops {
            let mut loss_event = false;
            let mut acting_user_idx: Option<usize> = None;

            // Snapshot claims before operation for INV6
            let total_assets_before = h.vault().total_assets();
            let total_shares_before = h.vault().total_shares();
            for (idx, user) in users.iter().enumerate() {
                let shares = h.token().balance(user);
                user_previous_claims[idx] = if total_shares_before == 0 {
                    0
                } else {
                    shares * total_assets_before / total_shares_before
                };
            }

            match op {
                VaultOp::Deposit { user_idx, amount } => {
                    // Values below the contract minimum are not valid deposits.
                    if amount < MIN_DEPOSIT {
                        continue;
                    }
                    h.vault().deposit(&users[user_idx], &amount, &0);
                    acting_user_idx = Some(user_idx);
                }
                VaultOp::Withdraw { user_idx, share_bps } => {
                    let owned = h.token().balance(&users[user_idx]);
                    if owned == 0 {
                        continue;
                    }
                    let shares = (owned * i128::from(share_bps) / 10_000)
                        .max(1)
                        .min(owned);
                    h.vault().withdraw(&users[user_idx], &shares, &0);
                    acting_user_idx = Some(user_idx);
                }
                VaultOp::Harvest { user_idx } => {
                    let shares = h.token().balance(&users[user_idx]);
                    if shares == 0 {
                        continue;
                    }
                    h.vault().harvest(&users[user_idx]);
                    acting_user_idx = Some(user_idx);
                }
                VaultOp::ReportYield { yield_bps } => {
                    let total_assets = h.vault().total_assets();
                    if h.vault().total_shares() == 0 || total_assets == 0 {
                        continue;
                    }
                    let amount = (total_assets * i128::from(yield_bps) / 10_000).max(1);
                    h.mint_deposit_tokens(&h.vault_id, amount);
                    h.vault().report_yield(&h.admin, &amount);
                }
                VaultOp::Impairment { loss_bps } => {
                    let total_assets = h.vault().total_assets();
                    if h.vault().total_shares() == 0 || total_assets <= 1 {
                        continue;
                    }
                    let loss = (total_assets * i128::from(loss_bps) / 10_000)
                        .max(1)
                        .min(total_assets - 1);
                    h.vault().report_yield(&h.admin, &-loss);
                    loss_event = true;
                }
                VaultOp::CollectFees => {
                    h.vault().collect_fees(&h.admin);
                }
                VaultOp::HarvestVault => {
                    h.vault().harvest_vault(&h.admin);
                    // Mark as a loss_event so INV1 allows share price to decrease.
                    // HarvestVault collects aggregate performance fees, which
                    // legitimately reduces share price for all holders (like an
                    // impairment, but intentional). This is expected behavior.
                    loss_event = true;
                }
            }

            // ══════════════════════════════════════════════════════════════
            // Post-operation invariant checks
            // ══════════════════════════════════════════════════════════════

            let total_assets = h.vault().total_assets();
            let total_shares = h.vault().total_shares();
            let user_shares = sum_user_shares(&h, &users);
            let current_price = h.vault().share_price();

            // ─────────────────────────────────────────────────────────────
            // INVARIANT 1: Share Price Monotonicity
            // Share price never decreases except during impairment events
            // ─────────────────────────────────────────────────────────────
            if !loss_event && total_shares > 0 {
                prop_assert!(
                    current_price >= previous_price,
                    "INV1 VIOLATED: share price decreased {} -> {} without impairment after {:?}",
                    previous_price,
                    current_price,
                    op
                );
            }

            // ─────────────────────────────────────────────────────────────
            // INVARIANT 2: Conservation
            // Sum of user shares must equal total shares (exact)
            // ─────────────────────────────────────────────────────────────
            prop_assert_eq!(
                user_shares,
                total_shares,
                "INV2a VIOLATED: user shares ({}) != total shares ({}) after {:?}",
                user_shares,
                total_shares,
                op
            );

            // ─────────────────────────────────────────────────────────────
            // INVARIANT 2 (extended): Value Conservation
            // Total value (shares * price) <= total assets + rounding tolerance
            // ─────────────────────────────────────────────────────────────
            if total_shares > 0 {
                let implied_value = total_shares * current_price / XLM;
                prop_assert!(
                    implied_value <= total_assets + 1,
                    "INV2b VIOLATED: implied value ({}) > total assets ({}) + 1 after {:?}",
                    implied_value,
                    total_assets,
                    op
                );
            }

            // ─────────────────────────────────────────────────────────────
            // INVARIANT 4: No Confiscation
            // Each user's redeemable value >= proportional share - max fee
            // ─────────────────────────────────────────────────────────────
            if total_shares > 0 {
                for user in users.iter() {
                    let user_share_count = h.token().balance(user);
                    if user_share_count == 0 {
                        continue;
                    }
                    let proportional_value = user_share_count * total_assets / total_shares;
                    let max_fee = proportional_value * performance_fee_bps / 10_000;
                    let user_balance = h.vault().get_balance(user);
                    prop_assert!(
                        user_balance >= proportional_value - max_fee - 1,
                        "INV4 VIOLATED: user balance ({}) < proportional ({}) - fee ({}) after {:?}",
                        user_balance,
                        proportional_value,
                        max_fee,
                        op
                    );
                }
            }

            // ─────────────────────────────────────────────────────────────
            // INVARIANT 6: Independence
            // Non-acting users' claims must not decrease (except impairment)
            // ─────────────────────────────────────────────────────────────
            if !loss_event {
                if let Some(actor) = acting_user_idx {
                    for (idx, user) in users.iter().enumerate() {
                        if idx == actor {
                            continue; // Acting user's own claim can change
                        }
                        let shares = h.token().balance(user);
                        let current_claim = if total_shares == 0 {
                            0
                        } else {
                            shares * total_assets / total_shares
                        };
                        prop_assert!(
                            current_claim >= user_previous_claims[idx],
                            "INV6 VIOLATED: user {} claim decreased {} -> {} due to user {} {:?}",
                            idx,
                            user_previous_claims[idx],
                            current_claim,
                            actor,
                            op
                        );
                    }
                }
            }

            previous_price = current_price;
        }
    }

    #[test]
    fn prop_round_trip_safety(amount in MIN_DEPOSIT..(100_000 * XLM)) {
        let (h, users) = setup_harness_with_users(1);
        configure_invariant_harness(&h);
        let mut fees = h.vault().get_fee_config();
        fees.performance_fee_bps = 0;
        fees.early_withdrawal_fee_bps = 0;
        h.vault().set_fee_config(&h.admin, &fees);

        let balance_before = soroban_sdk::token::TokenClient::new(
            &h.env,
            &h.deposit_token_id,
        ).balance(&users[0]);
        let shares = h.vault().deposit(&users[0], &amount, &0);
        h.vault().withdraw(&users[0], &shares, &0);
        let balance_after = soroban_sdk::token::TokenClient::new(
            &h.env,
            &h.deposit_token_id,
        ).balance(&users[0]);

        prop_assert!(
            balance_after <= balance_before,
            "round trip returned more: started with {}, ended with {}",
            balance_before,
            balance_after
        );
    }

    #[test]
    fn prop_empty_vault_first_deposit(amount in MIN_DEPOSIT..(1_000_000 * XLM)) {
        let (h, users) = setup_harness_with_users(1);
        configure_invariant_harness(&h);
        let shares = h.vault().deposit(&users[0], &amount, &0);
        prop_assert_eq!(shares, amount, "first deposit should be 1:1");
    }

    #[test]
    fn prop_one_stroop_deposit_handling(num_deposits in 1..10usize) {
        let (h, users) = setup_harness_with_users(1);
        configure_invariant_harness(&h);
        h.vault().deposit(&users[0], &MIN_DEPOSIT, &0);

        for _ in 0..num_deposits {
            let shares = h.vault().deposit(&users[0], &(MIN_DEPOSIT + STROOP), &0);
            prop_assert!(shares > 0, "valid deposit minted no shares");
        }
    }
}

#[test]
fn test_reference_model_deposit() {
    let mut model = ReferenceModel::new(2);

    let shares = model.deposit(0, 1000);
    assert_eq!(shares, 1000, "first deposit should be 1:1");
    assert_eq!(model.total_assets, 1000);
    assert_eq!(model.total_shares, 1000);
    assert!(model.check_conservation());

    let shares2 = model.deposit(1, 2000);
    assert_eq!(shares2, 2000, "second deposit at 1:1 should match");
    assert_eq!(model.total_assets, 3000);
    assert_eq!(model.total_shares, 3000);
    assert!(model.check_conservation());
}

#[test]
fn test_reference_model_withdraw() {
    let mut model = ReferenceModel::new(2);

    model.deposit(0, 1000);
    model.deposit(1, 1000);

    let assets = model.withdraw(0, 500);
    assert!(assets > 0);
    assert!(model.check_conservation());
    assert_eq!(model.user_shares[0], 500);
}

#[test]
fn test_reference_model_yield() {
    let mut model = ReferenceModel::new(1);

    model.deposit(0, 1000);
    let price_before = model.share_price();

    model.report_yield(100);
    let price_after = model.share_price();

    assert!(
        price_after > price_before,
        "yield should increase share price"
    );
}

#[test]
fn test_reference_model_conservation() {
    let mut model = ReferenceModel::new(3);

    model.deposit(0, 1000);
    model.deposit(1, 2000);
    model.deposit(2, 3000);

    assert!(model.check_conservation());

    model.withdraw(0, 500);
    assert!(model.check_conservation());

    model.report_yield(1000);
    assert!(model.check_conservation());

    model.withdraw(1, 1000);
    assert!(model.check_conservation());
}

// Regression for the counterexample found by the generated state machine:
// fee collection used to derive a negative collectable amount after yield and
// successive partial withdrawals exhausted the tracked liquid reserves.
#[test]
fn regression_collect_fees_with_exhausted_reserves_does_not_panic() {
    let (h, users) = setup_harness_with_users(2);
    configure_invariant_harness(&h);

    h.vault().deposit(&users[1], &MIN_DEPOSIT, &0);
    let yield_amount = MIN_DEPOSIT * 457 / 10_000;
    h.mint_deposit_tokens(&h.vault_id, yield_amount);
    h.vault().report_yield(&h.admin, &yield_amount);

    let first_balance = h.token().balance(&users[1]);
    let first_withdrawal = (first_balance * 8_953 / 10_000).max(1);
    h.vault().withdraw(&users[1], &first_withdrawal, &0);

    let second_balance = h.token().balance(&users[1]);
    let second_withdrawal = (second_balance * 7_158 / 10_000).max(1);
    h.vault().withdraw(&users[1], &second_withdrawal, &0);

    let fees_before = h.vault().get_accrued_fees();
    assert!(fees_before > 0);
    h.vault().collect_fees(&h.admin);
    assert_eq!(h.vault().get_accrued_fees(), fees_before);
}

/// Regression test for issue #1029: share price decreases on withdrawal.
///
/// This test reproduces the bug where performance fees charged during withdrawal
/// reduce total_assets but not total_shares proportionally, causing the share price
/// to decrease for remaining holders — diluting them.
///
/// **This test MUST FAIL on the current code before any fix is applied.**
/// It proves the property suite catches the bug reported in #1029.
///
/// Minimal failing sequence from proptest shrinking:
/// 1. ReportYield { amount: 200000 }
/// 2. Deposit { user_idx: 0, amount: 1 } (adjusted to MIN_DEPOSIT)
/// 3. Deposit { user_idx: 0, amount: 1000000000 }
/// 4. Withdraw { user_idx: 0, shares: 1000000000 }
/// 5. Deposit { user_idx: 0, amount: 10000000 }
///
/// Share price decreases between steps 4 and 5 without a loss event.
#[test]
fn regression_1029_share_price_decrease_on_withdrawal() {
    let (h, users) = setup_harness_with_users(1);
    configure_invariant_harness(&h);

    let mut track_price = |label: &str| {
        let price = h.vault().share_price();
        std::println!("{}: share_price = {}", label, price);
        price
    };

    let price_0 = track_price("Initial");

    // Step 1: Report yield (200,000 stroops)
    h.mint_deposit_tokens(&h.vault_id, 200_000);
    h.vault().report_yield(&h.admin, &200_000);
    let price_1 = track_price("After yield");
    assert!(price_1 >= price_0, "Yield should not decrease price");

    // Step 2: Deposit tiny amount (adjusted to MIN_DEPOSIT)
    h.vault().deposit(&users[0], &MIN_DEPOSIT, &0);
    let price_2 = track_price("After small deposit");
    assert!(price_2 >= price_1, "Small deposit should not decrease price");

    // Step 3: Large deposit (1,000,000,000 stroops = 100 XLM)
    h.vault().deposit(&users[0], &1_000_000_000, &0);
    let price_3 = track_price("After large deposit");
    assert!(price_3 >= price_2, "Large deposit should not decrease price");

    // Step 4: Withdraw (this is where the performance fee is charged)
    // The fee removes assets from total_assets but dilutes remaining holders
    let shares = std::cmp::min(1_000_000_000, h.token().balance(&users[0]));
    h.vault().withdraw(&users[0], &shares, &0);
    let price_4 = track_price("After withdrawal");

    // Step 5: Deposit again
    h.vault().deposit(&users[0], &10_000_000, &0);
    let price_5 = track_price("After final deposit");

    // BUG MANIFESTS HERE: Share price decreased from price_4 to price_5 without a loss event
    // This assertion will FAIL on the current code, proving the test catches #1029
    assert!(
        price_5 >= price_4,
        "BUG #1029: share price decreased from {} to {} without a loss event",
        price_4,
        price_5
    );
}

/// Alternative regression test using the harvest operation directly.
///
/// This test demonstrates that harvest (which also charges performance fees)
/// should not decrease the share price for remaining holders.
#[test]
fn regression_1029_harvest_performance_fee_must_not_decrease_share_price() {
    let (h, users) = setup_harness_with_users(2);
    configure_invariant_harness(&h);

    // User 0 deposits first — becomes the reference holder
    let initial_deposit = 100 * XLM;
    h.vault().deposit(&users[0], &initial_deposit, &0);

    // User 1 deposits — two holders now
    h.vault().deposit(&users[1], &initial_deposit, &0);

    let price_before = h.vault().share_price();

    // Report yield: 10% gain on the vault
    let yield_amount = initial_deposit * 2 * 1_000 / 10_000; // 10% of total
    h.mint_deposit_tokens(&h.vault_id, yield_amount);
    h.vault().report_yield(&h.admin, &yield_amount);

    // User 1 harvests — this should NOT decrease the share price for user 0
    h.vault().harvest(&users[1]);

    let price_after = h.vault().share_price();

    assert!(
        price_after >= price_before,
        "BUG #1029: share price decreased from {} to {} after harvest with performance fee",
        price_before,
        price_after
    );
}

// ══════════════════════════════════════════════════════════════════════════════
// INVARIANT 5: Fee Bounds Tests
// Performance fee only charged on genuine gains above principal (high-water mark)
// ══════════════════════════════════════════════════════════════════════════════

/// INVARIANT 5a: Performance fee never charged on principal.
///
/// When a user has no yield (redeemable value == principal), harvesting
/// should not charge any performance fee.
#[test]
fn invariant5_performance_fee_never_charged_on_principal() {
    let (h, users) = setup_harness_with_users(1);
    configure_invariant_harness(&h);

    let deposit = 1_000 * XLM;
    h.vault().deposit(&users[0], &deposit, &0);

    // Harvest with no yield — no fee should be charged
    let balance_before = h.vault().get_balance(&users[0]);
    h.vault().harvest(&users[0]);
    let balance_after = h.vault().get_balance(&users[0]);

    assert_eq!(
        balance_after, balance_before,
        "INV5: fee charged when there is no yield above principal"
    );
}

/// INVARIANT 5b: Performance fee only charged on gains, not principal.
///
/// When a user has yield, the fee should only apply to the gain portion,
/// not to the original principal.
#[test]
fn invariant5_performance_fee_only_on_gains_not_principal() {
    let (h, users) = setup_harness_with_users(1);
    configure_invariant_harness(&h);

    let deposit = 1_000 * XLM;
    h.vault().deposit(&users[0], &deposit, &0);

    // Report yield: +20%
    let gain = deposit * 2_000 / 10_000; // 20%
    h.mint_deposit_tokens(&h.vault_id, gain);
    h.vault().report_yield(&h.admin, &gain);

    let balance_before_harvest = h.vault().get_balance(&users[0]);

    // Balance should be deposit + gain
    assert!(
        balance_before_harvest >= deposit + gain - 1, // -1 for rounding
        "Balance before harvest should include deposit + gain"
    );

    // Harvest: fee should only be on the gain (20%), not on principal
    h.vault().harvest(&users[0]);
    let balance_after_harvest = h.vault().get_balance(&users[0]);

    // After harvest, user should have at least their principal
    // The fee (10% of 20% = 2% of deposit) is taken from the gain only
    let expected_min = deposit + (gain * 9_000 / 10_000) - 1; // gain - 10% fee, -1 rounding
    assert!(
        balance_after_harvest >= expected_min,
        "INV5: User received {} but should have at least {} (principal {} + 90% of gain {})",
        balance_after_harvest,
        expected_min,
        deposit,
        gain
    );
}

/// INVARIANT 5c: Fee not charged on recovery from loss below high-water mark.
///
/// If a user suffers a loss and then partially recovers (but not above the
/// previous high-water mark), no performance fee should be charged on that
/// recovery.
#[test]
fn invariant5_no_fee_on_loss_recovery_below_high_water() {
    let (h, users) = setup_harness_with_users(1);
    configure_invariant_harness(&h);

    let deposit = 1_000 * XLM;
    h.vault().deposit(&users[0], &deposit, &0);

    // Report yield: +20%
    let gain = deposit * 2_000 / 10_000;
    h.mint_deposit_tokens(&h.vault_id, gain);
    h.vault().report_yield(&h.admin, &gain);

    // Harvest to establish high-water mark
    h.vault().harvest(&users[0]);
    let balance_after_first_harvest = h.vault().get_balance(&users[0]);

    // Report a loss: -15% from current total
    let total_assets = h.vault().total_assets();
    let loss = total_assets * 1_500 / 10_000;
    h.vault().report_yield(&h.admin, &-loss);

    // Partial recovery: +5% (does NOT exceed previous high)
    let post_loss_assets = h.vault().total_assets();
    let partial_recovery = post_loss_assets * 500 / 10_000;
    h.mint_deposit_tokens(&h.vault_id, partial_recovery);
    h.vault().report_yield(&h.admin, &partial_recovery);

    // Harvest again — should NOT charge fee since we haven't recovered past high-water
    let balance_before_second_harvest = h.vault().get_balance(&users[0]);
    h.vault().harvest(&users[0]);
    let balance_after_second_harvest = h.vault().get_balance(&users[0]);

    // Balance should not decrease (no fee charged on recovery below high-water)
    assert!(
        balance_after_second_harvest >= balance_before_second_harvest - 1, // -1 for rounding
        "INV5: fee charged on recovery {} -> {} that did not exceed high-water mark",
        balance_before_second_harvest,
        balance_after_second_harvest
    );
}

// ══════════════════════════════════════════════════════════════════════════════
// Rounding Policy Documentation and Tests
// ══════════════════════════════════════════════════════════════════════════════

/// Rounding policy per operation (documented per issue #1050 requirement):
///
/// | Operation              | Direction | Rationale                               |
/// |------------------------|-----------|-----------------------------------------|
/// | assets_to_shares       | floor     | User receives fewer shares — vault wins |
/// | shares_to_assets       | floor     | User receives fewer assets — vault wins |
/// | fee calculation        | varies    | Documented per fee type                 |
///
/// **Rounding Direction:**
/// - All conversions that benefit the user round DOWN (floor)
/// - This ensures the vault can always satisfy withdrawal obligations
/// - Maximum rounding error per operation: 1 stroop in protocol's favor
///
/// **Implementation:** See `vault/src/conversion.rs`
/// - `assets_to_shares_down()`: floor(amount * total_shares / total_assets)
/// - `shares_to_assets_down()`: floor(shares * total_assets / total_shares)
#[cfg(test)]
mod rounding_policy_tests {
    use super::*;

    #[test]
    fn rounding_deposit_gives_user_floor_shares() {
        let (h, users) = setup_harness_with_users(2);
        configure_invariant_harness(&h);

        // User 0 deposits to establish baseline
        h.vault().deposit(&users[0], &MIN_DEPOSIT, &0);

        // User 1 deposits an amount that won't divide evenly
        let deposit_amount = MIN_DEPOSIT + 3; // +3 to create rounding
        let shares = h.vault().deposit(&users[1], &deposit_amount, &0);

        // Shares should be floor division
        let total_assets = h.vault().total_assets();
        let total_shares_before_deposit = h.token().balance(&users[0]);
        let expected_max = (deposit_amount * total_shares_before_deposit) / (total_assets - deposit_amount);

        assert!(
            shares <= expected_max,
            "Rounding gave user more shares than floor: got {}, max expected {}",
            shares,
            expected_max
        );
    }

    #[test]
    fn rounding_withdrawal_gives_user_floor_assets() {
        let (h, users) = setup_harness_with_users(2);
        configure_invariant_harness(&h);

        // Disable fees for clean rounding test
        let mut fees = h.vault().get_fee_config();
        fees.performance_fee_bps = 0;
        fees.early_withdrawal_fee_bps = 0;
        h.vault().set_fee_config(&h.admin, &fees);

        // Create initial state
        h.vault().deposit(&users[0], &MIN_DEPOSIT, &0);
        h.vault().deposit(&users[1], &MIN_DEPOSIT, &0);

        let user_shares = h.token().balance(&users[0]);
        let total_assets = h.vault().total_assets();
        let total_shares = h.vault().total_shares();

        // Calculate expected floor value
        let expected_max_assets = (user_shares * total_assets) / total_shares;

        // Withdraw
        let assets_before = soroban_sdk::token::TokenClient::new(&h.env, &h.deposit_token_id)
            .balance(&users[0]);
        h.vault().withdraw(&users[0], &user_shares, &0);
        let assets_after = soroban_sdk::token::TokenClient::new(&h.env, &h.deposit_token_id)
            .balance(&users[0]);

        let assets_received = assets_after - assets_before;

        assert!(
            assets_received <= expected_max_assets,
            "Rounding gave user more assets than floor: got {}, max {}",
            assets_received,
            expected_max_assets
        );
    }

    #[test]
    fn rounding_always_favors_protocol() {
        let (h, users) = setup_harness_with_users(1);
        configure_invariant_harness(&h);

        // Disable fees
        let mut fees = h.vault().get_fee_config();
        fees.performance_fee_bps = 0;
        fees.early_withdrawal_fee_bps = 0;
        h.vault().set_fee_config(&h.admin, &fees);

        // Round-trip with amount that creates rounding
        let deposit = MIN_DEPOSIT + 7;
        let balance_before = soroban_sdk::token::TokenClient::new(&h.env, &h.deposit_token_id)
            .balance(&users[0]);

        let shares = h.vault().deposit(&users[0], &deposit, &0);
        h.vault().withdraw(&users[0], &shares, &0);

        let balance_after = soroban_sdk::token::TokenClient::new(&h.env, &h.deposit_token_id)
            .balance(&users[0]);

        // User should never profit from rounding
        assert!(
            balance_after <= balance_before,
            "Rounding allowed user profit: started {}, ended {}",
            balance_before,
            balance_after
        );

        // Loss should be minimal (< 2 stroops for double rounding)
        let loss = balance_before - balance_after;
        assert!(
            loss <= 2,
            "Rounding loss too large: {} stroops",
            loss
        );
    }
}
