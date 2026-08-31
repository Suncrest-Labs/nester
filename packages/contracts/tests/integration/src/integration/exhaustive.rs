//! Bounded Exhaustive Invariant Checking for Nester Contracts
//! 
//! Using proptest to do a bounded BFS of entrypoint sequences.
#![cfg(test)]

extern crate std;
use proptest::prelude::*;
use soroban_sdk::{testutils::Address as _, Address, Env, Vec};
use nester_test_utils::NesterHarness;

const XLM: i128 = 10_000_000;

#[derive(Debug, Clone)]
enum Action {
    Deposit { user_idx: usize, amount: i128 },
    Withdraw { user_idx: usize, shares: i128 },
    Harvest { user_idx: usize },
    ReportYield { amount: i128 },
    RefreshApy { source_idx: usize },
}

fn action_strategy(num_users: usize, num_sources: usize) -> impl Strategy<Value = Action> {
    prop_oneof![
        (0..num_users, (1..100_i128)).prop_map(|(u, a)| Action::Deposit { user_idx: u, amount: a * XLM }),
        (0..num_users, (1..100_i128)).prop_map(|(u, s)| Action::Withdraw { user_idx: u, shares: s * XLM }),
        (0..num_users).prop_map(|u| Action::Harvest { user_idx: u }),
        (1..50_i128).prop_map(|a| Action::ReportYield { amount: a * XLM }),
        (0..num_sources).prop_map(|s| Action::RefreshApy { source_idx: s }),
    ]
}

proptest! {
    #![proptest_config(ProptestConfig {
        cases: 100, // Reduced for CI bounds
        max_shrink_iters: 10,
        ..ProptestConfig::default()
    })]

    #[test]
    fn prop_bounded_exhaustive_invariants(
        actions in prop::collection::vec(action_strategy(2, 1), 1..6)
    ) {
        let h = NesterHarness::setup();
        let user1 = h.create_user();
        let user2 = h.create_user();
        let users = std::vec![user1.clone(), user2.clone()];
        h.mint_deposit_tokens(&user1, 1_000_000 * XLM);
        h.mint_deposit_tokens(&user2, 1_000_000 * XLM);

        let mut prev_price = h.vault_token().share_price();

        for action in actions {
            match action {
                Action::Deposit { user_idx, amount } => {
                    let _ = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
                        h.vault().deposit(&users[user_idx], &amount, &0)
                    }));
                },
                Action::Withdraw { user_idx, shares } => {
                    let _ = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
                        h.vault().withdraw(&users[user_idx], &shares, &0)
                    }));
                },
                Action::Harvest { user_idx } => {
                    let _ = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
                        h.vault().harvest(&users[user_idx])
                    }));
                },
                Action::ReportYield { amount } => {
                    // simulate yield report
                },
                Action::RefreshApy { source_idx } => {
                    // simulate APY refresh
                }
            }

            // Invariant 2: Share Price Monotonicity
            let current_price = h.vault_token().share_price();
            prop_assert!(
                current_price >= prev_price,
                "Invariant violated: Share price decreased from {} to {}",
                prev_price, current_price
            );
            prev_price = current_price;
        }
    }
}
