#![no_std]

use soroban_sdk::{
    contract, contractimpl, contracttype, panic_with_error, symbol_short, token, Address, Env,
    Symbol,
};

use nester_common::ContractError;

const ESCROW: Symbol = symbol_short!("ESCROW");
const CONTRIB: Symbol = symbol_short!("CONTRIB");
const REFUND: Symbol = symbol_short!("REFUND");
const RELEASED: Symbol = symbol_short!("released");

// ---------------------------------------------------------------------------
// Storage
// ---------------------------------------------------------------------------

#[contracttype]
#[derive(Clone)]
enum DataKey {
    Token,
    Landlord,
    RentTarget,
    Contribution(Address),
    TotalContributions,
    Released,
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

fn require_initialized(env: &Env) {
    if !env.storage().instance().has(&DataKey::Token) {
        panic_with_error!(env, ContractError::NotInitialized);
    }
}

fn get_contribution(env: &Env, user: &Address) -> i128 {
    env.storage()
        .persistent()
        .get(&DataKey::Contribution(user.clone()))
        .unwrap_or(0)
}

fn set_contribution(env: &Env, user: &Address, amount: i128) {
    env.storage()
        .persistent()
        .set(&DataKey::Contribution(user.clone()), &amount);
}

fn get_total(env: &Env) -> i128 {
    env.storage()
        .instance()
        .get(&DataKey::TotalContributions)
        .unwrap_or(0)
}

fn set_total(env: &Env, amount: i128) {
    env.storage()
        .instance()
        .set(&DataKey::TotalContributions, &amount);
}

fn get_rent_target(env: &Env) -> i128 {
    env.storage()
        .instance()
        .get(&DataKey::RentTarget)
        .unwrap_or(0)
}

fn is_released(env: &Env) -> bool {
    env.storage()
        .instance()
        .get(&DataKey::Released)
        .unwrap_or(false)
}

// ---------------------------------------------------------------------------
// Contract
// ---------------------------------------------------------------------------

#[contract]
pub struct RentEscrowContract;

#[contractimpl]
impl RentEscrowContract {
    /// Initialize the rent escrow agreement.
    pub fn initialize(env: Env, landlord: Address, token_address: Address, rent_target: i128) {
        if env.storage().instance().has(&DataKey::Token) {
            panic_with_error!(&env, ContractError::AlreadyInitialized);
        }

        landlord.require_auth();

        if rent_target <= 0 {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }

        env.storage()
            .instance()
            .set(&DataKey::Token, &token_address);
        env.storage()
            .instance()
            .set(&DataKey::Landlord, &landlord);
        env.storage()
            .instance()
            .set(&DataKey::RentTarget, &rent_target);
        env.storage()
            .instance()
            .set(&DataKey::TotalContributions, &0_i128);
        env.storage()
            .instance()
            .set(&DataKey::Released, &false);
    }

    /// Contribute tokens toward the rent target.
    pub fn contribute(env: Env, from: Address, amount: i128) {
        require_initialized(&env);
        from.require_auth();

        if is_released(&env) {
            panic_with_error!(&env, ContractError::InvalidOperation);
        }

        if amount <= 0 {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }

        let token_address: Address = env
            .storage()
            .instance()
            .get(&DataKey::Token)
            .unwrap_or_else(|| panic_with_error!(&env, ContractError::NotInitialized));
        let contract_address = env.current_contract_address();

        token::Client::new(&env, &token_address).transfer(&from, &contract_address, &amount);

        let new_contribution = get_contribution(&env, &from) + amount;
        set_contribution(&env, &from, new_contribution);

        let new_total = get_total(&env) + amount;
        set_total(&env, new_total);

        env.events().publish((ESCROW, CONTRIB, from), amount);
    }

    /// Refund a specific user's contribution back to them.
    pub fn refund(env: Env, user: Address) {
        require_initialized(&env);
        user.require_auth();

        if is_released(&env) {
            panic_with_error!(&env, ContractError::InvalidOperation);
        }

        let contribution = get_contribution(&env, &user);
        if contribution <= 0 {
            panic_with_error!(&env, ContractError::InsufficientBalance);
        }

        let token_address: Address = env
            .storage()
            .instance()
            .get(&DataKey::Token)
            .unwrap_or_else(|| panic_with_error!(&env, ContractError::NotInitialized));
        let contract_address = env.current_contract_address();

        token::Client::new(&env, &token_address).transfer(
            &contract_address,
            &user,
            &contribution,
        );

        set_contribution(&env, &user, 0);
        let new_total = get_total(&env) - contribution;
        set_total(&env, new_total);

        env.events().publish((ESCROW, REFUND, user), contribution);
    }

    /// Release funds to the landlord once the rent target is fully funded.
    pub fn release(env: Env, landlord: Address) {
        require_initialized(&env);
        landlord.require_auth();

        if is_released(&env) {
            panic_with_error!(&env, ContractError::InvalidOperation);
        }

        let stored_landlord: Address = env
            .storage()
            .instance()
            .get(&DataKey::Landlord)
            .unwrap_or_else(|| panic_with_error!(&env, ContractError::NotInitialized));

        if landlord != stored_landlord {
            panic_with_error!(&env, ContractError::Unauthorized);
        }

        let total = get_total(&env);
        let target = get_rent_target(&env);

        if total < target {
            panic_with_error!(&env, ContractError::InsufficientBalance);
        }

        let token_address: Address = env
            .storage()
            .instance()
            .get(&DataKey::Token)
            .unwrap_or_else(|| panic_with_error!(&env, ContractError::NotInitialized));
        let contract_address = env.current_contract_address();

        token::Client::new(&env, &token_address).transfer(
            &contract_address,
            &landlord,
            &total,
        );

        env.storage()
            .instance()
            .set(&DataKey::Released, &true);

        env.events().publish((RELEASED,), total);
    }

    // -----------------------------------------------------------------------
    // View functions
    // -----------------------------------------------------------------------

    pub fn get_contribution(env: Env, user: Address) -> i128 {
        require_initialized(&env);
        get_contribution(&env, &user)
    }

    pub fn get_total_contributions(env: Env) -> i128 {
        require_initialized(&env);
        get_total(&env)
    }

    pub fn get_rent_target(env: Env) -> i128 {
        require_initialized(&env);
        get_rent_target(&env)
    }

    pub fn is_released(env: Env) -> bool {
        require_initialized(&env);
        is_released(&env)
    }
}

#[cfg(test)]
mod test;
