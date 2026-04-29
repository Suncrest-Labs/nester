//! Nester protocol orchestrator contract.
//!
//! This is the single on-chain entry point for the entire Nester protocol.
//! It holds the canonical addresses of every deployed protocol contract
//! (vaults, vault tokens, treasury, yield registry, allocation strategy)
//! and exposes them as read-only getters so frontends and off-chain services
//! never need to hardcode individual contract IDs.
//!
//! # Roles
//! Uses the shared `nester_access_control` library; the deployer is granted
//! `Role::Admin` during `initialize`.
//!
//! # Upgrade path
//! The `upgrade` entry point calls
//! `env.deployer().update_current_contract_wasm()` so the contract can be
//! migrated to a new WASM without redeploying and losing the contract ID.
//! The version counter is incremented on every successful upgrade, giving
//! integrations a cheap way to detect that the contract changed.

#![no_std]

use soroban_sdk::{
    contract, contractimpl, contracttype, panic_with_error, symbol_short, Address, BytesN, Env,
    Symbol,
};

use nester_access_control::{AccessControl, Role};
use nester_common::{emit_event, ContractError};

const NESTER: Symbol = symbol_short!("NESTER");
const INIT: Symbol = symbol_short!("INIT");
const UPGRADED: Symbol = symbol_short!("UPGRADED");
const CTR_UPD: Symbol = symbol_short!("CTR_UPD");

// ── Event payloads ────────────────────────────────────────────────────────────

#[contracttype]
#[derive(Clone, Debug)]
pub struct InitializedEventData {
    pub vault_usdc: Address,
    pub vault_xlm: Address,
    pub vault_token_usdc: Address,
    pub vault_token_xlm: Address,
    pub treasury: Address,
    pub yield_registry: Address,
    pub allocation_strategy: Address,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct ContractUpdatedEventData {
    pub kind: ContractKind,
    pub old_address: Address,
    pub new_address: Address,
}

// ── Public types ──────────────────────────────────────────────────────────────

/// Identifies one of the protocol contracts tracked by this orchestrator.
///
/// Used as a parameter to [`NesterContract::update_contract`] so callers
/// can update a single reference without touching the others.
#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum ContractKind {
    VaultUsdc,
    VaultXlm,
    VaultTokenUsdc,
    VaultTokenXlm,
    Treasury,
    YieldRegistry,
    AllocationStrategy,
}

// ── Storage ───────────────────────────────────────────────────────────────────

#[contracttype]
#[derive(Clone)]
enum DataKey {
    Contract(ContractKind),
    /// Monotonically increasing value; starts at 1, incremented by `upgrade`.
    Version,
}

// ── Private helpers ───────────────────────────────────────────────────────────

fn require_initialized(env: &Env) {
    if !env.storage().instance().has(&DataKey::Version) {
        panic_with_error!(env, ContractError::NotInitialized);
    }
}

fn get_contract(env: &Env, kind: &ContractKind) -> Address {
    env.storage()
        .instance()
        .get::<DataKey, Address>(&DataKey::Contract(kind.clone()))
        .unwrap_or_else(|| panic_with_error!(env, ContractError::NotInitialized))
}

// ── Contract ──────────────────────────────────────────────────────────────────

#[contract]
pub struct NesterContract;

#[contractimpl]
impl NesterContract {
    // ── Initialisation ────────────────────────────────────────────────────────

    /// Initialise the orchestrator with the addresses of all deployed protocol
    /// contracts. Can only be called once; a second call panics with
    /// `AlreadyInitialized`.
    pub fn initialize(
        env: Env,
        admin: Address,
        vault_usdc: Address,
        vault_xlm: Address,
        vault_token_usdc: Address,
        vault_token_xlm: Address,
        treasury: Address,
        yield_registry: Address,
        allocation_strategy: Address,
    ) {
        // AccessControl::initialize guards AlreadyInitialized and calls
        // admin.require_auth() internally.
        AccessControl::initialize(&env, &admin);

        let s = env.storage().instance();
        s.set(&DataKey::Contract(ContractKind::VaultUsdc), &vault_usdc);
        s.set(&DataKey::Contract(ContractKind::VaultXlm), &vault_xlm);
        s.set(
            &DataKey::Contract(ContractKind::VaultTokenUsdc),
            &vault_token_usdc,
        );
        s.set(
            &DataKey::Contract(ContractKind::VaultTokenXlm),
            &vault_token_xlm,
        );
        s.set(&DataKey::Contract(ContractKind::Treasury), &treasury);
        s.set(
            &DataKey::Contract(ContractKind::YieldRegistry),
            &yield_registry,
        );
        s.set(
            &DataKey::Contract(ContractKind::AllocationStrategy),
            &allocation_strategy,
        );
        s.set(&DataKey::Version, &1u32);

        emit_event(
            &env,
            NESTER,
            INIT,
            env.current_contract_address(),
            InitializedEventData {
                vault_usdc,
                vault_xlm,
                vault_token_usdc,
                vault_token_xlm,
                treasury,
                yield_registry,
                allocation_strategy,
            },
        );
    }

    // ── Upgrade ───────────────────────────────────────────────────────────────

    /// Replace the contract WASM with `new_wasm_hash`. Admin-only.
    ///
    /// The version counter is incremented and the event is published *before*
    /// the WASM swap so that the current ABI encodes both correctly.
    pub fn upgrade(env: Env, admin: Address, new_wasm_hash: BytesN<32>) {
        admin.require_auth();
        require_initialized(&env);
        AccessControl::require_role(&env, &admin, Role::Admin);

        let version: u32 = env
            .storage()
            .instance()
            .get(&DataKey::Version)
            .unwrap_or(1);
        let next_version = version + 1;
        env.storage()
            .instance()
            .set(&DataKey::Version, &next_version);

        env.events()
            .publish((NESTER, UPGRADED, admin), next_version);

        env.deployer().update_current_contract_wasm(new_wasm_hash);
    }

    // ── Protocol contract registry ────────────────────────────────────────────

    /// Update a single protocol contract reference. Admin-only.
    pub fn update_contract(
        env: Env,
        admin: Address,
        kind: ContractKind,
        new_address: Address,
    ) {
        admin.require_auth();
        require_initialized(&env);
        AccessControl::require_role(&env, &admin, Role::Admin);

        let old_address = get_contract(&env, &kind);

        env.storage()
            .instance()
            .set(&DataKey::Contract(kind.clone()), &new_address);

        emit_event(
            &env,
            NESTER,
            CTR_UPD,
            admin,
            ContractUpdatedEventData {
                kind,
                old_address,
                new_address,
            },
        );
    }

    // ── Address getters ───────────────────────────────────────────────────────

    pub fn vault_usdc(env: Env) -> Address {
        require_initialized(&env);
        get_contract(&env, &ContractKind::VaultUsdc)
    }

    pub fn vault_xlm(env: Env) -> Address {
        require_initialized(&env);
        get_contract(&env, &ContractKind::VaultXlm)
    }

    pub fn vault_token_usdc(env: Env) -> Address {
        require_initialized(&env);
        get_contract(&env, &ContractKind::VaultTokenUsdc)
    }

    pub fn vault_token_xlm(env: Env) -> Address {
        require_initialized(&env);
        get_contract(&env, &ContractKind::VaultTokenXlm)
    }

    pub fn treasury(env: Env) -> Address {
        require_initialized(&env);
        get_contract(&env, &ContractKind::Treasury)
    }

    pub fn yield_registry(env: Env) -> Address {
        require_initialized(&env);
        get_contract(&env, &ContractKind::YieldRegistry)
    }

    pub fn allocation_strategy(env: Env) -> Address {
        require_initialized(&env);
        get_contract(&env, &ContractKind::AllocationStrategy)
    }

    pub fn version(env: Env) -> u32 {
        env.storage()
            .instance()
            .get(&DataKey::Version)
            .unwrap_or(0)
    }

    // ── Access control ────────────────────────────────────────────────────────

    pub fn grant_role(env: Env, grantor: Address, grantee: Address, role: Role) {
        require_initialized(&env);
        AccessControl::grant_role(&env, &grantor, &grantee, role);
    }

    pub fn revoke_role(env: Env, revoker: Address, target: Address, role: Role) {
        require_initialized(&env);
        AccessControl::revoke_role(&env, &revoker, &target, role);
    }

    pub fn transfer_admin(env: Env, current_admin: Address, new_admin: Address) {
        require_initialized(&env);
        AccessControl::transfer_admin(&env, &current_admin, &new_admin);
    }

    pub fn accept_admin(env: Env, new_admin: Address) {
        require_initialized(&env);
        AccessControl::accept_admin(&env, &new_admin);
    }

    pub fn has_role(env: Env, account: Address, role: Role) -> bool {
        AccessControl::has_role(&env, &account, role)
    }
}

#[cfg(test)]
mod test;
