//! Nester Yield Source Registry
//!
//! On-chain registry that tracks approved yield sources (Aave, Blend,
//! Compound, etc.), lifecycle status, and performance metadata used by
//! allocation logic.
//!
//! # Roles
//! * Admin: register/update/remove sources, risk + limit updates.
//! * Operator: day-to-day performance refreshes (APY/TVL) and migration ops.
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
//! # Performance history
//! APY updates are stored as a fixed-size rolling history (`MAX_APY_HISTORY`)
//! so trend direction can be determined without unbounded storage growth.

#![no_std]

use soroban_sdk::{
    contract, contractimpl, contracttype, panic_with_error, symbol_short, Address, Env, Symbol, Vec,
};

use nester_access_control::{AccessControl, Role};
use nester_common::{emit_event_with_sym, ContractError, ProtocolType, SourceStatus};

const REGISTRY: Symbol = symbol_short!("REGISTRY");
const SOURCE_ADDED: Symbol = symbol_short!("SRC_ADD");
const SOURCE_UPDATED: Symbol = symbol_short!("SRC_UPD");
const SOURCE_REMOVED: Symbol = symbol_short!("SRC_REM");
const SOURCE_PERF: Symbol = symbol_short!("SRC_PERF");
const SOURCE_MIGRATION: Symbol = symbol_short!("SRC_MIG");

const DEFAULT_RISK_RATING: u32 = 5;
const MAX_RISK_RATING: u32 = 10;
pub const MAX_APY_HISTORY: u32 = 16;

#[contracttype]
#[derive(Clone, Debug)]
pub struct SourceAddedEventData {
    pub contract_address: Address,
    pub protocol_type: ProtocolType,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct SourceUpdatedEventData {
    pub old_status: SourceStatus,
    pub new_status: SourceStatus,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct SourcePerformanceUpdatedEventData {
    pub current_apy_bps: u32,
    pub tvl: i128,
    pub risk_rating: u32,
    pub min_deposit: i128,
    pub max_deposit: i128,
    pub last_updated: u64,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct SourceMigrationEventData {
    pub migration_required: bool,
    pub migration_completed: bool,
    pub migration_completed_at: u64,
}

// ---------------------------------------------------------------------------
// Public types
// ---------------------------------------------------------------------------

/// APY value captured at a specific ledger timestamp.
#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ApySnapshot {
    pub apy_bps: u32,
    pub timestamp: u64,
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

    /// Most recent annualized yield in basis points.
    pub current_apy_bps: u32,
    /// Rolling history of APY updates (oldest trimmed past MAX_APY_HISTORY).
    pub apy_history: Vec<ApySnapshot>,
    /// Total value locked in this source.
    pub tvl: i128,
    /// Relative source risk score, 1 (lowest) to 10 (highest).
    pub risk_rating: u32,
    /// Minimum allocatable amount accepted by the source.
    pub min_deposit: i128,
    /// Maximum allocatable amount accepted by the source. 0 means uncapped.
    pub max_deposit: i128,
    /// Last timestamp when any performance-related field was updated.
    pub last_updated: u64,

    /// Whether capital should be moved away from this source.
    pub migration_required: bool,
    /// Whether migration has been completed.
    pub migration_completed: bool,
    /// Timestamp for migration completion, or 0 if incomplete.
    pub migration_completed_at: u64,
}

/// Query-friendly projection for source performance data.
#[contracttype]
#[derive(Clone, Debug)]
pub struct SourcePerformance {
    pub current_apy_bps: u32,
    pub apy_history: Vec<ApySnapshot>,
    pub tvl: i128,
    pub risk_rating: u32,
    pub min_deposit: i128,
    pub max_deposit: i128,
    pub last_updated: u64,
    pub migration_required: bool,
    pub migration_completed: bool,
    pub migration_completed_at: u64,
}

// ---------------------------------------------------------------------------
// Storage keys
// ---------------------------------------------------------------------------

#[contracttype]
#[derive(Clone)]
enum DataKey {
    /// Symbol → YieldSource
    Source(Symbol),
    /// Ordered list of all registered source IDs.
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

    /// Register a new yield source with default performance metadata.
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

        let now = env.ledger().timestamp();
        let source = YieldSource {
            id: id.clone(),
            contract_address: contract_address.clone(),
            protocol_type: protocol_type.clone(),
            status: SourceStatus::Active,
            added_at: now,
            current_apy_bps: 0,
            apy_history: Vec::new(&env),
            tvl: 0,
            risk_rating: DEFAULT_RISK_RATING,
            min_deposit: 0,
            max_deposit: 0,
            last_updated: now,
            migration_required: false,
            migration_completed: false,
            migration_completed_at: 0,
        };

        save_source(&env, &id, &source);

        let mut list = source_list(&env);
        list.push_back(id.clone());
        env.storage().instance().set(&DataKey::SourceList, &list);

        emit_event_with_sym(
            &env,
            REGISTRY,
            SOURCE_ADDED,
            id.clone(),
            SourceAddedEventData {
                contract_address,
                protocol_type,
            },
        );
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

        let old_status = source.status.clone();
        source.status = new_status.clone();

        // Deprecation implies migration is required.
        if matches!(new_status, SourceStatus::Deprecated) {
            source.migration_required = true;
            source.migration_completed = false;
            source.migration_completed_at = 0;
            touch_source(&env, &mut source);
        }

        save_source(&env, &id, &source);

        emit_event_with_sym(
            &env,
            REGISTRY,
            SOURCE_UPDATED,
            id.clone(),
            SourceUpdatedEventData {
                old_status,
                new_status,
            },
        );
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

        env.storage()
            .instance()
            .remove(&DataKey::Source(id.clone()));

        let mut list = source_list(&env);
        let mut new_list = Vec::<Symbol>::new(&env);
        for sym in list.iter() {
            if sym != id {
                new_list.push_back(sym);
            }
        }
        list = new_list;
        env.storage().instance().set(&DataKey::SourceList, &list);

        emit_event_with_sym(&env, REGISTRY, SOURCE_REMOVED, id.clone(), ());
    }

    // -----------------------------------------------------------------------
    // Performance updates
    // -----------------------------------------------------------------------

    /// Update APY in basis points and append a snapshot to the rolling history.
    /// Callable by Admin or Operator.
    pub fn update_apy(env: Env, caller: Address, id: Symbol, new_apy_bps: u32) {
        caller.require_auth();
        require_admin_or_operator(&env, &caller);

        let mut source = get_source_or_panic(&env, &id);
        source.current_apy_bps = new_apy_bps;
        append_apy_snapshot(&env, &mut source, new_apy_bps);
        touch_source(&env, &mut source);
        save_source(&env, &id, &source);

        emit_event_with_sym(
            &env,
            REGISTRY,
            SOURCE_PERF,
            id,
            performance_event_data(&source),
        );
    }

    /// Update total value locked for a source.
    /// Callable by Admin or Operator.
    pub fn update_tvl(env: Env, caller: Address, id: Symbol, new_tvl: i128) {
        caller.require_auth();
        require_admin_or_operator(&env, &caller);

        if new_tvl < 0 {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }

        let mut source = get_source_or_panic(&env, &id);
        source.tvl = new_tvl;
        touch_source(&env, &mut source);
        save_source(&env, &id, &source);

        emit_event_with_sym(
            &env,
            REGISTRY,
            SOURCE_PERF,
            id,
            performance_event_data(&source),
        );
    }

    /// Update source risk rating (1..=10). Admin only.
    pub fn update_risk_rating(env: Env, caller: Address, id: Symbol, rating: u32) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);

        if rating == 0 || rating > MAX_RISK_RATING {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }

        let mut source = get_source_or_panic(&env, &id);
        source.risk_rating = rating;
        touch_source(&env, &mut source);
        save_source(&env, &id, &source);

        emit_event_with_sym(
            &env,
            REGISTRY,
            SOURCE_PERF,
            id,
            performance_event_data(&source),
        );
    }

    /// Set minimum and maximum deposit limits for a source. Admin only.
    ///
    /// `max_deposit == 0` means uncapped.
    pub fn update_deposit_limits(
        env: Env,
        caller: Address,
        id: Symbol,
        min_deposit: i128,
        max_deposit: i128,
    ) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);

        if min_deposit < 0 || max_deposit < 0 {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }
        if max_deposit != 0 && min_deposit > max_deposit {
            panic_with_error!(&env, ContractError::InvalidOperation);
        }

        let mut source = get_source_or_panic(&env, &id);
        source.min_deposit = min_deposit;
        source.max_deposit = max_deposit;
        touch_source(&env, &mut source);
        save_source(&env, &id, &source);

        emit_event_with_sym(
            &env,
            REGISTRY,
            SOURCE_PERF,
            id,
            performance_event_data(&source),
        );
    }

    /// Signal that allocations should be migrated away from a source. Admin only.
    pub fn signal_migration_required(env: Env, caller: Address, id: Symbol) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);

        let mut source = get_source_or_panic(&env, &id);
        source.migration_required = true;
        source.migration_completed = false;
        source.migration_completed_at = 0;
        touch_source(&env, &mut source);
        save_source(&env, &id, &source);

        emit_event_with_sym(
            &env,
            REGISTRY,
            SOURCE_MIGRATION,
            id,
            SourceMigrationEventData {
                migration_required: source.migration_required,
                migration_completed: source.migration_completed,
                migration_completed_at: source.migration_completed_at,
            },
        );
    }

    /// Mark migration complete after funds have been moved out.
    /// Callable by Admin or Operator.
    pub fn mark_migration_complete(env: Env, caller: Address, id: Symbol) {
        caller.require_auth();
        require_admin_or_operator(&env, &caller);

        let mut source = get_source_or_panic(&env, &id);
        if !source.migration_required && !matches!(source.status, SourceStatus::Deprecated) {
            panic_with_error!(&env, ContractError::InvalidOperation);
        }

        source.migration_required = false;
        source.migration_completed = true;
        source.migration_completed_at = env.ledger().timestamp();
        touch_source(&env, &mut source);
        save_source(&env, &id, &source);

        emit_event_with_sym(
            &env,
            REGISTRY,
            SOURCE_MIGRATION,
            id,
            SourceMigrationEventData {
                migration_required: source.migration_required,
                migration_completed: source.migration_completed,
                migration_completed_at: source.migration_completed_at,
            },
        );
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

    /// Return current performance metadata for `id`.
    pub fn get_source_performance(env: Env, id: Symbol) -> SourcePerformance {
        let source = get_source_or_panic(&env, &id);
        SourcePerformance {
            current_apy_bps: source.current_apy_bps,
            apy_history: source.apy_history,
            tvl: source.tvl,
            risk_rating: source.risk_rating,
            min_deposit: source.min_deposit,
            max_deposit: source.max_deposit,
            last_updated: source.last_updated,
            migration_required: source.migration_required,
            migration_completed: source.migration_completed,
            migration_completed_at: source.migration_completed_at,
        }
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

    /// Return all sources with a matching protocol type.
    pub fn get_sources_by_type(env: Env, protocol_type: ProtocolType) -> Vec<YieldSource> {
        let list = source_list(&env);
        let mut out = Vec::<YieldSource>::new(&env);
        for sym in list.iter() {
            if let Some(s) = env
                .storage()
                .instance()
                .get::<DataKey, YieldSource>(&DataKey::Source(sym))
            {
                if s.protocol_type == protocol_type {
                    out.push_back(s);
                }
            }
        }
        out
    }

    /// Return active sources with APY >= `min_apy_bps`.
    pub fn get_sources_above_apy(env: Env, min_apy_bps: u32) -> Vec<YieldSource> {
        let list = source_list(&env);
        let mut out = Vec::<YieldSource>::new(&env);
        for sym in list.iter() {
            if let Some(s) = env
                .storage()
                .instance()
                .get::<DataKey, YieldSource>(&DataKey::Source(sym))
            {
                if matches!(s.status, SourceStatus::Active) && s.current_apy_bps >= min_apy_bps {
                    out.push_back(s);
                }
            }
        }
        out
    }

    /// Return sources currently flagged for migration.
    pub fn get_sources_requiring_migration(env: Env) -> Vec<YieldSource> {
        let list = source_list(&env);
        let mut out = Vec::<YieldSource>::new(&env);
        for sym in list.iter() {
            if let Some(s) = env
                .storage()
                .instance()
                .get::<DataKey, YieldSource>(&DataKey::Source(sym))
            {
                if s.migration_required {
                    out.push_back(s);
                }
            }
        }
        out
    }

    /// Return total count of currently registered sources.
    pub fn source_count(env: Env) -> u32 {
        source_list(&env).len()
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

fn save_source(env: &Env, id: &Symbol, source: &YieldSource) {
    env.storage()
        .instance()
        .set(&DataKey::Source(id.clone()), source);
}

fn touch_source(env: &Env, source: &mut YieldSource) {
    source.last_updated = env.ledger().timestamp();
}

fn append_apy_snapshot(env: &Env, source: &mut YieldSource, apy_bps: u32) {
    let mut history = source.apy_history.clone();

    if history.len() >= MAX_APY_HISTORY {
        // Keep the newest N-1 entries, then append the latest APY at the end.
        let mut trimmed = Vec::<ApySnapshot>::new(env);
        for i in 1..history.len() {
            trimmed.push_back(history.get(i).unwrap());
        }
        history = trimmed;
    }

    history.push_back(ApySnapshot {
        apy_bps,
        timestamp: env.ledger().timestamp(),
    });

    source.apy_history = history;
}

fn performance_event_data(source: &YieldSource) -> SourcePerformanceUpdatedEventData {
    SourcePerformanceUpdatedEventData {
        current_apy_bps: source.current_apy_bps,
        tvl: source.tvl,
        risk_rating: source.risk_rating,
        min_deposit: source.min_deposit,
        max_deposit: source.max_deposit,
        last_updated: source.last_updated,
    }
}

fn require_admin_or_operator(env: &Env, caller: &Address) {
    if !AccessControl::has_role(env, caller, Role::Admin)
        && !AccessControl::has_role(env, caller, Role::Operator)
    {
        panic_with_error!(env, ContractError::Unauthorized);
    }
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod test;
