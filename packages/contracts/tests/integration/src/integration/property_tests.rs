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
    Harvest { user_idx: usize },          // Per-user harvest triggers performance fee (issue #1029)
    ReportYield { yield_bps: u32 },
    ReportLoss { loss_bps: u32 },
    CollectFees,
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
            .prop_map(|user_idx| VaultOp::Harvest { user_idx }),  // Exposes #1029 if not fixed
        2 => (1_u32..=2_000_u32)
            .prop_map(|yield_bps| VaultOp::ReportYield { yield_bps }),
        1 => (1_u32..=1_000_u32)
            .prop_map(|loss_bps| VaultOp::ReportLoss { loss_bps }),
        1 => Just(VaultOp::CollectFees),
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
            .unwrap_or(16),
        ..ProptestConfig::default()
    })]

    #[test]
    fn prop_randomized_share_accounting_invariants(
        ops in prop::collection::vec(op_strategy(3), 1..30)
    ) {
        let (h, users) = setup_harness_with_users(3);
        configure_invariant_harness(&h);
        let mut previous_price = h.vault().share_price();

        for op in ops {
            let mut loss_event = false;

            match op {
                VaultOp::Deposit { user_idx, amount } => {
                    // Values below the contract minimum are not valid deposits.
                    if amount < MIN_DEPOSIT {
                        continue;
                    }
                    h.vault().deposit(&users[user_idx], &amount, &0);
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
                }
                VaultOp::Harvest { user_idx } => {
                    let shares = h.token().balance(&users[user_idx]);
                    if shares == 0 {
                        continue;
                    }
                    h.vault().harvest(&users[user_idx]);
                    // NOTE: Per-user harvest does NOT reduce share price for other holders.
                    // The fee is paid by burning the harvesting user's own shares (fixed in #1157).
                    // Therefore, this is NOT a loss_event.
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
                VaultOp::ReportLoss { loss_bps } => {
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
            }

            let total_assets = h.vault().total_assets();
            let total_shares = h.vault().total_shares();
            let user_assets = sum_user_assets(&h, &users);
            let user_shares = sum_user_shares(&h, &users);
            let current_price = h.vault().share_price();

            prop_assert!(
                user_assets <= total_assets,
                "sum of user assets ({}) exceeds total assets ({}) after {:?}",
                user_assets,
                total_assets,
                op
            );
            prop_assert_eq!(
                user_shares,
                total_shares,
                "user shares do not sum to total shares after {:?}",
                op
            );
            if !loss_event && total_shares > 0 {
                prop_assert!(
                    current_price >= previous_price,
                    "share price decreased without a loss: {} -> {} after {:?}",
                    previous_price,
                    current_price,
                    op
                );
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

// Regression for issue #1029: per-user harvest must not dilute other holders.
// The performance fee is paid by burning the harvesting user's own shares (fixed in #1157),
// so the share price for passive holders must remain unchanged.
#[test]
fn regression_1029_harvest_does_not_dilute_passive_holders() {
    let (h, users) = setup_harness_with_users(2);
    configure_invariant_harness(&h);

    // User 0 deposits and generates yield
    let deposit_amount = 1_000 * XLM;
    h.vault().deposit(&users[0], &deposit_amount, &0);

    let yield_amount = deposit_amount * 20 / 100; // 20% yield
    h.mint_deposit_tokens(&h.vault_id, yield_amount);
    h.vault().report_yield(&h.admin, &yield_amount);

    // User 1 deposits after yield (becomes passive holder)
    h.vault().deposit(&users[1], &deposit_amount, &0);

    // Record passive holder's share price before harvest
    let price_before = h.vault().share_price();
    let passive_shares_before = h.token().balance(&users[1]);

    // User 0 harvests their gains
    h.vault().harvest(&users[0]);

    // Passive holder's share price must not decrease
    let price_after = h.vault().share_price();
    let passive_shares_after = h.token().balance(&users[1]);

    assert_eq!(
        passive_shares_after, passive_shares_before,
        "Passive holder's shares changed during another user's harvest"
    );
    assert!(
        price_after >= price_before,
        "Share price decreased for passive holder: {} -> {} (regression of #1029)",
        price_before,
        price_after
    );
}

// Regression for issue #1029: verify treasury receives the correct fee amount.
// When a user harvests, the performance fee should reach the treasury in full,
// paid by burning the harvesting user's shares.
#[test]
fn regression_1029_harvest_pays_treasury_correctly() {
    let (h, users) = setup_harness_with_users(1);
    configure_invariant_harness(&h);

    // Deposit and generate yield
    let deposit_amount = 1_000 * XLM;
    h.vault().deposit(&users[0], &deposit_amount, &0);

    let yield_amount = deposit_amount * 50 / 100; // 50% yield
    h.mint_deposit_tokens(&h.vault_id, yield_amount);
    h.vault().report_yield(&h.admin, &yield_amount);

    // Get treasury address from fee config
    let fee_config = h.vault().get_fee_config();
    let treasury_address = fee_config.treasury_address;

    // Create deposit token client to check treasury balance
    let deposit_token = soroban_sdk::token::TokenClient::new(&h.env, &h.deposit_token_id);

    // Record treasury balance before harvest
    let treasury_balance_before = deposit_token.balance(&treasury_address);

    // Harvest
    let result = h.vault().harvest(&users[0]);
    let reported_fee = result.performance_fee;

    // Treasury balance should increase by the reported fee
    let treasury_balance_after = deposit_token.balance(&treasury_address);
    let treasury_delta = treasury_balance_after - treasury_balance_before;

    assert_eq!(
        treasury_delta, reported_fee,
        "Treasury balance delta ({}) does not match reported performance fee ({})",
        treasury_delta, reported_fee
    );
    assert!(
        reported_fee > 0,
        "Performance fee should be positive for yield above principal"
    );
}
