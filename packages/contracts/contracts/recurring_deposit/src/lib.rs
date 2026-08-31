#![no_std]

mod test;

use soroban_sdk::{
    contract, contractimpl, contracttype, panic_with_error, symbol_short, Address, BytesN, Env,
    IntoVal, Symbol, Vec,
};

use nester_access_control::{AccessControl, Role};
use nester_common::{
    errors::Error,
    events::emit_event,
    upgrade::{PendingUpgrade, Upgrade},
};

pub const MAX_MANDATES_PER_USER: u32 = 50;
pub const MIN_PERIOD_SECS: u64 = 3600; // 1 hour minimum
pub const MAX_PERIOD_SECS: u64 = 31536000; // 1 year maximum

// Event topic constants
const MANDATE: Symbol = symbol_short!("MANDATE");
const MDT_CRTD: Symbol = symbol_short!("MDT_CRTD");
const MDT_EXEC: Symbol = symbol_short!("MDT_EXEC");
const MDT_CANC: Symbol = symbol_short!("MDT_CANC");
const MDT_PAUSE: Symbol = symbol_short!("MDT_PAUSE");

#[contract]
pub struct RecurringDepositContract;

#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Mandate {
    pub user: Address,
    pub vault: Address,
    pub token: Address,
    pub amount_per_period: i128,
    pub period_secs: u64,
    pub start_at: u64,
    pub expires_at: u64,
    pub max_total: i128,
    pub last_executed_at: u64,
    pub total_drawn: i128,
    pub is_active: bool,
    pub is_paused: bool,
}

#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum DataKey {
    MandateCounter,
    Mandate(u64),
    UserMandates(Address),
}

#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct MandateCreatedEventData {
    pub mandate_id: u64,
    pub user: Address,
    pub vault: Address,
    pub token: Address,
    pub amount_per_period: i128,
    pub period_secs: u64,
    pub expires_at: u64,
    pub max_total: i128,
}

#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct MandateExecutedEventData {
    pub mandate_id: u64,
    pub user: Address,
    pub vault: Address,
    pub amount: i128,
    pub total_drawn: i128,
    pub executor: Address,
}

#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct MandateCancelledEventData {
    pub mandate_id: u64,
    pub user: Address,
}

#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct MandatePausedEventData {
    pub mandate_id: u64,
    pub user: Address,
    pub paused: bool,
}

#[contractimpl]
impl RecurringDepositContract {
    /// Initialize the contract with admin
    pub fn initialize(env: Env, admin: Address) {
        AccessControl::initialize(&env, &admin);
        env.storage()
            .instance()
            .set(&DataKey::MandateCounter, &0u64);
    }

    /// Create a new recurring deposit mandate.
    ///
    /// The user authorises a standing instruction to pull `amount_per_period`
    /// tokens from their wallet into `vault` every `period_secs` seconds,
    /// starting no earlier than `start_at` and never past `expires_at`, up to
    /// a lifetime cap of `max_total`.
    pub fn create_mandate(
        env: Env,
        user: Address,
        vault: Address,
        token: Address,
        amount_per_period: i128,
        period_secs: u64,
        start_at: u64,
        expires_at: u64,
        max_total: i128,
    ) -> u64 {
        user.require_auth();

        // Validate inputs
        if amount_per_period <= 0 {
            panic_with_error!(&env, Error::InvalidAmount);
        }
        if period_secs < MIN_PERIOD_SECS || period_secs > MAX_PERIOD_SECS {
            panic_with_error!(&env, Error::InvalidAmount);
        }
        if expires_at <= start_at {
            panic_with_error!(&env, Error::InvalidAmount);
        }
        if max_total < amount_per_period {
            panic_with_error!(&env, Error::InvalidAmount);
        }

        // Check user doesn't exceed mandate limit
        let user_mandates: Vec<u64> = env
            .storage()
            .persistent()
            .get(&DataKey::UserMandates(user.clone()))
            .unwrap_or(Vec::new(&env));

        if user_mandates.len() >= MAX_MANDATES_PER_USER {
            panic_with_error!(&env, Error::ExceedsLimit);
        }

        // Generate new mandate ID
        let mut mandate_counter: u64 = env
            .storage()
            .instance()
            .get(&DataKey::MandateCounter)
            .unwrap_or(0);
        mandate_counter += 1;
        env.storage()
            .instance()
            .set(&DataKey::MandateCounter, &mandate_counter);

        let mandate = Mandate {
            user: user.clone(),
            vault: vault.clone(),
            token: token.clone(),
            amount_per_period,
            period_secs,
            start_at,
            expires_at,
            max_total,
            last_executed_at: 0,
            total_drawn: 0,
            is_active: true,
            is_paused: false,
        };

        // Store mandate
        env.storage()
            .persistent()
            .set(&DataKey::Mandate(mandate_counter), &mandate);

        // Update user's mandate list
        let mut updated_mandates = user_mandates;
        updated_mandates.push_back(mandate_counter);
        env.storage()
            .persistent()
            .set(&DataKey::UserMandates(user.clone()), &updated_mandates);

        // Emit event — topics: (MANDATE, MDT_CRTD, user)
        emit_event(
            &env,
            MANDATE,
            MDT_CRTD,
            user.clone(),
            MandateCreatedEventData {
                mandate_id: mandate_counter,
                user: user.clone(),
                vault: vault.clone(),
                token: token.clone(),
                amount_per_period,
                period_secs,
                expires_at,
                max_total,
            },
        );

        mandate_counter
    }

    /// Execute a mandate (permissionless).
    ///
    /// Any caller may execute a due mandate.  The function enforces:
    /// - Mandate must be active and not paused (`InvalidOperation`)
    /// - Current time ≥ `start_at` and < `expires_at` (`TimelockNotReady` / `TimelockExpired`)
    /// - At least one full `period_secs` has elapsed since the last execution (`TimelockNotReady`)
    /// - `total_drawn + amount_per_period ≤ max_total` (`BudgetExhausted`)
    ///
    /// Catch-up semantics: `last_executed_at` advances by exactly one period
    /// per call, so if the executor is late, multiple back-to-back calls are
    /// needed to fully catch up — no single call ever skips an entire period.
    pub fn execute_mandate(env: Env, caller: Address, user: Address, mandate_id: u64) {
        caller.require_auth();

        let mut mandate: Mandate = env
            .storage()
            .persistent()
            .get(&DataKey::Mandate(mandate_id))
            .unwrap_or_else(|| panic_with_error!(&env, Error::StrategyNotFound));

        // Check mandate belongs to user
        if mandate.user != user {
            panic_with_error!(&env, Error::Unauthorized);
        }

        // Check mandate is active and not paused — distinct typed errors
        if !mandate.is_active {
            panic_with_error!(&env, Error::InvalidOperation);
        }
        if mandate.is_paused {
            panic_with_error!(&env, Error::InvalidOperation);
        }

        let now = env.ledger().timestamp();

        // Check timing constraints — distinct typed errors
        if now < mandate.start_at {
            panic_with_error!(&env, Error::TimelockNotReady);
        }
        if now >= mandate.expires_at {
            panic_with_error!(&env, Error::TimelockExpired);
        }

        // Check if enough time has passed since last execution
        if mandate.last_executed_at > 0 && now < mandate.last_executed_at + mandate.period_secs {
            panic_with_error!(&env, Error::TimelockNotReady);
        }

        // Check total lifetime cap
        if mandate.total_drawn + mandate.amount_per_period > mandate.max_total {
            panic_with_error!(&env, Error::BudgetExhausted);
        }

        // Transfer tokens from user to vault on mandate execution
        let token_client = soroban_sdk::token::Client::new(&env, &mandate.token);
        token_client.transfer(&mandate.user, &mandate.vault, &mandate.amount_per_period);

        // Update mandate state with catch-up semantics:
        // advance last_executed_at by exactly one period per call.
        if mandate.last_executed_at == 0 {
            mandate.last_executed_at = now;
        } else {
            // Advance by exactly one period (catch-up: caller must call again
            // for each missed period)
            mandate.last_executed_at += mandate.period_secs;
        }
        mandate.total_drawn += mandate.amount_per_period;

        // Store updated mandate
        env.storage()
            .persistent()
            .set(&DataKey::Mandate(mandate_id), &mandate);

        // Emit event — topics: (MANDATE, MDT_EXEC, user)
        emit_event(
            &env,
            MANDATE,
            MDT_EXEC,
            user.clone(),
            MandateExecutedEventData {
                mandate_id,
                user: mandate.user.clone(),
                vault: mandate.vault.clone(),
                amount: mandate.amount_per_period,
                total_drawn: mandate.total_drawn,
                executor: caller,
            },
        );
    }

    /// Cancel a mandate (user only).
    ///
    /// Cancellation is immediate and irrevocable within the same transaction.
    /// Only the mandate owner can cancel.
    pub fn cancel_mandate(env: Env, user: Address, mandate_id: u64) {
        user.require_auth();

        let mut mandate: Mandate = env
            .storage()
            .persistent()
            .get(&DataKey::Mandate(mandate_id))
            .unwrap_or_else(|| panic_with_error!(&env, Error::StrategyNotFound));

        if mandate.user != user {
            panic_with_error!(&env, Error::Unauthorized);
        }

        if !mandate.is_active {
            panic_with_error!(&env, Error::InvalidOperation);
        }

        mandate.is_active = false;
        env.storage()
            .persistent()
            .set(&DataKey::Mandate(mandate_id), &mandate);

        // Emit event — topics: (MANDATE, MDT_CANC, user)
        emit_event(
            &env,
            MANDATE,
            MDT_CANC,
            user.clone(),
            MandateCancelledEventData { mandate_id, user },
        );
    }

    /// Pause or resume a mandate.
    ///
    /// Only the mandate owner can pause or resume. Paused mandates fail
    /// `execute_mandate` with `MandatePaused`.
    pub fn pause_mandate(env: Env, user: Address, mandate_id: u64, paused: bool) {
        user.require_auth();

        let mut mandate: Mandate = env
            .storage()
            .persistent()
            .get(&DataKey::Mandate(mandate_id))
            .unwrap_or_else(|| panic_with_error!(&env, Error::StrategyNotFound));

        if mandate.user != user {
            panic_with_error!(&env, Error::Unauthorized);
        }

        if !mandate.is_active {
            panic_with_error!(&env, Error::InvalidOperation);
        }

        mandate.is_paused = paused;
        env.storage()
            .persistent()
            .set(&DataKey::Mandate(mandate_id), &mandate);

        // Emit event — topics: (MANDATE, MDT_PAUSE, user)
        emit_event(
            &env,
            MANDATE,
            MDT_PAUSE,
            user.clone(),
            MandatePausedEventData {
                mandate_id,
                user,
                paused,
            },
        );
    }

    /// Resume a paused mandate (convenience wrapper for `pause_mandate(..., false)`).
    pub fn resume_mandate(env: Env, user: Address, mandate_id: u64) {
        Self::pause_mandate(env, user, mandate_id, false);
    }

    /// Get mandate by ID.
    pub fn get_mandate(env: Env, mandate_id: u64) -> Mandate {
        env.storage()
            .persistent()
            .get(&DataKey::Mandate(mandate_id))
            .unwrap_or_else(|| panic_with_error!(&env, Error::StrategyNotFound))
    }

    /// Get user's active mandate IDs (bounded to `MAX_MANDATES_PER_USER`).
    pub fn get_user_mandates(env: Env, user: Address) -> Vec<u64> {
        let user_mandates: Vec<u64> = env
            .storage()
            .persistent()
            .get(&DataKey::UserMandates(user.clone()))
            .unwrap_or(Vec::new(&env));

        // Filter to only return active mandates
        let mut active_mandates = Vec::new(&env);
        for mandate_id in user_mandates.iter() {
            if let Some(mandate) = env
                .storage()
                .persistent()
                .get::<_, Mandate>(&DataKey::Mandate(mandate_id))
            {
                if mandate.is_active {
                    active_mandates.push_back(mandate_id);
                }
            }
        }

        active_mandates
    }

    /// Calculate the next timestamp at which a mandate may be executed.
    ///
    /// Returns 0 if the mandate is inactive, paused, or has expired.
    pub fn next_execution_at(env: Env, user: Address, mandate_id: u64) -> u64 {
        let mandate: Mandate = env
            .storage()
            .persistent()
            .get(&DataKey::Mandate(mandate_id))
            .unwrap_or_else(|| panic_with_error!(&env, Error::StrategyNotFound));

        if mandate.user != user {
            panic_with_error!(&env, Error::Unauthorized);
        }

        if !mandate.is_active || mandate.is_paused {
            return 0; // Cannot execute
        }

        let now = env.ledger().timestamp();

        if now < mandate.start_at {
            return mandate.start_at;
        }

        if mandate.last_executed_at == 0 {
            return mandate.start_at;
        }

        let next = mandate.last_executed_at + mandate.period_secs;
        if next >= mandate.expires_at {
            return 0; // Expired
        }

        next
    }

    // -----------------------------------------------------------------------
    // Access control
    // -----------------------------------------------------------------------

    pub fn grant_role(env: Env, grantor: Address, grantee: Address, role: Role) {
        AccessControl::grant_role(&env, &grantor, &grantee, role);
    }

    pub fn revoke_role(env: Env, revoker: Address, target: Address, role: Role) {
        AccessControl::revoke_role(&env, &revoker, &target, role);
    }

    pub fn has_role(env: Env, account: Address, role: Role) -> bool {
        AccessControl::has_role(&env, &account, role)
    }

    pub fn transfer_admin(env: Env, current_admin: Address, new_admin: Address) {
        AccessControl::transfer_admin(&env, &current_admin, &new_admin);
    }

    pub fn accept_admin(env: Env, new_admin: Address) {
        AccessControl::accept_admin(&env, &new_admin);
    }

    // -----------------------------------------------------------------------
    // Upgrade
    // -----------------------------------------------------------------------

    pub fn propose_upgrade(env: Env, admin: Address, new_wasm_hash: BytesN<32>) {
        admin.require_auth();
        Upgrade::propose_upgrade(&env, &admin, new_wasm_hash, 0, u64::MAX);
    }

    pub fn execute_upgrade(env: Env, admin: Address, wasm_hash: BytesN<32>) {
        admin.require_auth();
        Upgrade::execute_upgrade(&env, &admin, wasm_hash);
    }

    pub fn cancel_upgrade(env: Env, admin: Address) {
        admin.require_auth();
        Upgrade::cancel_upgrade(&env, &admin);
    }

    pub fn get_pending_upgrade(env: Env) -> Option<PendingUpgrade> {
        Upgrade::get_pending_upgrade(&env)
    }

    pub fn get_schema_version(env: Env) -> u32 {
        Upgrade::get_schema_version(&env)
    }
}