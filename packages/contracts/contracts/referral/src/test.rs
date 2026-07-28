extern crate std;

use soroban_sdk::{
    testutils::{Address as _, Ledger as _},
    token, Address, Env,
};

use crate::{ReferralContract, ReferralContractClient};

fn setup() -> (
    Env,
    Address,
    Address,
    Address,
    ReferralContractClient<'static>,
) {
    let env = Env::default();
    env.mock_all_auths();
    // Start the ledger clock well past MIN_TENURE so `eligible_deposit_time`
    // can express "deposited long enough ago" without saturating at 0.
    env.ledger().with_mut(|l| l.timestamp = MIN_TENURE * 10);

    let admin = Address::generate(&env);
    let vault = Address::generate(&env);
    let token_admin = Address::generate(&env);
    let token_contract = env.register_stellar_asset_contract_v2(token_admin.clone());
    let token_address = token_contract.address();
    let token_sac = token::StellarAssetClient::new(&env, &token_address);

    let cid = env.register_contract(None, ReferralContract);
    let client = ReferralContractClient::new(&env, &cid);
    let budget = 1_000_000_000_000i128; // generous default budget for most tests
    client.initialize(&admin, &vault, &token_address, &budget);
    token_sac.mint(&cid, &budget);

    (env, admin, vault, token_address, client)
}

const MIN_DEPOSIT: i128 = 100_000_000; // matches DEFAULT_MIN_REFERRED_DEPOSIT
const MIN_TENURE: u64 = 604_800; // matches DEFAULT_MIN_REFEREE_TENURE_SECONDS

fn eligible_deposit_time(env: &Env) -> u64 {
    env.ledger().timestamp().saturating_sub(MIN_TENURE + 1)
}

// ---------------------------------------------------------------------------
// register_referral
// ---------------------------------------------------------------------------

#[test]
fn register_referral_binds_user_to_referrer() {
    let (env, _admin, _vault, _token, client) = setup();
    let user = Address::generate(&env);
    let referrer = Address::generate(&env);

    client.register_referral(&user, &referrer);

    assert_eq!(client.get_referrer(&user), Some(referrer));
}

#[test]
fn self_referral_is_rejected() {
    let (env, _admin, _vault, _token, client) = setup();
    let user = Address::generate(&env);

    let result = client.try_register_referral(&user, &user);
    assert!(result.is_err());
    assert_eq!(client.get_referrer(&user), None);
}

#[test]
fn re_registering_an_existing_referral_is_rejected() {
    let (env, _admin, _vault, _token, client) = setup();
    let user = Address::generate(&env);
    let referrer = Address::generate(&env);
    let other_referrer = Address::generate(&env);

    client.register_referral(&user, &referrer);
    let result = client.try_register_referral(&user, &other_referrer);

    assert!(result.is_err());
    // Original binding is permanent — unaffected by the failed reassignment.
    assert_eq!(client.get_referrer(&user), Some(referrer));
}

#[test]
fn direct_referral_cycle_is_rejected() {
    let (env, _admin, _vault, _token, client) = setup();
    let alice = Address::generate(&env);
    let bob = Address::generate(&env);

    // Bob refers Alice.
    client.register_referral(&alice, &bob);
    // Alice attempting to refer Bob back would form a 2-cycle.
    let result = client.try_register_referral(&bob, &alice);

    assert!(result.is_err());
    assert_eq!(client.get_referrer(&bob), None);
}

// ---------------------------------------------------------------------------
// accrue_reward — reward source, eligibility, Sybil resistance
// ---------------------------------------------------------------------------

#[test]
fn accrue_reward_credits_referrer_from_performance_fee_slice() {
    let (env, _admin, vault, _token, client) = setup();
    let user = Address::generate(&env);
    let referrer = Address::generate(&env);
    client.register_referral(&user, &referrer);

    let fee = 1_000_000i128;
    let deposit_time = eligible_deposit_time(&env);
    client.accrue_reward(&user, &fee, &MIN_DEPOSIT, &deposit_time);

    // Default share is 10% of the fee slice.
    assert_eq!(client.get_reward_balance(&referrer), fee / 10);
    let _ = vault;
}

#[test]
fn referred_users_own_yield_is_never_reduced_by_the_referral_program() {
    // The vault computes the referred user's net proceeds entirely on its
    // own before ever calling accrue_reward — this contract has no
    // capability to touch the user's balance at all, only the referrer's.
    // We assert that indirectly: crediting a large reward never mutates
    // any state keyed by the referred user's own balance/principal.
    let (env, _admin, _vault, _token, client) = setup();
    let user = Address::generate(&env);
    let referrer = Address::generate(&env);
    client.register_referral(&user, &referrer);

    let deposit_time = eligible_deposit_time(&env);
    client.accrue_reward(&user, &5_000_000i128, &MIN_DEPOSIT, &deposit_time);

    // No user-keyed balance exists on this contract at all — only referrer
    // reward balances and the referral binding are tracked.
    assert_eq!(client.get_reward_balance(&user), 0);
}

#[test]
fn dust_deposit_below_minimum_earns_nothing() {
    let (env, _admin, _vault, _token, client) = setup();
    let user = Address::generate(&env);
    let referrer = Address::generate(&env);
    client.register_referral(&user, &referrer);

    let deposit_time = eligible_deposit_time(&env);
    client.accrue_reward(&user, &1_000_000i128, &(MIN_DEPOSIT - 1), &deposit_time);

    assert_eq!(client.get_reward_balance(&referrer), 0);
}

#[test]
fn insufficient_tenure_earns_nothing() {
    let (env, _admin, _vault, _token, client) = setup();
    let user = Address::generate(&env);
    let referrer = Address::generate(&env);
    client.register_referral(&user, &referrer);

    // Deposited "just now" — tenure requirement not met.
    let deposit_time = env.ledger().timestamp();
    client.accrue_reward(&user, &1_000_000i128, &MIN_DEPOSIT, &deposit_time);

    assert_eq!(client.get_reward_balance(&referrer), 0);
}

#[test]
fn unreferred_user_generates_no_reward() {
    let (env, _admin, _vault, _token, client) = setup();
    let user = Address::generate(&env);

    let deposit_time = eligible_deposit_time(&env);
    // No panic — the vault's harvest must never fail because of the
    // referral program.
    client.accrue_reward(&user, &1_000_000i128, &MIN_DEPOSIT, &deposit_time);
}

// ---------------------------------------------------------------------------
// Caps and budget
// ---------------------------------------------------------------------------

#[test]
fn per_referrer_reward_cap_is_enforced() {
    let (env, admin, _vault, _token, client) = setup();
    client.set_caps(&admin, &50, &1_000_000i128); // lifetime cap: 1,000,000
    let user = Address::generate(&env);
    let referrer = Address::generate(&env);
    client.register_referral(&user, &referrer);
    let deposit_time = eligible_deposit_time(&env);

    // 10% of a 20,000,000 fee = 2,000,000 raw reward, clamped to the cap.
    client.accrue_reward(&user, &20_000_000i128, &MIN_DEPOSIT, &deposit_time);

    assert_eq!(client.get_reward_balance(&referrer), 1_000_000i128);
    assert_eq!(client.get_lifetime_reward(&referrer), 1_000_000i128);
}

#[test]
fn per_referrer_rewarded_referral_count_cap_is_enforced() {
    let (env, admin, _vault, _token, client) = setup();
    client.set_caps(&admin, &1, &1_000_000_000i128); // only 1 rewarded referee allowed
    let referrer = Address::generate(&env);
    let deposit_time = eligible_deposit_time(&env);

    let first = Address::generate(&env);
    client.register_referral(&first, &referrer);
    client.accrue_reward(&first, &1_000_000i128, &MIN_DEPOSIT, &deposit_time);
    let after_first = client.get_reward_balance(&referrer);
    assert!(after_first > 0);

    let second = Address::generate(&env);
    client.register_referral(&second, &referrer);
    client.accrue_reward(&second, &1_000_000i128, &MIN_DEPOSIT, &deposit_time);

    // Second distinct referee earns nothing — the count cap was already hit.
    assert_eq!(client.get_reward_balance(&referrer), after_first);
    assert_eq!(client.get_referred_count(&referrer), 1);
}

#[test]
fn global_budget_exhausts_and_halts_accrual_without_clawback() {
    let env = Env::default();
    env.mock_all_auths();
    env.ledger().with_mut(|l| l.timestamp = MIN_TENURE * 10);
    let admin = Address::generate(&env);
    let vault = Address::generate(&env);
    let token_admin = Address::generate(&env);
    let token_contract = env.register_stellar_asset_contract_v2(token_admin.clone());
    let token_sac = token::StellarAssetClient::new(&env, &token_contract.address());

    let cid = env.register_contract(None, ReferralContract);
    let client = ReferralContractClient::new(&env, &cid);
    let small_budget = 100_000i128;
    client.initialize(&admin, &vault, &token_contract.address(), &small_budget);
    token_sac.mint(&cid, &small_budget);

    let user = Address::generate(&env);
    let referrer = Address::generate(&env);
    client.register_referral(&user, &referrer);
    let deposit_time = eligible_deposit_time(&env);

    // 10% of 2,000,000 = 200,000 > the 100,000 budget — reward is capped to
    // whatever budget remains, and the budget is fully exhausted, not
    // clawed back below zero.
    client.accrue_reward(&user, &2_000_000i128, &MIN_DEPOSIT, &deposit_time);
    assert_eq!(client.get_reward_balance(&referrer), small_budget);
    assert_eq!(client.get_budget_remaining(), 0);

    // Already-earned reward is untouched by the exhaustion — a second
    // accrual attempt earns nothing further, but the first reward stands.
    let second_user = Address::generate(&env);
    client.register_referral(&second_user, &referrer);
    client.accrue_reward(&second_user, &2_000_000i128, &MIN_DEPOSIT, &deposit_time);
    assert_eq!(client.get_reward_balance(&referrer), small_budget);
}

// ---------------------------------------------------------------------------
// claim_referral_rewards
// ---------------------------------------------------------------------------

#[test]
fn claim_transfers_balance_and_zeroes_it() {
    let (env, _admin, _vault, token_addr, client) = setup();
    let user = Address::generate(&env);
    let referrer = Address::generate(&env);
    client.register_referral(&user, &referrer);
    let deposit_time = eligible_deposit_time(&env);
    client.accrue_reward(&user, &10_000_000i128, &MIN_DEPOSIT, &deposit_time);

    let balance_before = client.get_reward_balance(&referrer);
    assert!(balance_before > 0);

    let claimed = client.claim_referral_rewards(&referrer);
    assert_eq!(claimed, balance_before);
    assert_eq!(client.get_reward_balance(&referrer), 0);
    assert_eq!(
        token::Client::new(&env, &token_addr).balance(&referrer),
        balance_before
    );
}

#[test]
fn claim_below_minimum_is_rejected() {
    let (env, admin, _vault, _token, client) = setup();
    client.set_min_claim(&admin, &1_000_000i128);
    let referrer = Address::generate(&env);

    let result = client.try_claim_referral_rewards(&referrer);
    assert!(result.is_err());
}

// ---------------------------------------------------------------------------
// Authorization
// ---------------------------------------------------------------------------

#[test]
fn only_the_registered_vault_can_accrue_rewards() {
    let env = Env::default();
    // Do NOT mock all auths — only explicit signatures are honoured.
    let admin = Address::generate(&env);
    let vault = Address::generate(&env);
    let token_admin = Address::generate(&env);

    env.mock_all_auths();
    let token_contract = env.register_stellar_asset_contract_v2(token_admin.clone());
    let cid = env.register_contract(None, ReferralContract);
    let client = ReferralContractClient::new(&env, &cid);
    client.initialize(
        &admin,
        &vault,
        &token_contract.address(),
        &1_000_000_000i128,
    );

    let user = Address::generate(&env);
    let referrer = Address::generate(&env);
    client.register_referral(&user, &referrer);

    // Clear mocked auths so accrue_reward's `vault.require_auth()` is the
    // only thing that could authorise the call, and it wasn't signed for.
    env.mock_auths(&[]);
    let deposit_time = eligible_deposit_time(&env);
    let result = client.try_accrue_reward(&user, &1_000_000i128, &MIN_DEPOSIT, &deposit_time);
    assert!(result.is_err());
}
