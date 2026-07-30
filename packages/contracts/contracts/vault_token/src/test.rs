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

// burn() now requires vault auth (security fix for issue #500).
// With mock_all_auths the vault auth is satisfied, so the happy path still works.
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

/// A user calling burn() directly (without the vault authorising the call)
/// must be rejected. This prevents bypassing vault fee accounting.
#[test]
#[should_panic]
fn non_vault_cannot_burn_directly() {
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
    client.mint_for_deposit(&user, &5_000_i128);

    // Strip all mocked auths — the vault's require_auth() will now fire and panic.
    env.set_auths(&[]);
    client.burn(&user, &1_000_i128);
}

/// Vault-authorised burn updates both share balance and total_assets correctly.
#[test]
fn vault_authorised_burn_updates_accounting() {
    let env = Env::default();
    env.mock_all_auths();
    let (client, _, _) = setup(&env);

    let user = Address::generate(&env);
    // Deposit 10_000; simulate 10% yield → total_assets = 11_000
    client.mint_for_deposit(&user, &10_000_i128);
    client.set_total_assets(&11_000_i128);

    // Vault burns 5_000 shares on behalf of a withdrawal.
    // assets_to_reduce = 5_000 * 11_000 / 10_000 = 5_500
    client.burn(&user, &5_000_i128);

    assert_eq!(client.balance(&user), 5_000);
    assert_eq!(client.total_supply(), 5_000);
    assert_eq!(client.total_assets(), 5_500);
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

// ---------------------------------------------------------------------------
// Authorisation: vault-only mint / burn (#51)
// ---------------------------------------------------------------------------
//
// `mint_for_deposit` and `burn_for_withdrawal` go through `require_vault`,
// which calls `vault.require_auth()`. With no mocked auths the host-level
// auth check fails and the `try_*` client method returns an error — that is
// the contract's "unauthorized minter rejected" guarantee.

#[test]
fn mint_for_deposit_unauthorized_caller_rejected() {
    let env = Env::default();
    let (client, _vault, _) = setup(&env);

    env.mock_auths(&[]);
    let user = Address::generate(&env);
    let result = client.try_mint_for_deposit(&user, &1_000_i128);
    assert!(result.is_err(), "non-vault caller must not be able to mint");
}

#[test]
fn burn_for_withdrawal_unauthorized_caller_rejected() {
    let env = Env::default();
    let (client, _vault, _) = setup(&env);

    // Seed the user with shares first (vault auth still mocked here).
    let user = Address::generate(&env);
    client.mint_for_deposit(&user, &1_000_i128);

    env.mock_auths(&[]);
    let result = client.try_burn_for_withdrawal(&user, &500_i128);
    assert!(result.is_err(), "non-vault caller must not be able to burn");
}

#[test]
fn set_total_assets_unauthorized_caller_rejected() {
    let env = Env::default();
    let (client, _vault, _) = setup(&env);

    env.mock_auths(&[]);
    let result = client.try_set_total_assets(&500_i128);
    assert!(result.is_err(), "non-vault caller must not update total_assets");
}

// ---------------------------------------------------------------------------
// Balance + supply guards (#51)
// ---------------------------------------------------------------------------

#[test]
#[should_panic]
fn burn_more_than_balance_panics() {
    let env = Env::default();
    let (client, _vault, _) = setup(&env);
    let user = Address::generate(&env);
    client.mint_for_deposit(&user, &100_i128);
    // Burns more than the user holds → InsufficientBalance.
    client.burn(&user, &200_i128);
}

#[test]
#[should_panic]
fn burn_from_more_than_balance_panics() {
    let env = Env::default();
    let (current_ledger, _) = (env.ledger().sequence(), 0);
    let (client, _vault, _) = setup(&env);
    let owner = Address::generate(&env);
    let spender = Address::generate(&env);
    client.mint_for_deposit(&owner, &100_i128);
    client.approve(&owner, &spender, &500_i128, &(current_ledger + 1_000));
    // Allowance permits 500, but the owner only has 100. Must panic on the
    // balance check, not on the allowance check.
    client.burn_from(&spender, &owner, &200_i128);
}

#[test]
#[should_panic]
fn transfer_more_than_balance_panics() {
    let env = Env::default();
    let (client, _vault, _) = setup(&env);
    let from = Address::generate(&env);
    let to = Address::generate(&env);
    client.mint_for_deposit(&from, &100_i128);
    client.transfer(&from, &to, &200_i128);
}

#[test]
fn transfer_to_self_is_a_no_op() {
    let env = Env::default();
    let (client, _vault, _) = setup(&env);
    let user = Address::generate(&env);
    client.mint_for_deposit(&user, &1_000_i128);

    let before_supply = client.total_supply();
    client.transfer(&user, &user, &500_i128);

    assert_eq!(client.balance(&user), 1_000_i128, "self-transfer must preserve balance");
    assert_eq!(client.total_supply(), before_supply, "self-transfer must not change supply");
}

// ---------------------------------------------------------------------------
// Allowance guards (#51)
// ---------------------------------------------------------------------------

#[test]
#[should_panic]
fn transfer_from_over_allowance_panics() {
    let env = Env::default();
    let current_ledger = env.ledger().sequence();
    let (client, _vault, _) = setup(&env);
    let owner = Address::generate(&env);
    let spender = Address::generate(&env);
    let to = Address::generate(&env);
    client.mint_for_deposit(&owner, &1_000_i128);
    client.approve(&owner, &spender, &100_i128, &(current_ledger + 1_000));
    // Allowance is 100, attempted spend is 500 → must panic.
    client.transfer_from(&spender, &owner, &to, &500_i128);
}

#[test]
fn transfer_from_decrements_allowance() {
    let env = Env::default();
    let current_ledger = env.ledger().sequence();
    let (client, _vault, _) = setup(&env);
    let owner = Address::generate(&env);
    let spender = Address::generate(&env);
    let to = Address::generate(&env);
    client.mint_for_deposit(&owner, &1_000_i128);
    client.approve(&owner, &spender, &800_i128, &(current_ledger + 1_000));

    client.transfer_from(&spender, &owner, &to, &300_i128);
    assert_eq!(client.allowance(&owner, &spender), 500_i128, "allowance decremented");
    assert_eq!(client.balance(&to), 300_i128);
    assert_eq!(client.balance(&owner), 700_i128);
}

// ---------------------------------------------------------------------------
// Total supply invariant across a full mint → transfer → burn sequence (#51)
// ---------------------------------------------------------------------------

#[test]
fn total_supply_invariant_across_full_sequence() {
    let env = Env::default();
    let (client, _vault, _) = setup(&env);

    let a = Address::generate(&env);
    let b = Address::generate(&env);

    // 1. Mint 1_000 to A.
    client.mint_for_deposit(&a, &1_000_i128);
    assert_eq!(client.balance(&a), 1_000_i128);
    assert_eq!(client.balance(&b), 0_i128);
    assert_eq!(client.total_supply(), 1_000_i128);
    assert_eq!(client.total_supply(), client.balance(&a) + client.balance(&b));

    // 2. Transfer 400 A → B. Supply unchanged.
    client.transfer(&a, &b, &400_i128);
    assert_eq!(client.balance(&a), 600_i128);
    assert_eq!(client.balance(&b), 400_i128);
    assert_eq!(client.total_supply(), 1_000_i128, "supply must not change on transfer");
    assert_eq!(client.total_supply(), client.balance(&a) + client.balance(&b));

    // 3. Burn 100 from A's own balance. Supply decreases by 100.
    client.burn(&a, &100_i128);
    assert_eq!(client.balance(&a), 500_i128);
    assert_eq!(client.balance(&b), 400_i128);
    assert_eq!(client.total_supply(), 900_i128);
    assert_eq!(client.total_supply(), client.balance(&a) + client.balance(&b));

    // 4. Burn the rest via burn_for_withdrawal. Supply → 0.
    client.burn_for_withdrawal(&a, &500_i128);
    client.burn_for_withdrawal(&b, &400_i128);
    assert_eq!(client.balance(&a), 0_i128);
    assert_eq!(client.balance(&b), 0_i128);
    assert_eq!(client.total_supply(), 0_i128);
}

#[test]
fn balance_of_unfunded_account_is_zero() {
    let env = Env::default();
    let (client, _vault, _) = setup(&env);
    let stranger = Address::generate(&env);
    assert_eq!(client.balance(&stranger), 0_i128);
}
