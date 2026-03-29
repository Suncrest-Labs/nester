#![no_std]

use soroban_sdk::{
    contract, contractimpl, contracttype, panic_with_error, symbol_short, Address, Env, Symbol, token
};

use nester_access_control::{AccessControl, Role};
use nester_common::{ContractError};

const TREASURY: Symbol = symbol_short!("TREASURY");
const RECEIVE: Symbol = symbol_short!("RECEIVE");
const WITHDRAW: Symbol = symbol_short!("WITHDRAW");

#[contracttype]
#[derive(Clone)]
enum DataKey {
    Vault,
    TotalReceived,
}

#[contract]
pub struct TreasuryContract;

#[contractimpl]
impl TreasuryContract {
    pub fn initialize(env: Env, admin: Address, vault: Address) {
        if env.storage().instance().has(&DataKey::Vault) {
            panic_with_error!(&env, ContractError::AlreadyInitialized);
        }
        AccessControl::initialize(&env, &admin);
        env.storage().instance().set(&DataKey::Vault, &vault);
        env.storage().instance().set(&DataKey::TotalReceived, &0_i128);
    }

    pub fn receive_fees(env: Env, amount: i128) {
        let vault: Address = env.storage().instance().get(&DataKey::Vault).unwrap();
        vault.require_auth();

        if amount <= 0 {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }

        let total: i128 = env.storage().instance().get(&DataKey::TotalReceived).unwrap_or(0);
        env.storage().instance().set(&DataKey::TotalReceived, &(total + amount));

        env.events().publish((TREASURY, RECEIVE), amount);
    }

    pub fn withdraw(env: Env, caller: Address, to: Address, token: Address, amount: i128) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);

        if amount <= 0 {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }

        token::Client::new(&env, &token).transfer(&env.current_contract_address(), &to, &amount);

        env.events().publish((TREASURY, WITHDRAW), amount);
    }

    pub fn get_total_received(env: Env) -> i128 {
        env.storage().instance().get(&DataKey::TotalReceived).unwrap_or(0)
    }

    pub fn get_vault(env: Env) -> Address {
        env.storage().instance().get(&DataKey::Vault).unwrap()
    }
}

#[cfg(test)]
mod test;

