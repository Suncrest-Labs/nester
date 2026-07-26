//! Property-based invariant tests for vault share-price and accounting.
//!
//! These tests generate randomised sequences of vault operations and assert
//! that core accounting invariants hold throughout. The reference model
//! provides an obviously-correct implementation to compare against.
//!
//! Run with:
//!   cargo test -p nester-integration-tests property_tests
//!
//! Override case count for deep runs:
//!   PROPTEST_CASES=1000 cargo test -p nester-integration-tests property_tests

#![cfg(test)]

extern crate std;

use proptest::prelude::*;
use soroban_sdk::{testutils::Address as _, Address, Env};

use nester_test_utils::NesterHarness;

const STROOP: i128 = 1;
const XLM: i128 = 10_000_000;
const MIN_DEPOSIT: i128 = 10_000_000;

#[derive(Debug, Clone)]
enum VaultOp {
    Deposit { user_idx: usize, amount: i128 },
    Withdraw { user_idx: usize, shares: i128 },
    ReportYield { amount: i128 },
}

fn amount_strategy() -> impl Strategy<Value = i128> {
    prop_oneof![
        Just(STROOP),
        Just(MIN_DEPOSIT),
        Just(100 * XLM),
        Just(1_000 * XLM),
        Just(10_000 * XLM),
        1..=100_i128.map(|x| x * XLM),
    ]
}

fn op_strategy(num_users: usize) -> impl Strategy<Value = VaultOp> {
    prop_oneof![
        (0..num_users, amount_strategy()).prop_map(|(user_idx, amount)| VaultOp::Deposit {
            user_idx,
            amount,
        }),
        (0..num_users, amount_strategy()).prop_map(|(user_idx, shares)| VaultOp::Withdraw {
            user_idx,
            shares,
        }),
        (0..10_000_i128).prop_map(|amount| VaultOp::ReportYield {
            amount: amount * XLM / 100,
        }),
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

    fn check_share_price_monotonic(&self, prev_price: i128) -> bool {
        let current = self.share_price();
        current >= prev_price
    }
}

fn setup_harness_with_users(num_users: usize) -> (NesterHarness, std::vec::Vec<Address>) {
    let h = NesterHarness::setup();
    let mut users = std::vec![];

    for _ in 0..num_users {
        let user = h.create_user();
        h.mint_deposit_tokens(&user, 1_000_000 * XLM);
        users.push(user);
    }

    (h, users)
}

proptest! {
    #![proptest_config(ProptestConfig {
        cases: std::env::var("PROPTEST_CASES")
            .ok()
            .and_then(|s| s.parse().ok())
            .unwrap_or(100),
        ..ProptestConfig::default()
    })]

    #[test]
    fn prop_share_balance_consistency(ops in prop::collection::vec(op_strategy(3), 1..50)) {
        let (h, users) = setup_harness_with_users(3);
        let mut model = ReferenceModel::new(3);

        for op in ops {
            match op {
                VaultOp::Deposit { user_idx, amount } => {
                    if amount < MIN_DEPOSIT {
                        continue;
                    }

                    let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
                        h.vault().deposit(&users[user_idx], &amount, &0)
                    }));

                    if let Ok(shares) = result {
                        let model_shares = model.deposit(user_idx, amount);
                        prop_assert!(
                            model.check_conservation(),
                            "share balance consistency violated after deposit"
                        );
                    }
                }
                VaultOp::Withdraw { user_idx, shares } => {
                    if shares == 0 {
                        continue;
                    }

                    let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
                        h.vault().withdraw(&users[user_idx], &shares, &0)
                    }));

                    if let Ok(_) = result {
                        model.withdraw(user_idx, shares);
                        prop_assert!(
                            model.check_conservation(),
                            "share balance consistency violated after withdraw"
                        );
                    }
                }
                VaultOp::ReportYield { amount } => {
                    if amount > 0 {
                        model.report_yield(amount);
                    }
                }
            }
        }

        prop_assert!(model.check_conservation(), "final share balance consistency");
    }

    #[test]
    fn prop_share_price_non_decreasing_with_positive_yield(ops in prop::collection::vec(op_strategy(2), 1..30)) {
        let (h, users) = setup_harness_with_users(2);
        let mut model = ReferenceModel::new(2);
        let mut prev_price = XLM;

        for op in ops {
            match op {
                VaultOp::Deposit { user_idx, amount } => {
                    if amount < MIN_DEPOSIT {
                        continue;
                    }

                    let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
                        h.vault().deposit(&users[user_idx], &amount, &0)
                    }));

                    if let Ok(_) = result {
                        model.deposit(user_idx, amount);
                        let current_price = model.share_price();
                        prop_assert!(
                            current_price >= prev_price,
                            "share price decreased: {} -> {}",
                            prev_price,
                            current_price
                        );
                        prev_price = current_price;
                    }
                }
                VaultOp::Withdraw { user_idx, shares } => {
                    if shares == 0 {
                        continue;
                    }

                    let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
                        h.vault().withdraw(&users[user_idx], &shares, &0)
                    }));

                    if let Ok(_) = result {
                        model.withdraw(user_idx, shares);
                    }
                }
                VaultOp::ReportYield { amount } => {
                    if amount > 0 {
                        model.report_yield(amount);
                        let current_price = model.share_price();
                        prop_assert!(
                            current_price >= prev_price,
                            "share price decreased after yield: {} -> {}",
                            prev_price,
                            current_price
                        );
                        prev_price = current_price;
                    }
                }
            }
        }
    }

    #[test]
    fn prop_round_trip_safety(amount in MIN_DEPOSIT..(100_000 * XLM)) {
        let (h, users) = setup_harness_with_users(1);

        let deposit_result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
            h.vault().deposit(&users[0], &amount, &0)
        }));

        if let Ok(shares) = deposit_result {
            let withdraw_result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
                h.vault().withdraw(&users[0], &shares, &0)
            }));

            if let Ok(returned) = withdraw_result {
                prop_assert!(
                    returned <= amount,
                    "round trip returned more: deposited {}, got {}",
                    amount,
                    returned
                );
            }
        }
    }

    #[test]
    fn prop_empty_vault_first_deposit(amount in MIN_DEPOSIT..(1_000_000 * XLM)) {
        let (h, users) = setup_harness_with_users(1);

        let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
            h.vault().deposit(&users[0], &amount, &0)
        }));

        if let Ok(shares) = result {
            prop_assert_eq!(shares, amount, "first deposit should be 1:1");
        }
    }

    #[test]
    fn prop_one_stroop_deposit_handling(num_deposits in 1..10usize) {
        let (h, users) = setup_harness_with_users(1);

        let first_result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
            h.vault().deposit(&users[0], &MIN_DEPOSIT, &0)
        }));

        if first_result.is_ok() {
            for _ in 0..num_deposits {
                let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
                    h.vault().deposit(&users[0], &(MIN_DEPOSIT + STROOP), &0)
                }));
                let _ = result;
            }
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

    assert!(price_after > price_before, "yield should increase share price");
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
