#![cfg(test)]

extern crate std;

use super::*;
use soroban_sdk::{
    symbol_short,
    testutils::{Address as _, Events},
    vec, Address, Env,
};
use yield_registry::{
    ProtocolType as RegistryProtocolType, SourceStatus as RegistrySourceStatus,
    YieldRegistryContract, YieldRegistryContractClient,
};

// Helper: register a source with the registry using the new API.
fn reg(
    registry: &YieldRegistryContractClient,
    env: &Env,
    admin: &Address,
    id: soroban_sdk::Symbol,
) {
    registry.register_source(admin, &id, &Address::generate(env), &RegistryProtocolType::Lending);
}

#[test]
fn set_weights_and_calculate_allocation() {
    let env = Env::default();
    env.mock_all_auths();

    let admin = Address::generate(&env);
    let registry_id = env.register_contract(None, YieldRegistryContract);
    let strategy_id = env.register_contract(None, AllocationStrategyContract);

    let registry = YieldRegistryContractClient::new(&env, &registry_id);
    registry.initialize(&admin);
    reg(&registry, &env, &admin, symbol_short!("aave"));
    reg(&registry, &env, &admin, symbol_short!("blend"));
    reg(&registry, &env, &admin, symbol_short!("compound"));

    let client = AllocationStrategyContractClient::new(&env, &strategy_id);
    client.initialize(&admin, &registry_id);

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
            source_id: symbol_short!("compound"),
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
            (symbol_short!("compound"), 3_000_i128),
        ]
    );
    assert_eq!(client.get_source_allocation(&symbol_short!("blend")), 3_000);
    assert!(!env.events().all().is_empty());
}

#[test]
fn rejects_invalid_weight_sum() {
    let env = Env::default();
    env.mock_all_auths();

    let admin = Address::generate(&env);
    let registry_id = env.register_contract(None, YieldRegistryContract);
    let strategy_id = env.register_contract(None, AllocationStrategyContract);

    let registry = YieldRegistryContractClient::new(&env, &registry_id);
    registry.initialize(&admin);
    reg(&registry, &env, &admin, symbol_short!("aave"));
    reg(&registry, &env, &admin, symbol_short!("blend"));

    let client = AllocationStrategyContractClient::new(&env, &strategy_id);
    client.initialize(&admin, &registry_id);

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
    let env = Env::default();
    env.mock_all_auths();

    let admin = Address::generate(&env);
    let registry_id = env.register_contract(None, YieldRegistryContract);
    let strategy_id = env.register_contract(None, AllocationStrategyContract);

    let registry = YieldRegistryContractClient::new(&env, &registry_id);
    registry.initialize(&admin);
    reg(&registry, &env, &admin, symbol_short!("aave"));
    reg(&registry, &env, &admin, symbol_short!("blend"));
    reg(&registry, &env, &admin, symbol_short!("compound"));

    let client = AllocationStrategyContractClient::new(&env, &strategy_id);
    client.initialize(&admin, &registry_id);
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
                source_id: symbol_short!("compound"),
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
            (symbol_short!("compound"), 34_i128),
        ]
    );
}

#[test]
fn only_admin_can_update_weights() {
    let env = Env::default();
    env.mock_all_auths();

    let admin = Address::generate(&env);
    let outsider = Address::generate(&env);
    let registry_id = env.register_contract(None, YieldRegistryContract);
    let strategy_id = env.register_contract(None, AllocationStrategyContract);

    let registry = YieldRegistryContractClient::new(&env, &registry_id);
    registry.initialize(&admin);
    reg(&registry, &env, &admin, symbol_short!("aave"));

    let client = AllocationStrategyContractClient::new(&env, &strategy_id);
    client.initialize(&admin, &registry_id);

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
fn rejects_inactive_sources() {
    let env = Env::default();
    env.mock_all_auths();

    let admin = Address::generate(&env);
    let registry_id = env.register_contract(None, YieldRegistryContract);
    let strategy_id = env.register_contract(None, AllocationStrategyContract);

    let registry = YieldRegistryContractClient::new(&env, &registry_id);
    registry.initialize(&admin);
    reg(&registry, &env, &admin, symbol_short!("aave"));
    // Pause the source
    registry.update_status(&admin, &symbol_short!("aave"), &RegistrySourceStatus::Paused);

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
