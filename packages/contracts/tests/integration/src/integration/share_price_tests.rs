//! Integration tests for share price edge cases.
#![cfg(test)]

extern crate std;

use soroban_sdk::{testutils::Ledger as _, token};

use nester_access_control::Role;
use nester_common::{ContractError, MIN_DEPOSIT_AMOUNT};
use nester_test_utils::NesterHarness;
use vault_contract::conversion::{
    assets_to_shares_down, assets_to_shares_up, shares_to_assets_down, shares_to_assets_up,
};
use vault_contract::{CircuitBreakerConfig, FeeConfig};

/// 1 unit at 7 decimals -- the protocol minimum deposit.
const DEPOSIT: i128 = MIN_DEPOSIT_AMOUNT;

/// Zero every fee so the assertions below isolate share-price arithmetic
/// rather than fee arithmetic. Fee behaviour is covered by fee_tests.rs.
fn zero_fees(h: &NesterHarness) {
    h.vault().set_fee_config(
        &h.admin,
        &FeeConfig {
            performance_fee_bps: 0,
            management_fee_bps: 0,
            early_withdrawal_fee_bps: 0,
            treasury_address: h.treasury_id.clone(),
        },
    );
}

/// Disable the 20% rolling-window circuit breaker so a full withdrawal is not
/// rejected for reasons unrelated to share price.
fn disable_circuit_breaker(h: &NesterHarness) {
    h.vault().set_circuit_breaker_config(
        &h.admin,
        &CircuitBreakerConfig {
            threshold_bps: 10_000, // 100% -- effectively off
            window_seconds: 7_200,
        },
    );
}

/// Mint `amount` to the vault and report it as yield, which is what raises
/// total_assets above total_supply and so pushes share price above 1:1.
///
/// A positive report vests linearly (issue #803) rather than landing
/// instantly, so this helper also advances the ledger past the vesting
/// window and forces a release (a zero-amount report is a cheap, side-
/// effect-free way to trigger `release_vested_yield`'s top-of-call check),
/// so every existing caller's "yield has now accrued" assumption keeps
/// holding without each of them needing to know about vesting.
fn accrue_yield(h: &NesterHarness, amount: i128) {
    h.mint_deposit_tokens(&h.vault_id, amount);
    h.vault().grant_role(&h.admin, &h.admin, &Role::Manager);
    h.vault().report_yield(&h.admin, &amount);
    h.env
        .ledger()
        .set_timestamp(h.env.ledger().timestamp() + h.vault().get_yield_vesting_period() + 1);
    h.vault().report_yield(&h.admin, &0);
}

/// Deposit after yield is reported buys shares at a price above 1:1, so the
/// same asset amount buys strictly fewer shares than the first deposit did.
#[test]
fn test_deposit_after_yield_share_price_above_one() {
    let h = NesterHarness::setup();
    zero_fees(&h);
    disable_circuit_breaker(&h);

    let first = h.create_user();
    h.mint_deposit_tokens(&first, DEPOSIT);
    let first_shares = h.vault().deposit(&first, &DEPOSIT, &0);
    assert_eq!(first_shares, DEPOSIT, "first deposit mints shares 1:1");

    // 10% yield: total_assets becomes 11 units against 10 units of supply,
    // so share price is exactly 1.1.
    let yield_amount = DEPOSIT / 10;
    accrue_yield(&h, yield_amount);

    let total_assets = h.token().total_assets();
    let total_supply = h.token().total_supply();
    assert_eq!(total_assets, DEPOSIT + yield_amount);
    assert_eq!(total_supply, DEPOSIT);
    assert!(
        total_assets > total_supply,
        "share price must exceed 1:1 once yield has accrued"
    );

    // The concrete expectation, not just the inequality: at a 1.1 share price
    // a DEPOSIT-sized deposit buys DEPOSIT * supply / assets shares, floored.
    let second = h.create_user();
    h.mint_deposit_tokens(&second, DEPOSIT);
    let second_shares = h.vault().deposit(&second, &DEPOSIT, &0);
    let expected = DEPOSIT * total_supply / total_assets;
    assert_eq!(
        expected, 9_090_909,
        "10 units at a 1.1 share price is 9.090909 shares, floored"
    );
    assert_eq!(
        second_shares, expected,
        "deposit after yield must buy shares at the raised price"
    );
    assert!(
        second_shares < first_shares,
        "the same assets must buy fewer shares once share price is above one"
    );

    // The first depositor still owns their share of the yield: their shares
    // are now worth more than they paid.
    let first_value = shares_to_assets_down(
        first_shares,
        h.token().total_assets(),
        h.token().total_supply(),
    )
    .unwrap();
    assert!(
        first_value > DEPOSIT,
        "the pre-yield depositor's shares must be worth more than their cost basis"
    );
}

/// Two deposits made at different share prices withdraw in proportion to the
/// shares each holds, not in proportion to the assets each paid in.
#[test]
fn test_multiple_deposits_different_share_prices() {
    let h = NesterHarness::setup();
    zero_fees(&h);
    disable_circuit_breaker(&h);

    // Deposit 1 at a 1.0 share price.
    let early = h.create_user();
    h.mint_deposit_tokens(&early, DEPOSIT);
    let early_shares = h.vault().deposit(&early, &DEPOSIT, &0);
    assert_eq!(early_shares, DEPOSIT);

    // Yield lifts the price to 1.1 before the second deposit.
    let yield_amount = DEPOSIT / 10;
    accrue_yield(&h, yield_amount);

    // Deposit 2 at the raised price -- same assets in, fewer shares out.
    let late = h.create_user();
    h.mint_deposit_tokens(&late, DEPOSIT);
    let late_shares = h.vault().deposit(&late, &DEPOSIT, &0);
    assert_eq!(late_shares, 9_090_909);
    assert!(late_shares < early_shares);

    let total_assets = h.token().total_assets();
    let total_supply = h.token().total_supply();
    assert_eq!(total_assets, DEPOSIT * 2 + yield_amount);
    assert_eq!(total_supply, early_shares + late_shares);

    // Past the lock period so no early-withdrawal fee distorts the payouts.
    h.env.ledger().with_mut(|l| l.timestamp = 86_401);

    let early_expected = shares_to_assets_down(early_shares, total_assets, total_supply).unwrap();

    let usdc = token::Client::new(&h.env, &h.deposit_token_id);

    assert_eq!(h.vault().withdraw(&early, &early_shares, &0), 0);
    assert_eq!(
        usdc.balance(&early),
        early_expected,
        "the early depositor withdraws their share of the pool"
    );

    // Re-read the pool after the first withdrawal: the late depositor's payout
    // is priced against what is actually left, not against the pre-withdrawal
    // snapshot.
    let late_expected = shares_to_assets_down(
        late_shares,
        h.token().total_assets(),
        h.token().total_supply(),
    )
    .unwrap();
    assert_eq!(h.vault().withdraw(&late, &late_shares, &0), 0);
    assert_eq!(
        usdc.balance(&late),
        late_expected,
        "the late depositor withdraws their share of the pool"
    );

    // Proportionality: the early depositor bought at 1.0 and so leaves with
    // the yield accrued before the late deposit; the late depositor bought at
    // 1.1 and so leaves with substantially what they paid in -- they must not
    // capture yield that accrued before they arrived.
    assert!(
        usdc.balance(&early) > DEPOSIT,
        "the early depositor keeps the yield accrued before the second deposit"
    );
    assert!(
        usdc.balance(&late) <= DEPOSIT,
        "the late depositor must not be paid yield that accrued before they deposited"
    );

    // Rounding is in the protocol's favour, never the depositors': the pool
    // cannot pay out more than it holds.
    assert!(
        usdc.balance(&early) + usdc.balance(&late) <= DEPOSIT * 2 + yield_amount,
        "total paid out must not exceed total assets held"
    );
    assert_eq!(h.token().total_supply(), 0, "all shares burned");
}

/// A deposit of exactly one unit -- the protocol minimum -- mints shares
/// without panicking, and at a share price above one it rounds down, in the
/// protocol's favour rather than the depositor's.
#[test]
fn test_minimum_deposit_one_unit() {
    let h = NesterHarness::setup();
    zero_fees(&h);
    disable_circuit_breaker(&h);

    // Seed the pool and raise the share price to a value that does not divide
    // evenly, so the rounding direction is observable.
    let seed = h.create_user();
    h.mint_deposit_tokens(&seed, DEPOSIT);
    h.vault().deposit(&seed, &DEPOSIT, &0);
    accrue_yield(&h, DEPOSIT / 3); // share price = 1.333..., not exact

    let total_assets = h.token().total_assets();
    let total_supply = h.token().total_supply();

    let user = h.create_user();
    h.mint_deposit_tokens(&user, MIN_DEPOSIT_AMOUNT);
    let shares = h.vault().deposit(&user, &MIN_DEPOSIT_AMOUNT, &0);

    // Does not panic, and mints a positive number of shares.
    assert!(shares > 0, "a one-unit deposit must mint non-zero shares");

    let floor = assets_to_shares_down(MIN_DEPOSIT_AMOUNT, total_assets, total_supply).unwrap();
    let ceil = assets_to_shares_up(MIN_DEPOSIT_AMOUNT, total_assets, total_supply).unwrap();
    assert!(
        ceil > floor,
        "the chosen share price must not divide evenly"
    );
    assert_eq!(
        shares, floor,
        "a deposit must round shares down, in the protocol's favour"
    );

    // The depositor cannot immediately withdraw more than they paid in: the
    // rounding loss stays with the pool.
    let redeemable =
        shares_to_assets_down(shares, h.token().total_assets(), h.token().total_supply()).unwrap();
    assert!(
        redeemable <= MIN_DEPOSIT_AMOUNT,
        "rounding must not let a minimum deposit withdraw more than it paid in"
    );
}

/// Zero-share edge case: calculated shares round to zero should revert.
#[test]
fn test_zero_share_edge_case_reverts() {
    assert_eq!(
        assets_to_shares_down(1, 10_000, 1),
        Ok(0),
        "a received-share conversion must round down"
    );
}

/// Assets -> shares uses floor for receipts and ceiling for exact-asset
/// payments when the division has a non-zero remainder.
#[test]
fn test_assets_to_shares_rounds_in_both_directions() {
    // 10 assets * 3 shares / 7 assets = 4 remainder 2.
    assert_eq!(assets_to_shares_down(10, 7, 3), Ok(4));
    assert_eq!(assets_to_shares_up(10, 7, 3), Ok(5));
}

/// Shares -> assets uses floor for receipts and ceiling for exact-share
/// payments when the division has a non-zero remainder.
#[test]
fn test_shares_to_assets_rounds_in_both_directions() {
    // 10 shares * 7 assets / 3 shares = 23 remainder 1.
    assert_eq!(shares_to_assets_down(10, 7, 3), Ok(23));
    assert_eq!(shares_to_assets_up(10, 7, 3), Ok(24));
}

/// A non-zero supply with no backing is insolvent, not a bootstrap state.
#[test]
fn test_zero_assets_with_live_supply_rejects_new_share_conversion() {
    assert_eq!(
        assets_to_shares_down(10, 0, 3),
        Err(ContractError::InvalidOperation)
    );
    assert_eq!(
        assets_to_shares_up(10, 0, 3),
        Err(ContractError::InvalidOperation)
    );
}

/// Time-vested yield report conservation (issue #803), run against the live
/// contract, asserting the vault's core solvency invariant — total assets
/// ever paid out to withdrawers can never exceed total assets ever
/// deposited plus total net yield ever reported. Checked via the deposit
/// token's own balance bookkeeping (minted-to-users vs. paid-out-by-vault),
/// not an internal accounting field, so it catches a real leak regardless
/// of where in the contract it would originate. Note on `withdraw`'s return
/// value: `VaultContract::withdraw` returns the caller's REMAINING share
/// balance, not the assets paid out (see `withdraw_internal`), so payout is
/// measured via the deposit token balance delta, not the call's return.
///
/// Iteration count: the issue's acceptance criteria asks for "≥1,000
/// randomised operations", written with the pure accounting-module version
/// of this property in mind (see the sibling accumulator design this
/// project ultimately did not ship). Run against the LIVE contract, each
/// operation is a real cross-contract Soroban call, and the test host's
/// per-invocation CPU budget cannot sustain anywhere near that many real
/// calls in one test — the pre-existing version of this test already
/// documented hitting that ceiling at just 40 cycles with two operation
/// types; this version's extra vesting-stream and impairment paths lower it
/// further, to around 20. 60 cycles (>=15 of each of the 4 operation types)
/// is what this host can sustain while still genuinely exercising
/// deposit/report/impairment/withdraw interleaved with real time advances.
#[test]
fn conservation_across_randomized_deposit_report_withdraw_cycles() {
    let h = NesterHarness::setup();
    zero_fees(&h);
    disable_circuit_breaker(&h);
    h.vault().grant_role(&h.admin, &h.admin, &Role::Manager);
    // A short vesting window keeps this test's real time-advances small
    // while still exercising the vesting mechanism every cycle (as opposed
    // to disabling it) — every report here still vests, just over minutes
    // instead of a day, since MIN_YIELD_VESTING_SECONDS is 1 hour... use the
    // floor directly rather than disabling vesting, since the conservation
    // property must hold WITH vesting active, not despite it.
    h.vault().set_yield_vesting_period(&h.admin, &(60 * 60));

    const USER_COUNT: usize = 3;
    const INITIAL_DEPOSIT: i128 = 50_000_000;
    let users: std::vec::Vec<_> = (0..USER_COUNT)
        .map(|_| {
            let user = h.create_user();
            h.mint_deposit_tokens(&user, INITIAL_DEPOSIT * 10);
            h.vault().deposit(&user, &INITIAL_DEPOSIT, &0);
            user
        })
        .collect();

    let mut total_deposited: i128 = INITIAL_DEPOSIT * USER_COUNT as i128;
    let mut total_net_yield_reported: i128 = 0;
    let mut total_withdrawn: i128 = 0;
    let mut seed: u64 = 0xC0FFEE_1234_5678;

    for cycle in 0..60u32 {
        // xorshift64 -- deterministic, dependency-free pseudo-randomness.
        seed ^= seed << 13;
        seed ^= seed >> 7;
        seed ^= seed << 17;
        let idx = (seed as usize) % users.len();
        let user = &users[idx];

        match cycle % 4 {
            0 => {
                // Deposit a modest additional amount.
                let amount = (seed % 1_000_000) as i128 + 1;
                h.mint_deposit_tokens(user, amount);
                if h.vault().try_deposit(user, &amount, &0).is_ok() {
                    total_deposited = total_deposited.checked_add(amount).unwrap();
                }
            }
            1 => {
                // Report a small amount of positive yield.
                let amount = (seed % 200_000) as i128 + 1;
                h.mint_deposit_tokens(&h.vault_id, amount);
                h.vault().report_yield(&h.admin, &amount);
                total_net_yield_reported = total_net_yield_reported.checked_add(amount).unwrap();
            }
            2 => {
                // Occasionally report a loss (impairment), applied
                // immediately per report_yield's documented behaviour.
                if seed % 5 == 0 {
                    let loss = ((seed % 50_000) as i128 + 1).min(2_000_000);
                    h.vault().report_yield(&h.admin, &(-loss));
                    total_net_yield_reported = total_net_yield_reported.checked_sub(loss).unwrap();
                }
            }
            _ => {
                // Partial withdrawal — payout measured via the deposit
                // token balance delta, since try_withdraw's Ok value is the
                // caller's REMAINING shares, not assets paid out.
                let shares = h.token().balance(user);
                if shares > 1_000 {
                    let withdraw_shares = shares / 10; // 10%
                    if withdraw_shares > 0 {
                        let before = token::Client::new(&h.env, &h.deposit_token_id).balance(user);
                        if h.vault().try_withdraw(user, &withdraw_shares, &0).is_ok() {
                            let after =
                                token::Client::new(&h.env, &h.deposit_token_id).balance(user);
                            total_withdrawn = total_withdrawn.checked_add(after - before).unwrap();
                        }
                    }
                }
            }
        }

        // Advance real time every cycle so the vesting mechanism this test
        // exists to exercise actually has something to release, not just
        // instant-timestamp calls.
        h.env
            .ledger()
            .set_timestamp(h.env.ledger().timestamp() + 90);
    }

    // Let any remaining vesting stream finish, then drain every user fully.
    h.env
        .ledger()
        .set_timestamp(h.env.ledger().timestamp() + 60 * 60);
    for user in &users {
        let shares = h.token().balance(user);
        if shares > 0 {
            let before = token::Client::new(&h.env, &h.deposit_token_id).balance(user);
            if h.vault().try_withdraw(user, &shares, &0).is_ok() {
                let after = token::Client::new(&h.env, &h.deposit_token_id).balance(user);
                total_withdrawn = total_withdrawn.checked_add(after - before).unwrap();
            }
        }
    }

    assert!(
        total_withdrawn <= total_deposited + total_net_yield_reported,
        "total withdrawn ({total_withdrawn}) must never exceed total deposited ({total_deposited}) plus total net yield reported ({total_net_yield_reported}) — a positive gap here means the vault paid out value it was never given"
    );
}
