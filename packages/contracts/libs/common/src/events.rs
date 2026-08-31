use soroban_sdk::{contracttype, Address, BytesN, Env, IntoVal, Symbol, Val, Vec};

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


/// Data payload for the `value_attested` event emitted on every accepted
/// attested APY or TVL update.
///
/// See `EVENTS.md` → Yield Registry → `VAL_ATT` for the full schema.
#[soroban_sdk::contracttype]
#[derive(Clone, Debug)]
pub struct ValueAttestedEventData {
    /// The source whose value was updated.
    pub source_id: Symbol,
    /// Which field was updated (0x01 = APY, 0x02 = TVL).
    pub field_tag: u32,
    /// The value that was accepted (apy_bps for APY, tvl for TVL).
    pub value: i128,
    /// The ed25519 public keys of all attesters whose signatures were counted.
    pub attester_keys: Vec<BytesN<32>>,
    /// Nonces used by each attester (parallel array with attester_keys).
    pub nonces: Vec<u64>,
    /// Ledger timestamp at which the update was accepted.
    pub accepted_at: u64,
}

/// Emit a `value_attested` event on the `REGISTRY / VAL_ATT / source_id` topic.
pub fn emit_value_attested(
    env: &Env,
    registry_sym: Symbol,
    val_att_sym: Symbol,
    source_id: Symbol,
    data: ValueAttestedEventData,
) {
    env.events()
        .publish((registry_sym, val_att_sym, source_id), data);
}
