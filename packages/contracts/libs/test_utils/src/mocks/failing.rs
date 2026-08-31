use soroban_sdk::{contract, contractimpl, panic_with_error, Address, Env};

use nester_common::adapters::AdapterApy;
use nester_common::ContractError;

// ---------------------------------------------------------------------------
// MockFailingAdapter — implements the YieldAdapter surface, always reverts.
//
// Used by the failure-isolation integration test: rebalance must skip this
// source and complete across the remainder.
// ---------------------------------------------------------------------------

#[contract]
pub struct MockFailingAdapter;

#[contractimpl]
impl MockFailingAdapter {
    pub fn deposit(env: Env, _from: Address, _amount: i128, _min_units_out: i128) -> i128 {
        panic_with_error!(&env, ContractError::InvalidOperation)
    }

    pub fn withdraw(env: Env, _to: Address, _units: i128, _min_out: i128) -> i128 {
        panic_with_error!(&env, ContractError::InvalidOperation)
    }

    pub fn position_value(env: Env, _owner: Address) -> i128 {
        panic_with_error!(&env, ContractError::InvalidOperation)
    }

    pub fn current_apy(env: Env) -> AdapterApy {
        // Even the read path reverts — a totally bricked adapter.
        panic_with_error!(&env, ContractError::InvalidOperation)
    }

    pub fn underlying(env: Env) -> Address {
        panic_with_error!(&env, ContractError::InvalidOperation)
    }

    pub fn max_deposit(env: Env) -> i128 {
        panic_with_error!(&env, ContractError::InvalidOperation)
    }

    pub fn max_withdraw(env: Env) -> i128 {
        panic_with_error!(&env, ContractError::InvalidOperation)
    }
}

