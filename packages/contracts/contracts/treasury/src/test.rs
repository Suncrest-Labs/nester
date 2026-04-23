#![cfg(test)]

use super::*;
use soroban_sdk::{
    symbol_short,
    testutils::Address as _,
    Address, Env,
};

// ---------------------------------------------------------------------------
// Minimal mock vault — just needs to be a registered contract so its address
// can call require_auth() inside receive_fees().
// ---------------------------------------------------------------------------
#[contract]
pub struct MockVault;
#[contractimpl]
impl MockVault {}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

/// Mint `amount` of `token` directly into `contract_address`.
fn mint_to_contract(
    env: &Env,
    token_address: &Address,
    contract_address: &Address,
    amount: i128,
) {
    // We need a StellarAssetClient to mint; the token_admin is embedded in the
    // asset contract we registered in setup().
    // In tests the asset admin address is kept by the caller — see setup().
    let _ = (env, token_address, contract_address, amount); // satisfied below in each test
}

/// Create a fresh environment with mock auths, register all contracts,
/// initialise the treasury, and return the commonly needed handles.
fn setup() -> (
    Env,
    Address,                          // admin
    Address,                          // operator
    Address,                          // vault
    Address,                          // token
    TreasuryContractClient<'static>,  // treasury client
) {
    let env = Env::default();
    env.mock_all_auths();

    let admin = Address::generate(&env);
    let operator = Address::generate(&env);
    let vault = env.register_contract(None, MockVault);

    // Register a Stellar asset contract so we can mint tokens.
    let token_admin = Address::generate(&env);
    let token = env
        .register_stellar_asset_contract_v2(token_admin.clone())
        .address();

    let contract_id = env.register_contract(None, TreasuryContract);
    let client = TreasuryContractClient::new(&env, &contract_id);
    client.initialize(&admin, &vault);

    // Grant operator role to the operator address.
    // AccessControl::grant_role is called via the admin through whatever
    // interface the access_control contract exposes.  In tests with
    // mock_all_auths we can call it directly through any client that wraps
    // the contract — here we replicate the pattern used in other contract
    // tests in this repo by calling the method we know exists.
    // If the access_control module has a grant_role helper we call it;
    // otherwise we note that the operator tests below use `admin` as caller
    // and we add an operator test that asserts Unauthorized for a stranger.
    // For now we store operator separately and test the role boundary.

    (env, admin, operator, vault, token, client)
}

/// Build a standard three-way recipient list summing to 10 000 bps.
fn three_recipients(env: &Env, a: &Address, b: &Address, c: &Address) -> Vec<FeeRecipient> {
    let mut v = Vec::new(env);
    v.push_back(FeeRecipient {
        address: a.clone(),
        share_bps: 7_000,
        label: symbol_short!("protocol"),
        total_received: 0,
    });
    v.push_back(FeeRecipient {
        address: b.clone(),
        share_bps: 2_000,
        label: symbol_short!("operator"),
        total_received: 0,
    });
    v.push_back(FeeRecipient {
        address: c.clone(),
        share_bps: 1_000,
        label: symbol_short!("insurance"),
        total_received: 0,
    });
    v
}

/// Simulate the vault calling receive_fees.
fn vault_receive(env: &Env, vault: &Address, client: &TreasuryContractClient, amount: i128) {
    env.as_contract(vault, || {
        client.receive_fees(&amount);
    });
}

/// Mint tokens directly into the treasury contract.
fn fund_treasury(
    env: &Env,
    token_address: &Address,
    treasury_address: &Address,
    amount: i128,
) {
    let sac = soroban_sdk::token::StellarAssetClient::new(env, token_address);
    sac.mint(treasury_address, &amount);
}

// ---------------------------------------------------------------------------
// Existing behaviour — must not regress
// ---------------------------------------------------------------------------

#[test]
fn test_initialize() {
    let (_env, _admin, _op, vault, _token, client) = setup();
    assert_eq!(client.get_vault(), vault);
    assert_eq!(client.get_total_received(), 0);
    assert_eq!(client.get_total_distributed(), 0);
    assert_eq!(client.get_available_fees(), 0);
    assert!(client.get_recipients().is_empty());
    assert!(client.get_distribution_history().is_empty());
}

#[test]
#[should_panic]
fn test_initialize_rejects_double_init() {
    let (_env, admin, _op, vault, _token, client) = setup();
    // Second call must panic with AlreadyInitialized.
    client.initialize(&admin, &vault);
}

#[test]
fn test_receive_fees_accumulates() {
    let (env, _admin, _op, vault, _token, client) = setup();

    vault_receive(&env, &vault, &client, 1_000);
    assert_eq!(client.get_total_received(), 1_000);
    assert_eq!(client.get_available_fees(), 1_000);

    vault_receive(&env, &vault, &client, 500);
    assert_eq!(client.get_total_received(), 1_500);
    assert_eq!(client.get_available_fees(), 1_500);
}

#[test]
fn test_existing_withdraw_still_works() {
    let (env, admin, _op, _vault, token, client) = setup();
    let to = Address::generate(&env);
    fund_treasury(&env, &token, &client.address, 5_000);

    client.withdraw(&admin, &to, &token, &2_000);

    let token_client = soroban_sdk::token::TokenClient::new(&env, &token);
    assert_eq!(token_client.balance(&to), 2_000);
    assert_eq!(token_client.balance(&client.address), 3_000);
}

// ---------------------------------------------------------------------------
// set_recipients — validation
// ---------------------------------------------------------------------------

#[test]
fn test_set_recipients_happy_path() {
    let (env, admin, _op, _vault, _token, client) = setup();
    let a = Address::generate(&env);
    let b = Address::generate(&env);
    let c = Address::generate(&env);

    client.set_recipients(&admin, &three_recipients(&env, &a, &b, &c));

    let stored = client.get_recipients();
    assert_eq!(stored.len(), 3);
    assert_eq!(stored.get(0).unwrap().share_bps, 7_000);
    assert_eq!(stored.get(1).unwrap().share_bps, 2_000);
    assert_eq!(stored.get(2).unwrap().share_bps, 1_000);
}

#[test]
#[should_panic]
fn test_set_recipients_rejects_bad_bps_sum() {
    let (env, admin, _op, _vault, _token, client) = setup();
    let a = Address::generate(&env);
    let b = Address::generate(&env);

    // 6000 + 2000 = 8000, not 10000 — must panic.
    let mut v = Vec::new(&env);
    v.push_back(FeeRecipient {
        address: a,
        share_bps: 6_000,
        label: symbol_short!("a"),
        total_received: 0,
    });
    v.push_back(FeeRecipient {
        address: b,
        share_bps: 2_000,
        label: symbol_short!("b"),
        total_received: 0,
    });

    client.set_recipients(&admin, &v);
}

#[test]
#[should_panic]
fn test_set_recipients_rejects_over_bps() {
    let (env, admin, _op, _vault, _token, client) = setup();
    let a = Address::generate(&env);

    // 10001 bps — must panic.
    let mut v = Vec::new(&env);
    v.push_back(FeeRecipient {
        address: a,
        share_bps: 10_001,
        label: symbol_short!("a"),
        total_received: 0,
    });

    client.set_recipients(&admin, &v);
}

#[test]
#[should_panic]
fn test_set_recipients_rejects_empty_list() {
    let (env, admin, _op, _vault, _token, client) = setup();
    let empty: Vec<FeeRecipient> = Vec::new(&env);
    client.set_recipients(&admin, &empty);
}

#[test]
fn test_set_recipients_single_recipient_full_share() {
    let (env, admin, _op, _vault, _token, client) = setup();
    let a = Address::generate(&env);

    let mut v = Vec::new(&env);
    v.push_back(FeeRecipient {
        address: a.clone(),
        share_bps: 10_000,
        label: symbol_short!("solo"),
        total_received: 0,
    });

    client.set_recipients(&admin, &v);
    assert_eq!(client.get_recipients().len(), 1);
    assert_eq!(client.get_recipients().get(0).unwrap().address, a);
}

#[test]
fn test_set_recipients_can_be_updated() {
    let (env, admin, _op, _vault, _token, client) = setup();
    let a = Address::generate(&env);
    let b = Address::generate(&env);
    let c = Address::generate(&env);

    // First configuration.
    client.set_recipients(&admin, &three_recipients(&env, &a, &b, &c));
    assert_eq!(client.get_recipients().len(), 3);

    // Replace with a two-way split.
    let new_a = Address::generate(&env);
    let new_b = Address::generate(&env);
    let mut v = Vec::new(&env);
    v.push_back(FeeRecipient {
        address: new_a.clone(),
        share_bps: 5_000,
        label: symbol_short!("x"),
        total_received: 0,
    });
    v.push_back(FeeRecipient {
        address: new_b.clone(),
        share_bps: 5_000,
        label: symbol_short!("y"),
        total_received: 0,
    });

    client.set_recipients(&admin, &v);
    let stored = client.get_recipients();
    assert_eq!(stored.len(), 2);
    assert_eq!(stored.get(0).unwrap().address, new_a);
    assert_eq!(stored.get(1).unwrap().address, new_b);
}

// ---------------------------------------------------------------------------
// distribute — happy path
// ---------------------------------------------------------------------------

#[test]
fn test_distribute_three_recipients() {
    let (env, admin, _op, vault, token, client) = setup();
    let a = Address::generate(&env);
    let b = Address::generate(&env);
    let c = Address::generate(&env);

    client.set_recipients(&admin, &three_recipients(&env, &a, &b, &c));

    // Record fees AND fund the contract with the matching token balance.
    vault_receive(&env, &vault, &client, 10_000);
    fund_treasury(&env, &token, &client.address, 10_000);

    client.distribute(&admin, &token);

    let token_client = soroban_sdk::token::TokenClient::new(&env, &token);
    assert_eq!(token_client.balance(&a), 7_000); // 70 %
    assert_eq!(token_client.balance(&b), 2_000); // 20 %
    assert_eq!(token_client.balance(&c), 1_000); // 10 %

    assert_eq!(client.get_total_distributed(), 10_000);
    assert_eq!(client.get_available_fees(), 0);
}

#[test]
fn test_distribute_single_recipient_gets_everything() {
    let (env, admin, _op, vault, token, client) = setup();
    let a = Address::generate(&env);

    let mut v = Vec::new(&env);
    v.push_back(FeeRecipient {
        address: a.clone(),
        share_bps: 10_000,
        label: symbol_short!("solo"),
        total_received: 0,
    });
    client.set_recipients(&admin, &v);

    vault_receive(&env, &vault, &client, 9_999);
    fund_treasury(&env, &token, &client.address, 9_999);

    client.distribute(&admin, &token);

    let token_client = soroban_sdk::token::TokenClient::new(&env, &token);
    assert_eq!(token_client.balance(&a), 9_999);
    assert_eq!(client.get_available_fees(), 0);
}

#[test]
fn test_distribute_dust_goes_to_last_recipient() {
    // 10_001 tokens with a 70/20/10 split.
    // 7000 + 2000 + 1000 = 10000 — dust = 1 goes to last.
    let (env, admin, _op, vault, token, client) = setup();
    let a = Address::generate(&env);
    let b = Address::generate(&env);
    let c = Address::generate(&env);

    client.set_recipients(&admin, &three_recipients(&env, &a, &b, &c));
    vault_receive(&env, &vault, &client, 10_001);
    fund_treasury(&env, &token, &client.address, 10_001);

    client.distribute(&admin, &token);

    let token_client = soroban_sdk::token::TokenClient::new(&env, &token);
    assert_eq!(token_client.balance(&a), 7_000);
    assert_eq!(token_client.balance(&b), 2_000);
    assert_eq!(token_client.balance(&c), 1_001); // absorbs the 1-unit remainder
    assert_eq!(client.get_total_distributed(), 10_001);
}

#[test]
fn test_distribute_twice_only_distributes_new_fees() {
    let (env, admin, _op, vault, token, client) = setup();
    let a = Address::generate(&env);
    let b = Address::generate(&env);

    let mut v = Vec::new(&env);
    v.push_back(FeeRecipient {
        address: a.clone(),
        share_bps: 6_000,
        label: symbol_short!("a"),
        total_received: 0,
    });
    v.push_back(FeeRecipient {
        address: b.clone(),
        share_bps: 4_000,
        label: symbol_short!("b"),
        total_received: 0,
    });
    client.set_recipients(&admin, &v);

    // Round 1.
    vault_receive(&env, &vault, &client, 1_000);
    fund_treasury(&env, &token, &client.address, 1_000);
    client.distribute(&admin, &token);

    let token_client = soroban_sdk::token::TokenClient::new(&env, &token);
    assert_eq!(token_client.balance(&a), 600);
    assert_eq!(token_client.balance(&b), 400);

    // Round 2 — new fees only.
    vault_receive(&env, &vault, &client, 500);
    fund_treasury(&env, &token, &client.address, 500);
    client.distribute(&admin, &token);

    assert_eq!(token_client.balance(&a), 900);  // 600 + 300
    assert_eq!(token_client.balance(&b), 600);  // 400 + 200
    assert_eq!(client.get_total_distributed(), 1_500);
}

#[test]
fn test_distribute_updates_cumulative_per_recipient() {
    let (env, admin, _op, vault, token, client) = setup();
    let a = Address::generate(&env);
    let b = Address::generate(&env);
    let c = Address::generate(&env);

    client.set_recipients(&admin, &three_recipients(&env, &a, &b, &c));
    vault_receive(&env, &vault, &client, 10_000);
    fund_treasury(&env, &token, &client.address, 10_000);
    client.distribute(&admin, &token);

    // Receive and distribute a second round.
    vault_receive(&env, &vault, &client, 10_000);
    fund_treasury(&env, &token, &client.address, 10_000);
    client.distribute(&admin, &token);

    let recipients = client.get_recipients();
    assert_eq!(recipients.get(0).unwrap().total_received, 14_000); // 7000 * 2
    assert_eq!(recipients.get(1).unwrap().total_received, 4_000);  // 2000 * 2
    assert_eq!(recipients.get(2).unwrap().total_received, 2_000);  // 1000 * 2
}

// ---------------------------------------------------------------------------
// distribute — error cases
// ---------------------------------------------------------------------------

#[test]
#[should_panic]
fn test_distribute_fails_when_no_recipients_configured() {
    let (env, admin, _op, vault, token, client) = setup();
    vault_receive(&env, &vault, &client, 1_000);
    fund_treasury(&env, &token, &client.address, 1_000);
    // No set_recipients called — must panic.
    client.distribute(&admin, &token);
}

#[test]
#[should_panic]
fn test_distribute_fails_when_no_fees_available() {
    let (env, admin, _op, _vault, token, client) = setup();
    let a = Address::generate(&env);
    let mut v = Vec::new(&env);
    v.push_back(FeeRecipient {
        address: a,
        share_bps: 10_000,
        label: symbol_short!("a"),
        total_received: 0,
    });
    client.set_recipients(&admin, &v);
    // No fees received — available = 0 — must panic.
    client.distribute(&admin, &token);
}

#[test]
#[should_panic]
fn test_distribute_fails_after_all_fees_already_distributed() {
    let (env, admin, _op, vault, token, client) = setup();
    let a = Address::generate(&env);
    let mut v = Vec::new(&env);
    v.push_back(FeeRecipient {
        address: a,
        share_bps: 10_000,
        label: symbol_short!("a"),
        total_received: 0,
    });
    client.set_recipients(&admin, &v);

    vault_receive(&env, &vault, &client, 1_000);
    fund_treasury(&env, &token, &client.address, 1_000);
    client.distribute(&admin, &token); // succeeds

    // Second distribute with nothing new — must panic.
    client.distribute(&admin, &token);
}

// ---------------------------------------------------------------------------
// Permission checks
// ---------------------------------------------------------------------------

#[test]
#[should_panic]
fn test_set_recipients_requires_admin() {
    let (env, _admin, _op, _vault, _token, client) = setup();
    let stranger = Address::generate(&env);
    let a = Address::generate(&env);
    let mut v = Vec::new(&env);
    v.push_back(FeeRecipient {
        address: a,
        share_bps: 10_000,
        label: symbol_short!("a"),
        total_received: 0,
    });
    // Stranger has no role — must panic with Unauthorized.
    client.set_recipients(&stranger, &v);
}

#[test]
#[should_panic]
fn test_distribute_requires_admin_or_operator() {
    let (env, admin, _op, vault, token, client) = setup();
    let stranger = Address::generate(&env);
    let a = Address::generate(&env);

    let mut v = Vec::new(&env);
    v.push_back(FeeRecipient {
        address: a,
        share_bps: 10_000,
        label: symbol_short!("a"),
        total_received: 0,
    });
    client.set_recipients(&admin, &v);
    vault_receive(&env, &vault, &client, 1_000);
    fund_treasury(&env, &token, &client.address, 1_000);

    // Stranger has no role — must panic.
    client.distribute(&stranger, &token);
}

// ---------------------------------------------------------------------------
// Distribution history
// ---------------------------------------------------------------------------

#[test]
fn test_distribution_history_records_each_round() {
    let (env, admin, _op, vault, token, client) = setup();
    let a = Address::generate(&env);
    let b = Address::generate(&env);
    let c = Address::generate(&env);

    client.set_recipients(&admin, &three_recipients(&env, &a, &b, &c));

    vault_receive(&env, &vault, &client, 1_000);
    fund_treasury(&env, &token, &client.address, 1_000);
    client.distribute(&admin, &token);

    vault_receive(&env, &vault, &client, 2_000);
    fund_treasury(&env, &token, &client.address, 2_000);
    client.distribute(&admin, &token);

    let history = client.get_distribution_history();
    assert_eq!(history.len(), 2);
    assert_eq!(history.get(0).unwrap().total_amount, 1_000);
    assert_eq!(history.get(0).unwrap().recipient_count, 3);
    assert_eq!(history.get(1).unwrap().total_amount, 2_000);
}

#[test]
fn test_distribution_history_empty_before_any_distribution() {
    let (_, _, _, _, _, client) = setup();
    assert!(client.get_distribution_history().is_empty());
}

// ---------------------------------------------------------------------------
// get_available_fees
// ---------------------------------------------------------------------------

#[test]
fn test_available_fees_reflects_undistributed_balance() {
    let (env, admin, _op, vault, token, client) = setup();
    let a = Address::generate(&env);
    let mut v = Vec::new(&env);
    v.push_back(FeeRecipient {
        address: a,
        share_bps: 10_000,
        label: symbol_short!("a"),
        total_received: 0,
    });
    client.set_recipients(&admin, &v);

    vault_receive(&env, &vault, &client, 3_000);
    assert_eq!(client.get_available_fees(), 3_000);

    fund_treasury(&env, &token, &client.address, 3_000);
    client.distribute(&admin, &token);
    assert_eq!(client.get_available_fees(), 0);

    vault_receive(&env, &vault, &client, 1_000);
    assert_eq!(client.get_available_fees(), 1_000);
}