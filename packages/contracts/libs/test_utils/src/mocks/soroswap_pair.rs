//! Mock Soroswap router and pair.
//!
//! Shaped after the deployed testnet contracts rather than after what an
//! adapter would find convenient. `MockAmmPool` exposes
//! `deposit(address, amount)`, which is what `adapter_pool` assumes; a real
//! Soroswap pair takes `deposit(to)` with no amount and mints from the balance
//! that arrived. Testing against the convenient shape is how that adapter
//! stayed green while being unable to drive any deployed pair.
//!
//! Fidelity covers what the adapter depends on: the router's `add_liquidity`,
//! `remove_liquidity` and `swap_exact_tokens_for_tokens`, and the pair's
//! `get_reserves`, `total_supply` and `token_0`. Pricing is a constant-product
//! curve so swap output moves with the reserves, as it does on-chain.

use soroban_sdk::{contract, contractimpl, contracttype, token, Address, Env, Vec};

#[contracttype]
#[derive(Clone)]
enum PairKey {
    Token0,
    Token1,
    Reserve0,
    Reserve1,
    TotalSupply,
    Balance(Address),
}

#[contract]
pub struct MockSoroswapPair;

#[contractimpl]
impl MockSoroswapPair {
    pub fn initialize(env: Env, token_0: Address, token_1: Address) {
        env.storage().instance().set(&PairKey::Token0, &token_0);
        env.storage().instance().set(&PairKey::Token1, &token_1);
        env.storage().instance().set(&PairKey::Reserve0, &0i128);
        env.storage().instance().set(&PairKey::Reserve1, &0i128);
        env.storage().instance().set(&PairKey::TotalSupply, &0i128);
    }

    pub fn token_0(env: Env) -> Address {
        env.storage().instance().get(&PairKey::Token0).unwrap()
    }

    pub fn token_1(env: Env) -> Address {
        env.storage().instance().get(&PairKey::Token1).unwrap()
    }

    pub fn get_reserves(env: Env) -> (i128, i128) {
        (
            env.storage().instance().get(&PairKey::Reserve0).unwrap_or(0),
            env.storage().instance().get(&PairKey::Reserve1).unwrap_or(0),
        )
    }

    pub fn total_supply(env: Env) -> i128 {
        env.storage()
            .instance()
            .get(&PairKey::TotalSupply)
            .unwrap_or(0)
    }

    pub fn balance(env: Env, id: Address) -> i128 {
        env.storage()
            .instance()
            .get(&PairKey::Balance(id))
            .unwrap_or(0)
    }

    pub fn approve(_env: Env, _from: Address, _spender: Address, _amount: i128, _expiration: u32) {}

    /// Router-only bookkeeping: credit LP units and grow reserves.
    pub fn mint_for(env: Env, to: Address, amount_0: i128, amount_1: i128) -> i128 {
        let r0: i128 = env.storage().instance().get(&PairKey::Reserve0).unwrap_or(0);
        let r1: i128 = env.storage().instance().get(&PairKey::Reserve1).unwrap_or(0);
        let supply: i128 = env
            .storage()
            .instance()
            .get(&PairKey::TotalSupply)
            .unwrap_or(0);

        // First provider sets the price; later ones mint pro-rata, matching a
        // constant-product pair.
        let minted = if supply == 0 {
            amount_0
        } else {
            amount_0 * supply / r0.max(1)
        };

        env.storage().instance().set(&PairKey::Reserve0, &(r0 + amount_0));
        env.storage().instance().set(&PairKey::Reserve1, &(r1 + amount_1));
        env.storage()
            .instance()
            .set(&PairKey::TotalSupply, &(supply + minted));
        let held = Self::balance(env.clone(), to.clone());
        env.storage()
            .instance()
            .set(&PairKey::Balance(to), &(held + minted));
        minted
    }

    /// Router-only bookkeeping: burn LP units and shrink reserves pro-rata.
    pub fn burn_for(env: Env, from: Address, units: i128) -> (i128, i128) {
        let r0: i128 = env.storage().instance().get(&PairKey::Reserve0).unwrap_or(0);
        let r1: i128 = env.storage().instance().get(&PairKey::Reserve1).unwrap_or(0);
        let supply: i128 = env
            .storage()
            .instance()
            .get(&PairKey::TotalSupply)
            .unwrap_or(0);
        if supply <= 0 {
            return (0, 0);
        }

        let amount_0 = units * r0 / supply;
        let amount_1 = units * r1 / supply;

        env.storage().instance().set(&PairKey::Reserve0, &(r0 - amount_0));
        env.storage().instance().set(&PairKey::Reserve1, &(r1 - amount_1));
        env.storage()
            .instance()
            .set(&PairKey::TotalSupply, &(supply - units));
        let held = Self::balance(env.clone(), from.clone());
        env.storage()
            .instance()
            .set(&PairKey::Balance(from), &(held - units));
        (amount_0, amount_1)
    }

    /// Pays `amount` of `token` out of the pair. Router-only bookkeeping.
    pub fn pay_out(env: Env, token: Address, to: Address, amount: i128) {
        token::Client::new(&env, &token).transfer(&env.current_contract_address(), &to, &amount);
    }

    /// Applies a constant-product swap to the reserves and reports the output.
    pub fn swap_reserves(env: Env, zero_for_one: bool, amount_in: i128) -> i128 {
        let r0: i128 = env.storage().instance().get(&PairKey::Reserve0).unwrap_or(0);
        let r1: i128 = env.storage().instance().get(&PairKey::Reserve1).unwrap_or(0);
        let (reserve_in, reserve_out) = if zero_for_one { (r0, r1) } else { (r1, r0) };
        if reserve_in <= 0 || reserve_out <= 0 {
            return 0;
        }

        // x*y=k with the same 0.3% fee Soroswap charges.
        let amount_in_with_fee = amount_in * 997;
        let out = amount_in_with_fee * reserve_out / (reserve_in * 1000 + amount_in_with_fee);

        if zero_for_one {
            env.storage().instance().set(&PairKey::Reserve0, &(r0 + amount_in));
            env.storage().instance().set(&PairKey::Reserve1, &(r1 - out));
        } else {
            env.storage().instance().set(&PairKey::Reserve1, &(r1 + amount_in));
            env.storage().instance().set(&PairKey::Reserve0, &(r0 - out));
        }
        out
    }
}

