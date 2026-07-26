//! Lending-market yield adapter (Blend-style).
//!
//! Implements [`nester_common::adapters::YieldAdapter`] against a lending
//! market with the simple deposit / accrue / withdraw shape. The protocol
//! exposes a supply rate, so `current_apy` is `ProtocolReported`.
//!
//! The adapter is stateless with respect to depositors: it holds one
//! aggregate position on behalf of the vault only. Only the configured vault
//! can move value; anyone can read.

#![no_std]

use soroban_sdk::{
    contract, contractimpl, contracttype, panic_with_error, symbol_short, token, Address, Env,
    Symbol, Vec, IntoVal, Val,
};

use nester_common::adapters::{AdapterApy, ApyConfidence, YieldAdapter};
use nester_common::{emit_event, ContractError};

const ADAPTER: Symbol = symbol_short!("ADAPTER");
const DEPOSITED: Symbol = symbol_short!("DEPOSIT");
const WITHDRAWN: Symbol = symbol_short!("WITHDRAW");

#[contracttype]
#[derive(Clone)]
enum DataKey {
    /// Vault address — the only caller allowed to move value.
    Vault,
    /// External lending protocol contract.
    Protocol,
    /// Underlying asset accepted/paid by this adapter.
    Underlying,
    /// Aggregate position units held at the protocol.
    Units,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct AdapterMovedEventData {
    pub amount: i128,
    pub units: i128,
}

#[contract]
pub struct LendingAdapterContract;

#[contractimpl]
impl LendingAdapterContract {
    /// One-time wiring. No admin key is kept: the adapter is immutable after
    /// initialization, keeping its trust surface minimal.
    pub fn initialize(env: Env, vault: Address, protocol: Address, underlying: Address) {
        if env.storage().instance().has(&DataKey::Vault) {
            panic_with_error!(&env, ContractError::AlreadyInitialized);
        }
        env.storage().instance().set(&DataKey::Vault, &vault);
        env.storage().instance().set(&DataKey::Protocol, &protocol);
        env.storage().instance().set(&DataKey::Underlying, &underlying);
        env.storage().instance().set(&DataKey::Units, &0i128);
    }

    pub fn get_vault(env: Env) -> Address {
        env.storage().instance().get(&DataKey::Vault).unwrap()
    }

    pub fn get_protocol(env: Env) -> Address {
        env.storage().instance().get(&DataKey::Protocol).unwrap()
    }
}

#[contractimpl]
impl YieldAdapter for LendingAdapterContract {
    fn deposit(env: Env, from: Address, amount: i128, min_units_out: i128) -> i128 {
        from.require_auth();
        require_vault(&env, &from);
        if amount <= 0 {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }

        let underlying: Address = env.storage().instance().get(&DataKey::Underlying).unwrap();
        let protocol: Address = env.storage().instance().get(&DataKey::Protocol).unwrap();
        let me = env.current_contract_address();

        // Push the assets straight to the protocol, then tell it to credit
        // this adapter's position. Routing them through the adapter first
        // would make the protocol's pull a nested transfer authorized by a
        // non-root address, which Soroban's auth model rejects.
        token::Client::new(&env, &underlying).transfer(&from, &protocol, &amount);
        let units: i128 = env.invoke_contract(
            &protocol,
            &Symbol::new(&env, "deposit"),
            soroban_sdk::vec![&env, me.into_val(&env), amount.into_val(&env)],
        );

        if units < min_units_out {
            panic_with_error!(&env, ContractError::SlippageExceeded);
        }

        let held: i128 = env.storage().instance().get(&DataKey::Units).unwrap_or(0);
        env.storage().instance().set(&DataKey::Units, &(held + units));

        emit_event(
            &env,
            ADAPTER,
            DEPOSITED,
            from,
            AdapterMovedEventData { amount, units },
        );
        units
    }

    fn withdraw(env: Env, to: Address, units: i128, min_out: i128) -> i128 {
        let vault: Address = env.storage().instance().get(&DataKey::Vault).unwrap();
        vault.require_auth();
        if units <= 0 {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }
        let held: i128 = env.storage().instance().get(&DataKey::Units).unwrap_or(0);
        if units > held {
            panic_with_error!(&env, ContractError::InsufficientBalance);
        }

        let underlying: Address = env.storage().instance().get(&DataKey::Underlying).unwrap();
        let protocol: Address = env.storage().instance().get(&DataKey::Protocol).unwrap();
        let me = env.current_contract_address();

        let assets: i128 = env.invoke_contract(
            &protocol,
            &Symbol::new(&env, "withdraw"),
            soroban_sdk::vec![
                &env,
                me.clone().into_val(&env),
                me.clone().into_val(&env),
                units.into_val(&env),
            ],
        );

        if assets < min_out {
            panic_with_error!(&env, ContractError::SlippageExceeded);
        }

        env.storage().instance().set(&DataKey::Units, &(held - units));
        token::Client::new(&env, &underlying).transfer(&me, &to, &assets);

        emit_event(
            &env,
            ADAPTER,
            WITHDRAWN,
            to,
            AdapterMovedEventData {
                amount: assets,
                units,
            },
        );
        assets
    }

    /// Asset value of the aggregate position. `_owner` is ignored: the
    /// adapter holds one position, for the vault.
    fn position_value(env: Env, _owner: Address) -> i128 {
        let protocol: Address = env.storage().instance().get(&DataKey::Protocol).unwrap();
        let me = env.current_contract_address();
        env.invoke_contract(
            &protocol,
            &Symbol::new(&env, "position_value"),
            soroban_sdk::vec![&env, me.into_val(&env)],
        )
    }

    /// The lending market exposes a supply rate, so read it directly.
    /// If the protocol call fails, report `Unavailable` rather than panicking:
    /// the read path must never brick registry refreshes.
    fn current_apy(env: Env) -> AdapterApy {
        let protocol: Address = env.storage().instance().get(&DataKey::Protocol).unwrap();
        let args: Vec<Val> = Vec::new(&env);
        match env.try_invoke_contract::<u32, ContractError>(
            &protocol,
            &Symbol::new(&env, "supply_rate"),
            args,
        ) {
            Ok(Ok(rate_bps)) => AdapterApy {
                apy_bps: rate_bps,
                confidence: ApyConfidence::ProtocolReported,
            },
            _ => AdapterApy {
                apy_bps: 0,
                confidence: ApyConfidence::Unavailable,
            },
        }
    }

    fn underlying(env: Env) -> Address {
        env.storage().instance().get(&DataKey::Underlying).unwrap()
    }

    fn max_deposit(env: Env) -> i128 {
        let protocol: Address = env.storage().instance().get(&DataKey::Protocol).unwrap();
        let args: Vec<Val> = Vec::new(&env);
        match env.try_invoke_contract::<i128, ContractError>(
            &protocol,
            &Symbol::new(&env, "max_deposit"),
            args,
        ) {
            Ok(Ok(v)) => v,
            // A protocol that can't report capacity gets no new deposits.
            _ => 0,
        }
    }

    /// Withdrawable maximum in position units — everything we hold.
    fn max_withdraw(env: Env) -> i128 {
        env.storage().instance().get(&DataKey::Units).unwrap_or(0)
    }
}

fn require_vault(env: &Env, caller: &Address) {
    let vault: Address = env.storage().instance().get(&DataKey::Vault).unwrap();
    if caller != &vault {
        panic_with_error!(env, ContractError::Unauthorized);
    }
}

#[cfg(test)]
mod test;
