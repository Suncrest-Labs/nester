//! Nester Vault Share Token
//!
//! Implements the Soroban Token Interface (SEP-41) plus vault-specific
//! mint/burn and exchange-rate logic.
//!
//! # Share math
//! Exchange rate is always `total_assets / total_supply`.
//!
//! * **`mint_for_deposit(to, amount)`** — atomically calculates shares, mints
//!   them, and increases `total_assets`.  Called only by the Vault contract.
//! * **`burn_for_withdrawal(from, shares)`** — atomically calculates the
//!   underlying amount, burns shares, and decreases `total_assets`.  Called
//!   only by the Vault contract.
//!
//! # First-deposit edge case
//! When `total_supply == 0`, shares are issued 1:1 with the deposited amount.
//!
//! # SEP-41 compliance
//! Standard functions (`transfer`, `approve`, `allowance`, `burn`, `burn_from`)
//! let shares be composed with other Soroban contracts/wallets.

#![no_std]

use soroban_sdk::{
    contract, contractimpl, contracttype, panic_with_error, symbol_short, Address, Env, String,
};

use nester_common::ContractError;

// ---------------------------------------------------------------------------
// Storage types
// ---------------------------------------------------------------------------

#[contracttype]
#[derive(Clone)]
struct AllowanceValue {
    amount: i128,
    expiration_ledger: u32,
}

#[contracttype]
#[derive(Clone)]
enum DataKey {
    /// Token balance of an account.
    Balance(Address),
    /// Approved allowance: (owner, spender) → AllowanceValue.
    Allowance(Address, Address),
    /// Total shares in circulation.
    TotalSupply,
    /// Total underlying assets represented by all shares.
    TotalAssets,
    /// Address of the Vault contract (the only minter/burner).
    Vault,
    /// Token metadata.
    Name,
    Symbol,
    Decimals,
}

// ---------------------------------------------------------------------------
// Contract
// ---------------------------------------------------------------------------

#[contract]
pub struct VaultTokenContract;

#[contractimpl]
impl VaultTokenContract {
    // -----------------------------------------------------------------------
    // Initialisation
    // -----------------------------------------------------------------------

    /// Initialise the token.
    ///
    /// * `vault` — the Vault contract address; the only caller allowed to mint
    ///   or burn shares via [`Self::mint_for_deposit`] / [`Self::burn_for_withdrawal`].
    pub fn initialize(env: Env, vault: Address, name: String, symbol: String, decimals: u32) {
        if env.storage().instance().has(&DataKey::Vault) {
            panic_with_error!(&env, ContractError::AlreadyInitialized);
        }
        // No vault.require_auth() here — vault isn't initialized yet when this is called.
        // Auth for vault-only operations is enforced in mint_for_deposit, burn_for_withdrawal, etc.
        env.storage().instance().set(&DataKey::Vault, &vault);
        env.storage().instance().set(&DataKey::Name, &name);
        env.storage().instance().set(&DataKey::Symbol, &symbol);
        env.storage().instance().set(&DataKey::Decimals, &decimals);
        env.storage().instance().set(&DataKey::TotalSupply, &0_i128);
        env.storage().instance().set(&DataKey::TotalAssets, &0_i128);
    }

    // -----------------------------------------------------------------------
    // SEP-41 Token Interface
    // -----------------------------------------------------------------------

    /// Return the share balance of `id`.
    pub fn balance(env: Env, id: Address) -> i128 {
        get_balance(&env, &id)
    }

    /// Transfer `amount` shares from `from` to `to`.
    pub fn transfer(env: Env, from: Address, to: Address, amount: i128) {
        from.require_auth();
        spend_balance(&env, &from, amount);
        receive_balance(&env, &to, amount);
        env.events()
            .publish((symbol_short!("transfer"), from, to), amount);
    }

    /// Transfer `amount` shares from `from` to `to` using `spender`'s allowance.
    pub fn transfer_from(env: Env, spender: Address, from: Address, to: Address, amount: i128) {
        spender.require_auth();
        spend_allowance(&env, &from, &spender, amount);
        spend_balance(&env, &from, amount);
        receive_balance(&env, &to, amount);
        env.events()
            .publish((symbol_short!("xfer_frm"), spender, from), (to, amount));
    }

    /// Approve `spender` to transfer up to `amount` of `from`'s shares until
    /// `expiration_ledger`.
    pub fn approve(
        env: Env,
        from: Address,
        spender: Address,
        amount: i128,
        expiration_ledger: u32,
    ) {
        from.require_auth();
        env.storage().instance().set(
            &DataKey::Allowance(from.clone(), spender.clone()),
            &AllowanceValue {
                amount,
                expiration_ledger,
            },
        );
        env.events().publish(
            (symbol_short!("approve"), from, spender),
            (amount, expiration_ledger),
        );
    }

    /// Return the remaining allowance. Returns 0 if the approval has expired.
    pub fn allowance(env: Env, from: Address, spender: Address) -> i128 {
        get_allowance(&env, &from, &spender)
    }

    /// Burn `amount` of `from`'s own shares (SEP-41 user-initiated burn).
    pub fn burn(env: Env, from: Address, amount: i128) {
        from.require_auth();
        spend_balance(&env, &from, amount);
        set_total_supply(&env, get_total_supply(&env) - amount);
        env.events().publish((symbol_short!("burn"), from), amount);
    }

    /// Burn using an allowance.
    pub fn burn_from(env: Env, spender: Address, from: Address, amount: i128) {
        spender.require_auth();
        spend_allowance(&env, &from, &spender, amount);
        spend_balance(&env, &from, amount);
        set_total_supply(&env, get_total_supply(&env) - amount);
        env.events()
            .publish((symbol_short!("burn_frm"), spender, from), amount);
    }

    /// Token name.
    pub fn name(env: Env) -> String {
        env.storage().instance().get(&DataKey::Name).unwrap()
    }

    /// Token symbol.
    pub fn symbol(env: Env) -> String {
        env.storage().instance().get(&DataKey::Symbol).unwrap()
    }

    /// Token decimals.
    pub fn decimals(env: Env) -> u32 {
        env.storage()
            .instance()
            .get(&DataKey::Decimals)
            .unwrap_or(7u32)
    }

    // -----------------------------------------------------------------------
    // Vault-specific queries
    // -----------------------------------------------------------------------

    /// Total shares in circulation.
    pub fn total_supply(env: Env) -> i128 {
        get_total_supply(&env)
    }

    /// Total underlying assets tracked by the vault.
    pub fn total_assets(env: Env) -> i128 {
        get_total_assets(&env)
    }

    /// Preview: shares a deposit of `amount` would mint **right now**.
    /// Does not change state.
    pub fn shares_for_deposit(env: Env, amount: i128) -> i128 {
        shares_for_deposit_math(amount, get_total_supply(&env), get_total_assets(&env))
    }

    /// Preview: underlying that `shares` would redeem for **right now**.
    /// Does not change state.
    pub fn amount_for_shares(env: Env, shares: i128) -> i128 {
        amount_for_shares_math(shares, get_total_supply(&env), get_total_assets(&env))
    }

    // -----------------------------------------------------------------------
    // Vault-only operations
    // -----------------------------------------------------------------------

    /// Called by the Vault during a deposit.
    ///
    /// Atomically:
    /// 1. Calculates shares at the current exchange rate.
    /// 2. Mints those shares to `to`.
    /// 3. Increases `total_assets` by `amount`.
    ///
    /// Returns the number of shares minted.
    pub fn mint_for_deposit(env: Env, to: Address, amount: i128) -> i128 {
        require_vault(&env);
        let total_supply = get_total_supply(&env);
        let total_assets = get_total_assets(&env);
        let shares = shares_for_deposit_math(amount, total_supply, total_assets);
        receive_balance(&env, &to, shares);
        set_total_supply(&env, total_supply + shares);
        set_total_assets(&env, total_assets + amount);
        env.events()
            .publish((symbol_short!("mint"), to), (shares, amount));
        shares
    }

    /// Called by the Vault during a withdrawal.
    ///
    /// Atomically:
    /// 1. Calculates underlying amount for `shares` at the current rate.
    /// 2. Burns `shares` from `from`.
    /// 3. Decreases `total_assets` by the calculated amount.
    ///
    /// Returns the underlying amount to release to the user.
    pub fn burn_for_withdrawal(env: Env, from: Address, shares: i128) -> i128 {
        require_vault(&env);
        let total_supply = get_total_supply(&env);
        let total_assets = get_total_assets(&env);
        let amount = amount_for_shares_math(shares, total_supply, total_assets);
        spend_balance(&env, &from, shares);
        set_total_supply(&env, total_supply - shares);
        set_total_assets(&env, total_assets - amount);
        env.events()
            .publish((symbol_short!("vlt_burn"), from), (shares, amount));
        amount
    }

    /// Update total assets (e.g. after yield accrual). Vault only.
    pub fn set_total_assets(env: Env, new_total: i128) {
        require_vault(&env);
        set_total_assets(&env, new_total);
        env.events().publish((symbol_short!("ta_upd"),), new_total);
    }
}

// ---------------------------------------------------------------------------
// Pure exchange-rate math
// ---------------------------------------------------------------------------

/// Shares to mint for a deposit of `amount`.
///
/// * First deposit (`total_supply == 0`): issues 1:1.
/// * Subsequent: `floor(amount * total_supply / total_assets)`.
pub fn shares_for_deposit_math(amount: i128, total_supply: i128, total_assets: i128) -> i128 {
    if total_supply == 0 || total_assets == 0 {
        amount
    } else {
        amount * total_supply / total_assets
    }
}

/// Underlying amount for `shares`.
///
/// `floor(shares * total_assets / total_supply)`.
pub fn amount_for_shares_math(shares: i128, total_supply: i128, total_assets: i128) -> i128 {
    if total_supply == 0 {
        shares
    } else {
        shares * total_assets / total_supply
    }
}

// ---------------------------------------------------------------------------
// Storage helpers
// ---------------------------------------------------------------------------

fn get_balance(env: &Env, account: &Address) -> i128 {
    env.storage()
        .instance()
        .get::<DataKey, i128>(&DataKey::Balance(account.clone()))
        .unwrap_or(0_i128)
}

fn set_balance(env: &Env, account: &Address, amount: i128) {
    env.storage()
        .instance()
        .set(&DataKey::Balance(account.clone()), &amount);
}

fn receive_balance(env: &Env, account: &Address, amount: i128) {
    set_balance(env, account, get_balance(env, account) + amount);
}

fn spend_balance(env: &Env, account: &Address, amount: i128) {
    let current = get_balance(env, account);
    if current < amount {
        panic_with_error!(env, ContractError::InsufficientBalance);
    }
    set_balance(env, account, current - amount);
}

fn get_total_supply(env: &Env) -> i128 {
    env.storage()
        .instance()
        .get(&DataKey::TotalSupply)
        .unwrap_or(0_i128)
}

fn set_total_supply(env: &Env, value: i128) {
    env.storage().instance().set(&DataKey::TotalSupply, &value);
}

fn get_total_assets(env: &Env) -> i128 {
    env.storage()
        .instance()
        .get(&DataKey::TotalAssets)
        .unwrap_or(0_i128)
}

fn set_total_assets(env: &Env, value: i128) {
    env.storage().instance().set(&DataKey::TotalAssets, &value);
}

fn get_allowance(env: &Env, from: &Address, spender: &Address) -> i128 {
    match env
        .storage()
        .instance()
        .get::<DataKey, AllowanceValue>(&DataKey::Allowance(from.clone(), spender.clone()))
    {
        None => 0,
        Some(v) => {
            if env.ledger().sequence() > v.expiration_ledger {
                0
            } else {
                v.amount
            }
        }
    }
}

fn spend_allowance(env: &Env, from: &Address, spender: &Address, amount: i128) {
    let current = get_allowance(env, from, spender);
    if current < amount {
        panic_with_error!(env, ContractError::Unauthorized);
    }
    let key = DataKey::Allowance(from.clone(), spender.clone());
    let existing: AllowanceValue = env.storage().instance().get(&key).unwrap();
    env.storage().instance().set(
        &key,
        &AllowanceValue {
            amount: current - amount,
            expiration_ledger: existing.expiration_ledger,
        },
    );
}

/// Panic unless the invocation is authorised by the stored Vault address.
fn require_vault(env: &Env) {
    let vault: Address = env
        .storage()
        .instance()
        .get(&DataKey::Vault)
        .unwrap_or_else(|| panic_with_error!(env, ContractError::NotInitialized));
    vault.require_auth();
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod test;
