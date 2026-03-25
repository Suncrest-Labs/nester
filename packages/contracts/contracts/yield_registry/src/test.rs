#![cfg(test)]

extern crate std;

use soroban_sdk::{
    testutils::{Address as _, Events},
    Address, Env, Symbol,
};

use crate::{ProtocolType, SourceStatus, YieldRegistryContract, YieldRegistryContractClient};

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

fn setup(env: &Env) -> (YieldRegistryContractClient, Address) {
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

// ---------------------------------------------------------------------------
// Initialisation
// ---------------------------------------------------------------------------

#[test]
fn initialize_sets_empty_source_list() {
    let env = Env::default();
    let (client, _) = setup(&env);
    assert_eq!(client.get_active_sources().len(), 0);
}

#[test]
#[should_panic]
fn initialize_twice_panics() {
    let env = Env::default();
    let (client, admin) = setup(&env);
    client.initialize(&admin);
}

// ---------------------------------------------------------------------------
// register_source
// ---------------------------------------------------------------------------

#[test]
fn register_source_creates_active_record() {
    let env = Env::default();
    let (client, admin) = setup(&env);
    let addr = Address::generate(&env);

    client.register_source(&admin, &aave_id(&env), &addr, &ProtocolType::Lending);

    assert!(client.has_source(&aave_id(&env)));
    let s = client.get_source(&aave_id(&env));
    assert_eq!(s.status, SourceStatus::Active);
    assert_eq!(s.protocol_type, ProtocolType::Lending);
    assert_eq!(s.contract_address, addr);
}

#[test]
fn register_source_appears_in_active_list() {
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
        &ProtocolType::Lending,
    );

    let active = client.get_active_sources();
    assert_eq!(active.len(), 2);
}

#[test]
fn register_source_emits_event() {
    let env = Env::default();
    let (client, admin) = setup(&env);

    client.register_source(
        &admin,
        &aave_id(&env),
        &Address::generate(&env),
        &ProtocolType::Lending,
    );

    assert!(!env.events().all().is_empty());
}

#[test]
#[should_panic]
fn register_duplicate_id_panics() {
    let env = Env::default();
    let (client, admin) = setup(&env);

    client.register_source(
        &admin,
        &aave_id(&env),
        &Address::generate(&env),
        &ProtocolType::Lending,
    );
    // Second registration with same id must panic
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
// update_status
// ---------------------------------------------------------------------------

#[test]
fn active_to_paused_transition() {
    let env = Env::default();
    let (client, admin) = setup(&env);

    client.register_source(
        &admin,
        &aave_id(&env),
        &Address::generate(&env),
        &ProtocolType::Lending,
    );
    client.update_status(&admin, &aave_id(&env), &SourceStatus::Paused);

    assert_eq!(
        client.get_source_status(&aave_id(&env)),
        SourceStatus::Paused
    );
}

#[test]
fn paused_to_active_transition() {
    let env = Env::default();
    let (client, admin) = setup(&env);

    client.register_source(
        &admin,
        &aave_id(&env),
        &Address::generate(&env),
        &ProtocolType::Lending,
    );
    client.update_status(&admin, &aave_id(&env), &SourceStatus::Paused);
    client.update_status(&admin, &aave_id(&env), &SourceStatus::Active);

    assert_eq!(
        client.get_source_status(&aave_id(&env)),
        SourceStatus::Active
    );
}

#[test]
fn active_to_deprecated_transition() {
    let env = Env::default();
    let (client, admin) = setup(&env);

    client.register_source(
        &admin,
        &aave_id(&env),
        &Address::generate(&env),
        &ProtocolType::Lending,
    );
    client.update_status(&admin, &aave_id(&env), &SourceStatus::Deprecated);

    assert_eq!(
        client.get_source_status(&aave_id(&env)),
        SourceStatus::Deprecated
    );
}

#[test]
#[should_panic]
fn cannot_reactivate_deprecated_source() {
    let env = Env::default();
    let (client, admin) = setup(&env);

    client.register_source(
        &admin,
        &aave_id(&env),
        &Address::generate(&env),
        &ProtocolType::Lending,
    );
    client.update_status(&admin, &aave_id(&env), &SourceStatus::Deprecated);
    // Must panic — Deprecated is terminal
    client.update_status(&admin, &aave_id(&env), &SourceStatus::Active);
}

#[test]
#[should_panic]
fn cannot_pause_deprecated_source() {
    let env = Env::default();
    let (client, admin) = setup(&env);

    client.register_source(
        &admin,
        &aave_id(&env),
        &Address::generate(&env),
        &ProtocolType::Lending,
    );
    client.update_status(&admin, &aave_id(&env), &SourceStatus::Deprecated);
    client.update_status(&admin, &aave_id(&env), &SourceStatus::Paused);
}

#[test]
#[should_panic]
fn update_status_on_unknown_id_panics() {
    let env = Env::default();
    let (client, admin) = setup(&env);
    client.update_status(&admin, &aave_id(&env), &SourceStatus::Paused);
}

#[test]
fn update_status_emits_event() {
    let env = Env::default();
    let (client, admin) = setup(&env);

    client.register_source(
        &admin,
        &aave_id(&env),
        &Address::generate(&env),
        &ProtocolType::Lending,
    );
    let before = env.events().all().len();
    client.update_status(&admin, &aave_id(&env), &SourceStatus::Paused);
    assert!(env.events().all().len() > before);
}

#[test]
#[should_panic]
fn non_admin_cannot_update_status() {
    let env = Env::default();
    let (client, admin) = setup(&env);
    let outsider = Address::generate(&env);

    client.register_source(
        &admin,
        &aave_id(&env),
        &Address::generate(&env),
        &ProtocolType::Lending,
    );
    client.update_status(&outsider, &aave_id(&env), &SourceStatus::Paused);
}

// ---------------------------------------------------------------------------
// get_active_sources
// ---------------------------------------------------------------------------

#[test]
fn paused_source_excluded_from_active_list() {
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
        &ProtocolType::Lending,
    );
    client.update_status(&admin, &blend_id(&env), &SourceStatus::Paused);

    let active = client.get_active_sources();
    assert_eq!(active.len(), 1);
    assert_eq!(active.get(0).unwrap().id, aave_id(&env));
}

#[test]
fn deprecated_source_excluded_from_active_list() {
    let env = Env::default();
    let (client, admin) = setup(&env);

    client.register_source(
        &admin,
        &aave_id(&env),
        &Address::generate(&env),
        &ProtocolType::Lending,
    );
    client.update_status(&admin, &aave_id(&env), &SourceStatus::Deprecated);

    assert_eq!(client.get_active_sources().len(), 0);
}

// ---------------------------------------------------------------------------
// remove_source
// ---------------------------------------------------------------------------

#[test]
fn remove_source_deletes_record() {
    let env = Env::default();
    let (client, admin) = setup(&env);

    client.register_source(
        &admin,
        &aave_id(&env),
        &Address::generate(&env),
        &ProtocolType::Lending,
    );
    client.remove_source(&admin, &aave_id(&env));

    assert!(!client.has_source(&aave_id(&env)));
    assert_eq!(client.get_active_sources().len(), 0);
}

#[test]
fn remove_source_emits_event() {
    let env = Env::default();
    let (client, admin) = setup(&env);

    client.register_source(
        &admin,
        &aave_id(&env),
        &Address::generate(&env),
        &ProtocolType::Lending,
    );
    let before = env.events().all().len();
    client.remove_source(&admin, &aave_id(&env));
    assert!(env.events().all().len() > before);
}

#[test]
fn removed_source_can_be_re_registered() {
    let env = Env::default();
    let (client, admin) = setup(&env);

    client.register_source(
        &admin,
        &aave_id(&env),
        &Address::generate(&env),
        &ProtocolType::Lending,
    );
    client.remove_source(&admin, &aave_id(&env));
    // Should not panic
    client.register_source(
        &admin,
        &aave_id(&env),
        &Address::generate(&env),
        &ProtocolType::Staking,
    );
    assert!(client.has_source(&aave_id(&env)));
}

#[test]
#[should_panic]
fn remove_unknown_source_panics() {
    let env = Env::default();
    let (client, admin) = setup(&env);
    client.remove_source(&admin, &aave_id(&env));
}

#[test]
#[should_panic]
fn non_admin_cannot_remove_source() {
    let env = Env::default();
    let (client, admin) = setup(&env);
    let outsider = Address::generate(&env);

    client.register_source(
        &admin,
        &aave_id(&env),
        &Address::generate(&env),
        &ProtocolType::Lending,
    );
    client.remove_source(&outsider, &aave_id(&env));
}

// ---------------------------------------------------------------------------
// Admin transfer
// ---------------------------------------------------------------------------

#[test]
fn new_admin_can_manage_sources_after_transfer() {
    let env = Env::default();
    let (client, admin) = setup(&env);
    let new_admin = Address::generate(&env);

    client.transfer_admin(&admin, &new_admin);
    client.accept_admin(&new_admin);

    // New admin can register
    client.register_source(
        &new_admin,
        &aave_id(&env),
        &Address::generate(&env),
        &ProtocolType::Lending,
    );
    assert!(client.has_source(&aave_id(&env)));
}

#[test]
#[should_panic]
fn old_admin_cannot_register_after_transfer() {
    let env = Env::default();
    let (client, admin) = setup(&env);
    let new_admin = Address::generate(&env);

    client.transfer_admin(&admin, &new_admin);
    client.accept_admin(&new_admin);

    client.register_source(
        &admin,
        &aave_id(&env),
        &Address::generate(&env),
        &ProtocolType::Lending,
    );
}

// ---------------------------------------------------------------------------
// Backward-compat: has_source / get_source_status
// ---------------------------------------------------------------------------

#[test]
fn has_source_returns_false_for_unregistered() {
    let env = Env::default();
    let (client, _) = setup(&env);
    assert!(!client.has_source(&Symbol::new(&env, "ghost")));
}

#[test]
fn get_source_status_reflects_current_status() {
    let env = Env::default();
    let (client, admin) = setup(&env);

    client.register_source(
        &admin,
        &aave_id(&env),
        &Address::generate(&env),
        &ProtocolType::Lending,
    );
    assert_eq!(
        client.get_source_status(&aave_id(&env)),
        SourceStatus::Active
    );

    client.update_status(&admin, &aave_id(&env), &SourceStatus::Paused);
    assert_eq!(
        client.get_source_status(&aave_id(&env)),
        SourceStatus::Paused
    );
}
