#![no_std]
use soroban_sdk::{
    contract, contractimpl, contracttype, panic_with_error, symbol_short, token, Address, Env,
    Symbol, Vec,
};
use nester_access_control::{AccessControl, Role};
use nester_common::ContractError;

// ---------------------------------------------------------------------------
// Event topic constants
// ---------------------------------------------------------------------------
const TREASURY: Symbol = symbol_short!("TREASURY");
const RECEIVE: Symbol = symbol_short!("RECEIVE");
const WITHDRAW: Symbol = symbol_short!("WITHDRAW");
const DISTRIB: Symbol = symbol_short!("DISTRIB");  // one recipient paid
const RCPUPD: Symbol = symbol_short!("RCPUPD");    // recipients list replaced

// ---------------------------------------------------------------------------
// Basis-point denominator — shares must sum to exactly this
// ---------------------------------------------------------------------------
const BPS_TOTAL: u32 = 10_000;

// ---------------------------------------------------------------------------
// Storage keys
// ---------------------------------------------------------------------------
#[contracttype]
#[derive(Clone)]
enum DataKey {
    Vault,
    TotalReceived,
    TotalDistributed,
    Recipients,
    DistHistory,
}

// ---------------------------------------------------------------------------
// Data structures
// ---------------------------------------------------------------------------

/// A single fee recipient and its configured share.
#[contracttype]
#[derive(Clone)]
pub struct FeeRecipient {
    /// Wallet or contract that receives the share.
    pub address: Address,
    /// Share in basis points (e.g. 7000 = 70 %).
    pub share_bps: u32,
    /// Short human-readable label, e.g. symbol_short!("protocol").
    pub label: Symbol,
    /// Lifetime cumulative amount this recipient has received.
    pub total_received: i128,
}

/// Written to history after every successful `distribute()` call.
#[contracttype]
#[derive(Clone)]
pub struct DistributionRecord {
    /// Ledger timestamp at the time of distribution.
    pub timestamp: u64,
    /// Total amount split in this round.
    pub total_amount: i128,
    /// How many recipients were paid.
    pub recipient_count: u32,
}

// ---------------------------------------------------------------------------
// Contract
// ---------------------------------------------------------------------------
#[contract]
pub struct TreasuryContract;

#[contractimpl]
impl TreasuryContract {
    // -----------------------------------------------------------------------
    // Initialisation
    // -----------------------------------------------------------------------

    /// One-time setup.  Stores the vault address, seeds all counters, and
    /// configures AccessControl with `admin` as the initial admin.
    pub fn initialize(env: Env, admin: Address, vault: Address) {
        if env.storage().instance().has(&DataKey::Vault) {
            panic_with_error!(&env, ContractError::AlreadyInitialized);
        }

        AccessControl::initialize(&env, &admin);

        env.storage().instance().set(&DataKey::Vault, &vault);
        env.storage().instance().set(&DataKey::TotalReceived, &0_i128);
        env.storage().instance().set(&DataKey::TotalDistributed, &0_i128);

        let empty_recipients: Vec<FeeRecipient> = Vec::new(&env);
        let empty_history: Vec<DistributionRecord> = Vec::new(&env);
        env.storage().instance().set(&DataKey::Recipients, &empty_recipients);
        env.storage().instance().set(&DataKey::DistHistory, &empty_history);
    }

    // -----------------------------------------------------------------------
    // Fee ingestion  (vault only)
    // -----------------------------------------------------------------------

    /// Record incoming fees from the vault.  Only the registered vault
    /// address may call this.
    pub fn receive_fees(env: Env, amount: i128) {
        let vault: Address = env.storage().instance().get(&DataKey::Vault).unwrap();
        vault.require_auth();

        if amount <= 0 {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }

        let total: i128 = env
            .storage()
            .instance()
            .get(&DataKey::TotalReceived)
            .unwrap_or(0);
        env.storage()
            .instance()
            .set(&DataKey::TotalReceived, &(total + amount));

        env.events().publish((TREASURY, RECEIVE), amount);
    }

    // -----------------------------------------------------------------------
    // Recipient management  (Admin only)
    // -----------------------------------------------------------------------

    /// Atomically replace the recipient list.
    ///
    /// Validation:
    /// - List must not be empty.
    /// - Sum of all `share_bps` must equal exactly 10 000.
    ///
    /// Emits: `(TREASURY, RCPUPD, (count, caller))`
    pub fn set_recipients(env: Env, caller: Address, recipients: Vec<FeeRecipient>) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);

        if recipients.is_empty() {
            panic_with_error!(&env, ContractError::InvalidOperation);
        }

        // Validate shares sum to exactly BPS_TOTAL.
        let mut sum: u32 = 0;
        for i in 0..recipients.len() {
            let r = recipients.get(i).unwrap();
            sum = match sum.checked_add(r.share_bps) {
                Some(v) => v,
                None => panic_with_error!(&env, ContractError::ExceedsLimit),
            };
        }
        if sum != BPS_TOTAL {
            panic_with_error!(&env, ContractError::InvalidOperation);
        }

        env.storage().instance().set(&DataKey::Recipients, &recipients);

        env.events().publish(
            (TREASURY, RCPUPD),
            (recipients.len(), caller),
        );
    }

    // -----------------------------------------------------------------------
    // Distribution  (Admin or Operator)
    // -----------------------------------------------------------------------

    /// Split all undistributed fees among the configured recipients.
    ///
    /// `token` — the SAC token address the contract holds (e.g. USDC).
    ///
    /// The last recipient absorbs any integer-division remainder so that
    /// every unit of `available` is always distributed.
    ///
    /// Errors:
    /// - `Unauthorized`        — caller is neither Admin nor Operator.
    /// - `InvalidOperation`    — no recipients have been configured.
    /// - `InsufficientBalance` — available fees are zero.
    ///
    /// Emits per recipient: `(TREASURY, DISTRIB, (address, amount, share_bps, total_received))`
    pub fn distribute(env: Env, caller: Address, token: Address) {
        caller.require_auth();

        let is_admin = AccessControl::has_role(&env, &caller, Role::Admin);
        let is_operator = AccessControl::has_role(&env, &caller, Role::Operator);
        if !is_admin && !is_operator {
            panic_with_error!(&env, ContractError::Unauthorized);
        }

        // Load recipients — error if none configured.
        let mut recipients: Vec<FeeRecipient> = env
            .storage()
            .instance()
            .get(&DataKey::Recipients)
            .unwrap_or_else(|| Vec::new(&env));

        if recipients.is_empty() {
            panic_with_error!(&env, ContractError::InvalidOperation);
        }

        // Compute undistributed balance.
        let total_received: i128 = env
            .storage()
            .instance()
            .get(&DataKey::TotalReceived)
            .unwrap_or(0);
        let total_distributed: i128 = env
            .storage()
            .instance()
            .get(&DataKey::TotalDistributed)
            .unwrap_or(0);
        let available: i128 = total_received - total_distributed;

        if available <= 0 {
            panic_with_error!(&env, ContractError::InsufficientBalance);
        }

        let token_client = token::Client::new(&env, &token);
        let contract_addr = env.current_contract_address();

        let recipient_count = recipients.len();
        let mut total_sent: i128 = 0;

        for i in 0..recipient_count {
            let mut r = recipients.get(i).unwrap();

            // The last recipient collects the remainder so that available
            // is fully distributed even with integer division rounding.
            let amount = if i == recipient_count - 1 {
                available - total_sent
            } else {
                (available * r.share_bps as i128) / BPS_TOTAL as i128
            };

            token_client.transfer(&contract_addr, &r.address, &amount);

            r.total_received += amount;
            recipients.set(i, r.clone());
            total_sent += amount;

            env.events().publish(
                (TREASURY, DISTRIB),
                (r.address, amount, r.share_bps, r.total_received),
            );
        }

        // Persist updated per-recipient totals.
        env.storage().instance().set(&DataKey::Recipients, &recipients);

        // Advance the distributed counter.
        env.storage()
            .instance()
            .set(&DataKey::TotalDistributed, &(total_distributed + total_sent));

        // Append a history record.
        let mut history: Vec<DistributionRecord> = env
            .storage()
            .instance()
            .get(&DataKey::DistHistory)
            .unwrap_or_else(|| Vec::new(&env));

        history.push_back(DistributionRecord {
            timestamp: env.ledger().timestamp(),
            total_amount: total_sent,
            recipient_count,
        });
        env.storage().instance().set(&DataKey::DistHistory, &history);
    }

    // -----------------------------------------------------------------------
    // Existing withdraw  (Admin only — kept intact)
    // -----------------------------------------------------------------------

    /// Manual/emergency withdrawal — bypasses the distribution mechanism.
    pub fn withdraw(env: Env, caller: Address, to: Address, token: Address, amount: i128) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);

        if amount <= 0 {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }

        token::Client::new(&env, &token).transfer(&env.current_contract_address(), &to, &amount);
        env.events().publish((TREASURY, WITHDRAW), amount);
    }

    // -----------------------------------------------------------------------
    // Read-only views
    // -----------------------------------------------------------------------

    /// Current recipient list including per-recipient cumulative totals.
    pub fn get_recipients(env: Env) -> Vec<FeeRecipient> {
        env.storage()
            .instance()
            .get(&DataKey::Recipients)
            .unwrap_or_else(|| Vec::new(&env))
    }

    /// Full distribution history — one record per `distribute()` call.
    pub fn get_distribution_history(env: Env) -> Vec<DistributionRecord> {
        env.storage()
            .instance()
            .get(&DataKey::DistHistory)
            .unwrap_or_else(|| Vec::new(&env))
    }

    /// Lifetime cumulative fees received by this treasury.
    pub fn get_total_received(env: Env) -> i128 {
        env.storage()
            .instance()
            .get(&DataKey::TotalReceived)
            .unwrap_or(0)
    }

    /// Lifetime cumulative fees already distributed.
    pub fn get_total_distributed(env: Env) -> i128 {
        env.storage()
            .instance()
            .get(&DataKey::TotalDistributed)
            .unwrap_or(0)
    }

    /// Fees currently held by the treasury that have not yet been distributed.
    pub fn get_available_fees(env: Env) -> i128 {
        let received: i128 = env
            .storage()
            .instance()
            .get(&DataKey::TotalReceived)
            .unwrap_or(0);
        let distributed: i128 = env
            .storage()
            .instance()
            .get(&DataKey::TotalDistributed)
            .unwrap_or(0);
        received - distributed
    }

    /// Address of the vault contract authorised to submit fees.
    pub fn get_vault(env: Env) -> Address {
        env.storage().instance().get(&DataKey::Vault).unwrap()
    }
}

#[cfg(test)]
mod test;