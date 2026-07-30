//! Nester shared access-control module.
//!
//! Implements a role-based access control (RBAC) system used by all Nester
//! smart contracts.  This is a plain Rust library (`rlib`): it holds no
//! on-chain state of its own; it reads and writes into the *calling*
//! contract's instance storage.
//!
//! # Roles
//! * [`Role::Admin`]           – grant/revoke roles, configure non-critical parameters.
//! * [`Role::Operator`]        – day-to-day operations (e.g. updating weights).
//! * [`Role::Manager`]         – fee collection / specific vault operations.
//! * [`Role::Guardian`]        – safety-only: pause, halt deposits, trip the breaker.
//!   Deliberately asymmetric — a Guardian can always make the protocol *safer*
//!   (pause, halt, trip) and can **never** make it riskier (unpause, raise
//!   limits, upgrade, withdraw). Reversing a Guardian action always requires a
//!   higher role. A compromised Guardian key can at worst halt the protocol —
//!   annoying and recoverable, never a fund-drain.
//! * [`Role::Upgrader`]        – propose/execute upgrades through the timelock.
//! * [`Role::Attester`]        – signs APY/TVL/yield reports — a distinct key
//!   from fund-moving roles.
//! * [`Role::FeeManager`]      – adjusts fees within bounds.
//! * [`Role::RebalanceKeeper`] – triggers rebalances.
//! * [`Role::Treasurer`]       – authorises treasury outflows.
//!
//! # Admin transfer (two-step, legacy)
//! 1. Current admin calls [`AccessControl::transfer_admin`] — stores a pending proposal.
//! 2. Proposed new admin calls [`AccessControl::accept_admin`] — atomically grants them
//!    Admin and revokes the previous admin, then clears the proposal.
//!
//! # Generalised role transfer (two-step, any role)
//! [`AccessControl::transfer_role`] / [`AccessControl::accept_role`] /
//! [`AccessControl::cancel_role_transfer`] provide the same one-move-mistake
//! protection for every role, not just Admin.
//!
//! # Role expiry
//! [`AccessControl::grant_role_until`] grants a role that stops authorising
//! after `expires_at` without needing an explicit revocation. Expiry is
//! enforced lazily inside [`AccessControl::has_role`]: the first check after
//! expiry clears the flag, removes the address from the enumeration index,
//! and emits `role_expired`.
//!
//! # Last-admin protection
//! [`AccessControl::revoke_role`] will panic with [`ContractError::InvalidOperation`] if
//! the caller attempts to remove the last remaining Admin, preventing orphaned contracts.
//!
//! # Enumeration
//! [`AccessControl::get_role_members`] (bounded pagination) and
//! [`AccessControl::role_expires_at`] make the full access-control state
//! inspectable on-chain.
//!
//! # Events
//! Every role change emits an event so off-chain indexers can reconstruct the
//! full authorization history.

#![no_std]

use soroban_sdk::{contracttype, panic_with_error, symbol_short, Address, Env, Symbol, Vec};

use nester_common::{emit_event, ContractError};

const ACCESS: Symbol = symbol_short!("ACCESS");
const ROLE_GRANTED: Symbol = symbol_short!("GRANT");
const ROLE_REVOKED: Symbol = symbol_short!("REVOKE");
const ADMIN_TRANSFER: Symbol = symbol_short!("XFR_ACC");
const ROLE_XFR_PROP: Symbol = symbol_short!("RL_XFR_P");
const ROLE_XFR_ACC: Symbol = symbol_short!("RL_XFR_A");
const ROLE_XFR_CANCEL: Symbol = symbol_short!("RL_XFR_C");
const ROLE_EXPIRED: Symbol = symbol_short!("RL_EXP");

/// Upper bound on a single `get_role_members` page.
pub const MAX_MEMBERS_PAGE: u32 = 100;

#[contracttype]
#[derive(Clone, Debug)]
pub struct RoleEventData {
    pub role: Role,
    pub actor: Address,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct AdminTransferEventData {
    pub old_admin: Address,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct RoleTransferEventData {
    pub role: Role,
    pub from: Address,
    pub to: Address,
}

// ---------------------------------------------------------------------------
// Public types
// ---------------------------------------------------------------------------

/// The set of roles recognised by Nester contracts.
///
/// Stored as part of [`DataKey::HasRole`], so `#[contracttype]` is required
/// for XDR serialisation when used as a storage-key component.
///
/// New variants are appended at the end so existing on-chain role
/// assignments keep their XDR discriminant.
#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum Role {
    /// Full control: can grant/revoke roles and transfer admin.
    Admin,
    /// Operational role: can perform day-to-day tasks (e.g. weight updates).
    Operator,
    /// Manager role: can collect fees and manage specific vault operations.
    Manager,
    /// Safety-only role: pause, halt deposits, trip the breaker. Can never
    /// unpause, upgrade, or move funds.
    Guardian,
    /// Propose/execute upgrades through the timelock.
    Upgrader,
    /// Signs APY/TVL/yield reports.
    Attester,
    /// Adjusts fees within governed bounds.
    FeeManager,
    /// Triggers rebalances.
    RebalanceKeeper,
    /// Authorises treasury outflows.
    Treasurer,
    /// Permitted to create new vaults through the vault factory (issue #816).
    VaultCreator,
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

/// Payload stored while a generalised two-step role transfer is pending.
#[contracttype]
#[derive(Clone)]
pub struct RoleTransfer {
    pub from: Address,
    pub to: Address,
    pub role: Role,
}

#[contracttype]
#[derive(Clone)]
enum DataKey {
    /// `true` if `(address, role)` is currently active for that contract.
    HasRole(Address, Role),
    /// Pending two-step admin transfer, if any (legacy, Admin-only path).
    PendingTransfer,
    /// How many addresses currently hold the Admin role.
    /// Tracked to prevent revoking the last admin.
    AdminCount,
    /// Optional expiry timestamp for a `(address, role)` grant.
    RoleExpiry(Address, Role),
    /// Pending generalised two-step transfer for a given role.
    PendingRoleTransfer(Role),
    /// Bounded enumeration index: every address currently holding `role`.
    RoleMembers(Role),
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
        add_member(env, &Role::Admin, admin);
        env.storage().instance().set(&DataKey::AdminCount, &1u32);

        emit_event(
            env,
            ACCESS,
            ROLE_GRANTED,
            admin.clone(),
            RoleEventData {
                role: Role::Admin,
                actor: admin.clone(), // self-grant during init
            },
        );
    }

    /// Returns `true` if `account` currently holds `role`, `false` otherwise.
    ///
    /// Time-bounded grants are checked lazily: if the grant has an expiry and
    /// the ledger timestamp has passed it, the flag is cleared, the address
    /// is removed from the enumeration index, a `role_expired` event is
    /// emitted, and `false` is returned.
    pub fn has_role(env: &Env, account: &Address, role: Role) -> bool {
        let active = env
            .storage()
            .instance()
            .get::<DataKey, bool>(&DataKey::HasRole(account.clone(), role.clone()))
            .unwrap_or(false);

        if !active {
            return false;
        }

        if let Some(expires_at) = get_role_expiry(env, account, &role) {
            if env.ledger().timestamp() >= expires_at {
                internal_set_role(env, account, role.clone(), false);
                remove_role_expiry(env, account, &role);
                remove_member(env, &role, account);
                emit_event(
                    env,
                    ACCESS,
                    ROLE_EXPIRED,
                    account.clone(),
                    RoleEventData {
                        role,
                        actor: account.clone(),
                    },
                );
                return false;
            }
        }

        true
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
        internal_grant(env, grantee, role.clone());

        emit_event(
            env,
            ACCESS,
            ROLE_GRANTED,
            grantee.clone(),
            RoleEventData {
                role,
                actor: grantor.clone(),
            },
        );
    }

    /// Grant `role` to `grantee` with an expiry: the grant stops authorising
    /// once `env.ledger().timestamp() >= expires_at`, with no explicit
    /// revocation required (issue #820). Operational roles — keepers,
    /// temporary operators — should almost always be time-bounded.
    ///
    /// # Authorization
    /// `grantor` must hold [`Role::Admin`] and must have authorised this call.
    pub fn grant_role_until(
        env: &Env,
        grantor: &Address,
        grantee: &Address,
        role: Role,
        expires_at: u64,
    ) {
        grantor.require_auth();
        Self::require_role(env, grantor, Role::Admin);
        internal_grant(env, grantee, role.clone());
        env.storage().instance().set(
            &DataKey::RoleExpiry(grantee.clone(), role.clone()),
            &expires_at,
        );

        emit_event(
            env,
            ACCESS,
            ROLE_GRANTED,
            grantee.clone(),
            RoleEventData {
                role,
                actor: grantor.clone(),
            },
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
        remove_role_expiry(env, target, &role);
        remove_member(env, &role, target);

        emit_event(
            env,
            ACCESS,
            ROLE_REVOKED,
            target.clone(),
            RoleEventData {
                role,
                actor: revoker.clone(),
            },
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

    /// Same as [`Self::require_role`] but panics with the more specific
    /// [`ContractError::RoleRequired`]. Used by the newer granular-role
    /// entrypoints (two-step role transfer, expiry) added in issue #820.
    pub fn require_role_typed(env: &Env, account: &Address, role: Role) {
        if !Self::has_role(env, account, role) {
            panic_with_error!(env, ContractError::RoleRequired);
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

        internal_grant(env, new_admin, Role::Admin);

        // Revoke Admin from the proposer. Safe because count >= 2 at this point.
        internal_dec_admin_count(env);
        internal_set_role(env, &proposal.from, Role::Admin, false);
        remove_member(env, &Role::Admin, &proposal.from);

        env.storage().instance().remove(&DataKey::PendingTransfer);

        emit_event(
            env,
            ACCESS,
            ADMIN_TRANSFER,
            new_admin.clone(),
            AdminTransferEventData {
                old_admin: proposal.from,
            },
        );
    }

    /// **Step 1** of a generalised two-step transfer for any role (issue #820).
    ///
    /// Unlike [`Self::transfer_admin`], this works for every [`Role`], not
    /// just Admin. `current_holder` keeps `role` until `new_holder` accepts.
    ///
    /// # Authorization
    /// `current_holder` must hold `role` and must have authorised this call.
    pub fn transfer_role(env: &Env, current_holder: &Address, role: Role, new_holder: &Address) {
        current_holder.require_auth();
        Self::require_role_typed(env, current_holder, role.clone());

        let proposal = RoleTransfer {
            from: current_holder.clone(),
            to: new_holder.clone(),
            role: role.clone(),
        };
        env.storage()
            .instance()
            .set(&DataKey::PendingRoleTransfer(role.clone()), &proposal);

        emit_event(
            env,
            ACCESS,
            ROLE_XFR_PROP,
            new_holder.clone(),
            RoleTransferEventData {
                role,
                from: current_holder.clone(),
                to: new_holder.clone(),
            },
        );
    }

    /// **Step 2** of a generalised two-step role transfer.
    ///
    /// # Panics
    /// * [`ContractError::RoleTransferNotPending`] if no transfer is pending for `role`.
    /// * [`ContractError::Unauthorized`] if `new_holder` does not match the pending proposal.
    pub fn accept_role(env: &Env, new_holder: &Address, role: Role) {
        new_holder.require_auth();

        let proposal: RoleTransfer = env
            .storage()
            .instance()
            .get(&DataKey::PendingRoleTransfer(role.clone()))
            .unwrap_or_else(|| panic_with_error!(env, ContractError::RoleTransferNotPending));

        if proposal.to != *new_holder {
            panic_with_error!(env, ContractError::Unauthorized);
        }

        internal_grant(env, new_holder, role.clone());
        internal_set_role(env, &proposal.from, role.clone(), false);
        remove_role_expiry(env, &proposal.from, &role);
        remove_member(env, &role, &proposal.from);

        env.storage()
            .instance()
            .remove(&DataKey::PendingRoleTransfer(role.clone()));

        emit_event(
            env,
            ACCESS,
            ROLE_XFR_ACC,
            new_holder.clone(),
            RoleTransferEventData {
                role,
                from: proposal.from,
                to: new_holder.clone(),
            },
        );
    }

    /// Cancel a pending generalised role transfer before it is accepted.
    ///
    /// # Authorization
    /// Only the address that proposed the transfer may cancel it.
    ///
    /// # Panics
    /// * [`ContractError::RoleTransferNotPending`] if no transfer is pending for `role`.
    pub fn cancel_role_transfer(env: &Env, current_holder: &Address, role: Role) {
        current_holder.require_auth();

        let proposal: RoleTransfer = env
            .storage()
            .instance()
            .get(&DataKey::PendingRoleTransfer(role.clone()))
            .unwrap_or_else(|| panic_with_error!(env, ContractError::RoleTransferNotPending));

        if proposal.from != *current_holder {
            panic_with_error!(env, ContractError::Unauthorized);
        }

        env.storage()
            .instance()
            .remove(&DataKey::PendingRoleTransfer(role.clone()));

        emit_event(
            env,
            ACCESS,
            ROLE_XFR_CANCEL,
            current_holder.clone(),
            RoleTransferEventData {
                role,
                from: proposal.from,
                to: proposal.to,
            },
        );
    }

    /// Ledger timestamp at which `account`'s `role` grant expires, if any.
    /// Returns `None` for permanent grants (including all grants made before
    /// role expiry existed).
    pub fn role_expires_at(env: &Env, account: &Address, role: Role) -> Option<u64> {
        get_role_expiry(env, account, &role)
    }

    /// Bounded, paginated list of every address currently holding `role`.
    /// `limit` is capped at [`MAX_MEMBERS_PAGE`] regardless of the value passed.
    ///
    /// Note: this index is not expiry-aware between calls to [`Self::has_role`] —
    /// an expired-but-not-yet-lazily-swept grant may still appear until the
    /// next `has_role` check for that address clears it.
    pub fn get_role_members(env: &Env, role: Role, start: u32, limit: u32) -> Vec<Address> {
        let members = get_members(env, &role);
        let capped_limit = limit.min(MAX_MEMBERS_PAGE);
        let mut out = Vec::new(env);
        let mut i = start;
        let end = start.saturating_add(capped_limit).min(members.len());
        while i < end {
            out.push_back(members.get(i).unwrap());
            i += 1;
        }
        out
    }
}

// ---------------------------------------------------------------------------
// Private helpers
// ---------------------------------------------------------------------------

fn internal_grant(env: &Env, grantee: &Address, role: Role) {
    let already_has = AccessControl::has_role(env, grantee, role.clone());
    internal_set_role(env, grantee, role.clone(), true);
    add_member(env, &role, grantee);

    if matches!(role, Role::Admin) && !already_has {
        internal_inc_admin_count(env);
    }
}

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

fn get_role_expiry(env: &Env, account: &Address, role: &Role) -> Option<u64> {
    env.storage()
        .instance()
        .get(&DataKey::RoleExpiry(account.clone(), role.clone()))
}

fn remove_role_expiry(env: &Env, account: &Address, role: &Role) {
    env.storage()
        .instance()
        .remove(&DataKey::RoleExpiry(account.clone(), role.clone()));
}

fn get_members(env: &Env, role: &Role) -> Vec<Address> {
    env.storage()
        .instance()
        .get(&DataKey::RoleMembers(role.clone()))
        .unwrap_or(Vec::new(env))
}

fn add_member(env: &Env, role: &Role, account: &Address) {
    let mut members = get_members(env, role);
    for existing in members.iter() {
        if existing == *account {
            return;
        }
    }
    members.push_back(account.clone());
    env.storage()
        .instance()
        .set(&DataKey::RoleMembers(role.clone()), &members);
}

fn remove_member(env: &Env, role: &Role, account: &Address) {
    let members = get_members(env, role);
    let mut out = Vec::new(env);
    for existing in members.iter() {
        if existing != *account {
            out.push_back(existing);
        }
    }
    env.storage()
        .instance()
        .set(&DataKey::RoleMembers(role.clone()), &out);
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod test;
