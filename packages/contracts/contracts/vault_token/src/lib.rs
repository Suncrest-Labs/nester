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

soroban_sdk::contractmeta!(key = "name", val = "VaultTokenContract");
soroban_sdk::contractmeta!(key = "version", val = "0.1.0");
soroban_sdk::contractmeta!(key = "source_repo", val = "github.com/Suncrest-Labs/nester");
soroban_sdk::contractmeta!(key = "commit", val = "5a636a7b");

use soroban_sdk::{
    contract, contractimpl, contracttype, panic_with_error, symbol_short, Address, BytesN, Env,
    String,
};

use nester_access_control::{AccessControl, Role};
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
    /// Timestamp of the last deposit or inbound transfer for a holder.
    /// Used by the vault to determine early-withdrawal fee eligibility.
    DepositTimestamp(Address),
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
        // AccessControl uses vault as initial admin so role governance can be managed.
        AccessControl::initialize(&env, &vault);
        nester_common::Upgrade::init_schema_version(&env, 1);
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
        // Recipient gets a fresh deposit timestamp so they cannot inherit an
        // old timestamp and bypass the early-withdrawal fee immediately after
        // receiving shares.
        env.storage().persistent().set(
            &DataKey::DepositTimestamp(to.clone()),
            &env.ledger().timestamp(),
        );
        // Sender's timestamp is no longer meaningful once their balance is zero.
        if get_balance(&env, &from) == 0 {
            env.storage()
                .persistent()
                .remove(&DataKey::DepositTimestamp(from.clone()));
        }
        env.events()
            .publish((symbol_short!("transfer"), from, to), amount);
    }

    /// Transfer `amount` shares from `from` to `to` using `spender`'s allowance.
    pub fn transfer_from(env: Env, spender: Address, from: Address, to: Address, amount: i128) {
        spender.require_auth();
        spend_allowance(&env, &from, &spender, amount);
        spend_balance(&env, &from, amount);
        receive_balance(&env, &to, amount);
        // Same timestamp semantics as transfer().
        env.storage().persistent().set(
            &DataKey::DepositTimestamp(to.clone()),
            &env.ledger().timestamp(),
        );
        if get_balance(&env, &from) == 0 {
            env.storage()
                .persistent()
                .remove(&DataKey::DepositTimestamp(from.clone()));
        }
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

    /// Burn `amount` of `from`'s shares.
    ///
    /// Restricted to the vault contract only. Direct calls by token holders are
    /// rejected to prevent bypassing vault fee accounting (management fee,
    /// early-withdrawal fee, performance fee). The vault's `withdraw()` path
    /// must be used instead, which calls `burn_for_withdrawal` after applying
    /// all fees and updating `total_shares`.
    pub fn burn(env: Env, from: Address, amount: i128) {
        require_vault(&env);
        let total_supply = get_total_supply(&env);
        let total_assets = get_total_assets(&env);
        let assets_to_reduce = if total_supply > 0 && total_assets > 0 {
            amount * total_assets / total_supply
        } else {
            0
        };
        spend_balance(&env, &from, amount);
        set_total_supply(&env, total_supply - amount);
        set_total_assets(&env, total_assets - assets_to_reduce);
        env.events().publish((symbol_short!("burn"), from), amount);
    }

    /// Burn using an allowance.
    pub fn burn_from(env: Env, spender: Address, from: Address, amount: i128) {
        spender.require_auth();
        spend_allowance(&env, &from, &spender, amount);
        let total_supply = get_total_supply(&env);
        let total_assets = get_total_assets(&env);
        let assets_to_reduce = if total_supply > 0 && total_assets > 0 {
            amount * total_assets / total_supply
        } else {
            0
        };
        spend_balance(&env, &from, amount);
        set_total_supply(&env, total_supply - amount);
        set_total_assets(&env, total_assets - assets_to_reduce);
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

    /// Price per share. Uses 10,000,000 as the 1.0 unit denominator since decimals is 7.
    pub fn share_price(env: Env) -> i128 {
        let total_supply = get_total_supply(&env);
        let total_assets = get_total_assets(&env);
        amount_for_shares_math(10_000_000, total_supply, total_assets)
    }

    /// Calculates underlying asset value corresponding to the user's current token balance.
    pub fn underlying_balance(env: Env, user: Address) -> i128 {
        let balance = get_balance(&env, &user);
        amount_for_shares_math(balance, get_total_supply(&env), get_total_assets(&env))
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
        // Record deposit time so the vault can enforce the early-withdrawal
        // lock period even when shares are later received via transfer.
        env.storage().persistent().set(
            &DataKey::DepositTimestamp(to.clone()),
            &env.ledger().timestamp(),
        );
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

    /// Mint `shares` to `to` without changing `total_assets` (issue #802).
    ///
    /// Unlike [`Self::mint_for_deposit`], this does not correspond to new
    /// assets entering the vault — it is the yield-boost mechanism for
    /// locked positions, which mints a locked position extra shares out of
    /// the SAME total_assets increase every other holder's shares already
    /// benefit from (the vault computes the exact share count via
    /// `nester_common::fees::boosted_shares_after_yield` before calling
    /// this; see `contracts/vault/src/locks.rs`'s module doc for why this
    /// conserves total value exactly rather than inflating the vault).
    /// Vault only.
    pub fn mint_boost_shares(env: Env, to: Address, shares: i128) {
        require_vault(&env);
        if shares <= 0 {
            return;
        }
        let total_supply = get_total_supply(&env);
        receive_balance(&env, &to, shares);
        set_total_supply(&env, total_supply + shares);
        env.events()
            .publish((symbol_short!("bst_mint"), to), shares);
    }

    /// Burn `shares` from `from` without changing `total_assets` (issue
    /// #802).
    ///
    /// Used by [`break_lock`](../vault/index.html) (see
    /// `contracts/vault/src/locks.rs`): breaking a lock early transfers the
    /// underlying assets out and reduces `total_assets` through the vault's
    /// own penalty-escrow accounting (`charge_penalty`), a separate step
    /// from this call — burning the shares here just retires the locked
    /// position's claim on the pool without independently double-counting
    /// an asset-side change this contract does not have visibility into.
    /// Vault only.
    pub fn burn_shares(env: Env, from: Address, shares: i128) {
        require_vault(&env);
        if shares <= 0 {
            return;
        }
        let total_supply = get_total_supply(&env);
        spend_balance(&env, &from, shares);
        set_total_supply(&env, total_supply - shares);
        env.events()
            .publish((symbol_short!("lck_burn"), from), shares);
    }

    /// Return the deposit timestamp for `user` as tracked by this token
    /// contract.  Returns 0 if no timestamp has been recorded (e.g. the user
    /// has never received shares via deposit or transfer).
    pub fn get_deposit_time(env: Env, user: Address) -> u64 {
        env.storage()
            .persistent()
            .get(&DataKey::DepositTimestamp(user))
            .unwrap_or(0)
    }

    // -----------------------------------------------------------------------
    // Role management passthroughs
    // -----------------------------------------------------------------------

    /// Grant a role to an address. Only callable by the contract admin.
    pub fn grant_role(env: Env, grantor: Address, grantee: Address, role: Role) {
        AccessControl::grant_role(&env, &grantor, &grantee, role);
    }

    /// Revoke a role from an address. Only callable by the contract admin.
    pub fn revoke_role(env: Env, revoker: Address, target: Address, role: Role) {
        AccessControl::revoke_role(&env, &revoker, &target, role);
    }

    /// Initiate a two-step admin transfer.
    pub fn transfer_admin(env: Env, current_admin: Address, new_admin: Address) {
        AccessControl::transfer_admin(&env, &current_admin, &new_admin);
    }

    /// Accept pending admin transfer.
    pub fn accept_admin(env: Env, new_admin: Address) {
        AccessControl::accept_admin(&env, &new_admin);
    }

    /// Check if an account holds a specific role.
    pub fn has_role(env: Env, account: Address, role: Role) -> bool {
        AccessControl::has_role(&env, &account, role)
    }

    // -----------------------------------------------------------------------
    // Upgradeability & Schema Migration
    // -----------------------------------------------------------------------

    /// Proposes a new WASM upgrade for the vault token.
    ///
    /// Requires Upgrader role and enforces MIN_UPGRADE_DELAY_VAULT_TOKEN (48 hours).
    pub fn propose_upgrade(env: Env, admin: Address, new_wasm_hash: BytesN<32>, eta: u64) {
        AccessControl::require_role(&env, &admin, Role::Upgrader);
        nester_common::Upgrade::propose_upgrade(
            &env,
            &admin,
            new_wasm_hash,
            nester_common::MIN_UPGRADE_DELAY_VAULT_TOKEN,
            eta,
        );
    }

    /// Cancels a pending WASM upgrade for the vault token.
    ///
    /// Requires Upgrader role.
    pub fn cancel_upgrade(env: Env, admin: Address) {
        AccessControl::require_role(&env, &admin, Role::Upgrader);
        nester_common::Upgrade::cancel_upgrade(&env, &admin);
    }

    /// Executes a matured WASM upgrade for the vault token.
    ///
    /// Execution is permissionless after maturity.
    pub fn execute_upgrade(env: Env, caller: Address, wasm_hash: BytesN<32>) {
        nester_common::Upgrade::execute_upgrade(&env, &caller, wasm_hash);
    }

    /// Retrieves pending upgrade details if present.
    pub fn get_pending_upgrade(env: Env) -> Option<nester_common::PendingUpgrade> {
        nester_common::Upgrade::get_pending_upgrade(&env)
    }

    /// Returns current contract schema version.
    pub fn get_schema_version(env: Env) -> u32 {
        nester_common::Upgrade::get_schema_version(&env)
    }

    /// Bumps schema version if needed (idempotent).
    pub fn migrate(env: Env) -> u32 {
        let current = nester_common::Upgrade::get_schema_version(&env);
        let target = 1u32;
        if current < target {
            nester_common::Upgrade::set_schema_version(&env, target);
            target
        } else {
            current
        }
    }

    pub fn version(env: Env) -> soroban_sdk::String {
        soroban_sdk::String::from_str(&env, env!("CARGO_PKG_VERSION"))
    }

    /// Permissionless keeper entrypoint that extends storage TTL so dormant
    /// share balances are never archived (#1485).
    pub fn extend_ttl(env: Env, user: Option<Address>) {
        use nester_common::{
            INSTANCE_TTL_EXTEND_TO_LEDGERS, INSTANCE_TTL_THRESHOLD_LEDGERS,
            POSITION_TTL_EXTEND_TO_LEDGERS, POSITION_TTL_THRESHOLD_LEDGERS,
        };
        match user {
            None => {
                env.storage().instance().extend_ttl(
                    INSTANCE_TTL_THRESHOLD_LEDGERS,
                    INSTANCE_TTL_EXTEND_TO_LEDGERS,
                );
            }
            Some(u) => {
                for key in [
                    DataKey::Balance(u.clone()),
                    DataKey::DepositTimestamp(u.clone()),
                ] {
                    if env.storage().persistent().has(&key) {
                        env.storage().persistent().extend_ttl(
                            &key,
                            POSITION_TTL_THRESHOLD_LEDGERS,
                            POSITION_TTL_EXTEND_TO_LEDGERS,
                        );
                    }
                }
            }
        }
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
