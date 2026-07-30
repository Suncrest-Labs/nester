use soroban_sdk::{contracttype, Address, Env, IntoVal, Symbol, Val};

/// Helper to emit a standardized Nester event.
///
/// All events follow the 3-topic structure: `(contract, action, entity)`.
pub fn emit_event(
    env: &Env,
    contract: Symbol,
    action: Symbol,
    entity: Address,
    data: impl IntoVal<Env, Val>,
) {
    env.events().publish((contract, action, entity), data);
}

/// Helper to emit an event where the entity is a Symbol (e.g. Registry source_id).
pub fn emit_event_with_sym(
    env: &Env,
    contract: Symbol,
    action: Symbol,
    entity: Symbol,
    data: impl IntoVal<Env, Val>,
) {
    env.events().publish((contract, action, entity), data);
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct YieldIndexUpdatedEventData {
    pub old_index: i128,
    pub new_index: i128,
    pub yield_amount: i128,
    pub total_shares: i128,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct UserYieldAccruedEventData {
    pub user: Address,
    pub accrued_delta: i128,
    pub total_accrued: i128,
    pub user_index: i128,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct YieldHarvestedEventData {
    pub user: Address,
    pub amount: i128,
}
