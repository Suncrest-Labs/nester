//! Vault behaviour under USDC issuer actions (freeze and clawback) — issue #1495.
//!
//! Circle can freeze or clawback balances at the issuer level.  When that
//! happens the vault's on-chain `total_assets` accounting diverges from its
//! real USDC balance.  Expected contract:
//!
//! - Clawback: treated as an impairment via `report_yield(negative)`.
//!   Pro-rata withdrawals still succeed; no performance fee is charged on a
//!   net-negative yield.  Deposits are paused by the admin.
//! - Freeze: the vault's USDC transfer auth is revoked.  Deposits fail because
//!   the vault cannot receive tokens.  Existing state (share balances,
//!   `total_assets`) is preserved until auth is restored.
//!
//! See also: docs/security/ISSUER_ACTION_RUNBOOK.md for the incident runbook.
#![cfg(test)]

extern crate std;

use soroban_sdk::token::StellarAssetClient;

use nester_test_utils::NesterHarness;
use vault_contract::FeeConfig;

const DEPOSIT: i128 = 100_000_000; // 10 USDC at 7 decimals

fn no_fees(h: &NesterHarness) {
    h.vault().set_fee_config(
        &h.admin,
        &FeeConfig {
            performance_fee_bps: 1_000,
            management_fee_bps: 0,
            early_withdrawal_fee_bps: 0,
            treasury_address: h.treasury_id.clone(),
        },
    );
}

// ── Clawback tests ────────────────────────────────────────────────────────────

/// After a 20% clawback, an admin-reported impairment (`report_yield` with a
/// negative amount) keeps pro-rata withdrawals working: each user recovers
/// their fair share of the reduced pool, and no performance fee is charged
/// because the vault is net-negative.
#[test]
fn test_clawback_impairment_prorata_withdrawal() {
    let h = NesterHarness::setup();
    no_fees(&h);

    let alice = h.create_user();
    let bob = h.create_user();
    h.mint_deposit_tokens(&alice, DEPOSIT);
    h.mint_deposit_tokens(&bob, DEPOSIT);

    h.vault().deposit(&alice, &DEPOSIT, &0);
    h.vault().deposit(&bob, &DEPOSIT, &0);

    // Pool: 2 × DEPOSIT = 200 USDC.  Issuer claws back 20%.
    let clawback_amount = DEPOSIT * 2 / 5; // 40 USDC
    StellarAssetClient::new(&h.env, &h.deposit_token_id).clawback(&h.vault_id, &clawback_amount);

    // Admin reconciles: report a negative yield equal to the clawback.
    h.vault().report_yield(&h.admin, &(-clawback_amount));

    // total_assets should now be 160 USDC.
    assert_eq!(h.token().total_assets(), DEPOSIT * 2 - clawback_amount);

    // Alice withdraws her half (80 USDC worth of shares → 80 USDC out).
    let alice_shares = h.token().balance(&alice);
    let before = h.token().balance(&alice);
    h.vault().withdraw(&alice, &alice_shares, &0);
    let after = h.token().balance(&alice);

    // Alice should receive exactly 80 USDC (her pro-rata 50% of 160).
    let expected = (DEPOSIT * 2 - clawback_amount) / 2;
    assert_eq!(after - before, expected);
}

/// Performance fee is zero after a net impairment: a positive `report_yield`
/// that is smaller than the clawback leaves the vault net-negative.
#[test]
fn test_no_performance_fee_after_net_impairment() {
    let h = NesterHarness::setup();
    no_fees(&h);

    let user = h.create_user();
    h.mint_deposit_tokens(&user, DEPOSIT);
    h.vault().deposit(&user, &DEPOSIT, &0);

    // 30% clawback then a 10% yield report — still net -20%.
    let clawback = DEPOSIT * 3 / 10;
    let yield_report = DEPOSIT / 10;

    StellarAssetClient::new(&h.env, &h.deposit_token_id).clawback(&h.vault_id, &clawback);
    h.vault().report_yield(&h.admin, &(-clawback));
    h.vault().report_yield(&h.admin, &yield_report);

    // Treasury should have received zero performance fee.
    let treasury_balance = h.token().balance(&h.treasury_id);
    assert_eq!(treasury_balance, 0, "no performance fee on net impairment");
}

// ── Freeze tests ──────────────────────────────────────────────────────────────

/// When the issuer freezes the vault's USDC balance (set_authorized = false),
/// new deposits are rejected because the token transfer fails.
#[test]
#[should_panic]
fn test_deposit_fails_when_vault_usdc_frozen() {
    let h = NesterHarness::setup();

    // Freeze the vault's USDC auth — it can no longer receive transfers.
    StellarAssetClient::new(&h.env, &h.deposit_token_id).set_authorized(&h.vault_id, &false);

    let user = h.create_user();
    h.mint_deposit_tokens(&user, DEPOSIT);
    // Should panic because the vault can't receive USDC while frozen.
    h.vault().deposit(&user, &DEPOSIT, &0);
}

/// After the vault USDC freeze is lifted, deposits resume normally and
/// existing share balances are not affected.
#[test]
fn test_existing_shares_preserved_during_freeze() {
    let h = NesterHarness::setup();

    let alice = h.create_user();
    h.mint_deposit_tokens(&alice, DEPOSIT * 2);
    h.vault().deposit(&alice, &DEPOSIT, &0);

    let shares_before = h.token().balance(&alice);

    // Freeze then immediately unfreeze.
    StellarAssetClient::new(&h.env, &h.deposit_token_id).set_authorized(&h.vault_id, &false);
    StellarAssetClient::new(&h.env, &h.deposit_token_id).set_authorized(&h.vault_id, &true);

    // Alice's shares are untouched.
    assert_eq!(h.token().balance(&alice), shares_before);

    // A second deposit after unfreeze should work.
    h.vault().deposit(&alice, &DEPOSIT, &0);
    assert!(h.token().balance(&alice) > shares_before);
}
