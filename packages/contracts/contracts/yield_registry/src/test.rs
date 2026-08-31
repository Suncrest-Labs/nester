#![cfg(test)]

extern crate std;

use soroban_sdk::{
    testutils::{Address as _, Events},
    Address, Env, Symbol,
};

use nester_access_control::Role;

use crate::{
    ProtocolType, SourceStatus, YieldRegistryContract, YieldRegistryContractClient,
    DEFAULT_APY_DEVIATION_THRESHOLD_BPS, MAX_APY_HISTORY,
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
    client.register_source(admin, id, &Address::generate(env), &None, &ProtocolType::Lending);
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

    client.register_source(&admin, &aave_id(&env), &addr, &None, &ProtocolType::Lending);

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
        &None,
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
        &None,
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
fn exploit_status_sets_migration_required_and_is_excluded_from_active() {
    let env = Env::default();
    let (client, admin) = setup(&env);

    register_default(&client, &env, &admin, &aave_id(&env));
    client.update_status(&admin, &aave_id(&env), &SourceStatus::Exploit);

    let s = client.get_source(&aave_id(&env));
    assert_eq!(s.status, SourceStatus::Exploit);
    assert!(s.migration_required);
    assert!(!s.migration_completed);
    assert_eq!(client.get_sources_requiring_migration().len(), 1);
    assert_eq!(client.get_active_sources().len(), 0);
}

#[test]
#[should_panic]
fn cannot_reactivate_exploit_source() {
    let env = Env::default();
    let (client, admin) = setup(&env);

    register_default(&client, &env, &admin, &aave_id(&env));
    client.update_status(&admin, &aave_id(&env), &SourceStatus::Exploit);
    client.update_status(&admin, &aave_id(&env), &SourceStatus::Active);
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
        &None,
        &ProtocolType::Lending,
    );
    client.register_source(
        &admin,
        &blend_id(&env),
        &Address::generate(&env),
        &None,
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

// ---------------------------------------------------------------------------
// APY deviation guard (issue #509)
//
// The guard rejects a single APY update whose ABSOLUTE change (in bps) from the
// last stored APY exceeds the configured threshold (default
// DEFAULT_APY_DEVIATION_THRESHOLD_BPS = 5000). Boundary is INCLUSIVE: a change
// exactly equal to the threshold is accepted; only a strictly larger change is
// rejected. The first update (no prior non-zero APY) is always accepted.
// ---------------------------------------------------------------------------

#[test]
fn test_apy_update_within_threshold_succeeds() {
    let env = Env::default();
    let (client, admin) = setup(&env);
    register_default(&client, &env, &admin, &aave_id(&env));

    // First update establishes a non-zero baseline (bypasses the guard).
    client.update_apy(&admin, &aave_id(&env), &1_000);
    // +1000 bps is well within the default 5000-bps threshold.
    client.update_apy(&admin, &aave_id(&env), &2_000);

    assert_eq!(
        client
            .get_source_performance(&aave_id(&env))
            .current_apy_bps,
        2_000
    );
}

#[test]
fn test_apy_update_exceeds_threshold_rejected() {
    let env = Env::default();
    let (client, admin) = setup(&env);
    register_default(&client, &env, &admin, &aave_id(&env));

    client.update_apy(&admin, &aave_id(&env), &1_000);

    // +6000 bps exceeds the 5000-bps threshold and must be rejected.
    client.update_apy(&admin, &aave_id(&env), &7_000);

    // The rejected update must not have mutated stored state.
    assert_eq!(
        client
            .get_source_performance(&aave_id(&env))
            .current_apy_bps,
        1_000
    );
    assert_eq!(client.get_source_failure_count(&aave_id(&env)), 1);
}

#[test]
fn test_first_apy_update_always_accepted() {
    let env = Env::default();
    let (client, admin) = setup(&env);
    register_default(&client, &env, &admin, &aave_id(&env));

    // No previous (non-zero) APY to compare against, so even a jump far beyond
    // the threshold is accepted on the first update.
    let first = DEFAULT_APY_DEVIATION_THRESHOLD_BPS + 4_000; // 9000 bps
    client.update_apy(&admin, &aave_id(&env), &first);

    assert_eq!(
        client
            .get_source_performance(&aave_id(&env))
            .current_apy_bps,
        first
    );
}

#[test]
fn test_admin_can_override_deviation_guard() {
    let env = Env::default();
    let (client, admin) = setup(&env);
    register_default(&client, &env, &admin, &aave_id(&env));

    client.update_apy(&admin, &aave_id(&env), &1_000);

    // Sanity: the guarded path rejects this jump (dev 7000 > 5000).
    client.update_apy(&admin, &aave_id(&env), &8_000);
    assert_eq!(
        client
            .get_source_performance(&aave_id(&env))
            .current_apy_bps,
        1_000
    );

    // The admin emergency override bypasses the guard.
    client.update_apy_override(&admin, &aave_id(&env), &8_000);

    assert_eq!(
        client
            .get_source_performance(&aave_id(&env))
            .current_apy_bps,
        8_000
    );
}

#[test]
#[should_panic]
fn test_operator_cannot_override_deviation_guard() {
    let env = Env::default();
    let (client, admin) = setup(&env);
    let operator = Address::generate(&env);
    register_default(&client, &env, &admin, &aave_id(&env));
    client.grant_role(&admin, &operator, &Role::Operator);

    client.update_apy(&admin, &aave_id(&env), &1_000);

    // Operators may update within the threshold but must NOT be able to
    // override it — the override is gated on the Admin role.
    client.update_apy_override(&operator, &aave_id(&env), &8_000);
}

#[test]
fn test_apy_update_at_exact_threshold_boundary() {
    let env = Env::default();
    let (client, admin) = setup(&env);
    register_default(&client, &env, &admin, &aave_id(&env));

    client.update_apy(&admin, &aave_id(&env), &1_000);

    // One bps PAST the threshold (dev 5001) is rejected — leaves state at 1000.
    client.update_apy(&admin, &aave_id(&env), &6_001);
    assert_eq!(
        client
            .get_source_performance(&aave_id(&env))
            .current_apy_bps,
        1_000
    );

    // EXACTLY at the threshold (dev 5000) is accepted — boundary is inclusive.
    client.update_apy(&admin, &aave_id(&env), &6_000);
    assert_eq!(
        client
            .get_source_performance(&aave_id(&env))
            .current_apy_bps,
        6_000
    );
}

#[test]
fn test_threshold_is_configurable_from_storage() {
    let env = Env::default();
    let (client, admin) = setup(&env);
    register_default(&client, &env, &admin, &aave_id(&env));

    // Default is seeded at initialize.
    assert_eq!(
        client.get_apy_deviation_threshold(),
        DEFAULT_APY_DEVIATION_THRESHOLD_BPS
    );

    client.update_apy(&admin, &aave_id(&env), &1_000);

    // Tighten the threshold to 1000 bps.
    client.set_apy_deviation_threshold(&admin, &1_000);
    assert_eq!(client.get_apy_deviation_threshold(), 1_000);

    // A +1500 jump passed under the default 5000 but now exceeds the new 1000.
    client.update_apy(&admin, &aave_id(&env), &2_500);
    assert_eq!(
        client
            .get_source_performance(&aave_id(&env))
            .current_apy_bps,
        1_000
    );

    // Within the new threshold still works.
    client.update_apy(&admin, &aave_id(&env), &1_800);
    assert_eq!(
        client
            .get_source_performance(&aave_id(&env))
            .current_apy_bps,
        1_800
    );
}

#[test]
#[should_panic]
fn test_non_admin_cannot_set_threshold() {
    let env = Env::default();
    let (client, admin) = setup(&env);
    let operator = Address::generate(&env);
    register_default(&client, &env, &admin, &aave_id(&env));
    client.grant_role(&admin, &operator, &Role::Operator);

    client.set_apy_deviation_threshold(&operator, &1_000);
}

#[test]
#[should_panic]
fn test_set_threshold_above_max_apy_rejected() {
    let env = Env::default();
    let (client, admin) = setup(&env);
    register_default(&client, &env, &admin, &aave_id(&env));

    // Threshold cannot exceed the absolute APY ceiling (MAX_APY_BPS = 10000).
    client.set_apy_deviation_threshold(&admin, &10_001);
}

#[test]
fn consecutive_deviation_rejections_require_explicit_recovery() {
    let env = Env::default();
    let (client, admin) = setup(&env);
    let id = aave_id(&env);
    register_default(&client, &env, &admin, &id);

    client.set_apy_deviation_threshold(&admin, &100);
    client.set_failure_threshold(&admin, &2);
    client.update_apy(&admin, &id, &500);

    // The configured threshold is the tolerated failure count, so the next
    // failure crosses it and degrades the source.
    for expected_count in 1..=3 {
        client.update_apy(&admin, &id, &900);
        assert_eq!(client.get_source_failure_count(&id), expected_count);
    }

    assert_eq!(client.get_source_status(&id), SourceStatus::Degraded);
    assert_eq!(client.get_active_sources().len(), 0);
    assert_eq!(client.get_source_performance(&id).current_apy_bps, 500);

    // A subsequent valid reading may update performance, but must not make
    // the source eligible for allocation again without an admin decision.
    client.update_apy(&admin, &id, &550);
    assert_eq!(client.get_source_status(&id), SourceStatus::Degraded);
    assert_eq!(client.get_active_sources().len(), 0);

    client.recover_source(&admin, &id);
    assert_eq!(client.get_source_status(&id), SourceStatus::Active);
    assert_eq!(client.get_active_sources().len(), 1);
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

// ---------------------------------------------------------------------------
// Adapter-backed sources (#812)
//
// A local mock adapter is used rather than nester-test-utils: that crate
// depends on this one, so a dev-dependency back would be a cycle.
// ---------------------------------------------------------------------------

mod adapter_backed {
    use super::*;
    use nester_common::adapters::{AdapterApy, ApyConfidence};
    use soroban_sdk::{contract, contractimpl, contracttype, panic_with_error};

    #[contracttype]
    #[derive(Clone)]
    enum MockKey {
        Apy,
        Broken,
    }

    /// Minimal stand-in for a real adapter: reports whatever APY reading the
    /// test configures, or reverts on demand.
    #[contract]
    pub struct MockAdapter;

    #[contractimpl]
    impl MockAdapter {
        pub fn set_apy(env: Env, apy_bps: u32, derived: bool, unavailable: bool) {
            let confidence = if unavailable {
                ApyConfidence::Unavailable
            } else if derived {
                ApyConfidence::Derived
            } else {
                ApyConfidence::ProtocolReported
            };
            env.storage()
                .instance()
                .set(&MockKey::Apy, &AdapterApy { apy_bps, confidence });
        }

        pub fn set_broken(env: Env, broken: bool) {
            env.storage().instance().set(&MockKey::Broken, &broken);
        }

        pub fn current_apy(env: Env) -> AdapterApy {
            let broken: bool = env
                .storage()
                .instance()
                .get(&MockKey::Broken)
                .unwrap_or(false);
            if broken {
                panic_with_error!(&env, nester_common::ContractError::InvalidOperation);
            }
            env.storage()
                .instance()
                .get(&MockKey::Apy)
                .unwrap_or(AdapterApy {
                    apy_bps: 0,
                    confidence: ApyConfidence::Unavailable,
                })
        }
    }

    fn setup_with_adapter(
        env: &Env,
    ) -> (
        YieldRegistryContractClient<'_>,
        Address,
        MockAdapterClient<'_>,
        Symbol,
    ) {
        let (client, admin) = setup(env);
        let adapter_id = env.register_contract(None, MockAdapter);
        let adapter = MockAdapterClient::new(env, &adapter_id);
        let id = aave_id(env);
        client.register_source(
            &admin,
            &id,
            &Address::generate(env),
            &Some(adapter_id),
            &ProtocolType::Lending,
        );
        (client, admin, adapter, id)
    }

    #[test]
    fn registered_source_starts_with_unknown_apy() {
        let env = Env::default();
        let (client, _admin, _adapter, id) = setup_with_adapter(&env);
        let perf = client.get_source_performance(&id);
        assert_eq!(perf.apy_confidence, ApyConfidence::Unavailable);
        assert_eq!(perf.current_apy_bps, 0);
    }

    #[test]
    fn refresh_pulls_apy_without_any_role() {
        let env = Env::default();
        let (client, _admin, adapter, id) = setup_with_adapter(&env);
        adapter.set_apy(&640, &false, &false);

        // No caller argument at all — the pull is permissionless.
        let reading = client.refresh_apy_from_adapter(&id);
        assert_eq!(reading.apy_bps, 640);
        assert_eq!(reading.confidence, ApyConfidence::ProtocolReported);

        let perf = client.get_source_performance(&id);
        assert_eq!(perf.current_apy_bps, 640);
        assert_eq!(perf.apy_confidence, ApyConfidence::ProtocolReported);
        assert_eq!(perf.apy_history.len(), 1);
    }

    #[test]
    fn unavailable_reading_marks_unknown_not_zero() {
        let env = Env::default();
        let (client, _admin, adapter, id) = setup_with_adapter(&env);
        adapter.set_apy(&500, &false, &false);
        client.refresh_apy_from_adapter(&id);

        // Adapter loses confidence (e.g. position reset).
        adapter.set_apy(&0, &false, &true);
        client.refresh_apy_from_adapter(&id);

        let perf = client.get_source_performance(&id);
        assert_eq!(perf.apy_confidence, ApyConfidence::Unavailable);
        // Value is retained, NOT zeroed — unknown is not zero.
        assert_eq!(perf.current_apy_bps, 500);
    }

    #[test]
    fn unknown_apy_sources_excluded_from_apy_filter() {
        let env = Env::default();
        let (client, _admin, adapter, id) = setup_with_adapter(&env);
        adapter.set_apy(&0, &false, &true);
        client.refresh_apy_from_adapter(&id);

        // Unknown must never rank as if it were a real rate.
        assert_eq!(client.get_sources_above_apy(&0).len(), 0);

        adapter.set_apy(&300, &false, &false);
        client.refresh_apy_from_adapter(&id);
        assert_eq!(client.get_sources_above_apy(&0).len(), 1);
    }

    #[test]
    #[should_panic(expected = "Error(Contract, #9)")] // InvalidOperation
    fn refresh_without_adapter_is_rejected() {
        let env = Env::default();
        let (client, admin) = setup(&env);
        register_default(&client, &env, &admin, &blend_id(&env));
        client.refresh_apy_from_adapter(&blend_id(&env));
    }

    #[test]
    fn failing_adapter_increments_failure_count() {
        let env = Env::default();
        let (client, _admin, adapter, id) = setup_with_adapter(&env);
        adapter.set_broken(&true);

        // Must not revert: a panic would roll back the failure counter.
        let reading = client.refresh_apy_from_adapter(&id);
        assert_eq!(reading.confidence, ApyConfidence::Unavailable);
        assert_eq!(client.get_source_failure_count(&id), 1);
        // Still Active — one failure is under the threshold.
        assert_eq!(client.get_source_status(&id), SourceStatus::Active);
    }

    #[test]
    fn repeated_failures_flip_source_to_degraded() {
        let env = Env::default();
        let (client, _admin, adapter, id) = setup_with_adapter(&env);
        adapter.set_broken(&true);

        let threshold = client.get_failure_threshold();
        for _ in 0..=threshold {
            client.refresh_apy_from_adapter(&id);
        }

        assert_eq!(client.get_source_status(&id), SourceStatus::Degraded);
        assert!(client.get_source_failure_count(&id) > threshold);
        assert_eq!(client.get_degraded_sources().len(), 1);
    }

    #[test]
    fn degraded_source_is_not_active() {
        let env = Env::default();
        let (client, _admin, adapter, id) = setup_with_adapter(&env);
        adapter.set_broken(&true);
        for _ in 0..=client.get_failure_threshold() {
            client.refresh_apy_from_adapter(&id);
        }
        assert_eq!(client.get_active_sources().len(), 0);
    }

    #[test]
    fn recovery_requires_explicit_admin_action() {
        let env = Env::default();
        let (client, admin, adapter, id) = setup_with_adapter(&env);
        adapter.set_broken(&true);
        for _ in 0..=client.get_failure_threshold() {
            client.refresh_apy_from_adapter(&id);
        }
        assert_eq!(client.get_source_status(&id), SourceStatus::Degraded);

        // A working adapter alone must NOT silently re-activate the source.
        adapter.set_broken(&false);
        adapter.set_apy(&420, &false, &false);
        client.refresh_apy_from_adapter(&id);
        assert_eq!(
            client.get_source_status(&id),
            SourceStatus::Degraded,
            "recovery must never be automatic"
        );

        // Only an explicit admin call brings it back.
        client.recover_source(&admin, &id);
        assert_eq!(client.get_source_status(&id), SourceStatus::Active);
        assert_eq!(client.get_source_failure_count(&id), 0);
    }

    #[test]
    #[should_panic(expected = "Error(Contract, #9)")] // InvalidOperation
    fn recover_rejects_non_degraded_source() {
        let env = Env::default();
        let (client, admin, _adapter, id) = setup_with_adapter(&env);
        client.recover_source(&admin, &id);
    }

    #[test]
    fn successful_refresh_clears_failure_streak() {
        let env = Env::default();
        let (client, _admin, adapter, id) = setup_with_adapter(&env);
        adapter.set_broken(&true);
        client.refresh_apy_from_adapter(&id);
        assert_eq!(client.get_source_failure_count(&id), 1);

        adapter.set_broken(&false);
        adapter.set_apy(&250, &false, &false);
        client.refresh_apy_from_adapter(&id);
        assert_eq!(client.get_source_failure_count(&id), 0);
    }

    #[test]
    fn vault_reported_failures_also_degrade() {
        let env = Env::default();
        let (client, admin, _adapter, id) = setup_with_adapter(&env);
        // The vault reports failures it observes while skipping a source.
        for _ in 0..=client.get_failure_threshold() {
            client.report_source_failure(&admin, &id);
        }
        assert_eq!(client.get_source_status(&id), SourceStatus::Degraded);
    }

    #[test]
    fn deviation_guard_applies_to_adapter_pulls() {
        let env = Env::default();
        let (client, admin, adapter, id) = setup_with_adapter(&env);
        client.set_apy_deviation_threshold(&admin, &100);

        adapter.set_apy(&500, &false, &false);
        client.refresh_apy_from_adapter(&id);

        // A compromised adapter must not be able to move APY arbitrarily. The
        // reading is rejected and counted as a failure rather than reverting,
        // so the failure accounting survives (a panic would roll it back).
        adapter.set_apy(&9_000, &false, &false);
        let reading = client.refresh_apy_from_adapter(&id);
        assert_eq!(reading.confidence, ApyConfidence::Unavailable);
        assert_eq!(client.get_source_performance(&id).current_apy_bps, 500);
        assert_eq!(client.get_source_failure_count(&id), 1);

        // The admin override remains the escape hatch.
        client.update_apy_override(&admin, &id, &9_000);
        assert_eq!(client.get_source_performance(&id).current_apy_bps, 9_000);
    }

    #[test]
    fn adapter_can_be_attached_to_existing_source() {
        let env = Env::default();
        let (client, admin) = setup(&env);
        let id = blend_id(&env);
        register_default(&client, &env, &admin, &id);
        assert_eq!(client.get_source_adapter(&id), None);

        let adapter_id = env.register_contract(None, MockAdapter);
        MockAdapterClient::new(&env, &adapter_id).set_apy(&700, &false, &false);
        client.set_source_adapter(&admin, &id, &Some(adapter_id.clone()));

        assert_eq!(client.get_source_adapter(&id), Some(adapter_id));
        assert_eq!(client.refresh_apy_from_adapter(&id).apy_bps, 700);
    }

    #[test]
    fn beyond_deviation_reading_counts_as_failure_not_revert() {
        let env = Env::default();
        let (client, admin, adapter, id) = setup_with_adapter(&env);
        client.set_apy_deviation_threshold(&admin, &100);

        adapter.set_apy(&500, &false, &false);
        client.refresh_apy_from_adapter(&id);

        // An adapter pinning a wild value must not be able to make this call
        // revert forever while the source stays Active on a stale APY: each
        // rejected reading counts as a failure and eventually degrades it.
        adapter.set_apy(&9_000, &false, &false);
        for _ in 0..=client.get_failure_threshold() {
            let reading = client.refresh_apy_from_adapter(&id);
            assert_eq!(reading.confidence, ApyConfidence::Unavailable);
        }

        assert_eq!(client.get_source_status(&id), SourceStatus::Degraded);
        // The last known-good value is retained, never overwritten by garbage.
        assert_eq!(client.get_source_performance(&id).current_apy_bps, 500);
    }
}

// ---------------------------------------------------------------------------
// Failure accounting on terminal states (#812)
// ---------------------------------------------------------------------------

#[test]
fn failures_do_not_override_terminal_states() {
    let env = Env::default();
    let (client, admin) = setup(&env);
    let id = aave_id(&env);
    register_default(&client, &env, &admin, &id);

    client.update_status(&admin, &id, &SourceStatus::Deprecated);

    // Deprecated already keeps capital away; failures must not relabel it as
    // Degraded, which would imply a recoverable condition.
    for _ in 0..=client.get_failure_threshold() {
        client.report_source_failure(&admin, &id);
    }

    assert_eq!(client.get_source_status(&id), SourceStatus::Deprecated);
    assert!(client.get_source_failure_count(&id) > 0, "failures still counted");
}
