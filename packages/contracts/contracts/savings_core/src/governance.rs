//! Roles and the timelock.
//!
//! Risk can be cut instantly but only added slowly. Adding an adapter,
//! raising a cap, raising the fee, shortening the timelock and burning
//! reserve shares are queued for the current timelock; once due, anyone may
//! execute them. Lowering a cap or the fee, lengthening the timelock, pausing
//! deposits and revoking a queued change are instant.
//!
//! - owner: manages roles, unpauses deposits, removes empty adapters.
//! - curator: proposes changes, lowers caps, sets the withdraw order.
//! - guardian: pauses deposits, zeroes caps, revokes queued changes.

use nester_common::{POSITION_TTL_EXTEND_TO_LEDGERS, POSITION_TTL_THRESHOLD_LEDGERS};
use soroban_sdk::{contracttype, panic_with_error, Address, Env, Symbol, Vec};

use crate::{adapters, fees, reserve_address, shares, AdapterConfig, SavingsError};

/// Shortest allowed timelock: 1 hour.
pub const MIN_TIMELOCK: u64 = 3_600;
/// Longest allowed timelock: 14 days. Bounds instant lengthening so it can
/// never lock out future changes for good.
pub const MAX_TIMELOCK: u64 = 1_209_600;
/// Most changes that may be queued at once.
pub const MAX_PENDING: u32 = 16;

#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum Change {
    /// Register `(adapter, abs_cap, rel_cap_bps)`.
    AddAdapter(Address, i128, u32),
    /// Set `(adapter, abs_cap, rel_cap_bps)`. Queued only if either cap
    /// goes up; otherwise applied immediately.
    RaiseCap(Address, i128, u32),
    /// Set the timelock in seconds. Queued only if shorter than the current
    /// one; otherwise applied immediately.
    SetTimelock(u64),
    /// Set the performance fee in bps (at most `fees::MAX_FEE_BPS`). Queued
    /// only if higher than the current fee; otherwise applied immediately.
    SetFee(u32),
    /// Burn this many of the reserve's shares, raising the share price for
    /// every other holder. Always queued. Dropped at execution if the
    /// reserve no longer holds that many shares (never clamped) or if any
    /// adapter cannot be valued.
    BurnReserve(i128),
}

#[contracttype]
#[derive(Clone)]
enum GovKey {
    Owner,
    PendingOwner,
    Curator,
    Guardian,
    Timelock,
    Paused,
    /// Ids of the queued changes (instance storage, at most `MAX_PENDING`).
    PendingIds,
    /// `(change, eta)` of queued change `id` (persistent storage).
    Pending(u32),
    NextId,
}

fn get<V: soroban_sdk::TryFromVal<Env, soroban_sdk::Val>>(env: &Env, key: &GovKey) -> Option<V> {
    env.storage().instance().get(key)
}

fn set<V: soroban_sdk::IntoVal<Env, soroban_sdk::Val>>(env: &Env, key: &GovKey, value: &V) {
    env.storage().instance().set(key, value);
}

fn timelock_valid(seconds: u64) -> bool {
    (MIN_TIMELOCK..=MAX_TIMELOCK).contains(&seconds)
}

fn check_timelock(env: &Env, seconds: u64) {
    if !timelock_valid(seconds) {
        panic_with_error!(env, SavingsError::InvalidTimelock);
    }
}

pub fn init(env: &Env, owner: &Address, timelock: u64) {
    check_timelock(env, timelock);
    set(env, &GovKey::Owner, owner);
    set(env, &GovKey::Timelock, &timelock);
}

// --- Roles -----------------------------------------------------------------

pub fn owner(env: &Env) -> Address {
    get(env, &GovKey::Owner).unwrap()
}

pub fn curator(env: &Env) -> Option<Address> {
    get(env, &GovKey::Curator)
}

pub fn guardian(env: &Env) -> Option<Address> {
    get(env, &GovKey::Guardian)
}

pub fn set_curator(env: &Env, curator: &Address) {
    set(env, &GovKey::Curator, curator);
}

pub fn set_guardian(env: &Env, guardian: &Address) {
    set(env, &GovKey::Guardian, guardian);
}

pub fn transfer_ownership(env: &Env, new_owner: &Address) {
    set(env, &GovKey::PendingOwner, new_owner);
}

pub fn accept_ownership(env: &Env, new_owner: &Address) {
    new_owner.require_auth();
    let pending: Option<Address> = get(env, &GovKey::PendingOwner);
    if pending.as_ref() != Some(new_owner) {
        panic_with_error!(env, SavingsError::Unauthorized);
    }
    set(env, &GovKey::Owner, new_owner);
    env.storage().instance().remove(&GovKey::PendingOwner);
}

/// Require `caller`'s auth and that it holds one of `roles`.
fn require_role(env: &Env, caller: &Address, roles: &[GovKey]) {
    caller.require_auth();
    let held = roles
        .iter()
        .any(|role| get::<Address>(env, role).as_ref() == Some(caller));
    if !held {
        panic_with_error!(env, SavingsError::Unauthorized);
    }
}

pub fn require_owner(env: &Env, caller: &Address) {
    require_role(env, caller, &[GovKey::Owner]);
}

pub fn require_curator(env: &Env, caller: &Address) {
    require_role(env, caller, &[GovKey::Curator]);
}

pub fn require_guardian(env: &Env, caller: &Address) {
    require_role(env, caller, &[GovKey::Guardian]);
}

pub fn require_owner_or_curator(env: &Env, caller: &Address) {
    require_role(env, caller, &[GovKey::Owner, GovKey::Curator]);
}

pub fn require_owner_or_guardian(env: &Env, caller: &Address) {
    require_role(env, caller, &[GovKey::Owner, GovKey::Guardian]);
}

// --- Deposit pause -----------------------------------------------------------

pub fn deposits_paused(env: &Env) -> bool {
    get(env, &GovKey::Paused).unwrap_or(false)
}

pub fn set_deposits_paused(env: &Env, paused: bool) {
    set(env, &GovKey::Paused, &paused);
}

// --- Caps --------------------------------------------------------------------

/// Set caps no higher than the current ones.
pub fn lower_cap(env: &Env, adapter: &Address, abs_cap: i128, rel_cap_bps: u32) {
    if raises_cap(env, adapter, abs_cap, rel_cap_bps) {
        panic_with_error!(env, SavingsError::CapIncrease);
    }
    adapters::set_caps(env, adapter, abs_cap, rel_cap_bps);
}

fn raises_cap(env: &Env, adapter: &Address, abs_cap: i128, rel_cap_bps: u32) -> bool {
    let cur = adapters::config(env, adapter);
    abs_cap > cur.abs_cap || rel_cap_bps > cur.rel_cap_bps
}

// --- Timelock ----------------------------------------------------------------

pub fn timelock(env: &Env) -> u64 {
    get(env, &GovKey::Timelock).unwrap()
}

fn pending_ids(env: &Env) -> Vec<u32> {
    get(env, &GovKey::PendingIds).unwrap_or_else(|| Vec::new(env))
}

pub fn pending(env: &Env) -> Vec<(u32, Change, u64)> {
    let mut out = Vec::new(env);
    for id in pending_ids(env).iter() {
        let (change, eta) = load(env, id);
        out.push_back((id, change, eta));
    }
    out
}

fn load(env: &Env, id: u32) -> (Change, u64) {
    env.storage()
        .persistent()
        .get(&GovKey::Pending(id))
        .unwrap_or_else(|| panic_with_error!(env, SavingsError::NoSuchChange))
}

/// Queue a risk-adding change for `now + timelock` and return its id (from
/// 1). A change that adds no risk is applied immediately and returns 0.
pub fn propose(env: &Env, asset: &Address, change: Change) -> u32 {
    let adds_risk = match &change {
        Change::AddAdapter(adapter, abs_cap, rel_cap_bps) => {
            adapters::check_new(env, asset, &config(adapter, *abs_cap, *rel_cap_bps));
            true
        }
        Change::RaiseCap(adapter, abs_cap, rel_cap_bps) => {
            adapters::check_caps(env, *abs_cap, *rel_cap_bps);
            raises_cap(env, adapter, *abs_cap, *rel_cap_bps)
        }
        Change::SetTimelock(seconds) => {
            check_timelock(env, *seconds);
            *seconds < timelock(env)
        }
        Change::SetFee(bps) => {
            if *bps > fees::MAX_FEE_BPS {
                panic_with_error!(env, SavingsError::FeeTooHigh);
            }
            *bps > fees::fee_bps(env)
        }
        Change::BurnReserve(amount) => {
            if *amount <= 0 {
                panic_with_error!(env, SavingsError::ZeroAmount);
            }
            if *amount > reserve_shares(env) {
                panic_with_error!(env, SavingsError::InsufficientShares);
            }
            true
        }
    };
    if !adds_risk {
        apply(env, asset, &change);
        publish(env, "change_executed", 0, change);
        return 0;
    }

    let mut ids = pending_ids(env);
    if ids.len() >= MAX_PENDING {
        panic_with_error!(env, SavingsError::QueueFull);
    }
    let id: u32 = get(env, &GovKey::NextId).unwrap_or(1);
    let eta = env.ledger().timestamp().saturating_add(timelock(env));
    let key = GovKey::Pending(id);
    let store = env.storage().persistent();
    store.set(&key, &(change.clone(), eta));
    store.extend_ttl(
        &key,
        POSITION_TTL_THRESHOLD_LEDGERS,
        POSITION_TTL_EXTEND_TO_LEDGERS,
    );
    ids.push_back(id);
    set(env, &GovKey::PendingIds, &ids);
    set(env, &GovKey::NextId, &(id + 1));
    publish(env, "change_proposed", id, (change, eta));
    id
}

/// Apply a queued change once its ETA has passed. Callable by anyone.
///
/// The change is re-validated first. If it no longer holds (say the adapter
/// was added or removed since), it is dropped from the queue, a
/// `change_dropped` event is emitted and `false` is returned; returning
/// rather than panicking keeps the removal. Otherwise returns `true`.
pub fn execute(env: &Env, asset: &Address, id: u32) -> bool {
    let (change, eta) = load(env, id);
    if env.ledger().timestamp() < eta {
        panic_with_error!(env, SavingsError::NotReady);
    }
    remove(env, id);
    if !still_valid(env, asset, &change) {
        publish(env, "change_dropped", id, change);
        return false;
    }
    apply(env, asset, &change);
    publish(env, "change_executed", id, change);
    true
}

pub fn revoke(env: &Env, id: u32) {
    let (change, _) = load(env, id);
    remove(env, id);
    publish(env, "change_revoked", id, change);
}

fn remove(env: &Env, id: u32) {
    env.storage().persistent().remove(&GovKey::Pending(id));
    let mut ids = pending_ids(env);
    if let Some(i) = ids.first_index_of(id) {
        ids.remove(i);
    }
    set(env, &GovKey::PendingIds, &ids);
}

/// Whether `change` can still be applied. Never panics.
fn still_valid(env: &Env, asset: &Address, change: &Change) -> bool {
    match change {
        Change::AddAdapter(adapter, abs_cap, rel_cap_bps) => {
            adapters::can_add(env, asset, &config(adapter, *abs_cap, *rel_cap_bps))
        }
        Change::RaiseCap(adapter, abs_cap, rel_cap_bps) => {
            adapters::caps_valid(*abs_cap, *rel_cap_bps) && adapters::is_listed(env, adapter)
        }
        Change::SetTimelock(seconds) => timelock_valid(*seconds),
        Change::SetFee(bps) => *bps <= fees::MAX_FEE_BPS && fees::strict_total(env).is_some(),
        Change::BurnReserve(amount) => {
            *amount > 0 && *amount <= reserve_shares(env) && fees::strict_total(env).is_some()
        }
    }
}

fn apply(env: &Env, asset: &Address, change: &Change) {
    match change.clone() {
        Change::AddAdapter(adapter, abs_cap, rel_cap_bps) => {
            adapters::add(env, asset, config(&adapter, abs_cap, rel_cap_bps))
        }
        Change::RaiseCap(adapter, abs_cap, rel_cap_bps) => {
            adapters::set_caps(env, &adapter, abs_cap, rel_cap_bps)
        }
        Change::SetTimelock(seconds) => set(env, &GovKey::Timelock, &seconds),
        Change::SetFee(bps) => fees::set_fee_bps(env, bps),
        Change::BurnReserve(amount) => fees::burn_reserve(env, amount),
    }
}

fn reserve_shares(env: &Env) -> i128 {
    shares::balance(env, &reserve_address(env))
}

fn config(adapter: &Address, abs_cap: i128, rel_cap_bps: u32) -> AdapterConfig {
    AdapterConfig {
        adapter: adapter.clone(),
        abs_cap,
        rel_cap_bps,
    }
}

fn publish<D: soroban_sdk::IntoVal<Env, soroban_sdk::Val>>(
    env: &Env,
    kind: &str,
    id: u32,
    data: D,
) {
    env.events().publish((Symbol::new(env, kind), id), data);
}
