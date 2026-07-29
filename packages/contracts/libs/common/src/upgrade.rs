//! Reusable Upgrade and Storage Versioning Module for Soroban Contracts
//!
//! Provides standardized functions to manage timelock-governed in-place WASM upgrades
//! and contract schema migrations.

use soroban_sdk::{
    contracttype, panic_with_error, symbol_short, Address, BytesN, Env, Symbol,
};

use crate::{
    emit_event, ContractError, UpgradeCancelledEventData, UpgradeExecutedEventData,
    UpgradeProposedEventData,
};

pub const UPGRADE_SYMBOL: Symbol = symbol_short!("UPGRADE");
pub const PROP_UPG_SYMBOL: Symbol = symbol_short!("PROP_UPG");
pub const CAN_UPG_SYMBOL: Symbol = symbol_short!("CAN_UPG");
pub const EXEC_UPG_SYMBOL: Symbol = symbol_short!("EXEC_UPG");

#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PendingUpgrade {
    pub wasm_hash: BytesN<32>,
    pub eta: u64,
    pub proposer: Address,
}

#[contracttype]
#[derive(Clone)]
pub enum UpgradeDataKey {
    PendingUpgrade,
    SchemaVersion,
}

pub struct Upgrade;

impl Upgrade {
    /// Proposes a new WASM upgrade with a specified ETA and minimum delay.
    ///
    /// # Authorization
    /// `proposer` must authorize the invocation. The calling contract entry point
    /// is responsible for verifying that `proposer` holds `Role::Upgrader`.
    pub fn propose_upgrade(
        env: &Env,
        proposer: &Address,
        wasm_hash: BytesN<32>,
        min_delay: u64,
        eta: u64,
    ) {
        proposer.require_auth();

        let now = env.ledger().timestamp();
        if eta < now.saturating_add(min_delay) {
            panic_with_error!(env, ContractError::TimelockInvalidDelay);
        }

        let pending = PendingUpgrade {
            wasm_hash: wasm_hash.clone(),
            eta,
            proposer: proposer.clone(),
        };

        env.storage()
            .instance()
            .set(&UpgradeDataKey::PendingUpgrade, &pending);

        emit_event(
            env,
            UPGRADE_SYMBOL,
            PROP_UPG_SYMBOL,
            proposer.clone(),
            UpgradeProposedEventData {
                wasm_hash,
                eta,
                proposer: proposer.clone(),
            },
        );
    }

    /// Cancels a pending WASM upgrade.
    ///
    /// # Authorization
    /// `canceller` must authorize the invocation. The calling contract entry point
    /// is responsible for verifying that `canceller` holds `Role::Upgrader`.
    pub fn cancel_upgrade(env: &Env, canceller: &Address) {
        canceller.require_auth();

        let pending: PendingUpgrade = env
            .storage()
            .instance()
            .get(&UpgradeDataKey::PendingUpgrade)
            .unwrap_or_else(|| panic_with_error!(env, ContractError::NoPendingUpgrade));

        env.storage()
            .instance()
            .remove(&UpgradeDataKey::PendingUpgrade);

        emit_event(
            env,
            UPGRADE_SYMBOL,
            CAN_UPG_SYMBOL,
            canceller.clone(),
            UpgradeCancelledEventData {
                wasm_hash: pending.wasm_hash,
                cancelled_by: canceller.clone(),
            },
        );
    }

    /// Executes a matured WASM upgrade.
    ///
    /// Execution is permissionless after maturity. The execution `wasm_hash` must
    /// match the stored proposed hash.
    pub fn execute_upgrade(env: &Env, caller: &Address, wasm_hash: BytesN<32>) {
        caller.require_auth();

        let pending: PendingUpgrade = env
            .storage()
            .instance()
            .get(&UpgradeDataKey::PendingUpgrade)
            .unwrap_or_else(|| panic_with_error!(env, ContractError::NoPendingUpgrade));

        let now = env.ledger().timestamp();
        if now < pending.eta {
            panic_with_error!(env, ContractError::UpgradeNotMatured);
        }

        if wasm_hash != pending.wasm_hash {
            panic_with_error!(env, ContractError::UpgradeHashMismatch);
        }

        env.deployer().update_current_contract_wasm(pending.wasm_hash.clone());

        env.storage()
            .instance()
            .remove(&UpgradeDataKey::PendingUpgrade);

        emit_event(
            env,
            UPGRADE_SYMBOL,
            EXEC_UPG_SYMBOL,
            caller.clone(),
            UpgradeExecutedEventData {
                wasm_hash: pending.wasm_hash,
                executed_by: caller.clone(),
                execution_timestamp: now,
            },
        );
    }

    /// Retrieves the current pending upgrade, if any.
    pub fn get_pending_upgrade(env: &Env) -> Option<PendingUpgrade> {
        env.storage()
            .instance()
            .get(&UpgradeDataKey::PendingUpgrade)
    }

    /// Initializes contract schema version if not already set.
    pub fn init_schema_version(env: &Env, initial_version: u32) {
        if !env.storage().instance().has(&UpgradeDataKey::SchemaVersion) {
            env.storage()
                .instance()
                .set(&UpgradeDataKey::SchemaVersion, &initial_version);
        }
    }

    /// Returns current contract schema version.
    pub fn get_schema_version(env: &Env) -> u32 {
        env.storage()
            .instance()
            .get(&UpgradeDataKey::SchemaVersion)
            .unwrap_or(0)
    }

    /// Sets contract schema version.
    pub fn set_schema_version(env: &Env, version: u32) {
        env.storage()
            .instance()
            .set(&UpgradeDataKey::SchemaVersion, &version);
    }
}
