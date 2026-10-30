#![no_std]

//! Blend Capital lending-pool adapter.
//!
//! Blend does not expose `deposit`/`withdraw`. A pool takes a single
//! `submit(from, spender, to, requests)` call carrying a batch of `Request`
//! values, each tagged with a `request_type` — 0 supplies, 1 withdraws — and
//! returns the caller's whole `Positions` afterwards. The generic adapter in
//! `adapter_lending` invokes `deposit(address, amount)`, which no Blend pool
//! implements, so it compiles and passes against its mock while reverting
//! against the real protocol. This adapter speaks Blend's actual interface.
//!
//! Position accounting: `Positions.supply` is a map keyed by the pool's
//! *reserve index*, not by asset address, so the index for the underlying is
//! configured at initialization and read back out of the returned struct.
//! Units are bTokens (Blend's supply receipt); `max_withdraw` reports them.
//! `position_value` converts them to the underlying through the reserve's
//! `b_rate`, read from Blend v2's `get_reserve`. Blend v1 pools expose no
//! `get_reserve`, so this adapter targets v2 pools only.

soroban_sdk::contractmeta!(key = "name", val = "AdapterBlend");
include!(concat!(env!("OUT_DIR"), "/contractmeta.rs"));
soroban_sdk::contractmeta!(key = "source_repo", val = "github.com/Suncrest-Labs/nester");

use nester_common::adapters::{AdapterApy, ApyConfidence, YieldAdapter};
use nester_common::{emit_event, ContractError};
use soroban_sdk::{
    contract, contractimpl, contracttype, panic_with_error, symbol_short, token, Address, Env,
    IntoVal, Map, Symbol,
};

const ADAPTER: Symbol = symbol_short!("ADAPTER");
const DEPOSITED: Symbol = symbol_short!("DEPOSIT");
const WITHDRAWN: Symbol = symbol_short!("WITHDRAW");

#[contracttype]
#[derive(Clone, Debug)]
pub struct AdapterMovedEventData {
    pub amount: i128,
    pub units: i128,
}

/// Blend's supply request. Field order and names must match the pool's own
/// `Request` struct exactly — Soroban encodes a UDT by field name, and a
/// mismatch is a decode failure at the pool rather than a compile error here.
#[contracttype]
#[derive(Clone)]
pub struct BlendRequest {
    pub address: Address,
    pub amount: i128,
    pub request_type: u32,
}

/// Blend's `Positions` return value. Each map is keyed by reserve index.
#[contracttype]
#[derive(Clone)]
pub struct BlendPositions {
    pub collateral: Map<u32, i128>,
    pub liabilities: Map<u32, i128>,
    pub supply: Map<u32, i128>,
}

/// Blend v2 `ReserveConfig`. Unused beyond decoding, but a struct decodes
/// only when every field matches, so all of them are declared. Verified
/// against the v2 pool WASM's spec in `spec_test.rs`.
#[contracttype]
#[derive(Clone)]
pub struct BlendReserveConfig {
    pub c_factor: u32,
    pub decimals: u32,
    pub enabled: bool,
    pub index: u32,
    pub l_factor: u32,
    pub max_util: u32,
    pub r_base: u32,
    pub r_one: u32,
    pub r_three: u32,
    pub r_two: u32,
    pub reactivity: u32,
    pub supply_cap: i128,
    pub util: u32,
}

/// Blend v2 `ReserveData`; `b_rate` converts bTokens to underlying at
/// `B_RATE_SCALE`.
#[contracttype]
#[derive(Clone)]
pub struct BlendReserveData {
    pub b_rate: i128,
    pub b_supply: i128,
    pub backstop_credit: i128,
    pub d_rate: i128,
    pub d_supply: i128,
    pub ir_mod: i128,
    pub last_time: u64,
}

/// Blend v2 `get_reserve` return value.
#[contracttype]
#[derive(Clone)]
pub struct BlendReserve {
    pub asset: Address,
    pub config: BlendReserveConfig,
    pub data: BlendReserveData,
    pub scalar: i128,
}

/// Blend v2 `b_rate` scale (`SCALAR_12`).
const B_RATE_SCALE: i128 = 1_000_000_000_000;

/// `request_type` for supplying an asset without posting it as collateral.
const REQUEST_TYPE_SUPPLY: u32 = 0;
/// `request_type` for withdrawing a previously supplied asset.
const REQUEST_TYPE_WITHDRAW: u32 = 1;

#[contracttype]
#[derive(Clone)]
enum DataKey {
    /// Vault address — the only caller allowed to move value.
    Vault,
    /// Blend pool contract.
    Pool,
    /// Underlying asset accepted/paid by this adapter.
    Underlying,
    /// Index of the underlying's reserve within the pool, used to read
    /// `Positions.supply`.
    ReserveIndex,
    /// Aggregate bToken units held at the pool.
    Units,
}

#[contract]
pub struct BlendAdapterContract;

fn require_vault(env: &Env, caller: &Address) {
    let vault: Address = env
        .storage()
        .instance()
        .get(&DataKey::Vault)
        .unwrap_or_else(|| panic_with_error!(env, ContractError::NotInitialized));
    if *caller != vault {
        panic_with_error!(env, ContractError::Unauthorized);
    }
}

fn pool(env: &Env) -> Address {
    env.storage()
        .instance()
        .get(&DataKey::Pool)
        .unwrap_or_else(|| panic_with_error!(env, ContractError::NotInitialized))
}

fn underlying_asset(env: &Env) -> Address {
    env.storage()
        .instance()
        .get(&DataKey::Underlying)
        .unwrap_or_else(|| panic_with_error!(env, ContractError::NotInitialized))
}

fn reserve_index(env: &Env) -> u32 {
    env.storage()
        .instance()
        .get(&DataKey::ReserveIndex)
        .unwrap_or(0)
}

/// Supplied balance for this adapter's reserve, read from a `Positions`.
fn supplied_units(env: &Env, positions: &BlendPositions) -> i128 {
    positions.supply.get(reserve_index(env)).unwrap_or(0)
}

/// The reserve's current bToken -> underlying rate, at `B_RATE_SCALE`.
fn b_rate(env: &Env) -> i128 {
    let reserve: BlendReserve = env.invoke_contract(
        &pool(env),
        &Symbol::new(env, "get_reserve"),
        soroban_sdk::vec![env, underlying_asset(env).into_val(env)],
    );
    reserve.data.b_rate
}

/// Underlying value of `units` bTokens, rounded down as Blend pays it.
fn units_to_assets(units: i128, b_rate: i128) -> i128 {
    units * b_rate / B_RATE_SCALE
}

/// Calls `submit` with a single request and returns the resulting positions.
fn submit_one(env: &Env, request: BlendRequest) -> BlendPositions {
    let me = env.current_contract_address();
    let requests = soroban_sdk::vec![env, request];
    env.invoke_contract(
        &pool(env),
        &Symbol::new(env, "submit"),
        soroban_sdk::vec![
            env,
            me.into_val(env), // from: whose position changes
            me.into_val(env), // spender: who pays the tokens
            me.into_val(env), // to: who receives withdrawn tokens
            requests.into_val(env),
        ],
    )
}

#[contractimpl]
impl BlendAdapterContract {
    /// `reserve_index` is the position of `underlying` in the pool's reserve
    /// list. It cannot be derived without reading pool config, so it is
    /// supplied by the deployer and verified by the first deposit.
    pub fn initialize(
        env: Env,
        vault: Address,
        pool: Address,
        underlying: Address,
        reserve_index: u32,
    ) {
        if env.storage().instance().has(&DataKey::Vault) {
            panic_with_error!(&env, ContractError::AlreadyInitialized);
        }
        env.storage().instance().set(&DataKey::Vault, &vault);
        env.storage().instance().set(&DataKey::Pool, &pool);
        env.storage()
            .instance()
            .set(&DataKey::Underlying, &underlying);
        env.storage()
            .instance()
            .set(&DataKey::ReserveIndex, &reserve_index);
        env.storage().instance().set(&DataKey::Units, &0i128);
    }

    pub fn get_vault(env: Env) -> Address {
        env.storage()
            .instance()
            .get(&DataKey::Vault)
            .unwrap_or_else(|| panic_with_error!(&env, ContractError::NotInitialized))
    }

    pub fn get_pool(env: Env) -> Address {
        pool(&env)
    }

    pub fn get_reserve_index(env: Env) -> u32 {
        reserve_index(&env)
    }
}

#[contractimpl]
impl YieldAdapter for BlendAdapterContract {
    fn deposit(env: Env, from: Address, amount: i128, min_units_out: i128) -> i128 {
        from.require_auth();
        require_vault(&env, &from);
        if amount <= 0 {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }

        let underlying = underlying_asset(&env);
        let me = env.current_contract_address();

        // Pull the assets from the vault inside this invocation, exactly as the
        // generic adapter does: the vault pre-authorizes this transfer, so a
        // revert anywhere below rolls the pull back with it and never strands
        // funds at the adapter.
        token::Client::new(&env, &underlying).transfer(&from, &me, &amount);

        // Blend pulls the tokens itself during `submit`, so the pool must be
        // allowed to move exactly this amount from the adapter.
        token::Client::new(&env, &underlying).approve(
            &me,
            &pool(&env),
            &amount,
            &(env.ledger().sequence() + 1),
        );

        let before = env
            .storage()
            .instance()
            .get(&DataKey::Units)
            .unwrap_or(0i128);

        let positions = submit_one(
            &env,
            BlendRequest {
                address: underlying,
                amount,
                request_type: REQUEST_TYPE_SUPPLY,
            },
        );

        // Units minted are the change in the pool's own view of the position,
        // not a number the adapter computes: Blend applies its own exchange
        // rate, and trusting a local estimate would let rounding drift
        // accumulate against depositors.
        let after = supplied_units(&env, &positions);
        let units = after - before;
        if units < min_units_out {
            panic_with_error!(&env, ContractError::SlippageExceeded);
        }

        env.storage().instance().set(&DataKey::Units, &after);

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
        let vault: Address = env
            .storage()
            .instance()
            .get(&DataKey::Vault)
            .unwrap_or_else(|| panic_with_error!(&env, ContractError::NotInitialized));
        vault.require_auth();
        if units <= 0 {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }
        let held: i128 = env
            .storage()
            .instance()
            .get(&DataKey::Units)
            .unwrap_or(0i128);
        if units > held {
            panic_with_error!(&env, ContractError::InsufficientBalance);
        }

        let underlying = underlying_asset(&env);
        let me = env.current_contract_address();
        let balance_before = token::Client::new(&env, &underlying).balance(&me);

        // Blend withdraw requests are in the underlying, and Blend burns
        // ceil(amount / b_rate) bTokens, so asking for floor(units * b_rate)
        // never burns more than `units`. A full exit asks for one more so
        // Blend caps the burn at the whole position instead of leaving a
        // dust bToken; it then pays floor(held * b_rate).
        let mut amount = units_to_assets(units, b_rate(&env));
        if units == held {
            amount += 1;
        }

        let positions = submit_one(
            &env,
            BlendRequest {
                address: underlying.clone(),
                amount,
                request_type: REQUEST_TYPE_WITHDRAW,
            },
        );

        // Assets received are measured, not assumed: Blend may return less
        // than requested when a reserve is short on liquidity, and reporting
        // the requested figure would overstate what the vault actually holds.
        let assets = token::Client::new(&env, &underlying).balance(&me) - balance_before;
        if assets < min_out {
            panic_with_error!(&env, ContractError::SlippageExceeded);
        }

        env.storage()
            .instance()
            .set(&DataKey::Units, &supplied_units(&env, &positions));

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

    fn position_value(env: Env, _owner: Address) -> i128 {
        let units: i128 = env
            .storage()
            .instance()
            .get(&DataKey::Units)
            .unwrap_or(0i128);
        if units == 0 {
            return 0;
        }
        units_to_assets(units, b_rate(&env))
    }

    fn current_apy(env: Env) -> AdapterApy {
        // Blend publishes rates through reserve data rather than a
        // `supply_rate` entry point, and deriving one from position growth
        // needs a checkpoint this adapter does not yet keep. Reporting
        // `Unavailable` is deliberate: the interface documents that consumers
        // must ignore `apy_bps` in that case, so an honest "unknown" cannot be
        // mistaken for a real zero and churn the rebalancer.
        let _ = env;
        AdapterApy {
            apy_bps: 0,
            confidence: ApyConfidence::Unavailable,
        }
    }

    fn underlying(env: Env) -> Address {
        underlying_asset(&env)
    }

    fn core(env: Env) -> Address {
        Self::get_vault(env)
    }

    fn max_deposit(env: Env) -> i128 {
        // Blend enforces its own supply caps inside `submit` and reverts when
        // one is hit. Reporting i128::MAX here would claim unlimited capacity,
        // so the conservative reading is that capacity is unknown-but-open and
        // the pool is the authority.
        let _ = env;
        i128::MAX
    }

    fn max_withdraw(env: Env) -> i128 {
        env.storage()
            .instance()
            .get(&DataKey::Units)
            .unwrap_or(0i128)
    }
}

#[contractimpl]
impl BlendAdapterContract {
    pub fn version(env: Env) -> soroban_sdk::String {
        soroban_sdk::String::from_str(&env, env!("CARGO_PKG_VERSION"))
    }
}

#[cfg(test)]
mod test;

#[cfg(test)]
mod interface_test;

#[cfg(test)]
mod real_pool_test;

#[cfg(test)]
mod spec_test;
