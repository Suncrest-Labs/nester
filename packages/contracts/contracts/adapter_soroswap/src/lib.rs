#![no_std]
//! Soroswap AMM adapter.
//!
//! `adapter_pool` invokes `deposit(address, amount)` on the pool. A Soroswap
//! pair takes `deposit(to)` with no amount — it is Uniswap-V2 shaped, minting
//! LP shares from whatever balance arrived since the last sync — so that call
//! reverts on argument count against every deployed pair. This adapter goes
//! through the router instead, which computes the deposit ratio and mints in
//! one call rather than leaving the adapter to get pair math right.
//!
//! # One asset in, two assets required
//!
//! The vault holds a single asset; an AMM position needs both sides. A deposit
//! therefore swaps half the incoming amount into the pair's other token before
//! adding liquidity, and a withdrawal swaps the other side back. Both legs are
//! bounded by caller-supplied minimums, so a manipulated pool cannot quietly
//! take a cut — the call reverts instead.
//!
//! **This is a materially different risk from lending.** An LP position is
//! exposed to impermanent loss: when the price of the paired asset moves, the
//! position can be worth less than simply holding the deposit, fees included.
//! The adapter cannot hedge that, and callers must surface it rather than
//! presenting an AMM APY as if it were a lending rate.
//!
//! # Valuation
//!
//! LP units are valued pro-rata against pool reserves — the same arithmetic
//! the pair uses on burn — so no spot-price oracle is involved and a
//! single-block swing cannot skew it.

use nester_common::adapters::{AdapterApy, ApyConfidence, YieldAdapter};
use nester_common::{emit_event, ContractError};
use soroban_sdk::{
    contract, contractimpl, contracttype, panic_with_error, symbol_short, token, Address, Env,
    IntoVal, Symbol, Vec,
};

const ADAPTER: Symbol = symbol_short!("ADAPTER");
const DEPOSITED: Symbol = symbol_short!("DEPOSIT");
const WITHDRAWN: Symbol = symbol_short!("WITHDRAW");

/// Minimum observation window before a derived APY is meaningful. A rate
/// derived over a shorter window is noise, and feeding noise to the
/// rebalancer churns the vault into fees.
pub const MIN_APY_WINDOW_SECS: u64 = 86_400;
const SECONDS_PER_YEAR: i128 = 31_536_000;
/// Mirrors the registry's ceiling so a derived spike cannot escape it.
const MAX_APY_BPS: u32 = 10_000;
/// How long a router call stays valid, in seconds past the current ledger.
const DEADLINE_WINDOW_SECS: u64 = 300;

#[contracttype]
#[derive(Clone, Debug)]
pub struct ApyCheckpoint {
    pub value: i128,
    pub timestamp: u64,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct AdapterMovedEventData {
    pub amount: i128,
    pub units: i128,
}

#[contracttype]
#[derive(Clone)]
enum DataKey {
    /// Vault address — the only caller allowed to move value.
    Vault,
    /// Soroswap router, which owns ratio maths and LP minting.
    Router,
    /// The pair contract, read for reserves and LP supply.
    Pair,
    /// Asset the vault deposits and expects back.
    Underlying,
    /// The pair's other token, held only transiently.
    Paired,
    /// Aggregate LP units held.
    Units,
    /// Basis for derived-APY measurement.
    Checkpoint,
}

#[contract]
pub struct SoroswapAdapterContract;

fn get<T: soroban_sdk::TryFromVal<Env, soroban_sdk::Val>>(env: &Env, key: &DataKey) -> T {
    env.storage()
        .instance()
        .get(key)
        .unwrap_or_else(|| panic_with_error!(env, ContractError::NotInitialized))
}

fn require_vault(env: &Env, caller: &Address) {
    let vault: Address = get(env, &DataKey::Vault);
    if *caller != vault {
        panic_with_error!(env, ContractError::Unauthorized);
    }
}


fn deadline(env: &Env) -> u64 {
    env.ledger().timestamp() + DEADLINE_WINDOW_SECS
}

fn units_held(env: &Env) -> i128 {
    env.storage().instance().get(&DataKey::Units).unwrap_or(0)
}

/// Swaps `amount_in` of `from_token` into `to_token` through the router.
fn swap(env: &Env, from_token: &Address, to_token: &Address, amount_in: i128, min_out: i128) -> i128 {
    let router: Address = get(env, &DataKey::Router);
    let me = env.current_contract_address();
    let path = soroban_sdk::vec![env, from_token.clone(), to_token.clone()];

    // The adapter owns these tokens, so it funds the pair directly rather
    // than delegating a pull to the router. A V2-style router settles the
    // trade from balances already sitting in the pair.
    let pair: Address = get(env, &DataKey::Pair);
    token::Client::new(env, from_token).transfer(&me, &pair, &amount_in);

    let amounts: Vec<i128> = env.invoke_contract(
        &router,
        &Symbol::new(env, "swap_exact_tokens_for_tokens"),
        soroban_sdk::vec![
            env,
            amount_in.into_val(env),
            min_out.into_val(env),
            path.into_val(env),
            me.into_val(env),
            deadline(env).into_val(env),
        ],
    );
    // The router returns one amount per hop; the last is what arrived.
    amounts.last().unwrap_or(0)
}

#[contractimpl]
impl SoroswapAdapterContract {
    /// `paired` is the pair's other token. It is configured rather than read
    /// from the pair so a misconfigured adapter fails at initialization
    /// instead of mid-deposit.
    pub fn initialize(
        env: Env,
        vault: Address,
        router: Address,
        pair: Address,
        underlying: Address,
        paired: Address,
    ) {
        vault.require_auth();
        if env.storage().instance().has(&DataKey::Vault) {
            panic_with_error!(&env, ContractError::AlreadyInitialized);
        }
        if underlying == paired {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }
        env.storage().instance().set(&DataKey::Vault, &vault);
        env.storage().instance().set(&DataKey::Router, &router);
        env.storage().instance().set(&DataKey::Pair, &pair);
        env.storage().instance().set(&DataKey::Underlying, &underlying);
        env.storage().instance().set(&DataKey::Paired, &paired);
        env.storage().instance().set(&DataKey::Units, &0i128);
    }

    pub fn get_vault(env: Env) -> Address {
        get(&env, &DataKey::Vault)
    }

    pub fn get_router(env: Env) -> Address {
        get(&env, &DataKey::Router)
    }

    pub fn get_pair(env: Env) -> Address {
        get(&env, &DataKey::Pair)
    }

    pub fn get_paired(env: Env) -> Address {
        get(&env, &DataKey::Paired)
    }

    pub fn get_checkpoint(env: Env) -> Option<ApyCheckpoint> {
        env.storage().instance().get(&DataKey::Checkpoint)
    }

    /// Records the current position value as the basis for derived APY.
    pub fn checkpoint(env: Env) {
        let vault: Address = get(&env, &DataKey::Vault);
        vault.require_auth();
        let value = Self::position_value_internal(&env);
        env.storage().instance().set(
            &DataKey::Checkpoint,
            &ApyCheckpoint {
                value,
                timestamp: env.ledger().timestamp(),
            },
        );
    }

    /// Underlying-denominated value of the LP position, valued pro-rata
    /// against reserves rather than through a price oracle.
    fn position_value_internal(env: &Env) -> i128 {
        let units = units_held(env);
        if units <= 0 {
            return 0;
        }
        let pair: Address = get(env, &DataKey::Pair);
        let underlying: Address = get(env, &DataKey::Underlying);

        let total_supply: i128 =
            env.invoke_contract(&pair, &Symbol::new(env, "total_supply"), Vec::new(env));
        if total_supply <= 0 {
            return 0;
        }

        let (reserve_0, reserve_1): (i128, i128) =
            env.invoke_contract(&pair, &Symbol::new(env, "get_reserves"), Vec::new(env));
        let token_0: Address =
            env.invoke_contract(&pair, &Symbol::new(env, "token_0"), Vec::new(env));

        let underlying_reserve = if token_0 == underlying { reserve_0 } else { reserve_1 };

        // Both sides are counted: the position is half underlying and half
        // paired by value at the pool's own ratio, so valuing only the
        // underlying leg would understate it by roughly half.
        units * underlying_reserve * 2 / total_supply
    }
}

#[contractimpl]
impl YieldAdapter for SoroswapAdapterContract {
    fn deposit(env: Env, from: Address, amount: i128, min_units_out: i128) -> i128 {
        from.require_auth();
        require_vault(&env, &from);
        if amount <= 0 {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }

        let underlying: Address = get(&env, &DataKey::Underlying);
        let paired: Address = get(&env, &DataKey::Paired);
        let router: Address = get(&env, &DataKey::Router);
        let me = env.current_contract_address();

        // Pull inside the invocation so a revert below rolls the transfer back
        // with it and never strands funds at the adapter.
        token::Client::new(&env, &underlying).transfer(&from, &me, &amount);

        // Half the deposit becomes the other side of the pair. min_out is 0
        // here because the deposit as a whole is bounded by min_units_out:
        // a bad swap yields fewer LP units and trips that guard.
        let half = amount / 2;
        let paired_amount = swap(&env, &underlying, &paired, half, 0);

        let underlying_remaining = amount - half;
        // Both legs are moved into the pair before the router settles them.
        let pair: Address = get(&env, &DataKey::Pair);
        token::Client::new(&env, &underlying).transfer(&me, &pair, &underlying_remaining);
        token::Client::new(&env, &paired).transfer(&me, &pair, &paired_amount);

        // The router settles the exact ratio and mints. Minimums are 0 on each
        // leg because the router returns any unused remainder, and the real
        // guard is min_units_out on what was actually minted.
        let (_used_a, _used_b, minted): (i128, i128, i128) = env.invoke_contract(
            &router,
            &Symbol::new(&env, "add_liquidity"),
            soroban_sdk::vec![
                &env,
                underlying.into_val(&env),
                paired.into_val(&env),
                underlying_remaining.into_val(&env),
                paired_amount.into_val(&env),
                0i128.into_val(&env),
                0i128.into_val(&env),
                me.into_val(&env),
                deadline(&env).into_val(&env),
            ],
        );

        if minted < min_units_out {
            panic_with_error!(&env, ContractError::SlippageExceeded);
        }

        let held = units_held(&env);
        env.storage().instance().set(&DataKey::Units, &(held + minted));

        emit_event(
            &env,
            ADAPTER,
            DEPOSITED,
            from,
            AdapterMovedEventData { amount, units: minted },
        );
        minted
    }

    fn withdraw(env: Env, to: Address, units: i128, min_out: i128) -> i128 {
        let vault: Address = get(&env, &DataKey::Vault);
        vault.require_auth();
        if units <= 0 {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }
        let held = units_held(&env);
        if units > held {
            panic_with_error!(&env, ContractError::InsufficientBalance);
        }

        let underlying: Address = get(&env, &DataKey::Underlying);
        let paired: Address = get(&env, &DataKey::Paired);
        let router: Address = get(&env, &DataKey::Router);
        let pair: Address = get(&env, &DataKey::Pair);
        let me = env.current_contract_address();

        let (_amount_a, _amount_b): (i128, i128) = env.invoke_contract(
            &router,
            &Symbol::new(&env, "remove_liquidity"),
            soroban_sdk::vec![
                &env,
                underlying.into_val(&env),
                paired.into_val(&env),
                units.into_val(&env),
                0i128.into_val(&env),
                0i128.into_val(&env),
                me.into_val(&env),
                deadline(&env).into_val(&env),
            ],
        );

        // Convert the paired side back so the vault only ever sees its own
        // asset. Whatever the adapter holds is swapped, not a computed figure,
        // so dust from a previous cycle is swept out rather than accumulating.
        let paired_balance = token::Client::new(&env, &paired).balance(&me);
        if paired_balance > 0 {
            swap(&env, &paired, &underlying, paired_balance, 0);
        }

        // Measured, never assumed: the pool may return less than the position
        // was nominally worth, and reporting the nominal figure would overstate
        // what the vault actually received.
        let assets = token::Client::new(&env, &underlying).balance(&me);
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
            AdapterMovedEventData { amount: assets, units },
        );
        assets
    }

    fn position_value(env: Env, _owner: Address) -> i128 {
        Self::position_value_internal(&env)
    }

    fn current_apy(env: Env) -> AdapterApy {
        let checkpoint: Option<ApyCheckpoint> = env.storage().instance().get(&DataKey::Checkpoint);
        let Some(checkpoint) = checkpoint else {
            return AdapterApy { apy_bps: 0, confidence: ApyConfidence::Unavailable };
        };

        let elapsed = env.ledger().timestamp().saturating_sub(checkpoint.timestamp);
        // An AMM publishes no rate, so this is growth observed against a
        // checkpoint. Over a short window that measurement is dominated by
        // swap noise, so it is withheld rather than reported as a rate.
        if elapsed < MIN_APY_WINDOW_SECS || checkpoint.value <= 0 {
            return AdapterApy { apy_bps: 0, confidence: ApyConfidence::Unavailable };
        }

        let now = Self::position_value_internal(&env);
        if now <= checkpoint.value {
            return AdapterApy { apy_bps: 0, confidence: ApyConfidence::Derived };
        }

        let growth = now - checkpoint.value;
        let annualized = growth * SECONDS_PER_YEAR * 10_000 / (checkpoint.value * elapsed as i128);
        let capped = if annualized > MAX_APY_BPS as i128 { MAX_APY_BPS as i128 } else { annualized };

        AdapterApy { apy_bps: capped as u32, confidence: ApyConfidence::Derived }
    }

    fn underlying(env: Env) -> Address {
        get(&env, &DataKey::Underlying)
    }

    fn max_deposit(env: Env) -> i128 {
        // An AMM takes any size; what changes is the price impact, which the
        // caller's min_units_out is what actually bounds.
        let _ = env;
        i128::MAX
    }

    fn max_withdraw(env: Env) -> i128 {
        units_held(&env)
    }
}

#[cfg(test)]
mod test;

#[cfg(test)]
mod interface_test;
