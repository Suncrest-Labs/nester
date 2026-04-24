#![cfg(test)]

extern crate std;

use soroban_sdk::{
    testutils::{Address as _, Ledger as _},
    Address, Env, String,
};

use crate::{VaultTokenContract, VaultTokenContractClient};

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

fn setup(env: &Env) -> (VaultTokenContractClient<'_>, Address, Address) {
    env.mock_all_auths();
    let vault = Address::generate(env);
    let token_id = env.register_contract(None, VaultTokenContract);
    let client = VaultTokenContractClient::new(env, &token_id);
    client.initialize(
        &vault,
        &String::from_str(env, "Nester USDC Vault"),
        &String::from_str(env, "nUSDC"),
        &7u32,
    );
    (client, vault, token_id)
}

// ---------------------------------------------------------------------------
// Initialisation
// ---------------------------------------------------------------------------

#[test]
fn initialize_sets_metadata() {
    let env = Env::default();
    env.mock_all_auths();
    let (client, _, _) = setup(&env);

    assert_eq!(client.name(), String::from_str(&env, "Nester USDC Vault"));
    assert_eq!(client.symbol(), String::from_str(&env, "nUSDC"));
    assert_eq!(client.decimals(), 7u32);
}

#[test]
fn initialize_sets_zero_supply_and_assets() {
    let env = Env::default();
    env.mock_all_auths();
    let (client, _, _) = setup(&env);

    assert_eq!(client.total_supply(), 0);
    assert_eq!(client.total_assets(), 0);
}

#[test]
#[should_panic]
fn initialize_twice_panics() {
    let env = Env::default();
    env.mock_all_auths();
    let (client, vault, _) = setup(&env);
    client.initialize(
        &vault,
        &String::from_str(&env, "Dup"),
        &String::from_str(&env, "DUP"),
        &7u32,
    );
}

// ---------------------------------------------------------------------------
// First deposit — 1:1 share issuance
// ---------------------------------------------------------------------------

#[test]
fn first_deposit_mints_one_to_one() {
    let env = Env::default();
    env.mock_all_auths();
    let (client, vault, _) = setup(&env);
    let user = Address::generate(&env);

    let _ = vault; // vault auth is mocked
    let shares = client.mint_for_deposit(&user, &1_000_i128);

    assert_eq!(shares, 1_000);
    assert_eq!(client.balance(&user), 1_000);
    assert_eq!(client.total_supply(), 1_000);
    assert_eq!(client.total_assets(), 1_000);
}

#[test]
fn shares_for_deposit_preview_first_deposit() {
    let env = Env::default();
    env.mock_all_auths();
    let (client, _, _) = setup(&env);

    // total_supply == 0 → 1:1
    assert_eq!(client.shares_for_deposit(&5_000_i128), 5_000);
}

// ---------------------------------------------------------------------------
// Subsequent deposits — proportional share issuance
// ---------------------------------------------------------------------------

#[test]
fn second_deposit_proportional_shares() {
    let env = Env::default();
    env.mock_all_auths();
    let (client, _, _) = setup(&env);

    let alice = Address::generate(&env);
    let bob = Address::generate(&env);

    // Alice: first deposit 10_000 → 10_000 shares, 10_000 assets
    client.mint_for_deposit(&alice, &10_000_i128);

    // Simulate yield: total_assets grows to 12_000
    client.set_total_assets(&12_000_i128);

    // Bob deposits 6_000. shares = 6_000 * 10_000 / 12_000 = 5_000
    let bob_shares = client.mint_for_deposit(&bob, &6_000_i128);

    assert_eq!(bob_shares, 5_000);
    assert_eq!(client.balance(&bob), 5_000);
    assert_eq!(client.total_supply(), 15_000);
    assert_eq!(client.total_assets(), 18_000); // 12_000 + 6_000
}

#[test]
fn shares_for_deposit_preview_with_existing_supply() {
    let env = Env::default();
    env.mock_all_auths();
    let (client, _, _) = setup(&env);

    let user = Address::generate(&env);
    client.mint_for_deposit(&user, &10_000_i128);
    client.set_total_assets(&20_000_i128); // 2× yield

    // New deposit of 2_000: shares = 2_000 * 10_000 / 20_000 = 1_000
    assert_eq!(client.shares_for_deposit(&2_000_i128), 1_000);
}

// ---------------------------------------------------------------------------
// Withdrawals
// ---------------------------------------------------------------------------

#[test]
fn partial_withdrawal_burns_proportional_shares() {
    let env = Env::default();
    env.mock_all_auths();
    let (client, _, _) = setup(&env);

    let user = Address::generate(&env);
    client.mint_for_deposit(&user, &10_000_i128);

    // Burn 4_000 shares out of 10_000; assets = 10_000 * 4_000 / 10_000 = 4_000
    let amount = client.burn_for_withdrawal(&user, &4_000_i128);

    assert_eq!(amount, 4_000);
    assert_eq!(client.balance(&user), 6_000);
    assert_eq!(client.total_supply(), 6_000);
    assert_eq!(client.total_assets(), 6_000);
}

#[test]
fn full_withdrawal_leaves_zero_supply() {
    let env = Env::default();
    env.mock_all_auths();
    let (client, _, _) = setup(&env);

    let user = Address::generate(&env);
    client.mint_for_deposit(&user, &10_000_i128);

    let amount = client.burn_for_withdrawal(&user, &10_000_i128);

    assert_eq!(amount, 10_000);
    assert_eq!(client.balance(&user), 0);
    assert_eq!(client.total_supply(), 0);
    assert_eq!(client.total_assets(), 0);
}

#[test]
fn withdrawal_after_yield_returns_more_than_deposit() {
    let env = Env::default();
    env.mock_all_auths();
    let (client, _, _) = setup(&env);

    let user = Address::generate(&env);
    // Deposit 10_000 → 10_000 shares
    client.mint_for_deposit(&user, &10_000_i128);

    // Yield: assets grow to 11_000 (+10%)
    client.set_total_assets(&11_000_i128);

    // Redeem all 10_000 shares → 11_000 assets
    let amount = client.burn_for_withdrawal(&user, &10_000_i128);
    assert_eq!(amount, 11_000);
}

#[test]
fn amount_for_shares_preview_reflects_yield() {
    let env = Env::default();
    env.mock_all_auths();
    let (client, _, _) = setup(&env);

    let user = Address::generate(&env);
    client.mint_for_deposit(&user, &10_000_i128);
    client.set_total_assets(&15_000_i128);

    // 5_000 shares out of 10_000 supply = 50% of 15_000 assets = 7_500
    assert_eq!(client.amount_for_shares(&5_000_i128), 7_500);
}

// ---------------------------------------------------------------------------
// Yield accrual simulation
// ---------------------------------------------------------------------------

#[test]
fn two_depositors_share_yield_proportionally() {
    let env = Env::default();
    env.mock_all_auths();
    let (client, _, _) = setup(&env);

    let alice = Address::generate(&env);
    let bob = Address::generate(&env);

    // Alice deposits 10_000 → 10_000 shares
    client.mint_for_deposit(&alice, &10_000_i128);
    // Bob deposits 10_000 → 10_000 shares (same rate, no yield yet)
    client.mint_for_deposit(&bob, &10_000_i128);

    assert_eq!(client.total_supply(), 20_000);
    assert_eq!(client.total_assets(), 20_000);

    // Yield: total_assets goes to 24_000 (+20%)
    client.set_total_assets(&24_000_i128);

    // Each holds 10_000 / 20_000 = 50% of vault → 12_000 each
    let alice_out = client.burn_for_withdrawal(&alice, &10_000_i128);
    assert_eq!(alice_out, 12_000);

    // Remaining: 10_000 shares, 12_000 assets
    let bob_out = client.burn_for_withdrawal(&bob, &10_000_i128);
    assert_eq!(bob_out, 12_000);

    assert_eq!(client.total_supply(), 0);
    assert_eq!(client.total_assets(), 0);
}

#[test]
fn late_depositor_does_not_capture_prior_yield() {
    let env = Env::default();
    env.mock_all_auths();
    let (client, _, _) = setup(&env);

    let alice = Address::generate(&env);
    let bob = Address::generate(&env);

    // Alice deposits 10_000 → 10_000 shares
    client.mint_for_deposit(&alice, &10_000_i128);

    // Yield accrues: 10_000 → 12_000
    client.set_total_assets(&12_000_i128);

    // Bob deposits 12_000: shares = 12_000 * 10_000 / 12_000 = 10_000
    client.mint_for_deposit(&bob, &12_000_i128);

    // After Bob's deposit: supply=20_000, assets=24_000
    assert_eq!(client.total_supply(), 20_000);
    assert_eq!(client.total_assets(), 24_000);

    // Alice redeems 10_000 shares → 10_000/20_000 * 24_000 = 12_000
    let alice_out = client.burn_for_withdrawal(&alice, &10_000_i128);
    assert_eq!(alice_out, 12_000); // earned yield

    // Bob redeems 10_000 shares → 10_000/10_000 * 12_000 = 12_000
    let bob_out = client.burn_for_withdrawal(&bob, &10_000_i128);
    assert_eq!(bob_out, 12_000); // exactly what he deposited
}

// ---------------------------------------------------------------------------
// SEP-41: transfer
// ---------------------------------------------------------------------------

#[test]
fn transfer_moves_balance_between_accounts() {
    let env = Env::default();
    env.mock_all_auths();
    let (client, _, _) = setup(&env);

    let alice = Address::generate(&env);
    let bob = Address::generate(&env);

    client.mint_for_deposit(&alice, &10_000_i128);
    client.transfer(&alice, &bob, &3_000_i128);

    assert_eq!(client.balance(&alice), 7_000);
    assert_eq!(client.balance(&bob), 3_000);
    // Total supply unchanged
    assert_eq!(client.total_supply(), 10_000);
}

#[test]
#[should_panic]
fn transfer_insufficient_balance_panics() {
    let env = Env::default();
    env.mock_all_auths();
    let (client, _, _) = setup(&env);

    let alice = Address::generate(&env);
    let bob = Address::generate(&env);

    client.mint_for_deposit(&alice, &1_000_i128);
    client.transfer(&alice, &bob, &2_000_i128);
}

// ---------------------------------------------------------------------------
// SEP-41: approve / allowance / transfer_from
// ---------------------------------------------------------------------------

#[test]
fn approve_and_transfer_from() {
    let env = Env::default();
    env.mock_all_auths();
    let (client, _, _) = setup(&env);

    let alice = Address::generate(&env);
    let spender = Address::generate(&env);
    let bob = Address::generate(&env);

    client.mint_for_deposit(&alice, &10_000_i128);

    // Alice approves spender for 5_000, expires at ledger 100
    client.approve(&alice, &spender, &5_000_i128, &100u32);
    assert_eq!(client.allowance(&alice, &spender), 5_000);

    // Spender transfers 2_000 from Alice to Bob
    client.transfer_from(&spender, &alice, &bob, &2_000_i128);

    assert_eq!(client.balance(&alice), 8_000);
    assert_eq!(client.balance(&bob), 2_000);
    assert_eq!(client.allowance(&alice, &spender), 3_000);
}

#[test]
#[should_panic]
fn transfer_from_exceeds_allowance_panics() {
    let env = Env::default();
    env.mock_all_auths();
    let (client, _, _) = setup(&env);

    let alice = Address::generate(&env);
    let spender = Address::generate(&env);
    let bob = Address::generate(&env);

    client.mint_for_deposit(&alice, &10_000_i128);
    client.approve(&alice, &spender, &1_000_i128, &100u32);
    client.transfer_from(&spender, &alice, &bob, &2_000_i128);
}

#[test]
fn expired_allowance_returns_zero() {
    let env = Env::default();
    env.mock_all_auths();
    let (client, _, _) = setup(&env);

    let alice = Address::generate(&env);
    let spender = Address::generate(&env);

    client.mint_for_deposit(&alice, &10_000_i128);
    // Approve expiring at ledger 1 (already past when ledger is 0+)
    client.approve(&alice, &spender, &5_000_i128, &0u32);

    // Ledger sequence starts at 0, expiration at 0 → expired (sequence > expiration_ledger)
    // Advance the ledger past 0
    env.ledger().with_mut(|li| li.sequence_number = 1);
    assert_eq!(client.allowance(&alice, &spender), 0);
}

// ---------------------------------------------------------------------------
// SEP-41: burn / burn_from
// ---------------------------------------------------------------------------

#[test]
fn burn_reduces_supply() {
    let env = Env::default();
    env.mock_all_auths();
    let (client, _, _) = setup(&env);

    let user = Address::generate(&env);
    client.mint_for_deposit(&user, &5_000_i128);
    client.burn(&user, &2_000_i128);

    assert_eq!(client.balance(&user), 3_000);
    assert_eq!(client.total_supply(), 3_000);
    assert_eq!(client.total_assets(), 3_000);
}

#[test]
fn burn_from_uses_allowance() {
    let env = Env::default();
    env.mock_all_auths();
    let (client, _, _) = setup(&env);

    let user = Address::generate(&env);
    let spender = Address::generate(&env);

    client.mint_for_deposit(&user, &5_000_i128);
    client.approve(&user, &spender, &3_000_i128, &100u32);
    client.burn_from(&spender, &user, &1_000_i128);

    assert_eq!(client.balance(&user), 4_000);
    assert_eq!(client.total_supply(), 4_000);
    assert_eq!(client.total_assets(), 4_000);
    assert_eq!(client.allowance(&user, &spender), 2_000);
}

// ---------------------------------------------------------------------------
// Vault-only access enforcement
// ---------------------------------------------------------------------------

#[test]
#[should_panic]
fn non_vault_cannot_mint() {
    let env = Env::default();
    // Do NOT mock auths — let the vault auth check fire
    let vault = Address::generate(&env);
    let _non_vault = Address::generate(&env);
    let token_id = env.register_contract(None, VaultTokenContract);
    let client = VaultTokenContractClient::new(&env, &token_id);

    // Initialize with mock auths for setup
    env.mock_all_auths();
    client.initialize(
        &vault,
        &String::from_str(&env, "T"),
        &String::from_str(&env, "T"),
        &7u32,
    );

    // Now call without auths so vault auth fails
    env.set_auths(&[]);
    let user = Address::generate(&env);
    client.mint_for_deposit(&user, &1_000_i128);
}

#[test]
#[should_panic]
fn non_vault_cannot_burn_for_withdrawal() {
    let env = Env::default();
    let vault = Address::generate(&env);
    let token_id = env.register_contract(None, VaultTokenContract);
    let client = VaultTokenContractClient::new(&env, &token_id);

    env.mock_all_auths();
    client.initialize(
        &vault,
        &String::from_str(&env, "T"),
        &String::from_str(&env, "T"),
        &7u32,
    );
    let user = Address::generate(&env);
    client.mint_for_deposit(&user, &1_000_i128);

    env.set_auths(&[]);
    client.burn_for_withdrawal(&user, &500_i128);
}

// ---------------------------------------------------------------------------
// set_total_assets
// ---------------------------------------------------------------------------

#[test]
fn set_total_assets_updates_value() {
    let env = Env::default();
    env.mock_all_auths();
    let (client, _, _) = setup(&env);

    client.set_total_assets(&99_999_i128);
    assert_eq!(client.total_assets(), 99_999);
}

// ---------------------------------------------------------------------------
// Pure math: unit tests for exchange rate helpers
// ---------------------------------------------------------------------------

#[test]
fn pure_shares_for_deposit_first() {
    // total_supply == 0 → 1:1
    assert_eq!(crate::shares_for_deposit_math(1_000, 0, 0), 1_000);
}

#[test]
fn pure_shares_for_deposit_proportional() {
    // 1_000 into vault with 10_000 supply / 20_000 assets → 500 shares
    assert_eq!(crate::shares_for_deposit_math(1_000, 10_000, 20_000), 500);
}

#[test]
fn pure_amount_for_shares_no_supply() {
    // total_supply == 0 → 1:1
    assert_eq!(crate::amount_for_shares_math(500, 0, 0), 500);
}

#[test]
fn pure_amount_for_shares_proportional() {
    // 500 shares of 10_000 supply backed by 20_000 assets → 1_000
    assert_eq!(crate::amount_for_shares_math(500, 10_000, 20_000), 1_000);
}

#[test]
fn pure_floor_division_truncates() {
    // 1 share of 3 supply backed by 10 assets → floor(10/3) = 3
    assert_eq!(crate::amount_for_shares_math(1, 3, 10), 3);
}
