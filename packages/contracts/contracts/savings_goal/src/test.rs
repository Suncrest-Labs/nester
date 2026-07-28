#![cfg(test)]
extern crate std;

use soroban_sdk::{
    testutils::{Address as _, Events as _, Ledger as _},
    token, Address, BytesN, Env, IntoVal, Vec,
};

use vault_factory_contract::{VaultFactoryContract, VaultFactoryContractClient};

use crate::{GoalStatus, SavingsGoalContract, SavingsGoalContractClient, MAX_CONTRIBUTORS_PER_GOAL};

mod dummy_vault {
    soroban_sdk::contractimport!(file = "../vault_factory/fixtures/dummy_vault.wasm");
}

fn goal_id(env: &Env, seed: u8) -> BytesN<32> {
    BytesN::from_array(env, &[seed; 32])
}

fn setup() -> (
    Env,
    Address,
    Address,
    SavingsGoalContractClient<'static>,
) {
    let env = Env::default();
    env.mock_all_auths();
    let admin = Address::generate(&env);

    let wasm_hash = env.deployer().upload_contract_wasm(dummy_vault::WASM);
    let factory_id = env.register_contract(None, VaultFactoryContract);
    let factory = VaultFactoryContractClient::new(&env, &factory_id);
    factory.initialize(&admin, &wasm_hash);

    let init_args: Vec<soroban_sdk::Val> =
        soroban_sdk::vec![&env, admin.into_val(&env), false.into_val(&env)];
    let salt = BytesN::from_array(&env, &[9u8; 32]);
    let underlying = Address::generate(&env);
    let vault = factory.create_vault(&admin, &salt, &underlying, &init_args);

    let goal_cid = env.register_contract(None, SavingsGoalContract);
    let client = SavingsGoalContractClient::new(&env, &goal_cid);
    client.initialize(&admin, &factory_id);

    (env, admin, vault, client)
}

fn future_deadline(env: &Env) -> u64 {
    env.ledger().timestamp() + 30 * 24 * 60 * 60
}

#[test]
fn create_goal_stores_active_goal() {
    let (env, owner, vault, client) = setup();
    let id = goal_id(&env, 1);

    client.create_goal(&owner, &id, &vault, &1000i128, &future_deadline(&env));

    let goal = client.get_goal(&id);
    assert_eq!(goal.owner, owner);
    assert_eq!(goal.vault, vault);
    assert_eq!(goal.target_amount, 1000);
    assert_eq!(goal.contributed, 0);
    assert_eq!(goal.milestones, 0);
    assert_eq!(goal.status, GoalStatus::Active);
}

#[test]
fn create_goal_rejects_duplicate_id() {
    let (env, owner, vault, client) = setup();
    let id = goal_id(&env, 2);
    client.create_goal(&owner, &id, &vault, &1000i128, &future_deadline(&env));

    let result = client.try_create_goal(&owner, &id, &vault, &1000i128, &future_deadline(&env));
    assert!(result.is_err());
}

#[test]
fn create_goal_rejects_non_positive_target() {
    let (env, owner, vault, client) = setup();
    let id = goal_id(&env, 3);
    let result = client.try_create_goal(&owner, &id, &vault, &0i128, &future_deadline(&env));
    assert!(result.is_err());
}

#[test]
fn create_goal_rejects_past_deadline() {
    let (env, owner, vault, client) = setup();
    let id = goal_id(&env, 4);
    let past = env.ledger().timestamp();
    let result = client.try_create_goal(&owner, &id, &vault, &1000i128, &past);
    assert!(result.is_err());
}

#[test]
fn create_goal_rejects_vault_not_from_factory() {
    let (env, owner, _vault, client) = setup();
    let id = goal_id(&env, 5);
    // Deployed with the same code but never routed through the factory —
    // must not be accepted as a goal's vault.
    let lookalike = env.register_contract_wasm(None, dummy_vault::WASM);

    let result = client.try_create_goal(&owner, &id, &lookalike, &1000i128, &future_deadline(&env));
    assert!(result.is_err());
}

#[test]
fn milestone_bitmask_is_idempotent_across_repeated_calls() {
    let (env, owner, vault, client) = setup();
    let id = goal_id(&env, 6);
    client.create_goal(&owner, &id, &vault, &100i128, &future_deadline(&env));

    let contributor = Address::generate(&env);

    let before = env.events().all().len();
    // Crosses the 25% threshold — exactly one milestone event.
    client.contribute(&contributor, &id, &25i128);
    let after_first = env.events().all().len();
    assert_eq!(after_first - before, 1);

    // Two more calls that stay within the same (25%..50%) band: the bit is
    // already set, so no further milestone event fires despite calling
    // `contribute` again with the threshold already crossed.
    client.contribute(&contributor, &id, &1i128);
    client.contribute(&contributor, &id, &1i128);
    let after_repeats = env.events().all().len();
    assert_eq!(after_repeats, after_first);

    let goal = client.get_goal(&id);
    assert_eq!(goal.milestones, 0b0001);
}

#[test]
fn contribute_completes_goal_and_emits_completion() {
    let (env, owner, vault, client) = setup();
    let id = goal_id(&env, 7);
    client.create_goal(&owner, &id, &vault, &100i128, &future_deadline(&env));

    let contributor = Address::generate(&env);
    client.contribute(&contributor, &id, &100i128);

    let goal = client.get_goal(&id);
    assert_eq!(goal.status, GoalStatus::Completed);
    assert_eq!(goal.milestones, 0b1111);
}

#[test]
fn contribute_rejects_when_goal_not_active() {
    let (env, owner, vault, client) = setup();
    let id = goal_id(&env, 8);
    client.create_goal(&owner, &id, &vault, &100i128, &future_deadline(&env));
    client.abandon_goal(&owner, &id);

    let contributor = Address::generate(&env);
    let result = client.try_contribute(&contributor, &id, &10i128);
    assert!(result.is_err());
}

#[test]
fn finalize_goal_requires_target_reached() {
    let (env, owner, vault, client) = setup();
    let id = goal_id(&env, 9);
    client.create_goal(&owner, &id, &vault, &100i128, &future_deadline(&env));

    let result = client.try_finalize_goal(&id);
    assert!(result.is_err());

    let contributor = Address::generate(&env);
    client.contribute(&contributor, &id, &100i128);

    // Already completed inline by `contribute`; a second finalize is a no-op path (errors, not active).
    let result = client.try_finalize_goal(&id);
    assert!(result.is_err());
}

#[test]
fn expire_goal_requires_deadline_passed() {
    let (env, owner, vault, client) = setup();
    let id = goal_id(&env, 10);
    let deadline = future_deadline(&env);
    client.create_goal(&owner, &id, &vault, &1000i128, &deadline);

    let result = client.try_expire_goal(&id);
    assert!(result.is_err());

    env.ledger().with_mut(|l| l.timestamp = deadline + 1);
    client.expire_goal(&id);

    let goal = client.get_goal(&id);
    assert_eq!(goal.status, GoalStatus::Expired);
}

#[test]
fn abandon_goal_requires_owner() {
    let (env, owner, vault, client) = setup();
    let id = goal_id(&env, 11);
    client.create_goal(&owner, &id, &vault, &1000i128, &future_deadline(&env));

    let outsider = Address::generate(&env);
    let result = client.try_abandon_goal(&outsider, &id);
    assert!(result.is_err());

    client.abandon_goal(&owner, &id);
    let goal = client.get_goal(&id);
    assert_eq!(goal.status, GoalStatus::Abandoned);
}

#[test]
fn multi_contributor_accounting_is_tracked_per_address() {
    let (env, owner, vault, client) = setup();
    let id = goal_id(&env, 12);
    client.create_goal(&owner, &id, &vault, &1000i128, &future_deadline(&env));

    let alice = Address::generate(&env);
    let bob = Address::generate(&env);
    client.contribute(&alice, &id, &40i128);
    client.contribute(&bob, &id, &10i128);
    client.contribute(&alice, &id, &5i128);

    assert_eq!(client.get_contributor_amount(&id, &alice), 45);
    assert_eq!(client.get_contributor_amount(&id, &bob), 10);
    assert_eq!(client.get_contributors(&id).len(), 2);

    let goal = client.get_goal(&id);
    assert_eq!(goal.contributed, 55);
}

#[test]
fn contributor_cap_rejects_past_the_bound() {
    let (env, owner, vault, client) = setup();
    let id = goal_id(&env, 13);
    // Target large enough that the cap trips before completion.
    client.create_goal(&owner, &id, &vault, &1_000_000i128, &future_deadline(&env));

    for _ in 0..MAX_CONTRIBUTORS_PER_GOAL {
        let contributor = Address::generate(&env);
        client.contribute(&contributor, &id, &1i128);
    }

    let one_too_many = Address::generate(&env);
    let result = client.try_contribute(&one_too_many, &id, &1i128);
    assert!(result.is_err());
}

#[test]
fn registry_never_holds_token_balance_across_full_lifecycle() {
    let (env, owner, vault, client) = setup();

    let token_admin = Address::generate(&env);
    let sac = env.register_stellar_asset_contract_v2(token_admin.clone());
    let token_client = token::Client::new(&env, &sac.address());
    let token_admin_client = token::StellarAssetClient::new(&env, &sac.address());

    let depositor = Address::generate(&env);
    token_admin_client.mint(&depositor, &500i128);
    // Deposits flow through the vault directly — the registry is never a
    // party to any token movement, only accounting/attestation.
    token_client.transfer(&depositor, &vault, &500i128);

    let id = goal_id(&env, 14);
    client.create_goal(&owner, &id, &vault, &500i128, &future_deadline(&env));
    client.contribute(&depositor, &id, &500i128);

    let goal = client.get_goal(&id);
    assert_eq!(goal.status, GoalStatus::Completed);
    assert_eq!(token_client.balance(&client.address), 0);
}
