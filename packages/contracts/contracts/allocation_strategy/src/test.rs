#![cfg(test)]

extern crate std;

use super::*;
use nester_access_control::Role;
use nester_common::{ProtocolType as RegistryProtocolType, SourceStatus as RegistrySourceStatus};
use soroban_sdk::{
    symbol_short,
    testutils::{Address as _, Events},
    vec, Address, Env,
};
use yield_registry::{YieldRegistryContract, YieldRegistryContractClient};

// Authorization test matrix (U = unauthorized/unsigned, A = authorized,
// R = role revoked before a subsequent call):
//
// initialize / initialize_with_vault_type
//   U: initialize_requires_admin_signature;
//      initialize_with_vault_type_requires_signature
//   A: strategy_initialization_sets_vault_type_and_default_tables
//   R: N/A (one-shot bootstrap)
// update_strategy_params
//   U: non_admin_update_attempts_are_rejected;
//      privileged_strategy_calls_require_signatures
//   A: admin_can_update_strategy_parameters
//   R: revoked_admin_cannot_use_admin_operations
// set_weights
//   U: unauthorized_caller_cannot_update_weights;
//      privileged_strategy_calls_require_signatures
//   A: set_weights_and_calculate_allocation; operator_can_update_weights
//   R: revoked_operator_cannot_set_weights
// compute_allocation
//   U/A/R: compute_allocation_role_lifecycle
//   unsigned: privileged_strategy_calls_require_signatures
// set_allocations
//   U: test_set_allocations_unauthorized_is_rejected;
//      set_allocations_role_lifecycle; privileged_strategy_calls_require_signatures
//   A/R: set_allocations_role_lifecycle
// grant_role
//   U: operator_cannot_grant_roles; privileged_access_control_calls_require_signatures
//   A: operator_can_update_weights
//   R: revoked_admin_cannot_use_admin_operations
// revoke_role
//   U: operator_cannot_revoke_roles; privileged_access_control_calls_require_signatures
//   A: revoked_operator_cannot_set_weights
//   R: revoked_admin_cannot_use_admin_operations
// transfer_admin
//   U: operator_cannot_transfer_admin; privileged_access_control_calls_require_signatures
//   A/R: two_step_admin_transfer_moves_privileges
// accept_admin
//   U: wrong_address_cannot_accept_admin; privileged_access_control_calls_require_signatures
//   A: two_step_admin_transfer_moves_privileges             R: N/A (address-bound)
//
// Negative role tests intentionally pass values that satisfy every subsequent
// business-rule validation. Removing an authorization check must therefore turn
// the protected call into a success and make its test fail.

fn reg(
    registry: &YieldRegistryContractClient,
    env: &Env,
    admin: &Address,
    id: soroban_sdk::Symbol,
) {
    registry.register_source(
        admin,
        &id,
        &Address::generate(env),
        &RegistryProtocolType::Lending,
    );
}

fn setup_with_type(vault_type: VaultType) -> (Env, Address, Address, Address) {
    let env = Env::default();
    env.mock_all_auths();

    let admin = Address::generate(&env);
    let registry_id = env.register_contract(None, YieldRegistryContract);
    let strategy_id = env.register_contract(None, AllocationStrategyContract);

    let registry = YieldRegistryContractClient::new(&env, &registry_id);
    registry.initialize(&admin);
    reg(&registry, &env, &admin, symbol_short!("aave"));
    reg(&registry, &env, &admin, symbol_short!("blend"));
    reg(&registry, &env, &admin, symbol_short!("comp"));

    AllocationStrategyContractClient::new(&env, &strategy_id).initialize_with_vault_type(
        &admin,
        &registry_id,
        &vault_type,
    );

    (env, admin, registry_id, strategy_id)
}

fn weight_for(weights: &soroban_sdk::Vec<AllocationWeight>, source_id: soroban_sdk::Symbol) -> u32 {
    for weight in weights.iter() {
        if weight.source_id == source_id {
            return weight.weight_bps;
        }
    }
    0
}

fn weight_sum(weights: &soroban_sdk::Vec<AllocationWeight>) -> u32 {
    let mut sum = 0_u32;
    for weight in weights.iter() {
        sum += weight.weight_bps;
    }
    sum
}

fn valid_balanced_weights(env: &Env) -> soroban_sdk::Vec<AllocationWeight> {
    vec![
        env,
        AllocationWeight {
            source_id: symbol_short!("aave"),
            weight_bps: 4_000,
        },
        AllocationWeight {
            source_id: symbol_short!("blend"),
            weight_bps: 3_500,
        },
        AllocationWeight {
            source_id: symbol_short!("comp"),
            weight_bps: 2_500,
        },
    ]
}

fn valid_apys(env: &Env) -> soroban_sdk::Vec<SourceApy> {
    vec![
        env,
        SourceApy {
            source_id: symbol_short!("aave"),
            apy_bps: 300,
        },
        SourceApy {
            source_id: symbol_short!("blend"),
            apy_bps: 300,
        },
        SourceApy {
            source_id: symbol_short!("comp"),
            apy_bps: 400,
        },
    ]
}

#[test]
fn strategy_initialization_sets_vault_type_and_default_tables() {
    for (vault_type, aave, blend, comp) in [
        (VaultType::Conservative, 5_000, 3_000, 2_000),
        (VaultType::Balanced, 4_000, 3_500, 2_500),
        (VaultType::Growth, 2_000, 3_000, 5_000),
        (VaultType::DeFi500, 3_334, 3_333, 3_333),
    ] {
        let (env, _, _, strategy_id) = setup_with_type(vault_type.clone());
        let client = AllocationStrategyContractClient::new(&env, &strategy_id);

        assert_eq!(client.get_vault_type(), vault_type);

        let actual = client.get_weights();
        assert_eq!(weight_sum(&actual), 10_000);
        assert_eq!(weight_for(&actual, symbol_short!("aave")), aave);
        assert_eq!(weight_for(&actual, symbol_short!("blend")), blend);
        assert_eq!(weight_for(&actual, symbol_short!("comp")), comp);
    }
}

#[test]
#[should_panic]
fn strategy_reinitialization_is_rejected() {
    let env = Env::default();
    env.mock_all_auths();

    let admin = Address::generate(&env);
    let registry_id = env.register_contract(None, YieldRegistryContract);
    let strategy_id = env.register_contract(None, AllocationStrategyContract);

    let registry = YieldRegistryContractClient::new(&env, &registry_id);
    registry.initialize(&admin);
    reg(&registry, &env, &admin, symbol_short!("aave"));

    let client = AllocationStrategyContractClient::new(&env, &strategy_id);
    client.initialize_with_vault_type(&admin, &registry_id, &VaultType::Balanced);
    client.initialize_with_vault_type(&admin, &registry_id, &VaultType::Growth);
}

#[test]
fn initialize_requires_admin_signature() {
    let env = Env::default();
    env.mock_all_auths();

    let admin = Address::generate(&env);
    let registry_id = env.register_contract(None, YieldRegistryContract);
    YieldRegistryContractClient::new(&env, &registry_id).initialize(&admin);
    let strategy_id = env.register_contract(None, AllocationStrategyContract);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);

    env.mock_auths(&[]);

    assert!(client.try_initialize(&admin, &registry_id).is_err());
}

#[test]
fn initialize_with_vault_type_requires_signature() {
    let env = Env::default();
    env.mock_all_auths();

    let admin = Address::generate(&env);
    let registry_id = env.register_contract(None, YieldRegistryContract);
    YieldRegistryContractClient::new(&env, &registry_id).initialize(&admin);
    let strategy_id = env.register_contract(None, AllocationStrategyContract);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);

    env.mock_auths(&[]);

    assert!(client
        .try_initialize_with_vault_type(&admin, &registry_id, &VaultType::Balanced)
        .is_err());
}

#[test]
fn set_weights_and_calculate_allocation() {
    let (env, admin, _, strategy_id) = setup_with_type(VaultType::Balanced);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);

    let weights = vec![
        &env,
        AllocationWeight {
            source_id: symbol_short!("aave"),
            weight_bps: 4_000,
        },
        AllocationWeight {
            source_id: symbol_short!("blend"),
            weight_bps: 3_000,
        },
        AllocationWeight {
            source_id: symbol_short!("comp"),
            weight_bps: 3_000,
        },
    ];

    client.set_weights(&admin, &weights);

    let stored = client.get_weights();
    assert_eq!(stored, weights);

    let allocations = client.calculate_allocation(&10_000_i128);
    assert_eq!(
        allocations,
        vec![
            &env,
            (symbol_short!("aave"), 4_000_i128),
            (symbol_short!("blend"), 3_000_i128),
            (symbol_short!("comp"), 3_000_i128),
        ]
    );
    assert_eq!(client.get_source_allocation(&symbol_short!("blend")), 3_000);
    assert!(!env.events().all().is_empty());
}

#[test]
fn rejects_invalid_weight_sum() {
    let (env, admin, _, strategy_id) = setup_with_type(VaultType::Balanced);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);

    let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        client.set_weights(
            &admin,
            &vec![
                &env,
                AllocationWeight {
                    source_id: symbol_short!("aave"),
                    weight_bps: 4_000,
                },
                AllocationWeight {
                    source_id: symbol_short!("blend"),
                    weight_bps: 5_000,
                },
            ],
        );
    }));

    assert!(result.is_err());
}

#[test]
fn rejects_unknown_source_ids() {
    let env = Env::default();
    env.mock_all_auths();

    let admin = Address::generate(&env);
    let registry_id = env.register_contract(None, YieldRegistryContract);
    let strategy_id = env.register_contract(None, AllocationStrategyContract);
    let registry = YieldRegistryContractClient::new(&env, &registry_id);
    registry.initialize(&admin);

    let client = AllocationStrategyContractClient::new(&env, &strategy_id);
    client.initialize(&admin, &registry_id);

    let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        client.set_weights(
            &admin,
            &vec![
                &env,
                AllocationWeight {
                    source_id: symbol_short!("ghost"),
                    weight_bps: 10_000,
                },
            ],
        );
    }));

    assert!(result.is_err());
}

#[test]
fn sends_remainder_to_highest_weight() {
    let (env, admin, _, strategy_id) = setup_with_type(VaultType::Balanced);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);
    client.set_weights(
        &admin,
        &vec![
            &env,
            AllocationWeight {
                source_id: symbol_short!("aave"),
                weight_bps: 3_333,
            },
            AllocationWeight {
                source_id: symbol_short!("blend"),
                weight_bps: 3_333,
            },
            AllocationWeight {
                source_id: symbol_short!("comp"),
                weight_bps: 3_334,
            },
        ],
    );

    let allocations = client.calculate_allocation(&100_i128);
    assert_eq!(
        allocations,
        vec![
            &env,
            (symbol_short!("aave"), 33_i128),
            (symbol_short!("blend"), 33_i128),
            (symbol_short!("comp"), 34_i128),
        ]
    );
}

#[test]
fn unauthorized_caller_cannot_update_weights() {
    let (env, _admin, _, strategy_id) = setup_with_type(VaultType::Balanced);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);
    let outsider = Address::generate(&env);

    // These weights satisfy sum, source-status, and min/max validation. If the
    // role check is removed, this call succeeds and the test fails.
    assert!(client
        .try_set_weights(&outsider, &valid_balanced_weights(&env))
        .is_err());
}

#[test]
fn operator_can_update_weights() {
    let (env, admin, _, strategy_id) = setup_with_type(VaultType::Balanced);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);
    let operator = Address::generate(&env);
    client.grant_role(&admin, &operator, &Role::Operator);

    let weights = vec![
        &env,
        AllocationWeight {
            source_id: symbol_short!("aave"),
            weight_bps: 6_000,
        },
        AllocationWeight {
            source_id: symbol_short!("blend"),
            weight_bps: 4_000,
        },
    ];

    client.set_weights(&operator, &weights);
    assert_eq!(client.get_weights(), weights);
}

#[test]
fn operator_cannot_grant_roles() {
    let (env, admin, _, strategy_id) = setup_with_type(VaultType::Balanced);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);
    let operator = Address::generate(&env);
    let outsider = Address::generate(&env);
    client.grant_role(&admin, &operator, &Role::Operator);

    assert!(env.as_contract(&strategy_id, || {
        nester_access_control::AccessControl::has_role(&env, &operator, Role::Operator)
    }));
    assert!(!env.as_contract(&strategy_id, || {
        nester_access_control::AccessControl::has_role(&env, &operator, Role::Admin)
    }));
    assert_eq!(
        client.try_grant_role(&operator, &outsider, &Role::Operator),
        Err(Ok(soroban_sdk::Error::from_contract_error(
            nester_common::ContractError::Unauthorized as u32,
        ))),
    );
}

#[test]
fn operator_cannot_revoke_roles() {
    let (env, admin, _, strategy_id) = setup_with_type(VaultType::Balanced);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);
    let operator = Address::generate(&env);
    let target = Address::generate(&env);
    client.grant_role(&admin, &operator, &Role::Operator);
    client.grant_role(&admin, &target, &Role::Operator);
    assert!(env.as_contract(&strategy_id, || {
        nester_access_control::AccessControl::has_role(&env, &target, Role::Operator)
    }));

    // Revoke a non-Admin role so last-admin protection cannot mask a missing
    // authorization check.
    assert_eq!(
        client.try_revoke_role(&operator, &target, &Role::Operator),
        Err(Ok(soroban_sdk::Error::from_contract_error(
            nester_common::ContractError::Unauthorized as u32,
        ))),
    );
    assert!(env.as_contract(&strategy_id, || {
        nester_access_control::AccessControl::has_role(&env, &target, Role::Operator)
    }));
}

#[test]
fn operator_cannot_transfer_admin() {
    let (env, admin, _, strategy_id) = setup_with_type(VaultType::Balanced);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);
    let operator = Address::generate(&env);
    let outsider = Address::generate(&env);
    client.grant_role(&admin, &operator, &Role::Operator);

    assert!(env.as_contract(&strategy_id, || {
        nester_access_control::AccessControl::has_role(&env, &operator, Role::Operator)
    }));
    assert!(!env.as_contract(&strategy_id, || {
        nester_access_control::AccessControl::has_role(&env, &operator, Role::Admin)
    }));
    assert_eq!(
        client.try_transfer_admin(&operator, &outsider),
        Err(Ok(soroban_sdk::Error::from_contract_error(
            nester_common::ContractError::Unauthorized as u32,
        ))),
    );
}

#[test]
fn revoked_operator_cannot_set_weights() {
    let (env, admin, _, strategy_id) = setup_with_type(VaultType::Balanced);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);
    let operator = Address::generate(&env);
    let weights = valid_balanced_weights(&env);

    client.grant_role(&admin, &operator, &Role::Operator);
    client.set_weights(&operator, &weights);
    client.revoke_role(&admin, &operator, &Role::Operator);

    assert!(client.try_set_weights(&operator, &weights).is_err());
}

#[test]
fn compute_allocation_role_lifecycle() {
    let (env, admin, _, strategy_id) = setup_with_type(VaultType::Balanced);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);
    let operator = Address::generate(&env);
    let outsider = Address::generate(&env);
    let apys = valid_apys(&env);

    assert!(client.try_compute_allocation(&outsider, &apys).is_err());

    client.grant_role(&admin, &operator, &Role::Operator);
    let weights = client.compute_allocation(&operator, &apys);
    assert_eq!(weight_sum(&weights), 10_000);

    client.revoke_role(&admin, &operator, &Role::Operator);
    assert!(client.try_compute_allocation(&operator, &apys).is_err());
}

#[test]
fn revoked_admin_cannot_use_admin_operations() {
    let (env, initial_admin, _, strategy_id) = setup_with_type(VaultType::Balanced);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);
    let remaining_admin = Address::generate(&env);
    let operator = Address::generate(&env);

    client.grant_role(&initial_admin, &remaining_admin, &Role::Admin);
    client.grant_role(&initial_admin, &operator, &Role::Operator);
    client.revoke_role(&remaining_admin, &initial_admin, &Role::Admin);

    assert!(client
        .try_update_strategy_params(&initial_admin, &500, &6_500, &500)
        .is_err());
    assert!(client
        .try_grant_role(&initial_admin, &Address::generate(&env), &Role::Operator,)
        .is_err());
    assert!(client
        .try_revoke_role(&initial_admin, &operator, &Role::Operator)
        .is_err());
    assert!(client
        .try_transfer_admin(&initial_admin, &Address::generate(&env))
        .is_err());
}

#[test]
fn two_step_admin_transfer_moves_privileges() {
    let (env, initial_admin, _, strategy_id) = setup_with_type(VaultType::Balanced);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);
    let new_admin = Address::generate(&env);
    let operator = Address::generate(&env);

    client.transfer_admin(&initial_admin, &new_admin);
    client.accept_admin(&new_admin);

    client.update_strategy_params(&new_admin, &500, &6_500, &500);
    client.grant_role(&new_admin, &operator, &Role::Operator);
    assert!(client
        .try_update_strategy_params(&initial_admin, &500, &6_500, &500)
        .is_err());
    assert!(client
        .try_grant_role(&initial_admin, &Address::generate(&env), &Role::Operator,)
        .is_err());
}

#[test]
fn wrong_address_cannot_accept_admin() {
    let (env, admin, _, strategy_id) = setup_with_type(VaultType::Balanced);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);
    let proposed_admin = Address::generate(&env);
    let outsider = Address::generate(&env);
    client.transfer_admin(&admin, &proposed_admin);

    assert!(client.try_accept_admin(&outsider).is_err());

    // The failed attempt must leave the original proposal usable.
    client.accept_admin(&proposed_admin);
    client.update_strategy_params(&proposed_admin, &500, &6_500, &500);
}

#[test]
fn privileged_strategy_calls_require_signatures() {
    let (env, admin, _, strategy_id) = setup_with_type(VaultType::Balanced);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);
    let operator = Address::generate(&env);
    let weights = valid_balanced_weights(&env);
    let apys = valid_apys(&env);
    client.grant_role(&admin, &operator, &Role::Operator);

    env.mock_auths(&[]);

    assert!(client
        .try_update_strategy_params(&admin, &500, &6_500, &500)
        .is_err());
    assert!(client.try_set_weights(&admin, &weights).is_err());
    assert!(client.try_compute_allocation(&admin, &apys).is_err());
    assert!(client
        .try_set_allocations(&operator, &1_000_i128, &apys)
        .is_err());
}

#[test]
fn privileged_access_control_calls_require_signatures() {
    let (env, admin, _, strategy_id) = setup_with_type(VaultType::Balanced);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);
    let operator = Address::generate(&env);
    let proposed_admin = Address::generate(&env);
    client.grant_role(&admin, &operator, &Role::Operator);
    client.transfer_admin(&admin, &proposed_admin);

    env.mock_auths(&[]);

    assert!(client
        .try_grant_role(&admin, &Address::generate(&env), &Role::Operator)
        .is_err());
    assert!(client
        .try_revoke_role(&admin, &operator, &Role::Operator)
        .is_err());
    assert!(client
        .try_transfer_admin(&admin, &Address::generate(&env))
        .is_err());
    assert!(client.try_accept_admin(&proposed_admin).is_err());
}

#[test]
fn compute_allocation_preserves_weight_and_amount_invariants() {
    let (env, admin, _, strategy_id) = setup_with_type(VaultType::Growth);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);
    let operator = Address::generate(&env);
    client.grant_role(&admin, &operator, &Role::Operator);

    let apys = vec![
        &env,
        SourceApy {
            source_id: symbol_short!("aave"),
            apy_bps: 150,
        },
        SourceApy {
            source_id: symbol_short!("blend"),
            apy_bps: 300,
        },
        SourceApy {
            source_id: symbol_short!("comp"),
            apy_bps: 550,
        },
    ];

    // Weight invariant: compute_allocation weights sum to 10_000.
    let weights = client.compute_allocation(&admin, &apys);
    assert_eq!(weight_sum(&weights), 10_000);

    // Amount invariant: set_allocations stores amounts that sum to total_amount.
    for total in [1_i128, 7_i128, 101_i128, 10_001_i128] {
        client.set_allocations(&operator, &total, &apys);
        let allocated_total = client.get_source_allocation(&symbol_short!("aave"))
            + client.get_source_allocation(&symbol_short!("blend"))
            + client.get_source_allocation(&symbol_short!("comp"));
        assert_eq!(allocated_total, total);
    }
}

#[test]
fn conservative_strategy_caps_individual_protocol_weight() {
    let (env, admin, _, strategy_id) = setup_with_type(VaultType::Conservative);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);

    let weights = client.compute_allocation(
        &admin,
        &vec![
            &env,
            SourceApy {
                source_id: symbol_short!("aave"),
                apy_bps: 1_000,
            },
            SourceApy {
                source_id: symbol_short!("blend"),
                apy_bps: 1,
            },
            SourceApy {
                source_id: symbol_short!("comp"),
                apy_bps: 1,
            },
        ],
    );

    assert_eq!(weight_sum(&weights), 10_000);
    assert!(weight_for(&weights, symbol_short!("aave")) <= 5_000);
}

#[test]
fn growth_strategy_allocates_more_to_higher_apy_sources() {
    let (env, admin, _, strategy_id) = setup_with_type(VaultType::Growth);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);

    let weights = client.compute_allocation(
        &admin,
        &vec![
            &env,
            SourceApy {
                source_id: symbol_short!("aave"),
                apy_bps: 100,
            },
            SourceApy {
                source_id: symbol_short!("blend"),
                apy_bps: 300,
            },
            SourceApy {
                source_id: symbol_short!("comp"),
                apy_bps: 900,
            },
        ],
    );

    assert!(
        weight_for(&weights, symbol_short!("comp")) > weight_for(&weights, symbol_short!("blend"))
    );
    assert!(
        weight_for(&weights, symbol_short!("blend")) > weight_for(&weights, symbol_short!("aave"))
    );
}

#[test]
fn defi500_strategy_distributes_evenly_across_registered_sources() {
    let (env, admin, _, strategy_id) = setup_with_type(VaultType::DeFi500);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);

    let weights = client.compute_allocation(
        &admin,
        &vec![
            &env,
            SourceApy {
                source_id: symbol_short!("aave"),
                apy_bps: 10,
            },
            SourceApy {
                source_id: symbol_short!("blend"),
                apy_bps: 20,
            },
            SourceApy {
                source_id: symbol_short!("comp"),
                apy_bps: 30,
            },
        ],
    );

    assert_eq!(weight_sum(&weights), 10_000);
    assert!(
        weight_for(&weights, symbol_short!("aave"))
            .abs_diff(weight_for(&weights, symbol_short!("blend")))
            <= 1
    );
    assert!(
        weight_for(&weights, symbol_short!("blend"))
            .abs_diff(weight_for(&weights, symbol_short!("comp")))
            <= 1
    );
}

#[test]
fn zero_apy_source_receives_zero_allocation_weight() {
    let (env, admin, _, strategy_id) = setup_with_type(VaultType::Growth);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);

    let weights = client.compute_allocation(
        &admin,
        &vec![
            &env,
            SourceApy {
                source_id: symbol_short!("aave"),
                apy_bps: 0,
            },
            SourceApy {
                source_id: symbol_short!("blend"),
                apy_bps: 100,
            },
            SourceApy {
                source_id: symbol_short!("comp"),
                apy_bps: 200,
            },
        ],
    );

    assert_eq!(weight_for(&weights, symbol_short!("aave")), 0);
    assert_eq!(weight_sum(&weights), 10_000);
}

#[test]
fn deactivated_and_unregistered_sources_receive_zero_weight() {
    let (env, admin, registry_id, strategy_id) = setup_with_type(VaultType::Growth);
    let registry = YieldRegistryContractClient::new(&env, &registry_id);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);
    registry.update_status(
        &admin,
        &symbol_short!("aave"),
        &RegistrySourceStatus::Paused,
    );

    let weights = client.compute_allocation(
        &admin,
        &vec![
            &env,
            SourceApy {
                source_id: symbol_short!("aave"),
                apy_bps: 300,
            },
            SourceApy {
                source_id: symbol_short!("ghost"),
                apy_bps: 500,
            },
            SourceApy {
                source_id: symbol_short!("blend"),
                apy_bps: 200,
            },
            SourceApy {
                source_id: symbol_short!("comp"),
                apy_bps: 100,
            },
        ],
    );

    assert_eq!(weight_for(&weights, symbol_short!("aave")), 0);
    assert_eq!(weight_for(&weights, symbol_short!("ghost")), 0);
    assert!(weight_for(&weights, symbol_short!("blend")) > 0);
    assert!(weight_for(&weights, symbol_short!("comp")) > 0);
    assert_eq!(weight_sum(&weights), 10_000);
}

#[test]
fn needs_rebalance_when_drift_exceeds_threshold() {
    let (env, admin, _, strategy_id) = setup_with_type(VaultType::Balanced);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);
    client.update_strategy_params(&admin, &250, &6_500, &500);

    let current = vec![
        &env,
        AllocationWeight {
            source_id: symbol_short!("aave"),
            weight_bps: 6_000,
        },
        AllocationWeight {
            source_id: symbol_short!("blend"),
            weight_bps: 4_000,
        },
    ];
    let target = vec![
        &env,
        AllocationWeight {
            source_id: symbol_short!("aave"),
            weight_bps: 5_000,
        },
        AllocationWeight {
            source_id: symbol_short!("blend"),
            weight_bps: 5_000,
        },
    ];

    assert!(client.needs_rebalance(&current, &target));
}

#[test]
fn does_not_rebalance_when_within_tolerance() {
    let (env, admin, _, strategy_id) = setup_with_type(VaultType::Balanced);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);
    client.update_strategy_params(&admin, &250, &6_500, &500);

    let current = vec![
        &env,
        AllocationWeight {
            source_id: symbol_short!("aave"),
            weight_bps: 5_100,
        },
        AllocationWeight {
            source_id: symbol_short!("blend"),
            weight_bps: 4_900,
        },
    ];
    let target = vec![
        &env,
        AllocationWeight {
            source_id: symbol_short!("aave"),
            weight_bps: 5_000,
        },
        AllocationWeight {
            source_id: symbol_short!("blend"),
            weight_bps: 5_000,
        },
    ];

    assert!(!client.needs_rebalance(&current, &target));
}

#[test]
fn does_not_rebalance_at_threshold_boundary() {
    let (env, admin, _, strategy_id) = setup_with_type(VaultType::Balanced);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);
    client.update_strategy_params(&admin, &250, &6_500, &500);

    let current = vec![
        &env,
        AllocationWeight {
            source_id: symbol_short!("aave"),
            weight_bps: 5_250,
        },
        AllocationWeight {
            source_id: symbol_short!("blend"),
            weight_bps: 4_750,
        },
    ];
    let target = vec![
        &env,
        AllocationWeight {
            source_id: symbol_short!("aave"),
            weight_bps: 5_000,
        },
        AllocationWeight {
            source_id: symbol_short!("blend"),
            weight_bps: 5_000,
        },
    ];

    assert!(!client.needs_rebalance(&current, &target));
}

#[test]
fn admin_can_update_strategy_parameters() {
    let (env, admin, _, strategy_id) = setup_with_type(VaultType::Balanced);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);
    client.update_strategy_params(&admin, &125, &4_500, &300);

    assert_eq!(
        client.get_strategy_params(),
        StrategyParams {
            rebalance_threshold_bps: 125,
            max_weight_bps: 4_500,
            min_weight_bps: 300,
        }
    );
}

#[test]
fn non_admin_update_attempts_are_rejected() {
    let (env, _, _, strategy_id) = setup_with_type(VaultType::Balanced);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);
    let outsider = Address::generate(&env);

    assert!(!env.as_contract(&strategy_id, || {
        nester_access_control::AccessControl::has_role(&env, &outsider, Role::Admin)
    }));
    assert_eq!(
        client.try_update_strategy_params(&outsider, &125, &4_500, &500),
        Err(Ok(soroban_sdk::Error::from_contract_error(
            nester_common::ContractError::Unauthorized as u32,
        ))),
    );
}

#[test]
fn rejects_inactive_sources() {
    let env = Env::default();
    env.mock_all_auths();

    let admin = Address::generate(&env);
    let registry_id = env.register_contract(None, YieldRegistryContract);
    let strategy_id = env.register_contract(None, AllocationStrategyContract);

    let registry = YieldRegistryContractClient::new(&env, &registry_id);
    registry.initialize(&admin);
    reg(&registry, &env, &admin, symbol_short!("aave"));
    registry.update_status(
        &admin,
        &symbol_short!("aave"),
        &RegistrySourceStatus::Paused,
    );

    let client = AllocationStrategyContractClient::new(&env, &strategy_id);
    client.initialize(&admin, &registry_id);

    let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        client.set_weights(
            &admin,
            &vec![
                &env,
                AllocationWeight {
                    source_id: symbol_short!("aave"),
                    weight_bps: 10_000,
                },
            ],
        );
    }));

    assert!(result.is_err());
}

#[test]
fn suggest_weights_empty_when_no_sources() {
    let env = Env::default();
    env.mock_all_auths();

    let admin = Address::generate(&env);
    let registry_id = env.register_contract(None, YieldRegistryContract);
    let strategy_id = env.register_contract(None, AllocationStrategyContract);

    let registry = YieldRegistryContractClient::new(&env, &registry_id);
    registry.initialize(&admin);

    let client = AllocationStrategyContractClient::new(&env, &strategy_id);
    client.initialize(&admin, &registry_id);

    assert_eq!(client.suggest_weights().len(), 0);
}

#[test]
fn suggest_weights_uses_apy_and_risk_scores() {
    let env = Env::default();
    env.mock_all_auths();

    let admin = Address::generate(&env);
    let registry_id = env.register_contract(None, YieldRegistryContract);
    let strategy_id = env.register_contract(None, AllocationStrategyContract);

    let registry = YieldRegistryContractClient::new(&env, &registry_id);
    registry.initialize(&admin);

    reg(&registry, &env, &admin, symbol_short!("aave"));
    reg(&registry, &env, &admin, symbol_short!("blend"));
    reg(&registry, &env, &admin, symbol_short!("comp"));

    // score(aave)  = 800 * (11-2) = 7_200
    // score(blend) = 1000 * (11-9) = 2_000
    // score(comp)  = 400 * (11-1) = 4_000
    registry.update_apy(&admin, &symbol_short!("aave"), &800);
    registry.update_risk_rating(&admin, &symbol_short!("aave"), &2);

    registry.update_apy(&admin, &symbol_short!("blend"), &1_000);
    registry.update_risk_rating(&admin, &symbol_short!("blend"), &9);

    registry.update_apy(&admin, &symbol_short!("comp"), &400);
    registry.update_risk_rating(&admin, &symbol_short!("comp"), &1);

    let client = AllocationStrategyContractClient::new(&env, &strategy_id);
    client.initialize(&admin, &registry_id);

    let suggested = client.suggest_weights();
    assert_eq!(suggested.len(), 3);
    assert_eq!(
        weight_for(&suggested, symbol_short!("aave"))
            + weight_for(&suggested, symbol_short!("blend"))
            + weight_for(&suggested, symbol_short!("comp")),
        10_000
    );

    assert_eq!(weight_for(&suggested, symbol_short!("aave")), 5_455);
    assert_eq!(weight_for(&suggested, symbol_short!("blend")), 1_515);
    assert_eq!(weight_for(&suggested, symbol_short!("comp")), 3_030);
}

// ---------------------------------------------------------------------------
// Issue #507 — rebalance delta conservation
// ---------------------------------------------------------------------------

fn delta_sum(deltas: &soroban_sdk::Vec<AllocationDelta>) -> i128 {
    let mut sum = 0_i128;
    for d in deltas.iter() {
        sum += d.delta;
    }
    sum
}

fn delta_for(deltas: &soroban_sdk::Vec<AllocationDelta>, id: soroban_sdk::Symbol) -> i128 {
    for d in deltas.iter() {
        if d.source_id == id {
            return d.delta;
        }
    }
    0
}

/// Balanced rebalance: total == sum(current_allocations).
/// With weights 50%/30%/20% and current [700,150,150] (sum=1000),
/// deltas are [-200, +150, +50] — exactly zero sum.
#[test]
fn rebalance_deltas_sum_to_zero_for_balanced_rebalance() {
    let (env, admin, _, strategy_id) = setup_with_type(VaultType::Balanced);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);

    client.set_weights(
        &admin,
        &vec![
            &env,
            AllocationWeight {
                source_id: symbol_short!("aave"),
                weight_bps: 5_000,
            },
            AllocationWeight {
                source_id: symbol_short!("blend"),
                weight_bps: 3_000,
            },
            AllocationWeight {
                source_id: symbol_short!("comp"),
                weight_bps: 2_000,
            },
        ],
    );

    let current = vec![
        &env,
        CurrentAllocation {
            source_id: symbol_short!("aave"),
            amount: 700,
        },
        CurrentAllocation {
            source_id: symbol_short!("blend"),
            amount: 150,
        },
        CurrentAllocation {
            source_id: symbol_short!("comp"),
            amount: 150,
        },
    ];
    let deltas = client.calculate_rebalance_deltas(&current, &1_000_i128);

    assert_eq!(delta_sum(&deltas), 0);
    assert_eq!(delta_for(&deltas, symbol_short!("aave")), -200);
    assert_eq!(delta_for(&deltas, symbol_short!("blend")), 150);
    assert_eq!(delta_for(&deltas, symbol_short!("comp")), 50);
}

/// Unbalanced: total (300) != sum(current_allocations) (400).
/// Delta sum = 300 - 400 = -100 (net withdrawal to vault).
#[test]
fn rebalance_deltas_allows_net_withdrawal_to_vault() {
    let (env, admin, _, strategy_id) = setup_with_type(VaultType::Balanced);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);

    client.set_weights(
        &admin,
        &vec![
            &env,
            AllocationWeight {
                source_id: symbol_short!("aave"),
                weight_bps: 5_000,
            },
            AllocationWeight {
                source_id: symbol_short!("blend"),
                weight_bps: 5_000,
            },
        ],
    );

    // sum(current) = 400, but total = 300 → delta sum = -100
    let current = vec![
        &env,
        CurrentAllocation {
            source_id: symbol_short!("aave"),
            amount: 300,
        },
        CurrentAllocation {
            source_id: symbol_short!("blend"),
            amount: 100,
        },
    ];
    let deltas = client.calculate_rebalance_deltas(&current, &300_i128);
    assert_eq!(delta_sum(&deltas), -100);
}

/// Empty current allocations with total=0 → all deltas are zero → succeeds.
#[test]
fn rebalance_deltas_empty_allocations_and_zero_total_succeeds() {
    let (env, admin, _, strategy_id) = setup_with_type(VaultType::Balanced);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);

    client.set_weights(
        &admin,
        &vec![
            &env,
            AllocationWeight {
                source_id: symbol_short!("aave"),
                weight_bps: 5_000,
            },
            AllocationWeight {
                source_id: symbol_short!("blend"),
                weight_bps: 5_000,
            },
        ],
    );

    let empty: soroban_sdk::Vec<CurrentAllocation> = soroban_sdk::Vec::new(&env);
    let deltas = client.calculate_rebalance_deltas(&empty, &0_i128);

    assert_eq!(delta_sum(&deltas), 0);
}

/// Stored allocation weights must always sum to exactly 10_000 bps.
#[test]
fn allocation_weights_always_sum_to_ten_thousand_bps() {
    for vault_type in [
        VaultType::Conservative,
        VaultType::Balanced,
        VaultType::Growth,
        VaultType::DeFi500,
    ] {
        let (env, _, _, strategy_id) = setup_with_type(vault_type);
        let client = AllocationStrategyContractClient::new(&env, &strategy_id);
        let weights = client.get_weights();
        assert_eq!(
            weight_sum(&weights),
            10_000,
            "weights must sum to 10_000 bps"
        );
    }
}

#[test]
fn set_allocations_role_lifecycle() {
    let (env, admin, _, strategy_id) = setup_with_type(VaultType::Balanced);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);
    let operator = Address::generate(&env);
    let apys = valid_apys(&env);

    // Admin is intentionally not an implicit Operator for this entrypoint.
    assert!(client
        .try_set_allocations(&admin, &1_000_i128, &apys)
        .is_err());

    client.grant_role(&admin, &operator, &Role::Operator);
    client.set_allocations(&operator, &1_000_i128, &apys);
    let allocated = client.get_source_allocation(&symbol_short!("aave"))
        + client.get_source_allocation(&symbol_short!("blend"))
        + client.get_source_allocation(&symbol_short!("comp"));
    assert_eq!(allocated, 1_000);

    client.revoke_role(&admin, &operator, &Role::Operator);
    assert!(client
        .try_set_allocations(&operator, &1_000_i128, &apys)
        .is_err());
}

// AllocationStrategy.set_allocations requires operator role — unauthorized callers are rejected.
#[test]
fn test_set_allocations_unauthorized_is_rejected() {
    let (env, _, _, strategy_id) = setup_with_type(VaultType::Balanced);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);
    let attacker = Address::generate(&env);
    let apys = valid_apys(&env);

    assert!(!env.as_contract(&strategy_id, || {
        nester_access_control::AccessControl::has_role(&env, &attacker, Role::Operator)
    }));
    assert!(client
        .try_set_allocations(&attacker, &1_000_i128, &apys)
        .is_err());
}

#[test]
fn rebalance_skips_paused_protocol() {
    let (env, admin, registry_id, strategy_id) = setup_with_type(VaultType::Balanced);
    let registry = YieldRegistryContractClient::new(&env, &registry_id);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);

    // Initial state: A=50%, B=50%
    client.set_weights(
        &admin,
        &vec![
            &env,
            AllocationWeight {
                source_id: symbol_short!("aave"),
                weight_bps: 5_000,
            },
            AllocationWeight {
                source_id: symbol_short!("blend"),
                weight_bps: 5_000,
            },
        ],
    );

    // Current: A=400, B=600 (Total=1000)
    let current = vec![
        &env,
        CurrentAllocation {
            source_id: symbol_short!("aave"),
            amount: 400,
        },
        CurrentAllocation {
            source_id: symbol_short!("blend"),
            amount: 600,
        },
    ];

    // Mark Blend as Paused. It should keep its 600, and Aave should keep its 400.
    registry.update_status(&admin, &symbol_short!("blend"), &RegistrySourceStatus::Paused);

    let deltas = client.calculate_rebalance_deltas(&current, &1_000_i128);

    assert_eq!(delta_sum(&deltas), 0);
    assert_eq!(delta_for(&deltas, symbol_short!("blend")), 0);
    assert_eq!(delta_for(&deltas, symbol_short!("aave")), 0);
}

#[test]
fn rebalance_redistributes_from_unhealthy_to_active() {
    let (env, admin, registry_id, strategy_id) = setup_with_type(VaultType::Balanced);
    let registry = YieldRegistryContractClient::new(&env, &registry_id);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);

    // Initial state: A=50%, B=50%
    client.set_weights(
        &admin,
        &vec![
            &env,
            AllocationWeight {
                source_id: symbol_short!("aave"),
                weight_bps: 5_000,
            },
            AllocationWeight {
                source_id: symbol_short!("blend"),
                weight_bps: 5_000,
            },
        ],
    );

    // Current: A=500, B=500 (Total=1000)
    let current = vec![
        &env,
        CurrentAllocation {
            source_id: symbol_short!("aave"),
            amount: 500,
        },
        CurrentAllocation {
            source_id: symbol_short!("blend"),
            amount: 500,
        },
    ];

    // Mark Blend as Exploit. It should be drained to 0, and all 1000 should go to Aave.
    registry.update_status(&admin, &symbol_short!("blend"), &RegistrySourceStatus::Exploit);

    let deltas = client.calculate_rebalance_deltas(&current, &1_000_i128);

    assert_eq!(delta_sum(&deltas), 0);
    assert_eq!(delta_for(&deltas, symbol_short!("blend")), -500);
    assert_eq!(delta_for(&deltas, symbol_short!("aave")), 500);
}

#[test]
fn rebalance_withdraws_to_vault_when_all_unhealthy() {
    let (env, admin, registry_id, strategy_id) = setup_with_type(VaultType::DeFi500);
    let registry = YieldRegistryContractClient::new(&env, &registry_id);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);

    client.set_weights(
        &admin,
        &vec![
            &env,
            AllocationWeight {
                source_id: symbol_short!("aave"),
                weight_bps: 10_000,
            },
        ],
    );

    let current = vec![
        &env,
        CurrentAllocation {
            source_id: symbol_short!("aave"),
            amount: 1000,
        },
    ];

    // Mark Aave as Exploit.
    registry.update_status(&admin, &symbol_short!("aave"), &RegistrySourceStatus::Exploit);

    let deltas = client.calculate_rebalance_deltas(&current, &1_000_i128);

    // Sum should be -1000 (withdrawn to vault)
    assert_eq!(delta_sum(&deltas), -1000);
    assert_eq!(delta_for(&deltas, symbol_short!("aave")), -1000);
}

// ---------------------------------------------------------------------------
// should_rebalance / get_optimal_allocation (issue #637)
// ---------------------------------------------------------------------------

#[test]
fn should_rebalance_false_when_spread_below_threshold() {
    let (env, admin, registry_id, strategy_id) = setup_with_type(VaultType::Balanced);
    let registry = YieldRegistryContractClient::new(&env, &registry_id);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);

    // Equal APYs → optimal APY == current weighted APY → zero spread.
    registry.update_apy_override(&admin, &symbol_short!("aave"), &500);
    registry.update_apy_override(&admin, &symbol_short!("blend"), &500);
    registry.update_apy_override(&admin, &symbol_short!("comp"), &500);

    let vault = Address::generate(&env);
    assert!(!client.should_rebalance(&vault, &100));
}

#[test]
fn should_rebalance_true_when_spread_above_threshold() {
    let (env, admin, registry_id, strategy_id) = setup_with_type(VaultType::Balanced);
    let registry = YieldRegistryContractClient::new(&env, &registry_id);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);

    // One source far better than the rest → optimal (100% comp) APY >> current.
    registry.update_apy_override(&admin, &symbol_short!("aave"), &100);
    registry.update_apy_override(&admin, &symbol_short!("blend"), &100);
    registry.update_apy_override(&admin, &symbol_short!("comp"), &5_000);

    let vault = Address::generate(&env);
    assert!(client.should_rebalance(&vault, &500));
}

#[test]
fn get_optimal_allocation_picks_highest_apy_and_sums_to_full() {
    let (env, admin, registry_id, strategy_id) = setup_with_type(VaultType::Balanced);
    let registry = YieldRegistryContractClient::new(&env, &registry_id);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);

    registry.update_apy_override(&admin, &symbol_short!("aave"), &300);
    registry.update_apy_override(&admin, &symbol_short!("blend"), &900);
    registry.update_apy_override(&admin, &symbol_short!("comp"), &500);

    let vault = Address::generate(&env);
    let optimal = client.get_optimal_allocation(&vault);
    assert_eq!(weight_sum(&optimal), 10_000);
    assert_eq!(weight_for(&optimal, symbol_short!("blend")), 10_000);
}

#[test]
fn should_rebalance_false_for_single_protocol_already_optimal() {
    let env = Env::default();
    env.mock_all_auths();
    let admin = Address::generate(&env);
    let registry_id = env.register_contract(None, YieldRegistryContract);
    let strategy_id = env.register_contract(None, AllocationStrategyContract);

    let registry = YieldRegistryContractClient::new(&env, &registry_id);
    registry.initialize(&admin);
    reg(&registry, &env, &admin, symbol_short!("aave"));
    registry.update_apy_override(&admin, &symbol_short!("aave"), &800);

    let client = AllocationStrategyContractClient::new(&env, &strategy_id);
    client.initialize_with_vault_type(&admin, &registry_id, &VaultType::Balanced);

    // Default weights for a single source are 100% to it, which is already the
    // optimal allocation → never warrants a rebalance.
    let vault = Address::generate(&env);
    assert!(!client.should_rebalance(&vault, &1));
}

// ---------------------------------------------------------------------------
// Issue #514 — minimum allocation weight enforcement
// ---------------------------------------------------------------------------

#[test]
fn set_weights_rejects_below_minimum() {
    let (env, admin, _, strategy_id) = setup_with_type(VaultType::Balanced);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);

    // min_weight_bps for Balanced = 500; 499 is below minimum
    let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        client.set_weights(
            &admin,
            &vec![
                &env,
                AllocationWeight {
                    source_id: symbol_short!("aave"),
                    weight_bps: 499,
                },
                AllocationWeight {
                    source_id: symbol_short!("blend"),
                    weight_bps: 9_501,
                },
            ],
        );
    }));

    assert!(result.is_err());
}

#[test]
fn set_weights_rejects_above_maximum() {
    let (env, admin, _, strategy_id) = setup_with_type(VaultType::Balanced);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);

    // max_weight_bps for Balanced = 6_500; 7_000 exceeds it
    let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        client.set_weights(
            &admin,
            &vec![
                &env,
                AllocationWeight {
                    source_id: symbol_short!("aave"),
                    weight_bps: 7_000,
                },
                AllocationWeight {
                    source_id: symbol_short!("blend"),
                    weight_bps: 3_000,
                },
            ],
        );
    }));

    assert!(result.is_err());
}

#[test]
fn set_weights_accepts_valid_weights_within_bounds() {
    let (env, admin, _, strategy_id) = setup_with_type(VaultType::Balanced);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);

    // All weights ≥ 500 (min) and ≤ 6500 (max), sum == 10_000
    let weights = vec![
        &env,
        AllocationWeight {
            source_id: symbol_short!("aave"),
            weight_bps: 5_000,
        },
        AllocationWeight {
            source_id: symbol_short!("blend"),
            weight_bps: 3_000,
        },
        AllocationWeight {
            source_id: symbol_short!("comp"),
            weight_bps: 2_000,
        },
    ];

    client.set_weights(&admin, &weights);
    assert_eq!(client.get_weights(), weights);
}

#[test]
fn admin_can_update_min_weight_threshold() {
    let (env, admin, _, strategy_id) = setup_with_type(VaultType::Balanced);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);

    // Lower the minimum to 300 bps so a 400 bps weight is now accepted
    client.update_strategy_params(&admin, &500, &6_500, &300);
    assert_eq!(client.get_strategy_params().min_weight_bps, 300);

    client.set_weights(
        &admin,
        &vec![
            &env,
            AllocationWeight {
                source_id: symbol_short!("aave"),
                weight_bps: 400,
            },
            AllocationWeight {
                source_id: symbol_short!("blend"),
                weight_bps: 5_000,
            },
            AllocationWeight {
                source_id: symbol_short!("comp"),
                weight_bps: 4_600,
            },
        ],
    );
    assert_eq!(weight_for(&client.get_weights(), symbol_short!("aave")), 400);
}

#[test]
fn admin_can_update_max_weight_threshold() {
    let (env, admin, _, strategy_id) = setup_with_type(VaultType::Balanced);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);

    // Tighten the maximum to 5_500 bps; 6_000 should now be rejected
    client.update_strategy_params(&admin, &500, &5_500, &500);
    assert_eq!(client.get_strategy_params().max_weight_bps, 5_500);

    let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        client.set_weights(
            &admin,
            &vec![
                &env,
                AllocationWeight {
                    source_id: symbol_short!("aave"),
                    weight_bps: 6_000,
                },
                AllocationWeight {
                    source_id: symbol_short!("blend"),
                    weight_bps: 4_000,
                },
            ],
        );
    }));

    assert!(result.is_err());
}
