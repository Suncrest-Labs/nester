//! Single-asset DeFindex vault mock.
//!
//! Implements only the subset of the DeFindex vault interface that
//! `adapter_defindex` calls, with the exact names and argument types of
//! `defindex-io/stellar-contracts` `vault/src/lib.rs` (deployed testnet vault
//! WASM hash `f345228d…16be`): `deposit`, `withdraw`,
//! `get_asset_amounts_per_shares`, and the SEP-41 `balance` / `total_supply`
//! of the vault's share token. `adapter_defindex`'s `spec_test.rs` checks
//! these signatures against the real WASM.
//!
//! Share accounting follows DeFindex's single-asset path: the first deposit
//! mints shares 1:1 and locks `MINIMUM_LIQUIDITY` of them to the vault
//! itself; later deposits mint `supply * amount / total` and withdrawals pay
//! `total * shares / supply`, both rounded down. `total` is the underlying the
//! mock holds, so value grows through the `simulate_yield` hook. Strategies
//! are not modelled: `invest` is accepted and ignored.

use soroban_sdk::{
    contract, contracterror, contractimpl, contracttype, token, vec, Address, Env, Vec,
};

const MINIMUM_LIQUIDITY: i128 = 1000;

/// DeFindex's error codes for the paths this mock models.
#[contracterror]
#[derive(Copy, Clone, Debug, Eq, PartialEq, PartialOrd, Ord)]
#[repr(u32)]
pub enum ContractError {
    AmountNotAllowed = 110,
    WrongAmountsLength = 112,
    InsufficientAmount = 117,
    NoOptimalAmounts = 118,
    AmountOverTotalSupply = 124,
    InsufficientOutputAmount = 160,
}

/// DeFindex `StrategyAllocation`, part of `deposit`'s return type.
#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct StrategyAllocation {
    pub strategy_address: Address,
    pub amount: i128,
    pub paused: bool,
}

/// DeFindex `AssetInvestmentAllocation`, part of `deposit`'s return type.
#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AssetInvestmentAllocation {
    pub asset: Address,
    pub strategy_allocations: Vec<Option<StrategyAllocation>>,
}

#[contracttype]
enum DataKey {
    Asset,
    TotalSupply,
    Balance(Address),
}

fn asset(env: &Env) -> Address {
    env.storage().instance().get(&DataKey::Asset).unwrap()
}

fn total_managed(env: &Env) -> i128 {
    token::Client::new(env, &asset(env)).balance(&env.current_contract_address())
}

fn supply(env: &Env) -> i128 {
    env.storage()
        .instance()
        .get(&DataKey::TotalSupply)
        .unwrap_or(0)
}

fn shares_of(env: &Env, id: &Address) -> i128 {
    env.storage()
        .persistent()
        .get(&DataKey::Balance(id.clone()))
        .unwrap_or(0)
}

fn mint(env: &Env, to: &Address, amount: i128) {
    env.storage().persistent().set(
        &DataKey::Balance(to.clone()),
        &(shares_of(env, to) + amount),
    );
    env.storage()
        .instance()
        .set(&DataKey::TotalSupply, &(supply(env) + amount));
}

fn burn(env: &Env, from: &Address, amount: i128) {
    let held = shares_of(env, from);
    if held < amount {
        panic!("insufficient balance");
    }
    env.storage()
        .persistent()
        .set(&DataKey::Balance(from.clone()), &(held - amount));
    env.storage()
        .instance()
        .set(&DataKey::TotalSupply, &(supply(env) - amount));
}

#[contract]
pub struct MockDefindexVault;

#[contractimpl]
impl MockDefindexVault {
    pub fn initialize(env: Env, asset: Address) {
        env.storage().instance().set(&DataKey::Asset, &asset);
    }

    /// Test hook: `from` donates `amount` of the underlying, raising the value
    /// of every share the way strategy yield does.
    pub fn simulate_yield(env: Env, from: Address, amount: i128) {
        from.require_auth();
        token::Client::new(&env, &asset(&env)).transfer(
            &from,
            &env.current_contract_address(),
            &amount,
        );
    }

    // DeFindex's exact return type; an alias would change the contract spec.
    #[allow(clippy::type_complexity)]
    pub fn deposit(
        env: Env,
        amounts_desired: Vec<i128>,
        amounts_min: Vec<i128>,
        from: Address,
        invest: bool,
    ) -> Result<
        (
            Vec<i128>,
            i128,
            Option<Vec<Option<AssetInvestmentAllocation>>>,
        ),
        ContractError,
    > {
        let _ = invest;
        from.require_auth();
        if amounts_desired.len() != 1 || amounts_min.len() != 1 {
            return Err(ContractError::WrongAmountsLength);
        }
        let amount = amounts_desired.get(0).unwrap();
        if amount < 0 {
            return Err(ContractError::AmountNotAllowed);
        }
        let total_supply = supply(&env);
        let shares = if total_supply == 0 {
            amount
        } else {
            total_supply * amount / total_managed(&env)
        };
        if amount < amounts_min.get(0).unwrap() {
            return Err(ContractError::NoOptimalAmounts);
        }
        let me = env.current_contract_address();
        if amount > 0 {
            token::Client::new(&env, &asset(&env)).transfer(&from, &me, &amount);
        }
        if shares <= 0 || (total_supply == 0 && shares <= MINIMUM_LIQUIDITY) {
            return Err(ContractError::InsufficientAmount);
        }
        if total_supply == 0 {
            mint(&env, &me, MINIMUM_LIQUIDITY);
            mint(&env, &from, shares - MINIMUM_LIQUIDITY);
        } else {
            mint(&env, &from, shares);
        }
        Ok((amounts_desired, shares, None))
    }

    pub fn withdraw(
        env: Env,
        withdraw_shares: i128,
        min_amounts_out: Vec<i128>,
        from: Address,
    ) -> Result<Vec<i128>, ContractError> {
        from.require_auth();
        if withdraw_shares <= 0 {
            return Err(ContractError::AmountNotAllowed);
        }
        if min_amounts_out.len() != 1 {
            return Err(ContractError::WrongAmountsLength);
        }
        let min_out = min_amounts_out.get(0).unwrap();
        if min_out < 0 {
            return Err(ContractError::AmountNotAllowed);
        }
        let total_supply = supply(&env);
        if withdraw_shares > total_supply {
            return Err(ContractError::AmountOverTotalSupply);
        }
        let amount = total_managed(&env) * withdraw_shares / total_supply;
        burn(&env, &from, withdraw_shares);
        if amount < min_out {
            return Err(ContractError::InsufficientOutputAmount);
        }
        if amount > 0 {
            token::Client::new(&env, &asset(&env)).transfer(
                &env.current_contract_address(),
                &from,
                &amount,
            );
        }
        Ok(vec![&env, amount])
    }

    pub fn get_asset_amounts_per_shares(
        env: Env,
        vault_shares: i128,
    ) -> Result<Vec<i128>, ContractError> {
        let total_supply = supply(&env);
        if total_supply == 0 || vault_shares > total_supply {
            return Err(ContractError::AmountOverTotalSupply);
        }
        Ok(vec![
            &env,
            total_managed(&env) * vault_shares / total_supply,
        ])
    }

    pub fn balance(env: Env, id: Address) -> i128 {
        shares_of(&env, &id)
    }

    pub fn total_supply(env: Env) -> i128 {
        supply(&env)
    }
}
