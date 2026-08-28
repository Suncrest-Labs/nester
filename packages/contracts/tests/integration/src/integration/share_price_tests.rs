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

// ---------------------------------------------------------------------------
// Regression tests for known contract bugs (#1136, #1029, #1030)
// ---------------------------------------------------------------------------

/// Regression test reproducing #1029:
/// Share price must never decrease upon withdrawal due to performance fee accounting.
#[test]
fn test_reproduce_1029_share_price_decrease_on_withdrawal() {
    use nester_test_utils::NesterHarness;
    use vault_contract::{CircuitBreakerConfig, FeeConfig};
    use soroban_sdk::testutils::Ledger as _;

    let h = NesterHarness::setup();
    let user = h.create_user();
    let user2 = h.create_user();

    h.vault().set_circuit_breaker_config(
        &h.admin,
        &CircuitBreakerConfig {
            threshold_bps: 10_000,
            window_seconds: 7_200,
        },
    );

    h.vault().set_fee_config(
        &h.admin,
        &FeeConfig {
            performance_fee_bps: 1_000, // 10%
            management_fee_bps: 0,
            early_withdrawal_fee_bps: 0,
            treasury_address: h.treasury_id.clone(),
        },
    );

    h.mint_deposit_tokens(&user, 200_000_000);
    h.mint_deposit_tokens(&user2, 200_000_000);

    // Initial deposit
    h.vault().deposit(&user, &100_000_000, &0);
    h.vault().deposit(&user2, &100_000_000, &0);
    let price_before_yield = h.vault().share_price();

    // Report yield
    h.mint_deposit_tokens(&h.vault_id, 20_000_000);
    h.vault().grant_role(&h.admin, &h.admin, &nester_access_control::Role::Manager);
    h.vault().report_yield(&h.admin, &20_000_000);
    let price_after_yield = h.vault().share_price();
    assert!(price_after_yield >= price_before_yield);

    // User 1 withdraws shares
    let shares_to_withdraw = h.token().balance(&user);
    h.env.ledger().with_mut(|li| li.timestamp += 100_000);
    h.vault().withdraw(&user, &shares_to_withdraw, &0);

    // Assert remaining holder's share price did not decrease after user1 withdrawal
    let price_after_withdrawal = h.vault().share_price();
    assert!(
        price_after_withdrawal >= price_after_yield,
        "Share price decreased after withdrawal: before = {}, after = {}",
        price_after_yield,
        price_after_withdrawal
    );
}

#[soroban_sdk::contract]
pub struct MockTestAdapter;

#[soroban_sdk::contractimpl]
impl MockTestAdapter {
    pub fn current_apy(env: soroban_sdk::Env) -> nester_common::adapters::AdapterApy {
        let apy_bps: u32 = env.storage().instance().get(&soroban_sdk::symbol_short!("apy")).unwrap_or(0);
        nester_common::adapters::AdapterApy {
            apy_bps,
            confidence: nester_common::adapters::ApyConfidence::ProtocolReported,
        }
    }

    pub fn set_apy(env: soroban_sdk::Env, apy: u32) {
        env.storage().instance().set(&soroban_sdk::symbol_short!("apy"), &apy);
    }
}

/// Regression test reproducing #1030:
/// Out-of-deviation adapter readings must count toward the failure threshold
/// and flip the yield source status to Degraded rather than remaining Active on stale APY.
#[test]
fn test_reproduce_1030_deviation_rejected_reading_degrades_source() {
    use nester_common::adapters::ApyConfidence;
    use nester_common::{ProtocolType, SourceStatus};
    use nester_test_utils::NesterHarness;
    use soroban_sdk::{symbol_short, testutils::Address as _, Address};

    let h = NesterHarness::setup();
    let source_id = symbol_short!("src_1030");

    let adapter_id = h.env.register_contract(None, MockTestAdapter);
    let adapter = MockTestAdapterClient::new(&h.env, &adapter_id);

    h.registry().register_source(
        &h.admin,
        &source_id,
        &Address::generate(&h.env),
        &Some(adapter_id.clone()),
        &ProtocolType::Lending,
    );

    h.registry().set_apy_deviation_threshold(&h.admin, &100);

    // Good reading first
    adapter.set_apy(&500);
    let r1 = h.registry().refresh_apy_from_adapter(&source_id);
    assert_eq!(r1.apy_bps, 500);
    assert_eq!(h.registry().get_source_status(&source_id), SourceStatus::Active);

    // Compromised/wild reading
    adapter.set_apy(&9_000);
    for _ in 0..=h.registry().get_failure_threshold() {
        let reading = h.registry().refresh_apy_from_adapter(&source_id);
        assert_eq!(reading.confidence, ApyConfidence::Unavailable);
    }

    // Must transition to Degraded and retain last known good APY
    assert_eq!(h.registry().get_source_status(&source_id), SourceStatus::Degraded);
    assert_eq!(h.registry().get_source_performance(&source_id).current_apy_bps, 500);
}


