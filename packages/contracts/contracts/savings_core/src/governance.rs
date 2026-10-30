//! Roles and the timelock.
//!
//! Risk can be cut instantly but only added slowly. Adding an adapter,
//! raising a cap and shortening the timelock are queued for the current
//! timelock; once due, anyone may execute them. Lowering a cap, lengthening
//! the timelock, pausing deposits and revoking a queued change are instant.
//!
//! - owner: manages roles, unpauses deposits, removes empty adapters.
//! - curator: proposes changes, lowers caps, sets the withdraw order.
//! - guardian: pauses deposits, zeroes caps, revokes queued changes.

use soroban_sdk::{contracttype, panic_with_error, Address, Env, Symbol, Vec};

use crate::{adapters, AdapterConfig, SavingsError};

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
    Pending,
    NextId,
}

fn get<V: soroban_sdk::TryFromVal<Env, soroban_sdk::Val>>(env: &Env, key: &GovKey) -> Option<V> {
    env.storage().instance().get(key)
}

fn set<V: soroban_sdk::IntoVal<Env, soroban_sdk::Val>>(env: &Env, key: &GovKey, value: &V) {
    env.storage().instance().set(key, value);
}

pub fn init(env: &Env, owner: &Address, timelock: u64) {
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

pub fn pending(env: &Env) -> Vec<(u32, Change, u64)> {
    get(env, &GovKey::Pending).unwrap_or_else(|| Vec::new(env))
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
        Change::SetTimelock(seconds) => *seconds < timelock(env),
    };
    if !adds_risk {
        apply(env, asset, &change);
        publish(env, "change_executed", 0, change);
        return 0;
    }

    let id: u32 = get(env, &GovKey::NextId).unwrap_or(1);
    let eta = env.ledger().timestamp().saturating_add(timelock(env));
    let mut queue = pending(env);
    queue.push_back((id, change.clone(), eta));
    set(env, &GovKey::Pending, &queue);
    set(env, &GovKey::NextId, &(id + 1));
    publish(env, "change_proposed", id, (change, eta));
    id
}

/// Apply a queued change once its ETA has passed. Callable by anyone; the
/// change is re-validated as it is applied.
pub fn execute(env: &Env, asset: &Address, id: u32) {
    let (change, eta) = take(env, id);
    if env.ledger().timestamp() < eta {
        panic_with_error!(env, SavingsError::NotReady);
    }
    apply(env, asset, &change);
    publish(env, "change_executed", id, change);
}

pub fn revoke(env: &Env, id: u32) {
    let (change, _) = take(env, id);
    publish(env, "change_revoked", id, change);
}

/// Remove pending change `id` from the queue (a later panic reverts this).
fn take(env: &Env, id: u32) -> (Change, u64) {
    let mut queue = pending(env);
    let i = queue
        .iter()
        .position(|(pid, _, _)| pid == id)
        .unwrap_or_else(|| panic_with_error!(env, SavingsError::NoSuchChange)) as u32;
    let (_, change, eta) = queue.get_unchecked(i);
    queue.remove(i);
    set(env, &GovKey::Pending, &queue);
    (change, eta)
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
    }
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
