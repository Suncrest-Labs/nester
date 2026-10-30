//! Savings-share ledger (balances, allowances, supply) and the
//! assets <-> shares conversion math.

use nester_common::{POSITION_TTL_EXTEND_TO_LEDGERS, POSITION_TTL_THRESHOLD_LEDGERS};
use soroban_sdk::{contracttype, panic_with_error, Address, Env};

use crate::SavingsError;

/// Virtual shares and virtual assets added to both sides of every conversion.
/// Equal on both sides so the first deposit mints 1:1, and large enough that a
/// donation to the core is captured mostly by the virtual position instead of
/// stealing from the next depositor (ERC-4626 inflation-attack defence).
pub const VIRTUAL_OFFSET: i128 = 10_000;

#[contracttype]
#[derive(Clone)]
pub struct AllowanceValue {
    pub amount: i128,
    pub expiration_ledger: u32,
}

#[contracttype]
#[derive(Clone)]
enum ShareKey {
    Supply,
    Balance(Address),
    Allowance(Address, Address),
}

pub fn total_supply(env: &Env) -> i128 {
    env.storage().instance().get(&ShareKey::Supply).unwrap_or(0)
}

pub fn balance(env: &Env, id: &Address) -> i128 {
    env.storage()
        .persistent()
        .get(&ShareKey::Balance(id.clone()))
        .unwrap_or(0)
}

fn set_balance(env: &Env, id: &Address, amount: i128) {
    let key = ShareKey::Balance(id.clone());
    env.storage().persistent().set(&key, &amount);
    env.storage().persistent().extend_ttl(
        &key,
        POSITION_TTL_THRESHOLD_LEDGERS,
        POSITION_TTL_EXTEND_TO_LEDGERS,
    );
}

fn debit(env: &Env, id: &Address, amount: i128) {
    let bal = balance(env, id);
    if bal < amount {
        panic_with_error!(env, SavingsError::InsufficientShares);
    }
    set_balance(env, id, bal - amount);
}

fn credit(env: &Env, id: &Address, amount: i128) {
    set_balance(env, id, balance(env, id) + amount);
}

pub fn mint(env: &Env, to: &Address, amount: i128) {
    credit(env, to, amount);
    env.storage()
        .instance()
        .set(&ShareKey::Supply, &(total_supply(env) + amount));
}

pub fn burn(env: &Env, from: &Address, amount: i128) {
    debit(env, from, amount);
    env.storage()
        .instance()
        .set(&ShareKey::Supply, &(total_supply(env) - amount));
}

pub fn move_shares(env: &Env, from: &Address, to: &Address, amount: i128) {
    debit(env, from, amount);
    credit(env, to, amount);
}

pub fn allowance(env: &Env, from: &Address, spender: &Address) -> i128 {
    let value: Option<AllowanceValue> = env
        .storage()
        .temporary()
        .get(&ShareKey::Allowance(from.clone(), spender.clone()));
    match value {
        Some(v) if v.expiration_ledger >= env.ledger().sequence() => v.amount,
        _ => 0,
    }
}

pub fn set_allowance(
    env: &Env,
    from: &Address,
    spender: &Address,
    amount: i128,
    expiration_ledger: u32,
) {
    let ledger = env.ledger().sequence();
    if amount > 0 && expiration_ledger < ledger {
        panic_with_error!(env, SavingsError::InvalidExpiration);
    }
    let key = ShareKey::Allowance(from.clone(), spender.clone());
    env.storage().temporary().set(
        &key,
        &AllowanceValue {
            amount,
            expiration_ledger,
        },
    );
    if amount > 0 {
        let live_for = expiration_ledger - ledger;
        env.storage()
            .temporary()
            .extend_ttl(&key, live_for, live_for);
    }
}

pub fn spend_allowance(env: &Env, from: &Address, spender: &Address, amount: i128) {
    let current = allowance(env, from, spender);
    if current < amount {
        panic_with_error!(env, SavingsError::Unauthorized);
    }
    if amount > 0 {
        let key = ShareKey::Allowance(from.clone(), spender.clone());
        let mut value: AllowanceValue = env.storage().temporary().get(&key).unwrap();
        value.amount = current - amount;
        env.storage().temporary().set(&key, &value);
    }
}

fn mul_div_down(a: i128, b: i128, c: i128) -> i128 {
    a * b / c
}

fn mul_div_up(a: i128, b: i128, c: i128) -> i128 {
    (a * b + c - 1) / c
}

/// Shares minted for `assets` (rounded down, favours the vault).
pub fn to_shares_down(assets: i128, total_assets: i128, supply: i128) -> i128 {
    mul_div_down(
        assets,
        supply + VIRTUAL_OFFSET,
        total_assets + VIRTUAL_OFFSET,
    )
}

/// Shares burned to pay out exactly `assets` (rounded up, favours the vault).
pub fn to_shares_up(assets: i128, total_assets: i128, supply: i128) -> i128 {
    mul_div_up(
        assets,
        supply + VIRTUAL_OFFSET,
        total_assets + VIRTUAL_OFFSET,
    )
}

/// Assets paid for `shares` (rounded down, favours the vault).
pub fn to_assets_down(shares: i128, total_assets: i128, supply: i128) -> i128 {
    mul_div_down(
        shares,
        total_assets + VIRTUAL_OFFSET,
        supply + VIRTUAL_OFFSET,
    )
}
