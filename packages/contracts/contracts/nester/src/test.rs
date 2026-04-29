#![cfg(test)]

extern crate std;

use soroban_sdk::{testutils::Address as _, Address, Env};

use nester_access_control::Role;

use crate::{ContractKind, NesterContract, NesterContractClient};

// ── Helpers ───────────────────────────────────────────────────────────────────

struct ProtocolAddresses {
    vault_usdc: Address,
    vault_xlm: Address,
    vault_token_usdc: Address,
    vault_token_xlm: Address,
    treasury: Address,
    yield_registry: Address,
    allocation_strategy: Address,
}

fn fake_protocol(env: &Env) -> ProtocolAddresses {
    ProtocolAddresses {
        vault_usdc: Address::generate(env),
        vault_xlm: Address::generate(env),
        vault_token_usdc: Address::generate(env),
        vault_token_xlm: Address::generate(env),
        treasury: Address::generate(env),
        yield_registry: Address::generate(env),
        allocation_strategy: Address::generate(env),
    }
}

fn setup(env: &Env) -> (NesterContractClient<'_>, Address, ProtocolAddresses) {
    env.mock_all_auths();
    let admin = Address::generate(env);
    let p = fake_protocol(env);

    let id = env.register_contract(None, NesterContract);
    let client = NesterContractClient::new(env, &id);

    client.initialize(
        &admin,
        &p.vault_usdc,
        &p.vault_xlm,
        &p.vault_token_usdc,
        &p.vault_token_xlm,
        &p.treasury,
        &p.yield_registry,
        &p.allocation_strategy,
    );

    (client, admin, p)
}

// ── Initialization ────────────────────────────────────────────────────────────

#[test]
fn initialize_stores_all_addresses() {
    let env = Env::default();
    let (client, _, p) = setup(&env);

    assert_eq!(client.vault_usdc(), p.vault_usdc);
    assert_eq!(client.vault_xlm(), p.vault_xlm);
    assert_eq!(client.vault_token_usdc(), p.vault_token_usdc);
    assert_eq!(client.vault_token_xlm(), p.vault_token_xlm);
    assert_eq!(client.treasury(), p.treasury);
    assert_eq!(client.yield_registry(), p.yield_registry);
    assert_eq!(client.allocation_strategy(), p.allocation_strategy);
}

#[test]
fn initialize_sets_version_to_one() {
    let env = Env::default();
    let (client, _, _) = setup(&env);
    assert_eq!(client.version(), 1);
}

#[test]
fn initialize_grants_admin_role_to_deployer() {
    let env = Env::default();
    let (client, admin, _) = setup(&env);
    assert!(client.has_role(&admin, &Role::Admin));
}

#[test]
#[should_panic]
fn initialize_twice_panics() {
    let env = Env::default();
    let (client, admin, p) = setup(&env);
    // Second call must panic with AlreadyInitialized.
    client.initialize(
        &admin,
        &p.vault_usdc,
        &p.vault_xlm,
        &p.vault_token_usdc,
        &p.vault_token_xlm,
        &p.treasury,
        &p.yield_registry,
        &p.allocation_strategy,
    );
}

// ── version before initialization ─────────────────────────────────────────────

#[test]
fn version_returns_zero_before_initialize() {
    let env = Env::default();
    env.mock_all_auths();
    let id = env.register_contract(None, NesterContract);
    let client = NesterContractClient::new(&env, &id);
    assert_eq!(client.version(), 0);
}

// ── Getters panic before initialization ───────────────────────────────────────

#[test]
#[should_panic]
fn vault_usdc_panics_before_initialize() {
    let env = Env::default();
    env.mock_all_auths();
    let id = env.register_contract(None, NesterContract);
    let client = NesterContractClient::new(&env, &id);
    client.vault_usdc();
}

#[test]
#[should_panic]
fn treasury_panics_before_initialize() {
    let env = Env::default();
    env.mock_all_auths();
    let id = env.register_contract(None, NesterContract);
    let client = NesterContractClient::new(&env, &id);
    client.treasury();
}

// ── update_contract ───────────────────────────────────────────────────────────

#[test]
fn admin_can_update_contract_reference() {
    let env = Env::default();
    let (client, admin, _) = setup(&env);
    let new_vault = Address::generate(&env);

    client.update_contract(&admin, &ContractKind::VaultUsdc, &new_vault);

    assert_eq!(client.vault_usdc(), new_vault);
}

#[test]
fn update_contract_does_not_affect_other_references() {
    let env = Env::default();
    let (client, admin, p) = setup(&env);
    let new_vault_xlm = Address::generate(&env);

    client.update_contract(&admin, &ContractKind::VaultXlm, &new_vault_xlm);

    // Only VaultXlm changed; everything else is unchanged.
    assert_eq!(client.vault_xlm(), new_vault_xlm);
    assert_eq!(client.vault_usdc(), p.vault_usdc);
    assert_eq!(client.treasury(), p.treasury);
    assert_eq!(client.yield_registry(), p.yield_registry);
    assert_eq!(client.allocation_strategy(), p.allocation_strategy);
}

#[test]
fn update_contract_covers_all_kinds() {
    let env = Env::default();
    let (client, admin, _) = setup(&env);

    let kinds = [
        ContractKind::VaultUsdc,
        ContractKind::VaultXlm,
        ContractKind::VaultTokenUsdc,
        ContractKind::VaultTokenXlm,
        ContractKind::Treasury,
        ContractKind::YieldRegistry,
        ContractKind::AllocationStrategy,
    ];

    for kind in kinds {
        let new_addr = Address::generate(&env);
        client.update_contract(&admin, &kind, &new_addr);
        // Confirm each getter now returns the updated address.
        let actual = match kind {
            ContractKind::VaultUsdc => client.vault_usdc(),
            ContractKind::VaultXlm => client.vault_xlm(),
            ContractKind::VaultTokenUsdc => client.vault_token_usdc(),
            ContractKind::VaultTokenXlm => client.vault_token_xlm(),
            ContractKind::Treasury => client.treasury(),
            ContractKind::YieldRegistry => client.yield_registry(),
            ContractKind::AllocationStrategy => client.allocation_strategy(),
        };
        assert_eq!(actual, new_addr);
    }
}

#[test]
#[should_panic]
fn non_admin_cannot_update_contract_reference() {
    let env = Env::default();
    let (client, _, _) = setup(&env);
    let outsider = Address::generate(&env);

    client.update_contract(&outsider, &ContractKind::Treasury, &Address::generate(&env));
}

// ── Access control ────────────────────────────────────────────────────────────

#[test]
fn admin_can_grant_and_revoke_operator_role() {
    let env = Env::default();
    let (client, admin, _) = setup(&env);
    let operator = Address::generate(&env);

    client.grant_role(&admin, &operator, &Role::Operator);
    assert!(client.has_role(&operator, &Role::Operator));

    client.revoke_role(&admin, &operator, &Role::Operator);
    assert!(!client.has_role(&operator, &Role::Operator));
}

#[test]
#[should_panic]
fn non_admin_cannot_grant_roles() {
    let env = Env::default();
    let (client, _, _) = setup(&env);
    let outsider = Address::generate(&env);

    client.grant_role(&outsider, &Address::generate(&env), &Role::Operator);
}

#[test]
fn has_role_returns_false_for_unknown_account() {
    let env = Env::default();
    let (client, _, _) = setup(&env);
    let stranger = Address::generate(&env);
    assert!(!client.has_role(&stranger, &Role::Admin));
    assert!(!client.has_role(&stranger, &Role::Operator));
}

#[test]
fn two_step_admin_transfer() {
    let env = Env::default();
    let (client, admin, _) = setup(&env);
    let new_admin = Address::generate(&env);

    client.transfer_admin(&admin, &new_admin);
    client.accept_admin(&new_admin);

    assert!(client.has_role(&new_admin, &Role::Admin));
    assert!(!client.has_role(&admin, &Role::Admin));
}
