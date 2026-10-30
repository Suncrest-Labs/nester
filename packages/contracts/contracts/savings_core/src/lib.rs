//! Nester Savings Core
//!
//! Single-asset (USDC) savings vault. Depositors receive SEP-41 savings shares
//! held in this contract; the share price is `total_assets / total_supply`
//! with a virtual offset (see [`shares::VIRTUAL_OFFSET`]). `total_assets` is
//! computed on-chain; nothing can set the share price directly.

#![no_std]

soroban_sdk::contractmeta!(key = "name", val = "SavingsCore");
include!(concat!(env!("OUT_DIR"), "/contractmeta.rs"));
soroban_sdk::contractmeta!(key = "source_repo", val = "github.com/Suncrest-Labs/nester");

mod adapters;
mod shares;

pub use adapters::AdapterConfig;

use nester_common::{INSTANCE_TTL_EXTEND_TO_LEDGERS, INSTANCE_TTL_THRESHOLD_LEDGERS};
use soroban_sdk::{
    contract, contracterror, contractimpl, contracttype, panic_with_error, symbol_short, token,
    Address, Env, String, Symbol, Vec,
};

const DEFAULT_TIMELOCK: u64 = 259_200;
const DECIMALS: u32 = 7;
const PRICE_SCALE: i128 = 10_000_000;

#[contracterror]
#[derive(Copy, Clone, Debug, Eq, PartialEq, PartialOrd, Ord)]
#[repr(u32)]
pub enum SavingsError {
    /// Amount is zero (or negative), or the operation would move zero value.
    ZeroAmount = 1,
    Slippage = 2,
    InsufficientShares = 3,
    InsufficientLiquidity = 4,
    DepositsPaused = 5,
    AdapterUnavailable = 6,
    Unauthorized = 7,
    CapExceeded = 8,
    NotReady = 9,
    NoSuchChange = 10,
    FeeTooHigh = 11,
    AlreadyInitialized = 12,
    InvalidExpiration = 13,
    /// Adapter is unknown, already registered, misconfigured, or the
    /// withdraw order does not list every adapter exactly once.
    InvalidAdapter = 14,
}

/// Data payload of the `deposit` and `withdraw` events.
#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AmountShares {
    pub amount: i128,
    pub shares: i128,
}

#[contracttype]
#[derive(Clone)]
enum DataKey {
    Owner,
    Asset,
    Reserve,
    Name,
    Symbol,
    Timelock,
}

#[contract]
pub struct SavingsCore;

#[contractimpl]
impl SavingsCore {
    pub fn initialize(
        env: Env,
        owner: Address,
        asset: Address,
        reserve: Address,
        name: String,
        symbol: String,
        timelock: u64,
    ) {
        let store = env.storage().instance();
        if store.has(&DataKey::Asset) {
            panic_with_error!(&env, SavingsError::AlreadyInitialized);
        }
        let timelock = if timelock == 0 {
            DEFAULT_TIMELOCK
        } else {
            timelock
        };
        store.set(&DataKey::Owner, &owner);
        store.set(&DataKey::Asset, &asset);
        store.set(&DataKey::Reserve, &reserve);
        store.set(&DataKey::Name, &name);
        store.set(&DataKey::Symbol, &symbol);
        store.set(&DataKey::Timelock, &timelock);
        bump_instance(&env);
    }

    /// Deposit `assets` USDC from `from`; returns the shares minted.
    pub fn deposit(env: Env, from: Address, assets: i128, min_shares: i128) -> i128 {
        from.require_auth();
        require_positive(&env, assets);
        let shares = Self::convert_to_shares(env.clone(), assets);
        if shares <= 0 {
            panic_with_error!(&env, SavingsError::ZeroAmount);
        }
        if shares < min_shares {
            panic_with_error!(&env, SavingsError::Slippage);
        }
        asset_client(&env).transfer(&from, &env.current_contract_address(), &assets);
        shares::mint(&env, &from, shares);
        bump_instance(&env);
        publish(&env, "deposit", from, assets, shares);
        shares
    }

    /// Withdraw exactly `assets` USDC to `owner`; returns the shares burned.
    pub fn withdraw(env: Env, owner: Address, assets: i128, max_shares: i128) -> i128 {
        owner.require_auth();
        require_positive(&env, assets);
        let shares = shares::to_shares_up(
            assets,
            Self::total_assets(env.clone()),
            shares::total_supply(&env),
        );
        if shares > max_shares {
            panic_with_error!(&env, SavingsError::Slippage);
        }
        pay_out(&env, &owner, assets, shares);
        shares
    }

    /// Redeem `shares` from `owner` for USDC; returns the assets paid.
    pub fn redeem(env: Env, owner: Address, shares: i128, min_assets: i128) -> i128 {
        owner.require_auth();
        require_positive(&env, shares);
        let assets = Self::convert_to_assets(env.clone(), shares);
        if assets <= 0 {
            panic_with_error!(&env, SavingsError::ZeroAmount);
        }
        if assets < min_assets {
            panic_with_error!(&env, SavingsError::Slippage);
        }
        pay_out(&env, &owner, assets, shares);
        assets
    }

    /// Idle USDC plus every adapter's live position value. A failing adapter
    /// counts as zero (the conservative price used for withdrawals).
    pub fn total_assets(env: Env) -> i128 {
        adapters::valuation(&env, idle_assets(&env), false).unwrap()
    }

    pub fn total_supply(env: Env) -> i128 {
        shares::total_supply(&env)
    }

    /// Deposit preview. Values strictly: reverts with `AdapterUnavailable` if
    /// any adapter cannot be valued.
    pub fn convert_to_shares(env: Env, assets: i128) -> i128 {
        let total = adapters::valuation(&env, idle_assets(&env), true)
            .unwrap_or_else(|e| panic_with_error!(&env, e));
        shares::to_shares_down(assets, total, shares::total_supply(&env))
    }

    pub fn convert_to_assets(env: Env, shares: i128) -> i128 {
        shares::to_assets_down(
            shares,
            Self::total_assets(env.clone()),
            shares::total_supply(&env),
        )
    }

    /// Assets per 10^7 shares.
    pub fn share_price(env: Env) -> i128 {
        Self::convert_to_assets(env, PRICE_SCALE)
    }

    // --- Adapters -------------------------------------------------------

    /// Register an adapter with its caps (owner, instant). Task 4 replaces
    /// this with a timelocked proposal.
    pub fn add_adapter(env: Env, owner: Address, cfg: AdapterConfig) {
        require_owner(&env, &owner);
        adapters::add(&env, &asset_address(&env), cfg);
        bump_instance(&env);
    }

    pub fn set_withdraw_order(env: Env, owner: Address, order: Vec<Address>) {
        require_owner(&env, &owner);
        adapters::set_withdraw_order(&env, order);
        bump_instance(&env);
    }

    pub fn set_allocator(env: Env, owner: Address, allocator: Address) {
        require_owner(&env, &owner);
        adapters::set_allocator(&env, &allocator);
        bump_instance(&env);
    }

    /// Move `assets` of idle USDC into `adapter`, within its caps.
    pub fn allocate(env: Env, allocator: Address, adapter: Address, assets: i128) {
        adapters::require_allocator(&env, &allocator);
        require_positive(&env, assets);
        adapters::allocate(
            &env,
            &asset_address(&env),
            idle_assets(&env),
            &adapter,
            assets,
        );
        bump_instance(&env);
    }

    /// Withdraw `assets` worth of position from `adapter` back to idle.
    pub fn deallocate(env: Env, allocator: Address, adapter: Address, assets: i128) {
        adapters::require_allocator(&env, &allocator);
        require_positive(&env, assets);
        adapters::deallocate(&env, &adapter, assets);
        bump_instance(&env);
    }

    pub fn adapters(env: Env) -> Vec<AdapterConfig> {
        adapters::adapters(&env)
    }

    /// Live value of the core's position in `adapter` (0 if it cannot be read).
    pub fn allocation(env: Env, adapter: Address) -> i128 {
        adapters::try_value(&env, &adapter).unwrap_or(0)
    }

    pub fn withdraw_order(env: Env) -> Vec<Address> {
        adapters::withdraw_order(&env)
    }

    pub fn idle(env: Env) -> i128 {
        idle_assets(&env)
    }

    // --- SEP-41 ---------------------------------------------------------

    pub fn balance(env: Env, id: Address) -> i128 {
        shares::balance(&env, &id)
    }

    pub fn transfer(env: Env, from: Address, to: Address, amount: i128) {
        from.require_auth();
        require_non_negative(&env, amount);
        shares::move_shares(&env, &from, &to, amount);
        bump_instance(&env);
        env.events()
            .publish((symbol_short!("transfer"), from, to), amount);
    }

    pub fn transfer_from(env: Env, spender: Address, from: Address, to: Address, amount: i128) {
        spender.require_auth();
        require_non_negative(&env, amount);
        shares::spend_allowance(&env, &from, &spender, amount);
        shares::move_shares(&env, &from, &to, amount);
        bump_instance(&env);
        env.events()
            .publish((symbol_short!("transfer"), from, to), amount);
    }

    pub fn approve(
        env: Env,
        from: Address,
        spender: Address,
        amount: i128,
        expiration_ledger: u32,
    ) {
        from.require_auth();
        require_non_negative(&env, amount);
        shares::set_allowance(&env, &from, &spender, amount, expiration_ledger);
        bump_instance(&env);
        env.events().publish(
            (symbol_short!("approve"), from, spender),
            (amount, expiration_ledger),
        );
    }

    pub fn allowance(env: Env, from: Address, spender: Address) -> i128 {
        shares::allowance(&env, &from, &spender)
    }

    pub fn decimals(_env: Env) -> u32 {
        DECIMALS
    }

    pub fn name(env: Env) -> String {
        env.storage().instance().get(&DataKey::Name).unwrap()
    }

    pub fn symbol(env: Env) -> String {
        env.storage().instance().get(&DataKey::Symbol).unwrap()
    }
}

fn asset_address(env: &Env) -> Address {
    env.storage().instance().get(&DataKey::Asset).unwrap()
}

fn asset_client(env: &Env) -> token::Client<'_> {
    token::Client::new(env, &asset_address(env))
}

fn require_owner(env: &Env, caller: &Address) {
    caller.require_auth();
    let owner: Address = env.storage().instance().get(&DataKey::Owner).unwrap();
    if &owner != caller {
        panic_with_error!(env, SavingsError::Unauthorized);
    }
}

fn idle_assets(env: &Env) -> i128 {
    asset_client(env).balance(&env.current_contract_address())
}

fn pay_out(env: &Env, owner: &Address, assets: i128, shares: i128) {
    adapters::ensure_idle(env, || idle_assets(env), assets);
    shares::burn(env, owner, shares);
    asset_client(env).transfer(&env.current_contract_address(), owner, &assets);
    bump_instance(env);
    publish(env, "withdraw", owner.clone(), assets, shares);
}

fn publish(env: &Env, kind: &str, user: Address, amount: i128, shares: i128) {
    env.events().publish(
        (Symbol::new(env, kind), user),
        AmountShares { amount, shares },
    );
}

fn require_positive(env: &Env, amount: i128) {
    if amount <= 0 {
        panic_with_error!(env, SavingsError::ZeroAmount);
    }
}

fn require_non_negative(env: &Env, amount: i128) {
    if amount < 0 {
        panic_with_error!(env, SavingsError::ZeroAmount);
    }
}

fn bump_instance(env: &Env) {
    env.storage().instance().extend_ttl(
        INSTANCE_TTL_THRESHOLD_LEDGERS,
        INSTANCE_TTL_EXTEND_TO_LEDGERS,
    );
}

#[cfg(test)]
mod test;
