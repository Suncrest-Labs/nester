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
fn only_admin_can_update_weights() {
    let (env, _admin, _, strategy_id) = setup_with_type(VaultType::Balanced);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);
    let outsider = Address::generate(&env);

    let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        client.set_weights(
            &outsider,
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
#[should_panic]
fn operator_cannot_grant_roles() {
    let (env, admin, _, strategy_id) = setup_with_type(VaultType::Balanced);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);
    let operator = Address::generate(&env);
    let outsider = Address::generate(&env);
    client.grant_role(&admin, &operator, &Role::Operator);
    let _ = env;

    client.grant_role(&operator, &outsider, &Role::Operator);
}

#[test]
#[should_panic]
fn operator_cannot_revoke_roles() {
    let (env, admin, _, strategy_id) = setup_with_type(VaultType::Balanced);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);
    let operator = Address::generate(&env);
    client.grant_role(&admin, &operator, &Role::Operator);

    client.revoke_role(&operator, &admin, &Role::Admin);
}

#[test]
#[should_panic]
fn operator_cannot_transfer_admin() {
    let (env, admin, _, strategy_id) = setup_with_type(VaultType::Balanced);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);
    let operator = Address::generate(&env);
    let outsider = Address::generate(&env);
    client.grant_role(&admin, &operator, &Role::Operator);

    let _ = env;
    client.transfer_admin(&operator, &outsider);
}

#[test]
fn compute_allocation_preserves_weight_and_amount_invariants() {
    let (env, _, _, strategy_id) = setup_with_type(VaultType::Growth);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);

    for total in [1_i128, 7_i128, 101_i128, 10_001_i128] {
        let weights = client.compute_allocation(
            // &total,
            &vec![
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
            ],
        );

        assert_eq!(weight_sum(&weights), 10_000);
        let allocated_total = client.get_source_allocation(&symbol_short!("aave"))
            + client.get_source_allocation(&symbol_short!("blend"))
            + client.get_source_allocation(&symbol_short!("comp"));
        assert_eq!(allocated_total, total);
    }
}

#[test]
fn conservative_strategy_caps_individual_protocol_weight() {
    let (env, _, _, strategy_id) = setup_with_type(VaultType::Conservative);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);

    let weights = client.compute_allocation(
        // &10_000_i128,
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
    let (env, _, _, strategy_id) = setup_with_type(VaultType::Growth);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);

    let weights = client.compute_allocation(
        // &10_000_i128,
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
    let (env, _, _, strategy_id) = setup_with_type(VaultType::DeFi500);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);

    let weights = client.compute_allocation(
        // &10_000_i128,
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
    let (env, _, _, strategy_id) = setup_with_type(VaultType::Growth);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);

    let weights = client.compute_allocation(
        // &10_000_i128,
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
        // &10_000_i128,
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
    client.update_strategy_params(&admin, &250, &6_500);

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
    client.update_strategy_params(&admin, &250, &6_500);

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
    client.update_strategy_params(&admin, &250, &6_500);

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
    client.update_strategy_params(&admin, &125, &4_500);

    assert_eq!(
        client.get_strategy_params(),
        StrategyParams {
            rebalance_threshold_bps: 125,
            max_weight_bps: 4_500,
        }
    );
}

#[test]
#[should_panic]
fn non_admin_update_attempts_are_rejected() {
    let (env, _, _, strategy_id) = setup_with_type(VaultType::Balanced);
    let client = AllocationStrategyContractClient::new(&env, &strategy_id);
    let outsider = Address::generate(&env);
    client.update_strategy_params(&outsider, &125, &4_500);
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

// AllocationStrategy.set_allocations requires operator role — unauthorized callers are rejected.
#[test]
#[should_panic]
fn test_set_allocations_unauthorized_is_rejected() {
    let env = Env::default();
    env.mock_all_auths_allowing_non_root_auth();

    let registry_id = env.register_contract(None, YieldRegistryContract);
    let registry = YieldRegistryContractClient::new(&env, &registry_id);
    let admin = Address::generate(&env);
    registry.initialize(&admin);

    let contract_id = env.register_contract(None, AllocationStrategyContract);
    let client = AllocationStrategyContractClient::new(&env, &contract_id);
    client.initialize(&admin, &registry_id);

    let attacker = Address::generate(&env);
    let apys = soroban_sdk::Vec::new(&env);

    // attacker is not an operator — should panic
    client.set_allocations(&attacker, &1000_i128, &apys);
}
