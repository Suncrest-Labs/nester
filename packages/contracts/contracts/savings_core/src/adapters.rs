//! Adapter registry, caps, on-chain valuation and the movement of USDC
//! between the core's idle balance and its yield adapters.
//!
//! Adapters are least-trusted: every read that feeds pricing is a `try_` call
//! so one broken adapter is detectable. Deposits value strictly (any failure
//! reverts with `AdapterUnavailable`); withdrawals value conservatively (a
//! failing adapter counts as zero and is skipped when pulling liquidity).
//!
//! Adapter `withdraw` takes position units, while caps and allocations are in
//! assets. The interface exposes the units the core holds through
//! `max_withdraw()` (every adapter in this repo returns its position units
//! there), so assets convert to units pro-rata:
//! `units = ceil(units_held * assets / position_value)`, capped at
//! `units_held`.

use nester_common::adapters::YieldAdapterClient;
use soroban_sdk::{
    auth::{ContractContext, InvokerContractAuthEntry, SubContractInvocation},
    contracttype, panic_with_error, vec, Address, Env, IntoVal, Symbol, Vec,
};

use crate::SavingsError;

const BPS: i128 = 10_000;

/// Tolerated shortfall, in bps, between the assets requested from an adapter
/// and what it actually pays (or the units it mints), to absorb protocol
/// rounding.
const ADAPTER_TOLERANCE_BPS: i128 = 1;

#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AdapterConfig {
    pub adapter: Address,
    /// Maximum assets allocated to this adapter.
    pub abs_cap: i128,
    /// Maximum share of `total_assets`, in bps, allocated to this adapter.
    pub rel_cap_bps: u32,
}

#[contracttype]
#[derive(Clone)]
enum AdapterKey {
    Adapters,
    WithdrawOrder,
    Allocator,
}

pub fn adapters(env: &Env) -> Vec<AdapterConfig> {
    env.storage()
        .instance()
        .get(&AdapterKey::Adapters)
        .unwrap_or_else(|| Vec::new(env))
}

pub fn withdraw_order(env: &Env) -> Vec<Address> {
    env.storage()
        .instance()
        .get(&AdapterKey::WithdrawOrder)
        .unwrap_or_else(|| Vec::new(env))
}

/// Whether `cfg` can be registered: valid caps, not already registered, and
/// its underlying asset is `asset` and it is bound to this core. Never panics.
pub fn can_add(env: &Env, asset: &Address, cfg: &AdapterConfig) -> bool {
    caps_valid(cfg.abs_cap, cfg.rel_cap_bps)
        && !is_listed(env, &cfg.adapter)
        && matches!(
            YieldAdapterClient::new(env, &cfg.adapter).try_underlying(),
            Ok(Ok(ref u)) if u == asset
        )
        && matches!(
            YieldAdapterClient::new(env, &cfg.adapter).try_core(),
            Ok(Ok(ref c)) if *c == env.current_contract_address()
        )
}

pub fn check_new(env: &Env, asset: &Address, cfg: &AdapterConfig) {
    if !can_add(env, asset, cfg) {
        panic_with_error!(env, SavingsError::InvalidAdapter);
    }
}

pub fn caps_valid(abs_cap: i128, rel_cap_bps: u32) -> bool {
    abs_cap >= 0 && rel_cap_bps as i128 <= BPS
}

pub fn check_caps(env: &Env, abs_cap: i128, rel_cap_bps: u32) {
    if !caps_valid(abs_cap, rel_cap_bps) {
        panic_with_error!(env, SavingsError::InvalidAdapter);
    }
}

pub fn is_listed(env: &Env, adapter: &Address) -> bool {
    find(env, adapter).is_some()
}

/// Register an adapter and append it to the withdraw order.
pub fn add(env: &Env, asset: &Address, cfg: AdapterConfig) {
    check_new(env, asset, &cfg);
    let mut order = withdraw_order(env);
    order.push_back(cfg.adapter.clone());
    let mut list = adapters(env);
    list.push_back(cfg);
    env.storage().instance().set(&AdapterKey::Adapters, &list);
    env.storage()
        .instance()
        .set(&AdapterKey::WithdrawOrder, &order);
}

/// Replace the withdraw order. It must name every registered adapter exactly
/// once, so no adapter's liquidity can be left out of withdrawals.
pub fn set_withdraw_order(env: &Env, order: Vec<Address>) {
    let list = adapters(env);
    let mut valid = order.len() == list.len();
    for (i, a) in order.iter().enumerate() {
        if find(env, &a).is_none() || order.first_index_of(&a) != Some(i as u32) {
            valid = false;
        }
    }
    if !valid {
        panic_with_error!(env, SavingsError::InvalidAdapter);
    }
    env.storage()
        .instance()
        .set(&AdapterKey::WithdrawOrder, &order);
}

/// Set a registered adapter's caps.
pub fn set_caps(env: &Env, adapter: &Address, abs_cap: i128, rel_cap_bps: u32) {
    check_caps(env, abs_cap, rel_cap_bps);
    let mut list = adapters(env);
    let i = index_of(env, &list, adapter);
    list.set(
        i,
        AdapterConfig {
            adapter: adapter.clone(),
            abs_cap,
            rel_cap_bps,
        },
    );
    env.storage().instance().set(&AdapterKey::Adapters, &list);
}

/// Unregister an adapter whose position is worth exactly zero. An adapter
/// that cannot be valued is not removable: its position may still be live.
pub fn remove(env: &Env, adapter: &Address) {
    let mut list = adapters(env);
    let i = index_of(env, &list, adapter);
    match try_value(env, adapter) {
        Some(0) => {}
        Some(_) => panic_with_error!(env, SavingsError::AdapterNotEmpty),
        None => panic_with_error!(env, SavingsError::AdapterUnavailable),
    }
    list.remove(i);
    let mut order = withdraw_order(env);
    if let Some(j) = order.first_index_of(adapter) {
        order.remove(j);
    }
    env.storage().instance().set(&AdapterKey::Adapters, &list);
    env.storage()
        .instance()
        .set(&AdapterKey::WithdrawOrder, &order);
}

pub fn allocator(env: &Env) -> Option<Address> {
    env.storage().instance().get(&AdapterKey::Allocator)
}

pub fn set_allocator(env: &Env, allocator: &Address) {
    env.storage()
        .instance()
        .set(&AdapterKey::Allocator, allocator);
}

pub fn require_allocator(env: &Env, caller: &Address) {
    caller.require_auth();
    if allocator(env).as_ref() != Some(caller) {
        panic_with_error!(env, SavingsError::Unauthorized);
    }
}

fn find(env: &Env, adapter: &Address) -> Option<AdapterConfig> {
    adapters(env).iter().find(|c| &c.adapter == adapter)
}

/// The registered config for `adapter`; `InvalidAdapter` if unknown.
pub fn config(env: &Env, adapter: &Address) -> AdapterConfig {
    find(env, adapter).unwrap_or_else(|| panic_with_error!(env, SavingsError::InvalidAdapter))
}

fn index_of(env: &Env, list: &Vec<AdapterConfig>, adapter: &Address) -> u32 {
    list.iter()
        .position(|c| &c.adapter == adapter)
        .unwrap_or_else(|| panic_with_error!(env, SavingsError::InvalidAdapter)) as u32
}

/// The adapter's valuation of the core's position, or `None` if the call
/// fails or returns a negative value.
pub fn try_value(env: &Env, adapter: &Address) -> Option<i128> {
    match YieldAdapterClient::new(env, adapter).try_position_value(&env.current_contract_address())
    {
        Ok(Ok(v)) if v >= 0 => Some(v),
        _ => None,
    }
}

/// `idle + Σ position_value`. Strict: any failing adapter is an error.
/// Non-strict: a failing adapter counts as zero.
pub fn valuation(env: &Env, idle: i128, strict: bool) -> Result<i128, SavingsError> {
    let mut total = idle;
    for cfg in adapters(env).iter() {
        match try_value(env, &cfg.adapter) {
            Some(v) => total += v,
            None if strict => return Err(SavingsError::AdapterUnavailable),
            None => {}
        }
    }
    Ok(total)
}

fn units_held(env: &Env, adapter: &Address) -> Option<i128> {
    match YieldAdapterClient::new(env, adapter).try_max_withdraw() {
        Ok(Ok(u)) if u >= 0 => Some(u),
        _ => None,
    }
}

/// Units to burn to receive `assets` from a position worth `value`.
fn units_for(env: &Env, adapter: &Address, assets: i128, value: i128) -> Option<i128> {
    let held = units_held(env, adapter)?;
    if held == 0 || value <= 0 {
        return None;
    }
    if assets >= value {
        return Some(held);
    }
    Some(((held * assets + value - 1) / value).min(held))
}

fn with_tolerance(amount: i128) -> i128 {
    amount - (amount * ADAPTER_TOLERANCE_BPS + BPS - 1) / BPS
}

/// Withdraw `assets` worth of position from `adapter` into the core. Returns
/// the assets received, or `None` if the adapter could not pay.
fn try_pull(env: &Env, adapter: &Address, assets: i128, value: i128) -> Option<i128> {
    let units = units_for(env, adapter, assets, value)?;
    match YieldAdapterClient::new(env, adapter).try_withdraw(
        &env.current_contract_address(),
        &units,
        &with_tolerance(assets),
    ) {
        Ok(Ok(received)) => Some(received),
        _ => None,
    }
}

/// Move `assets` of idle USDC into `adapter`, enforcing its caps against the
/// strict valuation.
pub fn allocate(env: &Env, asset: &Address, idle: i128, adapter: &Address, assets: i128) {
    let cfg = config(env, adapter);
    if assets > idle {
        panic_with_error!(env, SavingsError::InsufficientLiquidity);
    }
    let total = valuation(env, idle, true).unwrap_or_else(|e| panic_with_error!(env, e));
    let current = try_value(env, adapter)
        .unwrap_or_else(|| panic_with_error!(env, SavingsError::AdapterUnavailable));
    let after = current + assets;
    if after > cfg.abs_cap || after * BPS > cfg.rel_cap_bps as i128 * total {
        panic_with_error!(env, SavingsError::CapExceeded);
    }

    let min_units = match units_held(env, adapter) {
        Some(held) if held > 0 && current > 0 => with_tolerance(held * assets / current).max(1),
        _ => 1,
    };
    authorize_pull(env, asset, adapter, assets);
    let me = env.current_contract_address();
    if !matches!(
        YieldAdapterClient::new(env, adapter).try_deposit(&me, &assets, &min_units),
        Ok(Ok(_))
    ) {
        panic_with_error!(env, SavingsError::AdapterUnavailable);
    }
}

/// Withdraw `assets` worth of position from `adapter` back to idle.
pub fn deallocate(env: &Env, adapter: &Address, assets: i128) {
    config(env, adapter);
    let value = try_value(env, adapter)
        .unwrap_or_else(|| panic_with_error!(env, SavingsError::AdapterUnavailable));
    if assets > value {
        panic_with_error!(env, SavingsError::InsufficientLiquidity);
    }
    if try_pull(env, adapter, assets, value).is_none() {
        panic_with_error!(env, SavingsError::AdapterUnavailable);
    }
}

/// Make at least `needed` USDC idle: pull from adapters in withdraw order,
/// skipping any that fail. Reverts with `InsufficientLiquidity` if it still
/// falls short.
pub fn ensure_idle(env: &Env, idle: impl Fn() -> i128, needed: i128) {
    let mut have = idle();
    for adapter in withdraw_order(env).iter() {
        if have >= needed {
            break;
        }
        let Some(value) = try_value(env, &adapter) else {
            continue;
        };
        if value == 0 {
            continue;
        }
        if try_pull(env, &adapter, (needed - have).min(value), value).is_some() {
            have = idle();
        }
    }
    if have < needed {
        panic_with_error!(env, SavingsError::InsufficientLiquidity);
    }
}

/// Pre-authorize `adapter` to transfer exactly `amount` of `token` out of the
/// core during the next invocation, and nothing else.
fn authorize_pull(env: &Env, token: &Address, adapter: &Address, amount: i128) {
    let me = env.current_contract_address();
    env.authorize_as_current_contract(vec![
        env,
        InvokerContractAuthEntry::Contract(SubContractInvocation {
            context: ContractContext {
                contract: token.clone(),
                fn_name: Symbol::new(env, "transfer"),
                args: (me, adapter.clone(), amount).into_val(env),
            },
            sub_invocations: Vec::new(env),
        }),
    ]);
}
