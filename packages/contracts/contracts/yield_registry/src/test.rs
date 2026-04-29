#![cfg(test)]

extern crate std;

use soroban_sdk::{
    testutils::{Address as _, Events},
    Address, Env, Symbol,
};

use nester_access_control::Role;

use crate::{
    ProtocolType, SourceStatus, YieldRegistryContract, YieldRegistryContractClient, MAX_APY_HISTORY,
};

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

fn setup(env: &Env) -> (YieldRegistryContractClient<'_>, Address) {
    env.mock_all_auths();
    let admin = Address::generate(env);
    let contract_id = env.register_contract(None, YieldRegistryContract);
    let client = YieldRegistryContractClient::new(env, &contract_id);
    client.initialize(&admin);
    (client, admin)
}

fn aave_id(env: &Env) -> Symbol {
    Symbol::new(env, "aave_v3")
}

fn blend_id(env: &Env) -> Symbol {
    Symbol::new(env, "blend")
}

fn register_default(client: &YieldRegistryContractClient, env: &Env, admin: &Address, id: &Symbol) {
    client.register_source(admin, id, &Address::generate(env), &ProtocolType::Lending);
}

// ---------------------------------------------------------------------------
// Initialisation / registration
// ---------------------------------------------------------------------------

#[test]
fn initialize_sets_empty_source_list() {
    let env = Env::default();
    let (client, _) = setup(&env);
    assert_eq!(client.get_active_sources().len(), 0);
    assert_eq!(client.source_count(), 0);
}

#[test]
#[should_panic]
fn initialize_twice_panics() {
    let env = Env::default();
    let (client, admin) = setup(&env);
    client.initialize(&admin);
}

#[test]
fn register_source_sets_default_performance_fields() {
    let env = Env::default();
    let (client, admin) = setup(&env);
    let addr = Address::generate(&env);

    client.register_source(&admin, &aave_id(&env), &addr, &ProtocolType::Lending);

    let s = client.get_source(&aave_id(&env));
    assert_eq!(s.status, SourceStatus::Active);
    assert_eq!(s.protocol_type, ProtocolType::Lending);
    assert_eq!(s.contract_address, addr);
    assert_eq!(s.current_apy_bps, 0);
    assert_eq!(s.tvl, 0);
    assert_eq!(s.risk_rating, 5);
    assert_eq!(s.min_deposit, 0);
    assert_eq!(s.max_deposit, 0);
    assert_eq!(s.apy_history.len(), 0);
    assert!(!s.migration_required);
    assert!(!s.migration_completed);
    assert_eq!(client.source_count(), 1);
}

#[test]
#[should_panic]
fn register_duplicate_id_panics() {
    let env = Env::default();
    let (client, admin) = setup(&env);

    register_default(&client, &env, &admin, &aave_id(&env));
    client.register_source(
        &admin,
        &aave_id(&env),
        &Address::generate(&env),
        &ProtocolType::Staking,
    );
}

#[test]
#[should_panic]
fn non_admin_cannot_register_source() {
    let env = Env::default();
    let (client, _) = setup(&env);
    let outsider = Address::generate(&env);

    client.register_source(
        &outsider,
        &aave_id(&env),
        &Address::generate(&env),
        &ProtocolType::Lending,
    );
}

// ---------------------------------------------------------------------------
// Status / deprecation / migration
// ---------------------------------------------------------------------------

#[test]
fn active_paused_active_transition_works() {
    let env = Env::default();
    let (client, admin) = setup(&env);

    register_default(&client, &env, &admin, &aave_id(&env));
    client.update_status(&admin, &aave_id(&env), &SourceStatus::Paused);
    assert_eq!(
        client.get_source_status(&aave_id(&env)),
        SourceStatus::Paused
    );

    client.update_status(&admin, &aave_id(&env), &SourceStatus::Active);
    assert_eq!(
        client.get_source_status(&aave_id(&env)),
        SourceStatus::Active
    );
}

#[test]
fn deprecating_source_sets_migration_required() {
    let env = Env::default();
    let (client, admin) = setup(&env);

    register_default(&client, &env, &admin, &aave_id(&env));
    client.update_status(&admin, &aave_id(&env), &SourceStatus::Deprecated);

    let s = client.get_source(&aave_id(&env));
    assert_eq!(s.status, SourceStatus::Deprecated);
    assert!(s.migration_required);
    assert!(!s.migration_completed);
    assert_eq!(client.get_sources_requiring_migration().len(), 1);
}

#[test]
#[should_panic]
fn cannot_reactivate_deprecated_source() {
    let env = Env::default();
    let (client, admin) = setup(&env);

    register_default(&client, &env, &admin, &aave_id(&env));
    client.update_status(&admin, &aave_id(&env), &SourceStatus::Deprecated);
    client.update_status(&admin, &aave_id(&env), &SourceStatus::Active);
}

#[test]
fn signal_and_complete_migration_flow() {
    let env = Env::default();
    let (client, admin) = setup(&env);
    let operator = Address::generate(&env);

    register_default(&client, &env, &admin, &aave_id(&env));
    client.grant_role(&admin, &operator, &Role::Operator);

    client.signal_migration_required(&admin, &aave_id(&env));
    let pending = client.get_source(&aave_id(&env));
    assert!(pending.migration_required);
    assert!(!pending.migration_completed);

    client.mark_migration_complete(&operator, &aave_id(&env));
    let done = client.get_source(&aave_id(&env));
    assert!(!done.migration_required);
    assert!(done.migration_completed);
    assert_eq!(client.get_sources_requiring_migration().len(), 0);
}

#[test]
#[should_panic]
fn cannot_complete_migration_without_signal_or_deprecation() {
    let env = Env::default();
    let (client, admin) = setup(&env);
    register_default(&client, &env, &admin, &aave_id(&env));
    client.mark_migration_complete(&admin, &aave_id(&env));
}

// ---------------------------------------------------------------------------
// Performance updates
// ---------------------------------------------------------------------------

#[test]
fn operator_can_update_apy_and_history_is_capped() {
    let env = Env::default();
    let (client, admin) = setup(&env);
    let operator = Address::generate(&env);

    register_default(&client, &env, &admin, &aave_id(&env));
    client.grant_role(&admin, &operator, &Role::Operator);

    for i in 1..=(MAX_APY_HISTORY + 4) {
        client.update_apy(&operator, &aave_id(&env), &i);
    }

    let perf = client.get_source_performance(&aave_id(&env));
    assert_eq!(perf.current_apy_bps, MAX_APY_HISTORY + 4);
    assert_eq!(perf.apy_history.len(), MAX_APY_HISTORY);

    // Expect the newest MAX_APY_HISTORY entries only.
    assert_eq!(perf.apy_history.get(0).unwrap().apy_bps, 5);
    assert_eq!(
        perf.apy_history.get(MAX_APY_HISTORY - 1).unwrap().apy_bps,
        MAX_APY_HISTORY + 4
    );
}

#[test]
#[should_panic]
fn outsider_cannot_update_apy() {
    let env = Env::default();
    let (client, admin) = setup(&env);
    let outsider = Address::generate(&env);

    register_default(&client, &env, &admin, &aave_id(&env));
    client.update_apy(&outsider, &aave_id(&env), &420);
}

#[test]
fn admin_can_update_tvl_risk_and_limits() {
    let env = Env::default();
    let (client, admin) = setup(&env);

    register_default(&client, &env, &admin, &aave_id(&env));
    client.update_tvl(&admin, &aave_id(&env), &150_000);
    client.update_risk_rating(&admin, &aave_id(&env), &3);
    client.update_deposit_limits(&admin, &aave_id(&env), &100, &1_000_000);

    let perf = client.get_source_performance(&aave_id(&env));
    assert_eq!(perf.tvl, 150_000);
    assert_eq!(perf.risk_rating, 3);
    assert_eq!(perf.min_deposit, 100);
    assert_eq!(perf.max_deposit, 1_000_000);
}

#[test]
#[should_panic]
fn risk_rating_must_be_in_range() {
    let env = Env::default();
    let (client, admin) = setup(&env);
    register_default(&client, &env, &admin, &aave_id(&env));
    client.update_risk_rating(&admin, &aave_id(&env), &11);
}

#[test]
#[should_panic]
fn tvl_cannot_be_negative() {
    let env = Env::default();
    let (client, admin) = setup(&env);
    register_default(&client, &env, &admin, &aave_id(&env));
    client.update_tvl(&admin, &aave_id(&env), &-1);
}

#[test]
#[should_panic]
fn invalid_deposit_limits_panics() {
    let env = Env::default();
    let (client, admin) = setup(&env);
    register_default(&client, &env, &admin, &aave_id(&env));
    client.update_deposit_limits(&admin, &aave_id(&env), &1000, &100);
}

// ---------------------------------------------------------------------------
// Queries and filtering
// ---------------------------------------------------------------------------

#[test]
fn get_sources_by_type_filters_correctly() {
    let env = Env::default();
    let (client, admin) = setup(&env);

    client.register_source(
        &admin,
        &aave_id(&env),
        &Address::generate(&env),
        &ProtocolType::Lending,
    );
    client.register_source(
        &admin,
        &blend_id(&env),
        &Address::generate(&env),
        &ProtocolType::Staking,
    );

    let lending = client.get_sources_by_type(&ProtocolType::Lending);
    let staking = client.get_sources_by_type(&ProtocolType::Staking);

    assert_eq!(lending.len(), 1);
    assert_eq!(lending.get(0).unwrap().id, aave_id(&env));
    assert_eq!(staking.len(), 1);
    assert_eq!(staking.get(0).unwrap().id, blend_id(&env));
}

#[test]
fn get_sources_above_apy_only_returns_active_qualifiers() {
    let env = Env::default();
    let (client, admin) = setup(&env);

    register_default(&client, &env, &admin, &aave_id(&env));
    register_default(&client, &env, &admin, &blend_id(&env));

    client.update_apy(&admin, &aave_id(&env), &650);
    client.update_apy(&admin, &blend_id(&env), &800);
    client.update_status(&admin, &blend_id(&env), &SourceStatus::Paused);

    let above = client.get_sources_above_apy(&700);
    assert_eq!(above.len(), 0);

    let above = client.get_sources_above_apy(&600);
    assert_eq!(above.len(), 1);
    assert_eq!(above.get(0).unwrap().id, aave_id(&env));
}

#[test]
fn source_count_updates_on_remove() {
    let env = Env::default();
    let (client, admin) = setup(&env);

    register_default(&client, &env, &admin, &aave_id(&env));
    register_default(&client, &env, &admin, &blend_id(&env));
    assert_eq!(client.source_count(), 2);

    client.remove_source(&admin, &aave_id(&env));
    assert_eq!(client.source_count(), 1);
}

// ---------------------------------------------------------------------------
// Existing compatibility checks
// ---------------------------------------------------------------------------

#[test]
fn has_source_returns_false_for_unregistered() {
    let env = Env::default();
    let (client, _) = setup(&env);
    assert!(!client.has_source(&Symbol::new(&env, "ghost")));
}

#[test]
fn status_and_update_emit_events() {
    let env = Env::default();
    let (client, admin) = setup(&env);
    register_default(&client, &env, &admin, &aave_id(&env));

    let before = env.events().all().len();
    client.update_status(&admin, &aave_id(&env), &SourceStatus::Paused);
    client.update_apy(&admin, &aave_id(&env), &999);

    assert!(env.events().all().len() > before);
}
