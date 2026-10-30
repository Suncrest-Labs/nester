use soroban_sdk::{contract, contractimpl, contracttype, panic_with_error, token, Address, Env};

use nester_common::adapters::{AdapterApy, ApyConfidence};
use nester_common::ContractError;

// ---------------------------------------------------------------------------
// MockYieldAdapter — a YieldAdapter with a test-controlled position value.
//
// `deposit` is an authorized pull (debits `amount` from `from` inside the
// call) and mints units pro-rata to the current value; `withdraw` burns units
// and pays `units * value / total_units` from the mock's own token balance.
// `set_value` moves the position value up or down to simulate yield or loss;
// any value above what was deposited must be backed by minting tokens to the
// mock if the test withdraws it. `set_failing` makes every call revert;
// `set_withdraw_failing` makes only `withdraw` revert (locked liquidity).
// ---------------------------------------------------------------------------

#[contracttype]
#[derive(Clone)]
enum MockKey {
    Underlying,
    Units,
    Value,
    Failing,
    WithdrawFailing,
}

#[contract]
pub struct MockYieldAdapter;

fn get(env: &Env, key: &MockKey) -> i128 {
    env.storage().instance().get(key).unwrap_or(0)
}

fn flag(env: &Env, key: &MockKey) -> bool {
    env.storage().instance().get(key).unwrap_or(false)
}

fn fail_if(env: &Env, key: &MockKey) {
    if flag(env, key) {
        panic_with_error!(env, ContractError::InvalidOperation);
    }
}

fn underlying_client(env: &Env) -> token::Client<'_> {
    let underlying: Address = env.storage().instance().get(&MockKey::Underlying).unwrap();
    token::Client::new(env, &underlying)
}

#[contractimpl]
impl MockYieldAdapter {
    pub fn initialize(env: Env, underlying: Address) {
        env.storage()
            .instance()
            .set(&MockKey::Underlying, &underlying);
    }

    pub fn set_value(env: Env, value: i128) {
        env.storage().instance().set(&MockKey::Value, &value);
    }

    pub fn set_failing(env: Env, failing: bool) {
        env.storage().instance().set(&MockKey::Failing, &failing);
    }

    pub fn set_withdraw_failing(env: Env, failing: bool) {
        env.storage()
            .instance()
            .set(&MockKey::WithdrawFailing, &failing);
    }

    pub fn deposit(env: Env, from: Address, amount: i128, min_units_out: i128) -> i128 {
        fail_if(&env, &MockKey::Failing);
        if amount <= 0 {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }
        underlying_client(&env).transfer(&from, &env.current_contract_address(), &amount);
        let units = get(&env, &MockKey::Units);
        let value = get(&env, &MockKey::Value);
        let minted = if units == 0 || value == 0 {
            amount
        } else {
            amount * units / value
        };
        if minted < min_units_out {
            panic_with_error!(&env, ContractError::SlippageExceeded);
        }
        env.storage()
            .instance()
            .set(&MockKey::Units, &(units + minted));
        env.storage()
            .instance()
            .set(&MockKey::Value, &(value + amount));
        minted
    }

    pub fn withdraw(env: Env, to: Address, units: i128, min_out: i128) -> i128 {
        fail_if(&env, &MockKey::Failing);
        fail_if(&env, &MockKey::WithdrawFailing);
        let held = get(&env, &MockKey::Units);
        if units <= 0 || units > held {
            panic_with_error!(&env, ContractError::InsufficientBalance);
        }
        let value = get(&env, &MockKey::Value);
        let out = if units == held {
            value
        } else {
            units * value / held
        };
        if out < min_out {
            panic_with_error!(&env, ContractError::SlippageExceeded);
        }
        env.storage()
            .instance()
            .set(&MockKey::Units, &(held - units));
        env.storage()
            .instance()
            .set(&MockKey::Value, &(value - out));
        underlying_client(&env).transfer(&env.current_contract_address(), &to, &out);
        out
    }

    pub fn position_value(env: Env, _owner: Address) -> i128 {
        fail_if(&env, &MockKey::Failing);
        get(&env, &MockKey::Value)
    }

    pub fn current_apy(env: Env) -> AdapterApy {
        fail_if(&env, &MockKey::Failing);
        AdapterApy {
            apy_bps: 0,
            confidence: ApyConfidence::Unavailable,
        }
    }

    pub fn underlying(env: Env) -> Address {
        fail_if(&env, &MockKey::Failing);
        env.storage().instance().get(&MockKey::Underlying).unwrap()
    }

    pub fn max_deposit(env: Env) -> i128 {
        fail_if(&env, &MockKey::Failing);
        i128::MAX
    }

    pub fn max_withdraw(env: Env) -> i128 {
        fail_if(&env, &MockKey::Failing);
        get(&env, &MockKey::Units)
    }
}
