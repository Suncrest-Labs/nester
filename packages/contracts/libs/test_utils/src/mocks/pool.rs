use soroban_sdk::{
    contract, contractimpl, contracttype, panic_with_error, token, Address, Env,
};

use nester_common::ContractError;

// ---------------------------------------------------------------------------
// MockAmmPool — Soroswap-style liquidity pool.
//
// Single-token-in mock: deposits mint LP shares pro-rata against reserve_a,
// withdrawals burn shares for a pro-rata slice of reserve_a. reserve_b exists
// so LP valuation math (2 * pro-rata of reserve_a) matches the real venue's
// shape. No protocol-exposed rate — adapters must derive APY from growth.
// ---------------------------------------------------------------------------

#[contracttype]
#[derive(Clone)]
enum PoolKey {
    TokenA,
    ReserveA,
    ReserveB,
    TotalShares,
    Shares(Address),
}

#[contract]
pub struct MockAmmPool;

#[contractimpl]
impl MockAmmPool {
    pub fn initialize(env: Env, token_a: Address, reserve_a: i128, reserve_b: i128) {
        env.storage().instance().set(&PoolKey::TokenA, &token_a);
        env.storage().instance().set(&PoolKey::ReserveA, &reserve_a);
        env.storage().instance().set(&PoolKey::ReserveB, &reserve_b);
        env.storage()
            .instance()
            .set(&PoolKey::TotalShares, &reserve_a);
    }

    /// Deposit `amount` of token A, minting LP shares pro-rata.
    pub fn deposit(env: Env, from: Address, amount: i128) -> i128 {
        if amount <= 0 {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }
        // Push model: the caller has already transferred `amount` to this
        // contract. See the note in the lending mock.
        let reserve_a: i128 = env.storage().instance().get(&PoolKey::ReserveA).unwrap();
        let total: i128 = env.storage().instance().get(&PoolKey::TotalShares).unwrap();
        let shares = if reserve_a == 0 {
            amount
        } else {
            amount * total / reserve_a
        };

        env.storage()
            .instance()
            .set(&PoolKey::ReserveA, &(reserve_a + amount));
        env.storage()
            .instance()
            .set(&PoolKey::TotalShares, &(total + shares));
        let prev: i128 = env
            .storage()
            .instance()
            .get(&PoolKey::Shares(from.clone()))
            .unwrap_or(0);
        env.storage()
            .instance()
            .set(&PoolKey::Shares(from), &(prev + shares));
        shares
    }

    /// Burn `shares`, paying out a pro-rata slice of reserve A.
    pub fn withdraw(env: Env, owner: Address, to: Address, shares: i128) -> i128 {
        if shares <= 0 {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }
        let held: i128 = env
            .storage()
            .instance()
            .get(&PoolKey::Shares(owner.clone()))
            .unwrap_or(0);
        if shares > held {
            panic_with_error!(&env, ContractError::InsufficientBalance);
        }

        let reserve_a: i128 = env.storage().instance().get(&PoolKey::ReserveA).unwrap();
        let total: i128 = env.storage().instance().get(&PoolKey::TotalShares).unwrap();
        let amount = shares * reserve_a / total;

        env.storage()
            .instance()
            .set(&PoolKey::ReserveA, &(reserve_a - amount));
        env.storage()
            .instance()
            .set(&PoolKey::TotalShares, &(total - shares));
        env.storage()
            .instance()
            .set(&PoolKey::Shares(owner), &(held - shares));

        let token_a: Address = env.storage().instance().get(&PoolKey::TokenA).unwrap();
        token::Client::new(&env, &token_a).transfer(
            &env.current_contract_address(),
            &to,
            &amount,
        );
        amount
    }

    pub fn get_reserves(env: Env) -> (i128, i128) {
        (
            env.storage().instance().get(&PoolKey::ReserveA).unwrap(),
            env.storage().instance().get(&PoolKey::ReserveB).unwrap(),
        )
    }

    pub fn total_shares(env: Env) -> i128 {
        env.storage().instance().get(&PoolKey::TotalShares).unwrap()
    }

    pub fn shares_of(env: Env, owner: Address) -> i128 {
        env.storage()
            .instance()
            .get(&PoolKey::Shares(owner))
            .unwrap_or(0)
    }

    /// Simulate swap-fee growth: inflate reserve A without minting shares, so
    /// each share is worth more. Mint matching tokens to the pool in tests if
    /// the grown value will actually be withdrawn.
    pub fn simulate_fee_growth(env: Env, amount: i128) {
        let reserve_a: i128 = env.storage().instance().get(&PoolKey::ReserveA).unwrap();
        env.storage()
            .instance()
            .set(&PoolKey::ReserveA, &(reserve_a + amount));
    }
}

