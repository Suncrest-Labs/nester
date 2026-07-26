use soroban_sdk::{
    contract, contractimpl, contracttype, panic_with_error, token, Address, Env,
};

use nester_common::ContractError;


// ---------------------------------------------------------------------------
// MockLendingProtocol — Blend-style lending market.
//
// Deposit / accrue / withdraw with an exchange index. Units are minted at the
// current index and grow in value as `accrue` advances the index. Exposes a
// protocol-reported supply rate like a real lending market.
// ---------------------------------------------------------------------------

#[contracttype]
#[derive(Clone)]
enum LendKey {
    Token,
    /// Exchange index in bps: assets = units * index / 10_000.
    IndexBps,
    /// Protocol-reported supply rate (bps).
    SupplyRateBps,
    /// Position units per depositor address.
    Units(Address),
    /// Optional deposit cap. 0 = uncapped.
    DepositCap,
}

const INDEX_ONE: i128 = 10_000;

#[contract]
pub struct MockLendingProtocol;

#[contractimpl]
impl MockLendingProtocol {
    pub fn initialize(env: Env, token: Address, supply_rate_bps: u32) {
        env.storage().instance().set(&LendKey::Token, &token);
        env.storage().instance().set(&LendKey::IndexBps, &INDEX_ONE);
        env.storage()
            .instance()
            .set(&LendKey::SupplyRateBps, &supply_rate_bps);
    }

    /// Deposit underlying; mints position units at the current index.
    pub fn deposit(env: Env, from: Address, amount: i128) -> i128 {
        if amount <= 0 {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }
        let cap: i128 = env
            .storage()
            .instance()
            .get(&LendKey::DepositCap)
            .unwrap_or(0);
        if cap != 0 && amount > cap {
            panic_with_error!(&env, ContractError::ExceedsLimit);
        }

        // Push model: the caller has already transferred `amount` to this
        // contract. Pulling it here would be a nested transfer authorized by a
        // non-root address, which Soroban's recording auth rejects.
        let index: i128 = env.storage().instance().get(&LendKey::IndexBps).unwrap();
        let units = amount * INDEX_ONE / index;

        let prev: i128 = env
            .storage()
            .instance()
            .get(&LendKey::Units(from.clone()))
            .unwrap_or(0);
        env.storage()
            .instance()
            .set(&LendKey::Units(from), &(prev + units));
        units
    }

    /// Burn `units` and pay out underlying at the current index.
    pub fn withdraw(env: Env, owner: Address, to: Address, units: i128) -> i128 {
        if units <= 0 {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }
        let held: i128 = env
            .storage()
            .instance()
            .get(&LendKey::Units(owner.clone()))
            .unwrap_or(0);
        if units > held {
            panic_with_error!(&env, ContractError::InsufficientBalance);
        }

        let index: i128 = env.storage().instance().get(&LendKey::IndexBps).unwrap();
        let assets = units * index / INDEX_ONE;

        env.storage()
            .instance()
            .set(&LendKey::Units(owner), &(held - units));

        let token_id: Address = env.storage().instance().get(&LendKey::Token).unwrap();
        token::Client::new(&env, &token_id).transfer(
            &env.current_contract_address(),
            &to,
            &assets,
        );
        assets
    }

    /// Position units held by `owner`.
    pub fn units_of(env: Env, owner: Address) -> i128 {
        env.storage()
            .instance()
            .get(&LendKey::Units(owner))
            .unwrap_or(0)
    }

    /// Asset-denominated value of `owner`'s position at the current index.
    pub fn position_value(env: Env, owner: Address) -> i128 {
        let units: i128 = env
            .storage()
            .instance()
            .get(&LendKey::Units(owner))
            .unwrap_or(0);
        let index: i128 = env.storage().instance().get(&LendKey::IndexBps).unwrap();
        units * index / INDEX_ONE
    }

    /// Protocol-reported supply rate, bps.
    pub fn supply_rate(env: Env) -> u32 {
        env.storage()
            .instance()
            .get(&LendKey::SupplyRateBps)
            .unwrap_or(0)
    }

    pub fn set_supply_rate(env: Env, rate_bps: u32) {
        env.storage()
            .instance()
            .set(&LendKey::SupplyRateBps, &rate_bps);
    }

    /// Advance the exchange index by `growth_bps` to simulate interest accrual.
    /// NOTE: only inflates the index; mint matching tokens to this contract in
    /// tests if withdrawals of the accrued value are exercised.
    pub fn accrue(env: Env, growth_bps: u32) {
        let index: i128 = env.storage().instance().get(&LendKey::IndexBps).unwrap();
        let new_index = index + index * growth_bps as i128 / 10_000;
        env.storage().instance().set(&LendKey::IndexBps, &new_index);
    }

    /// Set a per-deposit cap. 0 = uncapped.
    pub fn set_deposit_cap(env: Env, cap: i128) {
        env.storage().instance().set(&LendKey::DepositCap, &cap);
    }

    pub fn max_deposit(env: Env) -> i128 {
        let cap: i128 = env
            .storage()
            .instance()
            .get(&LendKey::DepositCap)
            .unwrap_or(0);
        if cap == 0 {
            i128::MAX
        } else {
            cap
        }
    }
}

