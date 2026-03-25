//! Nester Yield Source Registry
//!
//! On-chain registry that tracks approved yield sources (Aave, Blend,
//! Compound, etc.) and their current lifecycle status.
//!
//! # Roles
//! Only the Admin may register, update, or remove yield sources.
//! Role management is delegated to [`nester_access_control`].
//!
//! # Status transitions
//! ```text
//! Active ──► Paused ──► Active
//!   │                     │
//!   └──► Deprecated ◄─────┘
//! ```
//! A `Deprecated` source **cannot** be re-activated or paused — it is final.
//!
//! # `get_active_sources`
//! The contract maintains a `SourceList` (Vec<Symbol>) so it can return all
//! active sources without an off-chain index.

#![no_std]

use soroban_sdk::{
    contract, contractimpl, contracttype, panic_with_error, symbol_short, Address, Env, Symbol, Vec,
};

use nester_access_control::{AccessControl, Role};
use nester_common::ContractError;

// ---------------------------------------------------------------------------
// Public types
// ---------------------------------------------------------------------------

/// Lifecycle status of a yield source.
#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum SourceStatus {
    Active,
    Paused,
    Deprecated,
}

/// The category of yield-generating protocol.
#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum ProtocolType {
    Lending,
    Staking,
    LP,
}

/// Full record stored for each registered yield source.
#[contracttype]
#[derive(Clone, Debug)]
pub struct YieldSource {
    pub id: Symbol,
    pub contract_address: Address,
    pub protocol_type: ProtocolType,
    pub status: SourceStatus,
    /// Ledger timestamp at registration time.
    pub added_at: u64,
}

// ---------------------------------------------------------------------------
// Storage keys
// ---------------------------------------------------------------------------

#[contracttype]
#[derive(Clone)]
enum DataKey {
    /// Symbol → YieldSource
    Source(Symbol),
    /// Ordered list of all registered source IDs (used by get_active_sources).
    SourceList,
}

// ---------------------------------------------------------------------------
// Contract
// ---------------------------------------------------------------------------

#[contract]
pub struct YieldRegistryContract;

#[contractimpl]
impl YieldRegistryContract {
    // -----------------------------------------------------------------------
    // Initialisation
    // -----------------------------------------------------------------------

    /// Initialise the registry, granting `admin` the Admin role.
    pub fn initialize(env: Env, admin: Address) {
        AccessControl::initialize(&env, &admin);
        env.storage()
            .instance()
            .set(&DataKey::SourceList, &Vec::<Symbol>::new(&env));
    }

    // -----------------------------------------------------------------------
    // Source management — Admin only
    // -----------------------------------------------------------------------

    /// Register a new yield source.
    ///
    /// Panics with [`ContractError::InvalidOperation`] if `id` is already
    /// registered.
    pub fn register_source(
        env: Env,
        caller: Address,
        id: Symbol,
        contract_address: Address,
        protocol_type: ProtocolType,
    ) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);

        if env.storage().instance().has(&DataKey::Source(id.clone())) {
            panic_with_error!(&env, ContractError::InvalidOperation);
        }

        let source = YieldSource {
            id: id.clone(),
            contract_address,
            protocol_type,
            status: SourceStatus::Active,
            added_at: env.ledger().timestamp(),
        };

        env.storage()
            .instance()
            .set(&DataKey::Source(id.clone()), &source);

        let mut list = source_list(&env);
        list.push_back(id.clone());
        env.storage().instance().set(&DataKey::SourceList, &list);

        env.events()
            .publish((symbol_short!("src_add"), caller, id), ());
    }

    /// Update the lifecycle status of a registered source.
    ///
    /// Panics with [`ContractError::StrategyNotFound`] if `id` is unknown.
    /// Panics with [`ContractError::InvalidOperation`] if the transition is
    /// illegal (e.g. re-activating a `Deprecated` source).
    pub fn update_status(env: Env, caller: Address, id: Symbol, new_status: SourceStatus) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);

        let mut source = get_source_or_panic(&env, &id);

        // Deprecated is a terminal state.
        if matches!(source.status, SourceStatus::Deprecated) {
            panic_with_error!(&env, ContractError::InvalidOperation);
        }

        source.status = new_status;
        env.storage()
            .instance()
            .set(&DataKey::Source(id.clone()), &source);

        env.events()
            .publish((symbol_short!("src_upd"), caller, id), ());
    }

    /// Remove a yield source from the registry entirely.
    ///
    /// Panics with [`ContractError::StrategyNotFound`] if `id` is unknown.
    pub fn remove_source(env: Env, caller: Address, id: Symbol) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);

        if !env.storage().instance().has(&DataKey::Source(id.clone())) {
            panic_with_error!(&env, ContractError::StrategyNotFound);
        }

        env.storage().instance().remove(&DataKey::Source(id.clone()));

        let mut list = source_list(&env);
        let mut new_list = Vec::<Symbol>::new(&env);
        for sym in list.iter() {
            if sym != id {
                new_list.push_back(sym);
            }
        }
        list = new_list;
        env.storage().instance().set(&DataKey::SourceList, &list);

        env.events()
            .publish((symbol_short!("src_rem"), caller, id), ());
    }

    // -----------------------------------------------------------------------
    // Queries
    // -----------------------------------------------------------------------

    /// Return the full [`YieldSource`] record for `id`.
    ///
    /// Panics if the source does not exist.
    pub fn get_source(env: Env, id: Symbol) -> YieldSource {
        get_source_or_panic(&env, &id)
    }

    /// Return all sources whose status is [`SourceStatus::Active`].
    pub fn get_active_sources(env: Env) -> Vec<YieldSource> {
        let list = source_list(&env);
        let mut out = Vec::<YieldSource>::new(&env);
        for sym in list.iter() {
            if let Some(s) = env
                .storage()
                .instance()
                .get::<DataKey, YieldSource>(&DataKey::Source(sym))
            {
                if matches!(s.status, SourceStatus::Active) {
                    out.push_back(s);
                }
            }
        }
        out
    }

    /// Return `true` if a source with `id` is registered (any status).
    pub fn has_source(env: Env, id: Symbol) -> bool {
        env.storage().instance().has(&DataKey::Source(id))
    }

    /// Return the current [`SourceStatus`] for `id`.
    ///
    /// Panics if the source does not exist.
    pub fn get_source_status(env: Env, id: Symbol) -> SourceStatus {
        get_source_or_panic(&env, &id).status
    }

    // -----------------------------------------------------------------------
    // Role management — delegates to nester_access_control
    // -----------------------------------------------------------------------

    /// Grant `role` to `grantee`. Caller must be an Admin.
    pub fn grant_role(env: Env, grantor: Address, grantee: Address, role: Role) {
        AccessControl::grant_role(&env, &grantor, &grantee, role);
    }

    /// Revoke `role` from `target`. Caller must be an Admin.
    pub fn revoke_role(env: Env, revoker: Address, target: Address, role: Role) {
        AccessControl::revoke_role(&env, &revoker, &target, role);
    }

    /// Propose an admin transfer (step 1). Caller must be an Admin.
    pub fn transfer_admin(env: Env, current_admin: Address, new_admin: Address) {
        AccessControl::transfer_admin(&env, &current_admin, &new_admin);
    }

    /// Accept a pending admin transfer (step 2). Caller must be the proposed
    /// new admin.
    pub fn accept_admin(env: Env, new_admin: Address) {
        AccessControl::accept_admin(&env, &new_admin);
    }
}

// ---------------------------------------------------------------------------
// Private helpers
// ---------------------------------------------------------------------------

fn source_list(env: &Env) -> Vec<Symbol> {
    env.storage()
        .instance()
        .get(&DataKey::SourceList)
        .unwrap_or_else(|| Vec::new(env))
}

fn get_source_or_panic(env: &Env, id: &Symbol) -> YieldSource {
    env.storage()
        .instance()
        .get::<DataKey, YieldSource>(&DataKey::Source(id.clone()))
        .unwrap_or_else(|| panic_with_error!(env, ContractError::StrategyNotFound))
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod test;
