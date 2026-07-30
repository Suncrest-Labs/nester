extern crate std;

use soroban_sdk::{contract, contractimpl, symbol_short, Address, Env};
use vault_contract::VaultContractClient;

#[contract]
pub struct ReentrantYieldSourceContract;

#[contractimpl]
impl ReentrantYieldSourceContract {
    pub fn initialize(env: Env, vault: Address, attacker: Address) {
        env.storage().instance().set(&symbol_short!("vault"), &vault);
        env.storage().instance().set(&symbol_short!("att"), &attacker);
    }

    pub fn harvest(env: Env) {
        let vault: Address = env.storage().instance().get(&symbol_short!("vault")).unwrap();
        let attacker: Address = env.storage().instance().get(&symbol_short!("att")).unwrap();
        VaultContractClient::new(&env, &vault).withdraw(&attacker, &1_i128, &0_i128);
    }

    pub fn receive_fees(env: Env, _amount: i128) {
        Self::harvest(env);
    }
}

pub fn register_reentrant_yield_source(env: &Env, vault: &Address, attacker: &Address) -> Address {
    let id = env.register_contract(None, ReentrantYieldSourceContract);
    ReentrantYieldSourceContractClient::new(env, &id).initialize(vault, attacker);
    id
}
