//! Nester Yield Source Registry
//!
//! On-chain registry that tracks approved yield sources (Aave, Blend,
//! Compound, etc.), lifecycle status, and performance metadata used by
//! allocation logic.
//!
//! # Roles
//!
//! * Admin: register/update/remove sources, risk + limit updates.
//! * Operator: day-to-day performance refreshes (APY/TVL) and migration ops.
//!
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
    contract, contractimpl, contracttype, panic_with_error, symbol_short, Address, BytesN, Env,
    Symbol, Vec,
};

use nester_access_control::{AccessControl, Role};
use nester_common::{
    emit_event_with_sym, emit_value_attested, Attestation, AttestedField, AttestationPayload,
    ContractError, ProtocolType, SourceStatus, ValueAttestedEventData, FIELD_APY, FIELD_TVL,
};

const REGISTRY: Symbol = symbol_short!("REGISTRY");
const SOURCE_ADDED: Symbol = symbol_short!("SRC_ADD");
const SOURCE_UPDATED: Symbol = symbol_short!("SRC_UPD");
const SOURCE_REMOVED: Symbol = symbol_short!("SRC_REM");
const SOURCE_PERF: Symbol = symbol_short!("SRC_PERF");
const SOURCE_MIGRATION: Symbol = symbol_short!("SRC_MIG");
const VAL_ATT: Symbol = symbol_short!("VAL_ATT");

const DEFAULT_RISK_RATING: u32 = 5;
const MAX_RISK_RATING: u32 = 10;
pub const MAX_APY_HISTORY: u32 = 16;

// Hard cap for APY stored on-chain (basis points). 10000 == 100% APY.
const MAX_APY_BPS: u32 = 10_000;
// Default maximum single-update deviation (bps) allowed relative to the last
// stored APY. Used to seed the configurable threshold at `initialize`; the live
// value is read from storage (see `apy_deviation_threshold`) so an admin can
// tune it without a contract upgrade. 5000 == a 5000-bps (50 percentage-point)
// absolute change allowed in a single update.
pub const DEFAULT_APY_DEVIATION_THRESHOLD_BPS: u32 = 5_000;

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
    /// Configurable APY single-update deviation threshold, in basis points.
    ApyDeviationThresholdBps,
    // ------ Attestation keys -----------------------------------------------
    /// Set of registered attester public keys (BytesN<32>).
    Attesters,
    /// Per-attester last-seen nonce: BytesN<32> → u64.
    AttesterNonce(BytesN<32>),
    /// Optional human-readable label for an attester key (for audit logs).
    AttesterLabel(BytesN<32>),
    /// Minimum number of distinct valid attestations required for an APY update.
    AttestationThresholdApy,
    /// Minimum number of distinct valid attestations required for a TVL update.
    AttestationThresholdTvl,
}

// ---------------------------------------------------------------------------
// Attester types
// ---------------------------------------------------------------------------

/// Default m-of-n attestation threshold for APY updates (2 attesters required).
pub const DEFAULT_APY_ATTESTATION_THRESHOLD: u32 = 1;
/// Default m-of-n attestation threshold for TVL updates (1 attester required).
pub const DEFAULT_TVL_ATTESTATION_THRESHOLD: u32 = 1;

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
        env.storage().instance().set(
            &DataKey::ApyDeviationThresholdBps,
            &DEFAULT_APY_DEVIATION_THRESHOLD_BPS,
        );
        // Seed attester registry and default thresholds.
        env.storage()
            .instance()
            .set(&DataKey::Attesters, &Vec::<BytesN<32>>::new(&env));
        env.storage().instance().set(
            &DataKey::AttestationThresholdApy,
            &DEFAULT_APY_ATTESTATION_THRESHOLD,
        );
        env.storage().instance().set(
            &DataKey::AttestationThresholdTvl,
            &DEFAULT_TVL_ATTESTATION_THRESHOLD,
        );
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

        // Deprecated and Exploit are terminal states.
        if matches!(source.status, SourceStatus::Deprecated)
            || matches!(source.status, SourceStatus::Exploit)
        {
            panic_with_error!(&env, ContractError::InvalidOperation);
        }

        let old_status = source.status.clone();
        source.status = new_status.clone();

        // Deprecation or Exploit implies migration is required.
        if matches!(new_status, SourceStatus::Deprecated)
            || matches!(new_status, SourceStatus::Exploit)
        {
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
    ///
    /// A deviation guard rejects single-update jumps whose absolute change from
    /// the last stored APY exceeds the configured threshold (see
    /// [`Self::set_apy_deviation_threshold`]). The guard is **skipped for the
    /// first update** (when there is no prior non-zero APY to compare against),
    /// and the boundary is **inclusive**: a change exactly equal to the
    /// threshold is accepted; only a strictly larger change is rejected.
    ///
    /// To intentionally apply a change beyond the threshold (e.g. to correct a
    /// stuck APY), an Admin must use [`Self::update_apy_override`].
    pub fn update_apy(env: Env, caller: Address, id: Symbol, new_apy_bps: u32) {
        caller.require_auth();
        require_admin_or_operator(&env, &caller);

        // Validate range before touching storage
        if new_apy_bps > MAX_APY_BPS {
            panic_with_error!(env, ContractError::InvalidAmount);
        }

        let mut source = get_source_or_panic(&env, &id);

        // Deviation guard: reject single-update jumps that are implausible.
        // Threshold is read from storage so it can be tuned by an admin.
        let last_apy = source.current_apy_bps;
        if last_apy != 0 {
            let deviation = new_apy_bps.abs_diff(last_apy);
            if deviation > apy_deviation_threshold(&env) {
                panic_with_error!(env, ContractError::InvalidOperation);
            }
        }

        commit_apy_update(&env, &id, &mut source, new_apy_bps);
    }

    /// Emergency override: set a source's APY, **bypassing the deviation
    /// guard**. Admin only — Operators cannot override. The absolute
    /// [`MAX_APY_BPS`] ceiling is still enforced.
    ///
    /// This exists so an operator team can correct a stuck or stale APY when a
    /// legitimate market move is larger than the configured deviation
    /// threshold. Because it skips the guard, it is gated on the Admin role.
    pub fn update_apy_override(env: Env, caller: Address, id: Symbol, new_apy_bps: u32) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);

        if new_apy_bps > MAX_APY_BPS {
            panic_with_error!(env, ContractError::InvalidAmount);
        }

        let mut source = get_source_or_panic(&env, &id);
        commit_apy_update(&env, &id, &mut source, new_apy_bps);
    }

    /// Set the APY single-update deviation threshold, in basis points. Admin
    /// only. Must not exceed [`MAX_APY_BPS`]. A threshold of 0 means any change
    /// to a non-zero APY is rejected (only the override path can move it).
    pub fn set_apy_deviation_threshold(env: Env, caller: Address, threshold_bps: u32) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);

        if threshold_bps > MAX_APY_BPS {
            panic_with_error!(&env, ContractError::ConfigOutOfRange);
        }

        env.storage()
            .instance()
            .set(&DataKey::ApyDeviationThresholdBps, &threshold_bps);
    }

    /// Return the configured APY single-update deviation threshold (bps).
    pub fn get_apy_deviation_threshold(env: Env) -> u32 {
        apy_deviation_threshold(&env)
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
    // Attester management — Admin only
    // -----------------------------------------------------------------------

    /// Register an ed25519 public key as a trusted attester for APY/TVL
    /// updates.  Admin only.
    ///
    /// `key` is a 32-byte ed25519 raw public key.  `label` is an optional
    /// human-readable description (e.g. "backend-attester-v1") stored for
    /// audit purposes only.
    ///
    /// Panics with [`ContractError::InvalidOperation`] if the key is already
    /// registered.
    pub fn register_attester(
        env: Env,
        caller: Address,
        key: BytesN<32>,
        label: Symbol,
    ) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);

        let mut attesters = attester_list(&env);
        for existing in attesters.iter() {
            if existing == key {
                panic_with_error!(&env, ContractError::InvalidOperation);
            }
        }
        attesters.push_back(key.clone());
        env.storage()
            .instance()
            .set(&DataKey::Attesters, &attesters);
        // Initialise nonce to 0 for new attester.
        env.storage()
            .instance()
            .set(&DataKey::AttesterNonce(key.clone()), &0u64);
        // Store optional label.
        env.storage()
            .instance()
            .set(&DataKey::AttesterLabel(key), &label);
    }

    /// Revoke a previously registered attester key.  Admin only.
    ///
    /// After revocation the key is removed from the registered set; any
    /// signatures produced with it will be rejected immediately.
    ///
    /// Panics with [`ContractError::AttesterNotRegistered`] if the key is
    /// not in the registered set.
    pub fn revoke_attester(env: Env, caller: Address, key: BytesN<32>) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);

        let attesters = attester_list(&env);
        let mut new_list = Vec::<BytesN<32>>::new(&env);
        let mut found = false;
        for existing in attesters.iter() {
            if existing == key {
                found = true;
            } else {
                new_list.push_back(existing);
            }
        }
        if !found {
            panic_with_error!(&env, ContractError::AttesterNotRegistered);
        }
        env.storage()
            .instance()
            .set(&DataKey::Attesters, &new_list);
        // Leave nonce and label in storage — they are harmless and preserve
        // the audit trail; the key simply no longer appears in the active set.
    }

    /// Configure the minimum number of distinct valid attestations required
    /// for a given field.  Admin only.
    ///
    /// `field_tag` is `0x01` (APY) or `0x02` (TVL).  `threshold` must be ≥ 1.
    /// Setting `threshold` higher than the number of registered attesters
    /// effectively makes that field un-updatable until more attesters are
    /// registered.
    pub fn set_attestation_threshold(
        env: Env,
        caller: Address,
        field_tag: u32,
        threshold: u32,
    ) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);

        if threshold == 0 {
            panic_with_error!(&env, ContractError::ConfigOutOfRange);
        }

        let key = match field_tag {
            1 => DataKey::AttestationThresholdApy,
            2 => DataKey::AttestationThresholdTvl,
            _ => panic_with_error!(&env, ContractError::InvalidOperation),
        };
        env.storage().instance().set(&key, &threshold);
    }

    /// Return the currently configured attestation threshold for `field_tag`.
    pub fn get_attestation_threshold(env: Env, field_tag: u32) -> u32 {
        match field_tag {
            1 => env
                .storage()
                .instance()
                .get(&DataKey::AttestationThresholdApy)
                .unwrap_or(DEFAULT_APY_ATTESTATION_THRESHOLD),
            2 => env
                .storage()
                .instance()
                .get(&DataKey::AttestationThresholdTvl)
                .unwrap_or(DEFAULT_TVL_ATTESTATION_THRESHOLD),
            _ => panic_with_error!(&env, ContractError::InvalidOperation),
        }
    }

    // -----------------------------------------------------------------------
    // Signature-attested performance updates
    // -----------------------------------------------------------------------

    /// Update the APY for a yield source via cryptographic attestation.
    ///
    /// Each element of `attestations` must:
    /// - Carry an ed25519 public key registered in the attester set.
    /// - Have a signature that verifies over the canonical payload (see
    ///   `libs/common/src/attestation.rs` for the exact byte layout).
    /// - Fall within the `[valid_from, valid_until)` window.
    /// - Carry a nonce strictly greater than the last-seen nonce for that key.
    ///
    /// Distinct valid attesters are counted; if the count meets or exceeds the
    /// configured APY threshold the deviation check is applied and — if it
    /// passes — the APY is committed.  Both attestation and deviation limits
    /// are enforced; they are complementary, not alternatives.
    ///
    /// Panics with one of:
    /// - [`ContractError::AttesterNotRegistered`] — key not in registered set.
    /// - [`ContractError::AttestationExpired`]   — outside validity window.
    /// - [`ContractError::NonceReused`]           — nonce not monotonically increasing.
    /// - [`ContractError::SignatureInvalid`]      — signature verification failed.
    /// - [`ContractError::ThresholdNotMet`]       — too few valid attesters.
    /// - [`ContractError::InvalidOperation`]      — deviation check failed.
    /// - [`ContractError::InvalidAmount`]         — value out of range.
    pub fn update_apy_attested(
        env: Env,
        caller: Address,
        id: Symbol,
        new_apy_bps: u32,
        valid_from: u64,
        valid_until: u64,
        attestations: Vec<Attestation>,
    ) {
        caller.require_auth();
        require_admin_or_operator(&env, &caller);

        if new_apy_bps > MAX_APY_BPS {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }

        let now = env.ledger().timestamp();
        let threshold = env
            .storage()
            .instance()
            .get::<DataKey, u32>(&DataKey::AttestationThresholdApy)
            .unwrap_or(DEFAULT_APY_ATTESTATION_THRESHOLD);

        let payload = AttestationPayload {
            contract_address: env.current_contract_address(),
            source_id: id.clone(),
            field: AttestedField::Apy,
            apy_bps: new_apy_bps,
            tvl: 0,
            valid_from,
            valid_until,
        };

        let (valid_keys, used_nonces) =
            verify_and_collect(&env, &attestations, &payload, threshold, now);

        // Deviation check — attestation does not bypass the existing guard.
        let mut source = get_source_or_panic(&env, &id);
        let last_apy = source.current_apy_bps;
        if last_apy != 0 {
            let deviation = new_apy_bps.abs_diff(last_apy);
            if deviation > apy_deviation_threshold(&env) {
                panic_with_error!(&env, ContractError::InvalidOperation);
            }
        }

        commit_apy_update(&env, &id, &mut source, new_apy_bps);

        emit_value_attested(
            &env,
            REGISTRY,
            VAL_ATT,
            id.clone(),
            ValueAttestedEventData {
                source_id: id,
                field_tag: FIELD_APY as u32,
                value: new_apy_bps as i128,
                attester_keys: valid_keys,
                nonces: used_nonces,
                accepted_at: now,
            },
        );
    }

    /// Update the TVL for a yield source via cryptographic attestation.
    ///
    /// Verification is identical to [`Self::update_apy_attested`] except the
    /// TVL attestation threshold applies (default: 1-of-n).
    pub fn update_tvl_attested(
        env: Env,
        caller: Address,
        id: Symbol,
        new_tvl: i128,
        valid_from: u64,
        valid_until: u64,
        attestations: Vec<Attestation>,
    ) {
        caller.require_auth();
        require_admin_or_operator(&env, &caller);

        if new_tvl < 0 {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }

        let now = env.ledger().timestamp();
        let threshold = env
            .storage()
            .instance()
            .get::<DataKey, u32>(&DataKey::AttestationThresholdTvl)
            .unwrap_or(DEFAULT_TVL_ATTESTATION_THRESHOLD);

        let payload = AttestationPayload {
            contract_address: env.current_contract_address(),
            source_id: id.clone(),
            field: AttestedField::Tvl,
            apy_bps: 0,
            tvl: new_tvl,
            valid_from,
            valid_until,
        };

        let (valid_keys, used_nonces) =
            verify_and_collect(&env, &attestations, &payload, threshold, now);

        let mut source = get_source_or_panic(&env, &id);
        source.tvl = new_tvl;
        touch_source(&env, &mut source);
        save_source(&env, &id, &source);

        emit_event_with_sym(
            &env,
            REGISTRY,
            SOURCE_PERF,
            id.clone(),
            performance_event_data(&source),
        );

        emit_value_attested(
            &env,
            REGISTRY,
            VAL_ATT,
            id.clone(),
            ValueAttestedEventData {
                source_id: id,
                field_tag: FIELD_TVL as u32,
                value: new_tvl,
                attester_keys: valid_keys,
                nonces: used_nonces,
                accepted_at: now,
            },
        );
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

/// Read the configured APY deviation threshold, falling back to the default if
/// (for a registry initialised before this field existed) it is unset.
fn apy_deviation_threshold(env: &Env) -> u32 {
    env.storage()
        .instance()
        .get(&DataKey::ApyDeviationThresholdBps)
        .unwrap_or(DEFAULT_APY_DEVIATION_THRESHOLD_BPS)
}

/// Apply a validated APY value: persist it, append a history snapshot, bump the
/// last-updated timestamp, and emit the performance event. Shared by the
/// guarded (`update_apy`) and override (`update_apy_override`) paths.
fn commit_apy_update(env: &Env, id: &Symbol, source: &mut YieldSource, new_apy_bps: u32) {
    source.current_apy_bps = new_apy_bps;
    append_apy_snapshot(env, source, new_apy_bps);
    touch_source(env, source);
    save_source(env, id, source);

    emit_event_with_sym(
        env,
        REGISTRY,
        SOURCE_PERF,
        id.clone(),
        performance_event_data(source),
    );
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

/// APY/TVL reporting (issue #820): Admin, Operator, or the narrower
/// [`Role::Attester`] — a distinct key dedicated to signing yield reports,
/// separate from any fund-moving role.
fn require_admin_or_operator(env: &Env, caller: &Address) {
    if !AccessControl::has_role(env, caller, Role::Admin)
        && !AccessControl::has_role(env, caller, Role::Operator)
        && !AccessControl::has_role(env, caller, Role::Attester)
    {
        panic_with_error!(env, ContractError::Unauthorized);
    }
}

// ---------------------------------------------------------------------------
// Attestation helpers
// ---------------------------------------------------------------------------

fn attester_list(env: &Env) -> Vec<BytesN<32>> {
    env.storage()
        .instance()
        .get(&DataKey::Attesters)
        .unwrap_or_else(|| Vec::new(env))
}

fn is_registered_attester(env: &Env, key: &BytesN<32>) -> bool {
    let list = attester_list(env);
    for k in list.iter() {
        if &k == key {
            return true;
        }
    }
    false
}

fn attester_last_nonce(env: &Env, key: &BytesN<32>) -> u64 {
    env.storage()
        .instance()
        .get::<DataKey, u64>(&DataKey::AttesterNonce(key.clone()))
        .unwrap_or(0)
}

fn save_attester_nonce(env: &Env, key: &BytesN<32>, nonce: u64) {
    env.storage()
        .instance()
        .set(&DataKey::AttesterNonce(key.clone()), &nonce);
}

/// Verify all attestations, collect distinct valid attester keys, and assert
/// the threshold is met.  Updates each attester's last-seen nonce on success.
///
/// Returns (valid_keys, used_nonces) — parallel arrays for event emission.
fn verify_and_collect(
    env: &Env,
    attestations: &Vec<Attestation>,
    payload: &AttestationPayload,
    threshold: u32,
    now: u64,
) -> (Vec<BytesN<32>>, Vec<u64>) {
    let mut valid_keys: Vec<BytesN<32>> = Vec::new(env);
    let mut used_nonces: Vec<u64> = Vec::new(env);

    for att in attestations.iter() {
        let registered = is_registered_attester(env, &att.public_key);
        let last_nonce = attester_last_nonce(env, &att.public_key);

        // verify_attestation panics on any failure — errors are distinct typed
        // errors (AttesterNotRegistered, AttestationExpired, NonceReused, and
        // a host-level panic for SignatureInvalid which we cannot catch in
        // no_std; the host will surface it as ContractError to the caller).
        nester_common::verify_attestation(env, &att, payload, registered, last_nonce, now);

        // Update nonce immediately so a duplicate key in the same Vec is
        // caught as NonceReused on the second occurrence.
        save_attester_nonce(env, &att.public_key, att.nonce);
        valid_keys.push_back(att.public_key.clone());
        used_nonces.push_back(att.nonce);
    }

    if (valid_keys.len() as u32) < threshold {
        panic_with_error!(env, ContractError::ThresholdNotMet);
    }

    (valid_keys, used_nonces)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod test;
