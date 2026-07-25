extern crate std;

use soroban_sdk::{contract, contractimpl, symbol_short, Address, Env, Vec};
use vault_contract::{AllocationDeltaView, CurrentAllocationView, VaultContractClient};

#[contract]
pub struct ReentrantStrategyContract;

#[contractimpl]
impl ReentrantStrategyContract {
    pub fn initialize(env: Env, vault: Address, attacker: Address) {
        env.storage().instance().set(&symbol_short!("vault"), &vault);
        env.storage().instance().set(&symbol_short!("att"), &attacker);
    }

    pub fn calculate_rebalance_deltas(
        env: Env,
        _current: Vec<CurrentAllocationView>,
        _total: i128,
    ) -> Vec<AllocationDeltaView> {
        let vault: Address = env.storage().instance().get(&symbol_short!("vault")).unwrap();
        let attacker: Address = env.storage().instance().get(&symbol_short!("att")).unwrap();
        let client = VaultContractClient::new(&env, &vault);
        client.withdraw(&attacker, &1_i128, &0_i128);
        Vec::new(&env)
    }

    pub fn validate_allocations(
        _env: Env,
        _current: Vec<CurrentAllocationView>,
        _total: i128,
    ) -> bool {
        true
    }
}

pub fn register_reentrant_strategy(env: &Env, vault: &Address, attacker: &Address) -> Address {
    let id = env.register_contract(None, ReentrantStrategyContract);
    ReentrantStrategyContractClient::new(env, &id).initialize(vault, attacker);
    id
}
