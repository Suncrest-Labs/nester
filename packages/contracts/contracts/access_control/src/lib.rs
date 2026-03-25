//! Nester shared access-control module.
//!
//! Implements a role-based access control (RBAC) system used by all Nester
//! smart contracts.  This is a plain Rust library (`rlib`): it holds no
//! on-chain state of its own; it reads and writes into the *calling*
//! contract's instance storage.
//!
//! # Roles
//! * [`Role::Admin`]    – full control; can grant/revoke any role and initiate
//!   admin transfers.
//! * [`Role::Operator`] – day-to-day operations (e.g. updating weights); cannot
//!   change role assignments.
//!
//! # Admin transfer (two-step)
//! 1. Current admin calls [`AccessControl::transfer_admin`] — stores a pending proposal.
//! 2. Proposed new admin calls [`AccessControl::accept_admin`] — atomically grants them
//!    Admin and revokes the previous admin, then clears the proposal.
//!
//! This prevents accidental admin loss from mis-typed addresses.
//!
//! # Last-admin protection
//! [`AccessControl::revoke_role`] will panic with [`ContractError::InvalidOperation`] if
//! the caller attempts to remove the last remaining Admin, preventing orphaned contracts.
//!
//! # Events
//! Every role change emits an event so off-chain indexers can reconstruct the
//! full authorization history.

#![no_std]

use soroban_sdk::{contracttype, panic_with_error, symbol_short, Address, Env};

use nester_common::ContractError;

// ---------------------------------------------------------------------------
// Public types
// ---------------------------------------------------------------------------

/// The set of roles recognised by Nester contracts.
///
/// Stored as part of [`DataKey::HasRole`], so `#[contracttype]` is required
/// for XDR serialisation when used as a storage-key component.
#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum Role {
    /// Full control: can grant/revoke roles and transfer admin.
    Admin,
    /// Operational role: can perform day-to-day tasks (e.g. weight updates).
    Operator,
}

// ---------------------------------------------------------------------------
// Internal storage keys  (not exported — callers use the public API only)
// ---------------------------------------------------------------------------

/// Payload stored while a two-step admin transfer is pending.
#[contracttype]
#[derive(Clone)]
pub struct AdminTransfer {
    /// The current admin who proposed the transfer.
    pub from: Address,
    /// The address that must call [`AccessControl::accept_admin`] to complete the transfer.
    pub to: Address,
}

#[contracttype]
#[derive(Clone)]
enum DataKey {
    /// `true` if `(address, role)` is currently active for that contract.
    HasRole(Address, Role),
    /// Pending two-step admin transfer, if any.
    PendingTransfer,
    /// How many addresses currently hold the Admin role.
    /// Tracked to prevent revoking the last admin.
    AdminCount,
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

pub struct AccessControl;

impl AccessControl {
    /// Initialise access control for the calling contract.
    ///
    /// Grants [`Role::Admin`] to `admin` and stores the initial admin count.
    /// Must be called exactly once; subsequent calls panic with
    /// [`ContractError::AlreadyInitialized`].
    ///
    /// # Authorization
    /// `admin` must have authorised this invocation.
    pub fn initialize(env: &Env, admin: &Address) {
        if env.storage().instance().has(&DataKey::AdminCount) {
            panic_with_error!(env, ContractError::AlreadyInitialized);
        }

        admin.require_auth();

        internal_set_role(env, admin, Role::Admin, true);
        env.storage().instance().set(&DataKey::AdminCount, &1u32);

        env.events()
            .publish((symbol_short!("ac_init"), admin.clone()), Role::Admin);
    }

    /// Returns `true` if `account` currently holds `role`, `false` otherwise.
    pub fn has_role(env: &Env, account: &Address, role: Role) -> bool {
        env.storage()
            .instance()
            .get::<DataKey, bool>(&DataKey::HasRole(account.clone(), role))
            .unwrap_or(false)
    }

    /// Grant `role` to `grantee`.
    ///
    /// # Authorization
    /// `grantor` must hold [`Role::Admin`] and must have authorised this call.
    ///
    /// # Panics
    /// * [`ContractError::Unauthorized`] if `grantor` is not an Admin.
    pub fn grant_role(env: &Env, grantor: &Address, grantee: &Address, role: Role) {
        grantor.require_auth();
        Self::require_role(env, grantor, Role::Admin);

        let already_has = Self::has_role(env, grantee, role.clone());
        internal_set_role(env, grantee, role.clone(), true);

        if matches!(role, Role::Admin) && !already_has {
            internal_inc_admin_count(env);
        }

        env.events().publish(
            (symbol_short!("grant"), grantor.clone(), grantee.clone()),
            role,
        );
    }

    /// Revoke `role` from `target`.
    ///
    /// # Authorization
    /// `revoker` must hold [`Role::Admin`] and must have authorised this call.
    ///
    /// # Panics
    /// * [`ContractError::InvalidOperation`] when revoking Admin would leave zero
    ///   admins (last-admin protection).
    /// * [`ContractError::Unauthorized`] if `revoker` is not an Admin.
    pub fn revoke_role(env: &Env, revoker: &Address, target: &Address, role: Role) {
        revoker.require_auth();
        Self::require_role(env, revoker, Role::Admin);

        if matches!(role, Role::Admin) {
            let count = internal_admin_count(env);
            if count <= 1 {
                panic_with_error!(env, ContractError::InvalidOperation);
            }
            internal_dec_admin_count(env);
        }

        internal_set_role(env, target, role.clone(), false);

        env.events().publish(
            (symbol_short!("revoke"), revoker.clone(), target.clone()),
            role,
        );
    }

    /// Assert that `account` holds `role`.
    ///
    /// Panics with [`ContractError::Unauthorized`] when the check fails.
    /// This is the primary guard used inside contract entrypoints.
    pub fn require_role(env: &Env, account: &Address, role: Role) {
        if !Self::has_role(env, account, role) {
            panic_with_error!(env, ContractError::Unauthorized);
        }
    }

    /// **Step 1** of a two-step admin transfer.
    ///
    /// Records `new_admin` as the pending successor.  The current admin retains
    /// their role until `new_admin` calls [`Self::accept_admin`].
    ///
    /// # Authorization
    /// `current_admin` must hold [`Role::Admin`] and must have authorised this call.
    pub fn transfer_admin(env: &Env, current_admin: &Address, new_admin: &Address) {
        current_admin.require_auth();
        Self::require_role(env, current_admin, Role::Admin);

        let proposal = AdminTransfer {
            from: current_admin.clone(),
            to: new_admin.clone(),
        };
        env.storage()
            .instance()
            .set(&DataKey::PendingTransfer, &proposal);

        env.events().publish(
            (
                symbol_short!("xfr_prop"),
                current_admin.clone(),
                new_admin.clone(),
            ),
            (),
        );
    }

    /// **Step 2** of a two-step admin transfer.
    ///
    /// `new_admin` accepts the pending proposal: they are granted [`Role::Admin`]
    /// and the proposing admin is atomically revoked.  The pending proposal is then
    /// cleared.
    ///
    /// # Authorization
    /// `new_admin` must have authorised this call and must match the address stored
    /// by the preceding [`Self::transfer_admin`] call.
    ///
    /// # Panics
    /// * [`ContractError::InvalidOperation`] if no transfer has been proposed.
    /// * [`ContractError::Unauthorized`] if `new_admin` does not match the pending proposal.
    pub fn accept_admin(env: &Env, new_admin: &Address) {
        new_admin.require_auth();

        let proposal: AdminTransfer = env
            .storage()
            .instance()
            .get(&DataKey::PendingTransfer)
            .unwrap_or_else(|| panic_with_error!(env, ContractError::InvalidOperation));

        if proposal.to != *new_admin {
            panic_with_error!(env, ContractError::Unauthorized);
        }

        let already_admin = Self::has_role(env, new_admin, Role::Admin);
        internal_set_role(env, new_admin, Role::Admin, true);
        if !already_admin {
            internal_inc_admin_count(env);
        }

        // Revoke Admin from the proposer. Safe because count >= 2 at this point.
        internal_dec_admin_count(env);
        internal_set_role(env, &proposal.from, Role::Admin, false);

        env.storage().instance().remove(&DataKey::PendingTransfer);

        env.events().publish(
            (symbol_short!("xfr_acc"), new_admin.clone()),
            proposal.from.clone(),
        );
    }

    /// Step 1 of two-step admin transfer.
    pub fn transfer_admin(env: &Env, current_admin: &Address, new_admin: &Address) {
        current_admin.require_auth();
        Self::require_role(env, current_admin, Role::Admin);
        env.storage().instance().set(
            &DataKey::PendingTransfer,
            &AdminTransfer {
                from: current_admin.clone(),
                to: new_admin.clone(),
            },
        );
        env.events().publish(
            (
                symbol_short!("xfr_prop"),
                current_admin.clone(),
                new_admin.clone(),
            ),
            (),
        );
    }

    /// Step 2 of two-step admin transfer. Grants new Admin, revokes old.
    pub fn accept_admin(env: &Env, new_admin: &Address) {
        new_admin.require_auth();
        let proposal: AdminTransfer = env
            .storage()
            .instance()
            .get(&DataKey::PendingTransfer)
            .unwrap_or_else(|| panic_with_error!(env, ContractError::InvalidOperation));
        if proposal.to != *new_admin {
            panic_with_error!(env, ContractError::Unauthorized);
        }
        let already_admin = Self::has_role(env, new_admin, Role::Admin);
        internal_set_role(env, new_admin, Role::Admin, true);
        if !already_admin {
            internal_inc_admin_count(env);
        }
        internal_dec_admin_count(env);
        internal_set_role(env, &proposal.from, Role::Admin, false);
        env.storage().instance().remove(&DataKey::PendingTransfer);
        env.events().publish(
            (symbol_short!("xfr_acc"), new_admin.clone()),
            proposal.from.clone(),
        );
    }
}

// ---------------------------------------------------------------------------
// Private helpers
// ---------------------------------------------------------------------------

fn internal_set_role(env: &Env, account: &Address, role: Role, active: bool) {
    env.storage()
        .instance()
        .set(&DataKey::HasRole(account.clone(), role), &active);
}

fn internal_admin_count(env: &Env) -> u32 {
    env.storage()
        .instance()
        .get(&DataKey::AdminCount)
        .unwrap_or(0u32)
}

// ---------------------------------------------------------------------------
// Private helpers
// ---------------------------------------------------------------------------

fn internal_set_role(env: &Env, account: &Address, role: Role, active: bool) {
    env.storage()
        .instance()
        .set(&DataKey::HasRole(account.clone(), role), &active);
}

fn internal_admin_count(env: &Env) -> u32 {
    env.storage()
        .instance()
        .get(&DataKey::AdminCount)
        .unwrap_or(0u32)
}

fn internal_inc_admin_count(env: &Env) {
    let count = internal_admin_count(env);
    env.storage()
        .instance()
        .set(&DataKey::AdminCount, &(count + 1));
}

fn internal_dec_admin_count(env: &Env) {
    let count = internal_admin_count(env);
    env.storage()
        .instance()
        .set(&DataKey::AdminCount, &(count - 1));
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod test;
