#![no_std]

use soroban_sdk::{
    contract, contractimpl, contracttype, panic_with_error, symbol_short, Address, Env, Error,
    IntoVal, Symbol, Val, Vec,
};

use nester_access_control::{AccessControl, Role};
use nester_common::{
    emit_event, fees::mul_div, ContractError, ProtocolType, SourceStatus, BASIS_POINT_SCALE,
};

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
#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AllocationWeight {
    pub source_id: Symbol,
    pub weight_bps: u32,
}

#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SourceApy {
    pub source_id: Symbol,
    pub apy_bps: i128,
}

#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CurrentAllocation {
    pub source_id: Symbol,
    pub amount: i128,
}

/// A single transfer required to move toward target weights.
///
/// `delta` is positive when funds need to flow INTO the source,
/// negative when funds need to be withdrawn FROM it.
#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AllocationDelta {
    pub source_id: Symbol,
    pub delta: i128,
}

#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum VaultType {
    Conservative,
    Balanced,
    Growth,
    DeFi500,
}

#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct StrategyParams {
    pub rebalance_threshold_bps: u32,
    pub max_weight_bps: u32,
}

#[contracttype]
#[derive(Clone)]
enum DataKey {
    RegistryId,
    VaultType,
    Weights,
    Allocation(Symbol),
    RebalanceThresholdBps,
    MaxWeightBps,
}

#[contract]
pub struct AllocationStrategyContract;

#[contractimpl]
impl AllocationStrategyContract {
    pub fn initialize(env: Env, admin: Address, registry_id: Address) {
        Self::initialize_with_vault_type(env, admin, registry_id, VaultType::Balanced);
    }

    pub fn initialize_with_vault_type(
        env: Env,
        admin: Address,
        registry_id: Address,
        vault_type: VaultType,
    ) {
        AccessControl::initialize(&env, &admin);
        env.storage().instance().set(&DataKey::RegistryId, &registry_id);
        env.storage().instance().set(&DataKey::VaultType, &vault_type);

        let params = default_strategy_params(&vault_type);
        env.storage()
            .instance()
            .set(&DataKey::RebalanceThresholdBps, &params.rebalance_threshold_bps);
        env.storage()
            .instance()
            .set(&DataKey::MaxWeightBps, &params.max_weight_bps);

        let default_weights = build_default_weights(&env, &registry_id, &vault_type);
        env.storage().instance().set(&DataKey::Weights, &default_weights);
    }

    pub fn get_vault_type(env: Env) -> VaultType {
        env.storage().instance().get(&DataKey::VaultType).unwrap()
    }

    pub fn get_strategy_params(env: Env) -> StrategyParams {
        StrategyParams {
            rebalance_threshold_bps: env.storage().instance().get(&DataKey::RebalanceThresholdBps).unwrap(),
            max_weight_bps: env.storage().instance().get(&DataKey::MaxWeightBps).unwrap(),
        }
    }

    pub fn update_strategy_params(
        env: Env,
        caller: Address,
        rebalance_threshold_bps: u32,
        max_weight_bps: u32,
    ) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);

        if rebalance_threshold_bps > BASIS_POINT_SCALE
            || max_weight_bps == 0
            || max_weight_bps > BASIS_POINT_SCALE
        {
            panic_with_error!(&env, ContractError::InvalidOperation);
        }

        env.storage()
            .instance()
            .set(&DataKey::RebalanceThresholdBps, &rebalance_threshold_bps);
        env.storage().instance().set(&DataKey::MaxWeightBps, &max_weight_bps);
    }

    pub fn set_weights(env: Env, caller: Address, weights: Vec<AllocationWeight>) {
        caller.require_auth();
        require_admin_or_operator(&env, &caller);

        validate_weight_sum(&env, &weights);

        let registry_id: Address = env.storage().instance().get(&DataKey::RegistryId).unwrap();

        for weight in weights.iter() {
            if !registry_has_source(&env, &registry_id, &weight.source_id) {
                panic_with_error!(&env, ContractError::StrategyNotFound);
            }
            if registry_get_source_status(&env, &registry_id, &weight.source_id)
                != SourceStatus::Active
            {
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

    pub fn compute_allocation(
        env: Env,
        total_amount: i128,
        apys: Vec<SourceApy>,
    ) -> Vec<AllocationWeight> {
        let registry_id: Address = env.storage().instance().get(&DataKey::RegistryId).unwrap();
        let vault_type = Self::get_vault_type(env.clone());
        let params = Self::get_strategy_params(env.clone());

        let mut results = zero_weights_from_entries(&env, &apys);
        let mut eligible_indices = Vec::new(&env);
        let mut scores = Vec::new(&env);

        for (index, entry) in apys.iter().enumerate() {
            let is_registered = registry_has_source(&env, &registry_id, &entry.source_id);
            let is_active = is_registered
                && registry_get_source_status(&env, &registry_id, &entry.source_id)
                    == SourceStatus::Active;

            if is_active && entry.apy_bps > 0 {
                eligible_indices.push_back(index as u32);
                let score = match vault_type {
                    VaultType::DeFi500 => 1_i128,
                    _ => entry.apy_bps,
                };
                scores.push_back(score);
            }
        }

        if eligible_indices.len() > 0 {
            let computed = match vault_type {
                VaultType::DeFi500 => even_distribution(&env, eligible_indices.len() as usize),
                _ => proportional_with_cap(&env, &scores, params.max_weight_bps),
            };

            for (slot, index) in eligible_indices.iter().enumerate() {
                let mut weight = results.get(index).unwrap();
                weight.weight_bps = computed.get(slot as u32).unwrap();
                results.set(index, weight);
            }}

        env.storage().instance().set(&DataKey::Weights, &results);
        persist_allocations(&env, total_amount, &results);
        results
    }

    pub fn calculate_allocation(env: Env, total: i128) -> Vec<(Symbol, i128)> {
        let weights = Self::get_weights(env.clone());
        let allocations = allocation_amounts(&weights, total);

        for (symbol, amount) in allocations.iter() {
            env.storage()
                .instance()
                .set(&DataKey::Allocation(symbol.clone()), &amount);
        }

        let mut out = Vec::new(&env);
        for (symbol, amount) in allocations {
            out.push_back((symbol, amount));
        }
        out
    }

    pub fn needs_rebalance(
        env: Env,
        current_weights: Vec<AllocationWeight>,
        target_weights: Vec<AllocationWeight>,
    ) -> bool {
        let threshold = Self::get_strategy_params(env).rebalance_threshold_bps;
        let mut seen = Vec::new(&current_weights.env());

        for weight in current_weights.iter() {
            if !contains_symbol(&seen, &weight.source_id) {
                seen.push_back(weight.source_id.clone());
            }
        }
        for weight in target_weights.iter() {
            if !contains_symbol(&seen, &weight.source_id) {
                seen.push_back(weight.source_id.clone());
            }
        }

        for symbol in seen {
            let current = lookup_weight(&current_weights, &symbol);
            let target = lookup_weight(&target_weights, &symbol);
            if current.abs_diff(target) > threshold {
                return true;
            }
        }

        false
    }

    /// Compute per-source transfers required to move from `current_allocations`
    /// to the stored target weights, given `total` assets across all sources.
    ///
    /// Positive `delta` = source is under-allocated, needs funds added.
    /// Negative `delta` = source is over-allocated, needs funds removed.
    /// Returned vector is keyed by every source present in either the targets
    /// or `current_allocations` so callers see the full picture.
    pub fn calculate_rebalance_deltas(
        env: Env,
        current_allocations: Vec<CurrentAllocation>,
        total: i128,
    ) -> Vec<AllocationDelta> {
        let target_weights = Self::get_weights(env.clone());
        let target_amounts = allocation_amounts(&target_weights, total);

        let mut deltas = Vec::new(&env);
        let mut seen = Vec::new(&env);

        for (source_id, target_amount) in target_amounts.iter() {
            let current = current_amount_for(&current_allocations, &source_id);
            seen.push_back(source_id.clone());
            deltas.push_back(AllocationDelta {
                source_id,
                delta: target_amount - current,
            });
        }

        // Surface sources held but not in current target weights so callers
        // know to drain them entirely.
        for current in current_allocations.iter() {
            if !contains_symbol(&seen, &current.source_id) {
                deltas.push_back(AllocationDelta {
                    source_id: current.source_id,
                    delta: -current.amount,
                });
            }
        }

        deltas
    }

    /// Returns `true` when `actual_allocations` are within the configured
    /// rebalance threshold of the stored target weights, `false` otherwise.
    /// `total` is the sum of all source balances; pass 0 to skip the check
    /// (treats empty vault as in-spec).
    pub fn validate_allocations(
        env: Env,
        actual_allocations: Vec<CurrentAllocation>,
        total: i128,
    ) -> bool {
        if total <= 0 {
            return true;
        }

        let threshold_bps = Self::get_strategy_params(env.clone()).rebalance_threshold_bps as i128;
        let target_weights = Self::get_weights(env.clone());
        let scale = BASIS_POINT_SCALE as i128;

        let mut seen = Vec::new(&env);
        for w in target_weights.iter() {
            seen.push_back(w.source_id.clone());
        }
        for a in actual_allocations.iter() {
            if !contains_symbol(&seen, &a.source_id) {
                seen.push_back(a.source_id.clone());
            }
        }

        for source_id in seen.iter() {
            let target_bps = lookup_weight(&target_weights, &source_id) as i128;
            let actual_amount = current_amount_for(&actual_allocations, &source_id);
            // actual_bps = actual_amount * scale / total
            let actual_bps = match mul_div(actual_amount, scale, total) {
                Ok(v) => v,
                Err(e) => panic_with_error!(&env, e),
            };
            let drift = (actual_bps - target_bps).abs();
            if drift > threshold_bps {
                return false;
            }
        }

        true
    }

    pub fn get_source_allocation(env: Env, source_id: Symbol) -> i128 {
        env.storage()
            .instance()
            .get(&DataKey::Allocation(source_id))
            .unwrap_or(0_i128)
    }

    pub fn grant_role(env: Env, grantor: Address, grantee: Address, role: Role) {
        AccessControl::grant_role(&env, &grantor, &grantee, role);
    }

    pub fn revoke_role(env: Env, revoker: Address, target: Address, role: Role) {
        AccessControl::revoke_role(&env, &revoker, &target, role);
    }

    pub fn transfer_admin(env: Env, current_admin: Address, new_admin: Address) {
        AccessControl::transfer_admin(&env, &current_admin, &new_admin);
    }

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
        let weight_bps = match mul_div(score, BASIS_POINT_SCALE as i128, total_score) {
            Ok(v) => v as u32,
            Err(e) => panic_with_error!(env, e),
        };
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

fn default_strategy_params(vault_type: &VaultType) -> StrategyParams {
    match vault_type {
        VaultType::Conservative => StrategyParams {
            rebalance_threshold_bps: 250,
            max_weight_bps: 5_000,
        },
        VaultType::Balanced => StrategyParams {
            rebalance_threshold_bps: 500,
            max_weight_bps: 6_500,
        },
        VaultType::Growth => StrategyParams {
            rebalance_threshold_bps: 750,
            max_weight_bps: 8_500,
        },
        VaultType::DeFi500 => StrategyParams {
            rebalance_threshold_bps: 100,
            max_weight_bps: 10_000,
        },
    }
}

fn build_default_weights(env: &Env, registry_id: &Address, vault_type: &VaultType) -> Vec<AllocationWeight> {
    // Defensive: always convert to Vec<AllocationWeight> with correct types and length
    let active_sources: Vec<RegistrySource> = registry_get_active_sources(env, registry_id);
    let mut source_ids = Vec::new(env);
    for source in active_sources.iter() {
        source_ids.push_back(source.id);
    }
    let count = source_ids.len() as usize;
    let distribution = match vault_type {
        VaultType::Conservative => template_distribution(env, count, &[5_000, 3_000, 2_000]),
        VaultType::Balanced => template_distribution(env, count, &[4_000, 3_500, 2_500]),
        VaultType::Growth => template_distribution(env, count, &[2_000, 3_000, 5_000]),
        VaultType::DeFi500 => even_distribution(env, count),
    };
    let mut out = Vec::new(env);
    for (index, source_id) in source_ids.iter().enumerate() {
        let weight_bps = distribution.get(index as u32).unwrap_or(0);
        out.push_back(AllocationWeight {
            source_id,
            weight_bps,
        });
    }
    out
}

fn template_distribution(env: &Env, count: usize, template: &[u32]) -> Vec<u32> {
    if count == 0 {
        return Vec::new(env);
    }
    if count == 1 {
        let mut out = Vec::new(env);
        out.push_back(BASIS_POINT_SCALE);
        return out;
    }
    if count == 2 {
        let mut out = Vec::new(env);
        out.push_back(template[0] + (template[1] / 2));
        out.push_back(template[2] + (template[1] / 2));
        return out;
    }

    let mut out = Vec::new(env);
    for _ in 0..count {
        out.push_back(0_u32);
    }
    out.set(0, template[0]);
    out.set(1, template[1]);
    out.set(2, template[2]);
    out
}

fn even_distribution(env: &Env, count: usize) -> Vec<u32> {
    if count == 0 {
        return Vec::new(env);
    }

    let base = BASIS_POINT_SCALE / count as u32;
    let remainder = BASIS_POINT_SCALE % count as u32;
    let mut out = Vec::new(env);

    for _ in 0..count {
        out.push_back(base);
    }

    for index in 0..remainder {
        let weight = out.get(index).unwrap();
        out.set(index, weight + 1);
    }

    out
}

fn proportional_with_cap(env: &Env, scores: &Vec<i128>, max_weight_bps: u32) -> Vec<u32> {
    if scores.len() == 0 {
        return Vec::new(env);
    }

    if max_weight_bps as usize * (scores.len() as usize) < BASIS_POINT_SCALE as usize {
        panic_with_error!(env, ContractError::AllocationError);
    }

    let len = scores.len();
    let mut assigned = Vec::new(env);
    let mut active = Vec::new(env);
    for _ in 0..len {
        assigned.push_back(0_u32);
        active.push_back(true);
    }
    let mut remaining_total = BASIS_POINT_SCALE;

    while remaining_total > 0 {
        let mut total_score = 0_i128;
        for index in 0..len {
            if active.get(index).unwrap() {
                total_score += scores.get(index).unwrap();
            }
        }

        if total_score == 0 {
            break;
        }

        let snapshot_remaining = remaining_total;

        let mut floors = Vec::new(env);
        let mut remainders = Vec::new(env);
        for _ in 0..len {
            floors.push_back(0_u32);
            remainders.push_back(0_i128);
        }
        let mut capped_any = false;

        for index in 0..len {
            if !active.get(index).unwrap() {
                continue;
            }

            let current_assigned = assigned.get(index).unwrap();
            let capacity = max_weight_bps - current_assigned;
            let numerator = scores.get(index).unwrap() * snapshot_remaining as i128;
            let floor = (numerator / total_score) as u32;
            let ceil = ((numerator + total_score - 1) / total_score) as u32;
            floors.set(index, floor);
            remainders.set(index, numerator % total_score);

            if ceil >= capacity {
                assigned.set(index, current_assigned + capacity);
                remaining_total -= capacity;
                active.set(index, false);
                capped_any = true;
            }
        }

        if capped_any {
            continue;
        }

        let mut distributed = 0_u32;
        for index in 0..len {
            if !active.get(index).unwrap() {
                continue;
            }
            let floor = floors.get(index).unwrap();
            assigned.set(index, assigned.get(index).unwrap() + floor);
            distributed += floor;
        }

        remaining_total -= distributed;

        while remaining_total > 0 {
            let mut best_index = None;
            let mut best_remainder = -1_i128;

            for index in 0..len {
                if !active.get(index).unwrap() || assigned.get(index).unwrap() >= max_weight_bps {
                    continue;
                }
                let remainder = remainders.get(index).unwrap();
                if remainder > best_remainder {
                    best_remainder = remainder;
                    best_index = Some(index);
                }
            }

            match best_index {
                Some(index) => {
                    assigned.set(index, assigned.get(index).unwrap() + 1);
                    remaining_total -= 1;
                }
                None => break,
            }
        }

        break;
    }

    assigned
}

fn zero_weights_from_entries(env: &Env, apys: &Vec<SourceApy>) -> Vec<AllocationWeight> {
    let mut out = Vec::new(env);
    for entry in apys.iter() {
        out.push_back(AllocationWeight {
            source_id: entry.source_id,
            weight_bps: 0,
        });
    }
    out
}

fn validate_weight_sum(env: &Env, weights: &Vec<AllocationWeight>) {
    let mut sum = 0_u32;
    for weight in weights.iter() {
        sum += weight.weight_bps;
    }
    if sum != BASIS_POINT_SCALE {
        panic_with_error!(env, ContractError::AllocationError);
    }
}

fn persist_allocations(env: &Env, total_amount: i128, weights: &Vec<AllocationWeight>) {
    for (symbol, amount) in allocation_amounts(weights, total_amount) {
        env.storage().instance().set(&DataKey::Allocation(symbol), &amount);
    }
}

fn allocation_amounts(weights: &Vec<AllocationWeight>, total_amount: i128) -> Vec<(Symbol, i128)> {
    let scale = BASIS_POINT_SCALE as i128;
    let env = weights.env();
    let mut out = Vec::new(&env);
    let mut total_allocated = 0_i128;
    let mut max_index = None;
    let mut max_weight = 0_u32;

    for (index, weight) in weights.iter().enumerate() {
        let amount = match mul_div(total_amount, weight.weight_bps as i128, scale) {
            Ok(v) => v,
            Err(e) => panic_with_error!(&env, e),
        };
        total_allocated += amount;
        if weight.weight_bps > max_weight {
            max_weight = weight.weight_bps;
            max_index = Some(index as usize);
        }
        out.push_back((weight.source_id, amount));
    }

    if let Some(index) = max_index {
        let remainder = total_amount - total_allocated;
        if remainder > 0 {
            let (symbol, amount) = out.get(index as u32).unwrap();
            out.set(index as u32, (symbol, amount + remainder));
        }
    }

    out
}

fn contains_symbol(symbols: &Vec<Symbol>, target: &Symbol) -> bool {
    for symbol in symbols {
        if symbol == *target {
            return true;
        }
    }
    false
}

fn current_amount_for(allocations: &Vec<CurrentAllocation>, target: &Symbol) -> i128 {
    for a in allocations.iter() {
        if a.source_id == *target {
            return a.amount;
        }
    }
    0
}

fn lookup_weight(weights: &Vec<AllocationWeight>, target: &Symbol) -> u32 {
    for weight in weights.iter() {
        if weight.source_id == *target {
            return weight.weight_bps;
        }
    }
    0
}

fn registry_has_source(env: &Env, registry_id: &Address, source_id: &Symbol) -> bool {
    env.invoke_contract(
        registry_id,
        &Symbol::new(env, "has_source"),
        (source_id.clone(),).into_val(env),
    )
}

fn registry_get_source_status(
    env: &Env,
    registry_id: &Address,
    source_id: &Symbol,
) -> SourceStatus {
    env.invoke_contract(
        registry_id,
        &Symbol::new(env, "get_source_status"),
        (source_id.clone(),).into_val(env),
    )
}

fn registry_get_active_sources(env: &Env, registry_id: &Address) -> Vec<RegistrySource> {
    match env.try_invoke_contract::<Vec<RegistrySource>, Error>(
        registry_id,
        &Symbol::new(env, "get_active_sources"),
        ().into_val(env),
    ) {
        Ok(Ok(v)) => v,
        Ok(Err(_)) => Vec::new(env),
        Err(_) => Vec::new(env),
    }
}

#[cfg(test)]
mod test;
