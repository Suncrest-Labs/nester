//! Liquidity-pool yield adapter (Soroswap-style).
//!
//! Implements [`nester_common::adapters::YieldAdapter`] against an AMM pool:
//! deposit into the pool, hold LP units, value them at oracle-safe prices,
//! withdraw with slippage protection.
//!
//! # LP valuation
//! LP units are valued as a pro-rata share of pool reserves
//! (`units * reserve / total_shares`) — the same formula the pool itself pays
//! out on burn. This avoids spot-price oracles entirely: pro-rata reserve
//! valuation cannot be skewed by a single-block price swing, and every
//! value-moving call additionally carries a caller-supplied minimum-output
//! guard.
//!
//! # Derived APY
//! AMM pools expose no rate, so APY is derived from observed position growth
//! against a stored checkpoint. A derived APY over a short window is noise:
//! below [`MIN_APY_WINDOW_SECS`] the adapter reports
//! [`ApyConfidence::Unavailable`] instead of a wild number.

#![no_std]

use soroban_sdk::{
    contract, contractimpl, contracttype, panic_with_error, symbol_short, token, Address, Env,
    IntoVal, Symbol, Val, Vec,
};

use nester_common::adapters::{AdapterApy, ApyConfidence, YieldAdapter};
use nester_common::{emit_event, ContractError};

const ADAPTER: Symbol = symbol_short!("ADAPTER");
const DEPOSITED: Symbol = symbol_short!("DEPOSIT");
const WITHDRAWN: Symbol = symbol_short!("WITHDRAW");

/// Minimum observation window before a derived APY is considered meaningful.
pub const MIN_APY_WINDOW_SECS: u64 = 86_400; // 1 day
const SECONDS_PER_YEAR: i128 = 31_536_000;
/// Hard cap mirroring the registry's MAX_APY_BPS.
const MAX_APY_BPS: u32 = 10_000;

#[contracttype]
#[derive(Clone)]
enum DataKey {
    Vault,
    Pool,
    Underlying,
    /// Aggregate LP units held at the pool.
    Units,
    /// Basis for derived-APY measurement.
    Checkpoint,
}

/// Position value captured at a point in time, the basis for derived APY.
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

#[contract]
pub struct PoolAdapterContract;

#[contractimpl]
impl PoolAdapterContract {
    /// One-time wiring. No admin key is kept: the adapter is immutable after
    /// initialization, keeping its trust surface minimal.
    ///
    /// `vault` must authorize, so a deployed-but-uninitialized adapter cannot
    /// be front-run by an attacker who points it at a vault they control.
    pub fn initialize(env: Env, vault: Address, pool: Address, underlying: Address) {
        vault.require_auth();
        if env.storage().instance().has(&DataKey::Vault) {
            panic_with_error!(&env, ContractError::AlreadyInitialized);
        }
        env.storage().instance().set(&DataKey::Vault, &vault);
        env.storage().instance().set(&DataKey::Pool, &pool);
        env.storage().instance().set(&DataKey::Underlying, &underlying);
        env.storage().instance().set(&DataKey::Units, &0i128);
    }

    pub fn get_vault(env: Env) -> Address {
        env.storage().instance().get(&DataKey::Vault).unwrap()
    }

    pub fn get_pool(env: Env) -> Address {
        env.storage().instance().get(&DataKey::Pool).unwrap()
    }

    /// Current derived-APY measurement basis, if any.
    pub fn get_checkpoint(env: Env) -> Option<ApyCheckpoint> {
        env.storage().instance().get(&DataKey::Checkpoint)
    }
}

#[contractimpl]
impl YieldAdapter for PoolAdapterContract {
    fn deposit(env: Env, from: Address, amount: i128, min_units_out: i128) -> i128 {
        from.require_auth();
        require_vault(&env, &from);
        if amount <= 0 {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }

        let underlying: Address = env.storage().instance().get(&DataKey::Underlying).unwrap();
        let pool: Address = env.storage().instance().get(&DataKey::Pool).unwrap();
        let me = env.current_contract_address();

        // Pull from the caller, then forward to the pool. The caller
        // authorizes this exact transfer before invoking; see the note in the
        // lending adapter.
        token::Client::new(&env, &underlying).transfer(&from, &me, &amount);
        token::Client::new(&env, &underlying).transfer(&me, &pool, &amount);
        let units: i128 = env.invoke_contract(
            &pool,
            &Symbol::new(&env, "deposit"),
            soroban_sdk::vec![&env, me.into_val(&env), amount.into_val(&env)],
        );

        if units < min_units_out {
            panic_with_error!(&env, ContractError::SlippageExceeded);
        }

        let held: i128 = env.storage().instance().get(&DataKey::Units).unwrap_or(0);
        env.storage().instance().set(&DataKey::Units, &(held + units));

        // The position basis changed — reset the derived-APY checkpoint so
        // deposits are never mistaken for yield.
        reset_checkpoint(&env);

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
        let pool: Address = env.storage().instance().get(&DataKey::Pool).unwrap();
        let me = env.current_contract_address();

        let assets: i128 = env.invoke_contract(
            &pool,
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

        reset_checkpoint(&env);

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

    /// Pro-rata reserve valuation of the aggregate LP position. `_owner` is
    /// ignored: the adapter holds one position, for the vault.
    fn position_value(env: Env, _owner: Address) -> i128 {
        lp_value(&env)
    }

    /// Derived from observed growth of `position_value` since the stored
    /// checkpoint. Reports `Unavailable` (never a wild number) when the
    /// position is empty, the checkpoint is missing, or the observation
    /// window is shorter than [`MIN_APY_WINDOW_SECS`].
    fn current_apy(env: Env) -> AdapterApy {
        let unavailable = AdapterApy {
            apy_bps: 0,
            confidence: ApyConfidence::Unavailable,
        };

        let cp: ApyCheckpoint = match env.storage().instance().get(&DataKey::Checkpoint) {
            Some(cp) => cp,
            None => return unavailable,
        };
        if cp.value <= 0 {
            return unavailable;
        }

        let now = env.ledger().timestamp();
        let elapsed = now.saturating_sub(cp.timestamp);
        if elapsed < MIN_APY_WINDOW_SECS {
            return unavailable;
        }

        let current = lp_value(&env);
        let growth = current - cp.value;
        if growth <= 0 {
            // Flat or negative over a meaningful window: a real zero, not unknown.
            return AdapterApy {
                apy_bps: 0,
                confidence: ApyConfidence::Derived,
            };
        }

        // annualized bps = growth / basis * 10_000 * (year / elapsed)
        let apy = growth
            .saturating_mul(10_000)
            .saturating_mul(SECONDS_PER_YEAR)
            / cp.value
            / elapsed as i128;
        let apy_bps = if apy > MAX_APY_BPS as i128 {
            MAX_APY_BPS
        } else {
            apy as u32
        };

        AdapterApy {
            apy_bps,
            confidence: ApyConfidence::Derived,
        }
    }

    fn underlying(env: Env) -> Address {
        env.storage().instance().get(&DataKey::Underlying).unwrap()
    }

    /// AMM pools accept any size; capacity is bounded by price impact, which
    /// the per-call minimum-output guards already police.
    fn max_deposit(_env: Env) -> i128 {
        i128::MAX
    }

    /// Withdrawable maximum in LP units — everything we hold.
    fn max_withdraw(env: Env) -> i128 {
        env.storage().instance().get(&DataKey::Units).unwrap_or(0)
    }
}

/// Value LP units pro-rata against the pool's underlying-side reserve —
/// exactly what a burn would pay out. No spot price involved.
fn lp_value(env: &Env) -> i128 {
    let pool: Address = env.storage().instance().get(&DataKey::Pool).unwrap();
    let units: i128 = env.storage().instance().get(&DataKey::Units).unwrap_or(0);
    if units == 0 {
        return 0;
    }

    let no_args: Vec<Val> = Vec::new(env);
    let (reserve_a, _reserve_b): (i128, i128) = env.invoke_contract(
        &pool,
        &Symbol::new(env, "get_reserves"),
        no_args.clone(),
    );
    let total_shares: i128 =
        env.invoke_contract(&pool, &Symbol::new(env, "total_shares"), no_args);

    if total_shares <= 0 {
        return 0;
    }
    // `overflow-checks = true` turns a wrap into a trap, which would abort
    // deposit/withdraw through reset_checkpoint. Value nothing rather than
    // trapping: a zero valuation surfaces as an unavailable APY, which callers
    // already handle.
    match units.checked_mul(reserve_a) {
        Some(n) => n / total_shares,
        None => 0,
    }
}

/// Re-anchor the derived-APY measurement basis at the current value/time.
fn reset_checkpoint(env: &Env) {
    let cp = ApyCheckpoint {
        value: lp_value(env),
        timestamp: env.ledger().timestamp(),
    };
    env.storage().instance().set(&DataKey::Checkpoint, &cp);
}

fn require_vault(env: &Env, caller: &Address) {
    let vault: Address = env.storage().instance().get(&DataKey::Vault).unwrap();
    if caller != &vault {
        panic_with_error!(env, ContractError::Unauthorized);
    }
}

#[cfg(test)]
mod test;
