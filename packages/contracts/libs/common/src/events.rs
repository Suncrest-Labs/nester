use soroban_sdk::{contracttype, Address, BytesN, Env, IntoVal, Symbol, Val};

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
pub struct UpgradeProposedEventData {
    pub wasm_hash: BytesN<32>,
    pub eta: u64,
    pub proposer: Address,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct UpgradeCancelledEventData {
    pub wasm_hash: BytesN<32>,
    pub cancelled_by: Address,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct UpgradeExecutedEventData {
    pub wasm_hash: BytesN<32>,
    pub executed_by: Address,
    pub execution_timestamp: u64,
}

