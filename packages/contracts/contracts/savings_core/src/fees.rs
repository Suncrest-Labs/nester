//! Performance fee on new share-price highs, minted as shares to the reserve.
//!
//! The high-water mark (HWM) is the highest share price on which a fee has
//! been charged. When the price rises above it, `fee_bps` of the gain above
//! the mark is minted as new shares to the reserve, priced so those shares
//! are worth exactly the fee after minting, and the mark moves to the
//! post-mint price. Losses leave the mark where it is, so no fee is charged
//! again until the price recovers past it.
//!
//! Accrual values strictly: if any adapter cannot be valued the fee is
//! skipped entirely (no fee off a partial valuation) and the mark is left
//! alone; it is charged on a later accrual once every adapter values again.
//!
//! Reserve shares leave only through a `BurnReserve` schedule that vests
//! linearly over `BURN_VESTING`: each accrual burns the portion vested since
//! the last one. A one-step burn would be a share-price jump that anyone
//! could capture by depositing just before it and redeeming just after;
//! vesting spreads it so the value goes to holders in proportion to time
//! held. Each vested burn raises the mark to the post-burn price if higher,
//! so value returned by a burn is never charged a fee.
//!
//! The mark is kept internally as assets per 10^14 shares (`HWM_SCALE`) so
//! repeated small accruals do not lose the sub-stroop part of the price and
//! overcharge; `high_water_mark()` reports it per 10^7 shares like
//! `share_price()`.

use soroban_sdk::{contracttype, symbol_short, Env};

use crate::shares::{self, VIRTUAL_OFFSET};
use crate::{adapters, idle_assets, reserve_address, PRICE_SCALE};

pub const DEFAULT_FEE_BPS: u32 = 1_000;
pub const MAX_FEE_BPS: u32 = 2_000;
const BPS: i128 = 10_000;
const HWM_SCALE: i128 = 100_000_000_000_000;
/// Seconds over which a reserve burn vests: 7 days.
pub const BURN_VESTING: u64 = 604_800;

#[contracttype]
#[derive(Clone)]
enum FeeKey {
    FeeBps,
    Hwm,
    Burn,
}

#[contracttype]
#[derive(Clone)]
struct BurnSchedule {
    total: i128,
    burned: i128,
    start: u64,
    end: u64,
}

pub fn init(env: &Env) {
    env.storage()
        .instance()
        .set(&FeeKey::FeeBps, &DEFAULT_FEE_BPS);
    set_hwm(env, HWM_SCALE);
}

pub fn fee_bps(env: &Env) -> u32 {
    env.storage().instance().get(&FeeKey::FeeBps).unwrap()
}

/// Accrue under the current fee, then switch to `bps`.
pub fn set_fee_bps(env: &Env, bps: u32) {
    accrue(env);
    env.storage().instance().set(&FeeKey::FeeBps, &bps);
}

/// The mark in assets per 10^7 shares (the `share_price()` unit).
pub fn high_water_mark(env: &Env) -> i128 {
    hwm(env) / (HWM_SCALE / PRICE_SCALE)
}

fn hwm(env: &Env) -> i128 {
    env.storage().instance().get(&FeeKey::Hwm).unwrap()
}

fn set_hwm(env: &Env, value: i128) {
    env.storage().instance().set(&FeeKey::Hwm, &value);
}

fn price(total: i128, supply: i128) -> i128 {
    (total + VIRTUAL_OFFSET) * HWM_SCALE / (supply + VIRTUAL_OFFSET)
}

/// Strict valuation, or `None` if any adapter cannot be valued.
pub fn strict_total(env: &Env) -> Option<i128> {
    adapters::valuation(env, idle_assets(env), true).ok()
}

/// Charge any fee due and return the fee shares minted. Never panics on an
/// unavailable adapter: accrual is skipped and 0 returned.
/// Also burns any vested reserve shares; when skipped, they burn later.
pub fn accrue(env: &Env) -> i128 {
    let Some(total) = strict_total(env) else {
        return 0;
    };
    let minted = accrue_at(env, total);
    burn_vested(env, total);
    minted
}

fn accrue_at(env: &Env, total: i128) -> i128 {
    let supply = shares::total_supply(env);
    let mark = hwm(env);
    if price(total, supply) <= mark {
        return 0;
    }
    // Assets the supply would be worth at the mark, rounded up so the gain
    // (and the fee) rounds down.
    let at_mark = (mark * (supply + VIRTUAL_OFFSET) + HWM_SCALE - 1) / HWM_SCALE;
    let gain = total + VIRTUAL_OFFSET - at_mark;
    let fee_bps = fee_bps(env);
    let fee_assets = gain * fee_bps as i128 / BPS;
    // ERC-4626 fee shares: worth `fee_assets` after they are minted.
    let fee_shares = if fee_assets > 0 {
        fee_assets * (supply + VIRTUAL_OFFSET) / (total + VIRTUAL_OFFSET - fee_assets)
    } else {
        0
    };
    if fee_shares == 0 && fee_bps > 0 {
        // Too small to charge yet; keep the mark so the gain carries over.
        return 0;
    }
    if fee_shares > 0 {
        let reserve = reserve_address(env);
        shares::mint(env, &reserve, fee_shares);
        env.events()
            .publish((symbol_short!("fee"), reserve), (fee_assets, fee_shares));
    }
    set_hwm(env, price(total, supply + fee_shares).max(mark));
    fee_shares
}

fn schedule(env: &Env) -> Option<BurnSchedule> {
    env.storage().instance().get(&FeeKey::Burn)
}

/// `(total, burned, start, end)` of the latest burn schedule.
pub fn burn_schedule(env: &Env) -> Option<(i128, i128, u64, u64)> {
    schedule(env).map(|s| (s.total, s.burned, s.start, s.end))
}

/// Whether a burn schedule still has shares left to burn.
pub fn burn_vesting(env: &Env) -> bool {
    schedule(env).is_some_and(|s| s.burned < s.total)
}

/// Start burning `amount` reserve shares linearly over `BURN_VESTING`.
pub fn start_burn(env: &Env, amount: i128) {
    let start = env.ledger().timestamp();
    let schedule = BurnSchedule {
        total: amount,
        burned: 0,
        start,
        end: start + BURN_VESTING,
    };
    env.storage().instance().set(&FeeKey::Burn, &schedule);
}

fn burn_vested(env: &Env, total: i128) {
    let Some(mut s) = schedule(env) else {
        return;
    };
    if s.burned >= s.total {
        return;
    }
    let elapsed = env.ledger().timestamp().min(s.end) - s.start;
    let vested = s.total * elapsed as i128 / (s.end - s.start) as i128;
    let reserve = reserve_address(env);
    let amount = (vested - s.burned).min(shares::balance(env, &reserve));
    if amount <= 0 {
        return;
    }
    shares::burn(env, &reserve, amount);
    s.burned += amount;
    env.storage().instance().set(&FeeKey::Burn, &s);
    let after = price(total, shares::total_supply(env));
    if after > hwm(env) {
        set_hwm(env, after);
    }
    env.events()
        .publish((symbol_short!("burn"), reserve), amount);
}
