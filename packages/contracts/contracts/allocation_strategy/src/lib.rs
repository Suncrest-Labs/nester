#![no_std]

use soroban_sdk::{
    contract, contractimpl, contracttype, panic_with_error, symbol_short, vec, Address, Env, IntoVal,
    Symbol, Val, Vec,
};

use nester_access_control::{AccessControl, Role};
use nester_common::{emit_event, ContractError, ProtocolType, SourceStatus, BASIS_POINT_SCALE};

const STRATEGY: Symbol = symbol_short!("STRATEGY");
const WEIGHTS_UPDATED: Symbol = symbol_short!("WTS_SET");
const MAX_RISK_RATING: u32 = 10;

#[contracttype]
#[derive(Clone, Debug)]
struct RegistryApySnapshot {
    pub apy_bps: u32,
    pub timestamp: u64,
}

#[contracttype]
#[derive(Clone, Debug)]
struct RegistrySource {
    pub id: Symbol,
    pub contract_address: Address,
    pub protocol_type: ProtocolType,
    pub status: SourceStatus,
    pub added_at: u64,
    pub current_apy_bps: u32,
    pub apy_history: Vec<RegistryApySnapshot>,
    pub tvl: i128,
    pub risk_rating: u32,
    pub min_deposit: i128,
    pub max_deposit: i128,
    pub last_updated: u64,
    pub migration_required: bool,
    pub migration_completed: bool,
    pub migration_completed_at: u64,
}

struct RegistryClient<'a> {
    env: &'a Env,
    contract_id: &'a Address,
}

impl<'a> RegistryClient<'a> {
    fn new(env: &'a Env, contract_id: &'a Address) -> Self {
        Self { env, contract_id }
    }

    fn has_source(&self, source_id: &Symbol) -> bool {
        self.env.invoke_contract(
            self.contract_id,
            &Symbol::new(self.env, "has_source"),
            vec![self.env, source_id.clone().into_val(self.env)],
        )
    }

    fn get_source_status(&self, source_id: &Symbol) -> SourceStatus {
        self.env.invoke_contract(
            self.contract_id,
            &Symbol::new(self.env, "get_source_status"),
            vec![self.env, source_id.clone().into_val(self.env)],
        )
    }

    fn get_active_sources(&self) -> Vec<RegistrySource> {
        self.env.invoke_contract(
            self.contract_id,
            &Symbol::new(self.env, "get_active_sources"),
            Vec::<Val>::new(self.env),
        )
    }
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct WeightsUpdatedEventData {
    pub old_weights: Vec<AllocationWeight>,
    pub new_weights: Vec<AllocationWeight>,
}

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

/// A single allocation weight expressed in basis points (1 bp = 0.01%).
/// All weights in a set must sum to exactly 10 000 bp (100 %).
#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AllocationWeight {
    pub source_id: Symbol,
    /// Share of total allocation in basis points (0–10 000).
    pub weight_bps: u32,
}

#[contracttype]
#[derive(Clone)]
enum DataKey {
    /// Address of the YieldRegistry contract used to validate sources.
    RegistryId,
    /// Currently active allocation weights.
    Weights,
    /// Last computed allocation amount for a specific source.
    Allocation(Symbol),
}

// ---------------------------------------------------------------------------
// Contract
// ---------------------------------------------------------------------------

#[contract]
pub struct AllocationStrategyContract;

#[contractimpl]
impl AllocationStrategyContract {
    /// Initialise the strategy, granting `admin` the Admin role and recording
    /// the address of the yield registry.
    pub fn initialize(env: Env, admin: Address, registry_id: Address) {
        AccessControl::initialize(&env, &admin);
        env.storage()
            .instance()
            .set(&DataKey::RegistryId, &registry_id);
    }

    // -----------------------------------------------------------------------
    // Weight management — Admin or Operator
    // -----------------------------------------------------------------------

    /// Set the allocation weights.
    ///
    /// Validation:
    /// * `caller` must hold [`Role::Admin`] or [`Role::Operator`].
    /// * `weights` must sum to exactly [`BASIS_POINT_SCALE`] (10 000 bp).
    /// * Every `source_id` must exist in the registry and be [`SourceStatus::Active`].
    pub fn set_weights(env: Env, caller: Address, weights: Vec<AllocationWeight>) {
        caller.require_auth();
        require_admin_or_operator(&env, &caller);

        // Validate weight sum.
        let mut sum: u32 = 0;
        for w in weights.iter() {
            sum += w.weight_bps;
        }
        if sum != BASIS_POINT_SCALE {
            panic_with_error!(&env, ContractError::AllocationError);
        }

        // Validate each source against the registry.
        let registry_id: Address = env.storage().instance().get(&DataKey::RegistryId).unwrap();
        let registry = RegistryClient::new(&env, &registry_id);

        for w in weights.iter() {
            if !registry.has_source(&w.source_id) {
                panic_with_error!(&env, ContractError::StrategyNotFound);
            }
            if registry.get_source_status(&w.source_id) != SourceStatus::Active {
                panic_with_error!(&env, ContractError::InvalidOperation);
            }
        }

        let old_weights = Self::get_weights(env.clone());
        env.storage().instance().set(&DataKey::Weights, &weights);

        emit_event(
            &env,
            STRATEGY,
            WEIGHTS_UPDATED,
            caller,
            WeightsUpdatedEventData {
                old_weights,
                new_weights: weights,
            },
        );
    }

    /// Suggest APY/risk-aware weights using active registry sources.
    ///
    /// Scoring model:
    /// * Higher APY increases score.
    /// * Higher risk decreases score.
    /// * Score = max(APY, 1) * (11 - risk_rating), with risk clamped to 1..=10.
    ///
    /// Returns weights that sum to 10_000 bps. No state is written.
    pub fn suggest_weights(env: Env) -> Vec<AllocationWeight> {
        let registry_id: Address = env.storage().instance().get(&DataKey::RegistryId).unwrap();
        let registry = RegistryClient::new(&env, &registry_id);
        let active_sources = registry.get_active_sources();

        suggest_weights_from_sources(&env, active_sources)
    }

    /// Return the currently stored allocation weights.
    pub fn get_weights(env: Env) -> Vec<AllocationWeight> {
        env.storage()
            .instance()
            .get(&DataKey::Weights)
            .unwrap_or_else(|| Vec::new(&env))
    }

    // -----------------------------------------------------------------------
    // Allocation calculation
    // -----------------------------------------------------------------------

    /// Compute how `total` units should be distributed across sources according
    /// to the stored weights.
    ///
    /// Uses floor division per source; any rounding remainder is assigned to the
    /// source with the highest weight to ensure the full `total` is distributed.
    ///
    /// The computed allocations are persisted and can be retrieved individually
    /// with [`get_source_allocation`].
    pub fn calculate_allocation(env: Env, total: i128) -> Vec<(Symbol, i128)> {
        let weights: Vec<AllocationWeight> = env
            .storage()
            .instance()
            .get(&DataKey::Weights)
            .unwrap_or_else(|| Vec::new(&env));

        let scale = BASIS_POINT_SCALE as i128;
        let n = weights.len();

        let mut allocations: Vec<(Symbol, i128)> = Vec::new(&env);
        let mut total_allocated: i128 = 0;
        let mut max_weight: u32 = 0;
        let mut max_idx: u32 = 0;

        for i in 0..n {
            let w = weights.get(i).unwrap();
            let amount = (total * w.weight_bps as i128) / scale;
            total_allocated += amount;
            allocations.push_back((w.source_id.clone(), amount));
            if w.weight_bps > max_weight {
                max_weight = w.weight_bps;
                max_idx = i;
            }
        }

        // Assign rounding remainder to the highest-weight source.
        let remainder = total - total_allocated;
        if remainder > 0 && n > 0 {
            let (sym, amount) = allocations.get(max_idx).unwrap();
            allocations.set(max_idx, (sym, amount + remainder));
        }

        // Persist per-source allocations for `get_source_allocation` lookups.
        for i in 0..allocations.len() {
            let (sym, amount) = allocations.get(i).unwrap();
            env.storage()
                .instance()
                .set(&DataKey::Allocation(sym), &amount);
        }

        allocations
    }

    /// Return the last computed allocation amount for `source_id`.
    /// Returns 0 if [`calculate_allocation`] has not been called yet.
    pub fn get_source_allocation(env: Env, source_id: Symbol) -> i128 {
        env.storage()
            .instance()
            .get(&DataKey::Allocation(source_id))
            .unwrap_or(0_i128)
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

    /// Accept a pending admin transfer (step 2). Caller must be the proposed new admin.
    pub fn accept_admin(env: Env, new_admin: Address) {
        AccessControl::accept_admin(&env, &new_admin);
    }
}

// ---------------------------------------------------------------------------
// Private helpers
// ---------------------------------------------------------------------------

fn suggest_weights_from_sources(env: &Env, sources: Vec<RegistrySource>) -> Vec<AllocationWeight> {
    if sources.is_empty() {
        return Vec::new(env);
    }

    let mut scored: Vec<(Symbol, i128)> = Vec::new(env);
    let mut total_score: i128 = 0;
    let mut max_score: i128 = -1;
    let mut max_idx: u32 = 0;

    for i in 0..sources.len() {
        let source = sources.get(i).unwrap();
        let score = source_score(&source);
        total_score += score;

        if score > max_score {
            max_score = score;
            max_idx = i;
        }

        scored.push_back((source.id, score));
    }

    let mut weights = Vec::<AllocationWeight>::new(env);
    let mut allocated: u32 = 0;

    for (source_id, score) in scored.iter() {
        let weight_bps = ((score * BASIS_POINT_SCALE as i128) / total_score) as u32;
        allocated += weight_bps;
        weights.push_back(AllocationWeight {
            source_id,
            weight_bps,
        });
    }

    // Allocate basis-point rounding remainder to the best-scoring source.
    let remainder = BASIS_POINT_SCALE - allocated;
    if remainder > 0 {
        let mut top = weights.get(max_idx).unwrap();
        top.weight_bps += remainder;
        weights.set(max_idx, top);
    }

    weights
}

fn source_score(source: &RegistrySource) -> i128 {
    let raw_risk = source.risk_rating;
    let clamped_risk = if raw_risk == 0 {
        MAX_RISK_RATING
    } else if raw_risk > MAX_RISK_RATING {
        MAX_RISK_RATING
    } else {
        raw_risk
    };

    let risk_factor = (MAX_RISK_RATING + 1 - clamped_risk) as i128;
    let apy = if source.current_apy_bps == 0 {
        1_i128
    } else {
        source.current_apy_bps as i128
    };

    apy * risk_factor
}

/// Panic with [`ContractError::Unauthorized`] unless `account` holds Admin or
/// Operator. Day-to-day operations (e.g. weight updates) are open to both.
fn require_admin_or_operator(env: &Env, account: &Address) {
    if !AccessControl::has_role(env, account, Role::Admin)
        && !AccessControl::has_role(env, account, Role::Operator)
    {
        panic_with_error!(env, ContractError::Unauthorized);
    }
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod test;
