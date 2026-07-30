//! Vault factory: deterministic, registry-tracked vault deployment (issue #816).
//!
//! Deploys new vaults from a single governed WASM hash via the Soroban
//! deployer, records every created vault in an on-chain registry keyed by
//! both salt and address (the latter for O(1) `is_nester_vault` lookups),
//! and exposes address prediction so integrators can compute a vault's
//! address before it exists on chain.
//!
//! # Atomicity
//! `create_vault` deploys the contract and then invokes its `initialize`
//! entrypoint in the *same* top-level call. Soroban's host executes a whole
//! top-level invocation atomically: if `initialize` panics, every storage
//! write made during the call — including the deployment itself and this
//! registry's bookkeeping — is rolled back. A half-configured vault can
//! never end up in the registry, and no orphaned deployment is left behind.
//!
//! # Governance
//! `vault_wasm_hash` can only change through the shared [`nester_timelock`]
//! module: [`VaultFactoryContract::propose_wasm_hash`] queues the change,
//! [`VaultFactoryContract::apply_wasm_hash`] applies it after the delay. A
//! compromised factory that could silently start deploying malicious vault
//! code would be equivalent to compromising every future vault, so this
//! path is deliberately slow.
//!
//! # Generic init args
//! The factory forwards `init_args` (a `Vec<Val>`) straight to the deployed
//! contract's `initialize` function without knowing its schema, so the
//! factory does not need recompiling every time the vault's initialize
//! signature gains a parameter.

#![no_std]

use soroban_sdk::{
    contract, contractimpl, contracttype, panic_with_error, symbol_short, Address, Bytes, BytesN,
    Env, Symbol, Val, Vec,
};

use nester_access_control::{AccessControl, Role};
use nester_common::{emit_event, ContractError};
use nester_timelock::Timelock;

const FACTORY: Symbol = symbol_short!("FACTORY");
const VAULT_CREATED: Symbol = symbol_short!("VLT_NEW");
const VAULT_DEPRECATED: Symbol = symbol_short!("VLT_DEP");
const WASM_HASH_SET: Symbol = symbol_short!("WASM_SET");

/// Upper bound on a single `list_vaults` page.
pub const MAX_VAULTS_PAGE: u32 = 100;

#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum VaultRegistryStatus {
    Active,
    Deprecated,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct VaultRecord {
    pub address: Address,
    pub salt: BytesN<32>,
    pub created_at: u64,
    pub underlying_asset: Address,
    pub wasm_hash: BytesN<32>,
    pub status: VaultRegistryStatus,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct VaultCreatedEventData {
    pub salt: BytesN<32>,
    pub address: Address,
    pub wasm_hash: BytesN<32>,
}

#[contracttype]
#[derive(Clone)]
enum DataKey {
    VaultWasmHash,
    /// Canonical record, keyed by address — the O(1) `is_nester_vault` path.
    VaultByAddress(Address),
    /// Pointer from salt to address, so `get_vault(salt)` is also O(1).
    AddressBySalt(BytesN<32>),
    /// Append-only list of every created vault's address, for pagination.
    VaultAddresses,
}

#[contract]
pub struct VaultFactoryContract;

#[contractimpl]
impl VaultFactoryContract {
    /// Initialise the factory. `vault_wasm_hash` must already be uploaded to
    /// the ledger (via `env.deployer().upload_contract_wasm`) before this
    /// call. `admin` can create vaults immediately via the Admin-or-
    /// VaultCreator check in [`Self::create_vault`] without a separate
    /// self-grant (which would require authorising the same address twice
    /// in one invocation frame — not permitted). VaultCreator can still be
    /// granted to other addresses later to delegate creation without
    /// handing out full Admin.
    pub fn initialize(env: Env, admin: Address, vault_wasm_hash: BytesN<32>) {
        AccessControl::initialize(&env, &admin);
        Timelock::initialize(&env);
        env.storage()
            .instance()
            .set(&DataKey::VaultWasmHash, &vault_wasm_hash);
        env.storage()
            .instance()
            .set(&DataKey::VaultAddresses, &Vec::<Address>::new(&env));
    }

    /// The address a vault created with `salt` would be deployed to. Pure
    /// function of the factory's own address and `salt` — callable before
    /// the vault exists.
    pub fn predict_vault_address(env: Env, salt: BytesN<32>) -> Address {
        env.deployer()
            .with_current_contract(salt)
            .deployed_address()
    }

    /// Deploy a new vault from the governed WASM hash and initialise it
    /// atomically. `underlying_asset` is recorded in the registry directly
    /// (rather than parsed out of `init_args`) so the factory stays
    /// decoupled from the vault's exact initialize signature.
    ///
    /// # Panics
    /// * [`ContractError::Unauthorized`] unless `caller` holds Admin or VaultCreator.
    /// * Whatever the deployed contract's `initialize` panics with — and in
    ///   that case nothing is deployed or registered (see module docs).
    pub fn create_vault(
        env: Env,
        caller: Address,
        salt: BytesN<32>,
        underlying_asset: Address,
        init_args: Vec<Val>,
    ) -> Address {
        caller.require_auth();
        if !AccessControl::has_role(&env, &caller, Role::Admin)
            && !AccessControl::has_role(&env, &caller, Role::VaultCreator)
        {
            panic_with_error!(&env, ContractError::Unauthorized);
        }

        if env
            .storage()
            .instance()
            .has(&DataKey::AddressBySalt(salt.clone()))
        {
            panic_with_error!(&env, ContractError::AlreadyInitialized);
        }

        let wasm_hash: BytesN<32> = env
            .storage()
            .instance()
            .get(&DataKey::VaultWasmHash)
            .unwrap_or_else(|| panic_with_error!(&env, ContractError::NotInitialized));

        let deployed = env
            .deployer()
            .with_current_contract(salt.clone())
            .deploy(wasm_hash.clone());

        // Atomic with the deploy above: if this panics, the whole
        // `create_vault` invocation — deploy included — rolls back.
        let _: Val = env.invoke_contract(&deployed, &Symbol::new(&env, "initialize"), init_args);

        let record = VaultRecord {
            address: deployed.clone(),
            salt: salt.clone(),
            created_at: env.ledger().timestamp(),
            underlying_asset,
            wasm_hash: wasm_hash.clone(),
            status: VaultRegistryStatus::Active,
        };
        env.storage()
            .instance()
            .set(&DataKey::VaultByAddress(deployed.clone()), &record);
        env.storage()
            .instance()
            .set(&DataKey::AddressBySalt(salt.clone()), &deployed);

        let mut addresses: Vec<Address> = env
            .storage()
            .instance()
            .get(&DataKey::VaultAddresses)
            .unwrap_or(Vec::new(&env));
        addresses.push_back(deployed.clone());
        env.storage()
            .instance()
            .set(&DataKey::VaultAddresses, &addresses);

        emit_event(
            &env,
            FACTORY,
            VAULT_CREATED,
            deployed.clone(),
            VaultCreatedEventData {
                salt,
                address: deployed.clone(),
                wasm_hash,
            },
        );

        deployed
    }

    /// O(1): is `address` a vault genuinely created by this factory?
    pub fn is_nester_vault(env: Env, address: Address) -> bool {
        env.storage()
            .instance()
            .has(&DataKey::VaultByAddress(address))
    }

    pub fn get_vault(env: Env, salt: BytesN<32>) -> Option<VaultRecord> {
        let address: Address = env
            .storage()
            .instance()
            .get(&DataKey::AddressBySalt(salt))?;
        env.storage()
            .instance()
            .get(&DataKey::VaultByAddress(address))
    }

    pub fn get_vault_by_address(env: Env, address: Address) -> Option<VaultRecord> {
        env.storage()
            .instance()
            .get(&DataKey::VaultByAddress(address))
    }

    /// Bounded, paginated listing of every vault this factory has created,
    /// most-recently-created last.
    pub fn list_vaults(env: Env, start: u32, limit: u32) -> Vec<VaultRecord> {
        let addresses: Vec<Address> = env
            .storage()
            .instance()
            .get(&DataKey::VaultAddresses)
            .unwrap_or(Vec::new(&env));
        let capped_limit = limit.min(MAX_VAULTS_PAGE);
        let mut out = Vec::new(&env);
        let mut i = start;
        let end = start.saturating_add(capped_limit).min(addresses.len());
        while i < end {
            let addr = addresses.get(i).unwrap();
            if let Some(record) = env
                .storage()
                .instance()
                .get::<DataKey, VaultRecord>(&DataKey::VaultByAddress(addr))
            {
                out.push_back(record);
            }
            i += 1;
        }
        out
    }

    pub fn vault_count(env: Env) -> u32 {
        env.storage()
            .instance()
            .get::<DataKey, Vec<Address>>(&DataKey::VaultAddresses)
            .map(|v| v.len())
            .unwrap_or(0)
    }

    /// Mark a vault as no longer recommended without removing it from the
    /// registry — existing positions stay recognised, integrators just stop
    /// routing new deposits to it. Admin only.
    pub fn deprecate_vault(env: Env, caller: Address, address: Address) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);

        let mut record: VaultRecord = env
            .storage()
            .instance()
            .get(&DataKey::VaultByAddress(address.clone()))
            .unwrap_or_else(|| panic_with_error!(&env, ContractError::NotInitialized));

        record.status = VaultRegistryStatus::Deprecated;
        env.storage()
            .instance()
            .set(&DataKey::VaultByAddress(address.clone()), &record);

        emit_event(&env, FACTORY, VAULT_DEPRECATED, address, ());
    }

    // -----------------------------------------------------------------------
    // Governed WASM hash (timelocked)
    // -----------------------------------------------------------------------

    pub fn get_vault_wasm_hash(env: Env) -> BytesN<32> {
        env.storage()
            .instance()
            .get(&DataKey::VaultWasmHash)
            .unwrap_or_else(|| panic_with_error!(&env, ContractError::NotInitialized))
    }

    /// Propose changing the WASM hash the factory deploys from. Must go
    /// through the timelock — see module docs for why.
    pub fn propose_wasm_hash(env: Env, caller: Address, new_hash: BytesN<32>) -> u64 {
        let payload = Bytes::from_slice(&env, &new_hash.to_array());
        Timelock::propose(&env, &caller, symbol_short!("SET_WASM"), payload)
    }

    /// Apply a previously-proposed WASM hash change after its delay elapses.
    pub fn apply_wasm_hash(env: Env, caller: Address, op_id: u64) {
        let payload = Timelock::execute(&env, &caller, op_id);
        let mut buf = [0u8; 32];
        payload.copy_into_slice(&mut buf);
        let new_hash: BytesN<32> = BytesN::from_array(&env, &buf);

        env.storage()
            .instance()
            .set(&DataKey::VaultWasmHash, &new_hash);
        emit_event(&env, FACTORY, WASM_HASH_SET, caller, new_hash);
    }

    // -----------------------------------------------------------------------
    // Role management passthroughs
    // -----------------------------------------------------------------------

    pub fn grant_role(env: Env, grantor: Address, grantee: Address, role: Role) {
        AccessControl::grant_role(&env, &grantor, &grantee, role);
    }

    pub fn revoke_role(env: Env, revoker: Address, target: Address, role: Role) {
        AccessControl::revoke_role(&env, &revoker, &target, role);
    }

    pub fn transfer_admin(env: Env, current_admin: Address, new_admin: Address) {
        AccessControl::transfer_admin(&env, &current_admin, &new_admin);
    }

    pub fn accept_admin(env: Env, new_admin: Address) {
        AccessControl::accept_admin(&env, &new_admin);
    }
}

#[cfg(test)]
mod test;
