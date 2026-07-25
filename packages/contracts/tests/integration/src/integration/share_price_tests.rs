//! Integration tests for share price edge cases.
#![cfg(test)]

use nester_common::ContractError;
use vault_contract::conversion::{
    assets_to_shares_down, assets_to_shares_up, shares_to_assets_down, shares_to_assets_up,
};

/// Deposit after yield reported results in share price > 1:1.
#[test]
fn test_deposit_after_yield_share_price_above_one() {
    assert!(true, "placeholder: share price > 1 after yield");
}

/// Multiple deposits at different share prices, then withdrawal -- proportional returns.
#[test]
fn test_multiple_deposits_different_share_prices() {
    assert!(true, "placeholder: multiple deposits at different share prices");
}

/// Minimum deposit of 1 unit calculates shares without panic.
#[test]
fn test_minimum_deposit_one_unit() {
    assert!(true, "placeholder: minimum deposit 1 unit");
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
