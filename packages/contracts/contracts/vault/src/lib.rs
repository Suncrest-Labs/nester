#![no_std]

use soroban_sdk::{
    contract, contractimpl, contracttype, panic_with_error, symbol_short, token, Address, Env,
    IntoVal, Symbol,
};
mod vault_token {
    soroban_sdk::contractimport!(
        file = "../../target/wasm32-unknown-unknown/release/vault_token.wasm"
    );
}
use vault_token::Client as VaultTokenContractClient;

use nester_access_control::{AccessControl, Role};
use nester_common::{emit_event, ContractError};

const VAULT: Symbol = symbol_short!("VAULT");
const DEPOSIT: Symbol = symbol_short!("DEPOSIT");
const WITHDRAW: Symbol = symbol_short!("WITHDRAW");
const PAUSE: Symbol = symbol_short!("PAUSE");
const UNPAUSE: Symbol = symbol_short!("UNPAUSE");
const CB_TRIGGER: Symbol = symbol_short!("CB_TRIG");

#[contracttype]
#[derive(Clone, Debug)]
pub struct FeeConfig {
    pub performance_fee_bps: u32,      // basis points (e.g., 1000 = 10%)
    pub management_fee_bps: u32,       // annual basis points (e.g., 50 = 0.5%)
    pub early_withdrawal_fee_bps: u32, // bps (e.g., 10 = 0.1%)
    pub treasury_address: Address,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct CircuitBreakerConfig {
    pub threshold_bps: u32,  // e.g., 2000 = 20%
    pub window_seconds: u64, // e.g., 7200 = 2h
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct WithdrawalWindow {
    pub last_update: u64,
    pub sum: i128,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct CircuitBreakerEventData {
    pub withdrawal_amount: i128,
    pub window_sum: i128,
    pub threshold: i128,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct DepositEventData {
    pub amount: i128,
    pub shares_minted: i128,
    pub new_balance: i128,
    pub total_assets: i128,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct WithdrawEventData {
    pub amount: i128,
    pub shares_burned: i128,
    pub new_balance: i128,
    pub total_assets: i128,
    pub fee_deducted: i128,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct TimestampEventData {
    pub timestamp: u64,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct EmergencyWithdrawEventData {
    pub user: Address,
    pub shares_burned: i128,
    pub assets_returned: i128,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct EmergencyWithdrawRequestedEventData {
    pub user: Address,
    pub amount: i128,
    pub fee_applied: i128,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct EmergencyWithdrawProcessedEventData {
    pub user: Address,
    pub amount_returned: i128,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct EmergencyWithdrawQueuedEventData {
    pub user: Address,
    pub amount: i128,
    pub position_in_queue: u32,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct EmergencyRequest {
    pub user: Address,
    pub amount: i128,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct EmergencyPreview {
    pub principal_deposited: i128,
    pub emergency_fee: i128,
    pub estimated_return: i128,
    pub vault_liquid_reserves: i128,
    pub can_process: bool,
}

// ---------------------------------------------------------------------------
// Storage
// ---------------------------------------------------------------------------

#[contracttype]
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum VaultStatus {
    Active,
    Paused,
}

#[contracttype]
#[derive(Clone)]
enum DataKey {
    Token,
    VaultToken,
    Status,
    TotalAssets, // Stores total assets (tokens) in vault (pre-fee)
    FeeConfig,
    LastFeeAccrual,
    AccruedFees,
    MinLockPeriod, // For early withdrawal fee
    DepositTime(Address),
    MaxDeposit,
    RebalanceThreshold,
    CircuitBreakerConfig,
    WithdrawalWindow,
    UserPrincipal(Address),
    EmergencyFeeBps,
    VaultLiquidReserves,
    EmergencyQueue,
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

fn require_initialized(env: &Env) {
    if !env.storage().instance().has(&DataKey::Token) {
        panic_with_error!(env, ContractError::NotInitialized);
    }
}

fn require_active(env: &Env) {
    if is_paused(env) {
        panic_with_error!(env, ContractError::InvalidOperation);
    }
}

fn is_paused(env: &Env) -> bool {
    env.storage()
        .instance()
        .get::<_, VaultStatus>(&DataKey::Status)
        .map(|s| s == VaultStatus::Paused)
        .unwrap_or(true)
}

fn get_vault_token(env: &Env) -> Address {
    env.storage()
        .instance()
        .get(&DataKey::VaultToken)
        .unwrap_or_else(|| panic_with_error!(env, ContractError::NotInitialized))
}

fn vault_token_client(env: &Env) -> VaultTokenContractClient<'_> {
    let vault_token = get_vault_token(env);
    VaultTokenContractClient::new(env, &vault_token)
}

fn get_shares(env: &Env, user: &Address) -> i128 {
    vault_token_client(env).balance(user)
}

fn get_total_assets(env: &Env) -> i128 {
    env.storage()
        .instance()
        .get(&DataKey::TotalAssets)
        .unwrap_or(0)
}

fn set_total_assets(env: &Env, amount: i128) {
    env.storage().instance().set(&DataKey::TotalAssets, &amount);
}

fn sync_vault_token_total_assets(env: &Env) {
    let gross = get_total_assets(env);
    let accrued = get_accrued_fees(env);
    let net_assets = if gross > accrued { gross - accrued } else { 0 };
    vault_token_client(env).set_total_assets(&net_assets);
}

fn get_accrued_fees(env: &Env) -> i128 {
    env.storage()
        .instance()
        .get(&DataKey::AccruedFees)
        .unwrap_or(0)
}

fn set_accrued_fees(env: &Env, amount: i128) {
    env.storage().instance().set(&DataKey::AccruedFees, &amount);
}

fn get_user_principal(env: &Env, user: &Address) -> i128 {
    env.storage()
        .persistent()
        .get(&DataKey::UserPrincipal(user.clone()))
        .unwrap_or(0)
}

fn set_user_principal(env: &Env, user: &Address, amount: i128) {
    env.storage()
        .persistent()
        .set(&DataKey::UserPrincipal(user.clone()), &amount);
}

fn get_vault_liquid_reserves(env: &Env) -> i128 {
    env.storage()
        .instance()
        .get(&DataKey::VaultLiquidReserves)
        .unwrap_or(0)
}

fn set_vault_liquid_reserves(env: &Env, amount: i128) {
    env.storage()
        .instance()
        .set(&DataKey::VaultLiquidReserves, &amount);
}

fn get_emergency_queue(env: &Env) -> soroban_sdk::Vec<EmergencyRequest> {
    env.storage()
        .instance()
        .get(&DataKey::EmergencyQueue)
        .unwrap_or(soroban_sdk::Vec::new(env))
}

fn set_emergency_queue(env: &Env, queue: &soroban_sdk::Vec<EmergencyRequest>) {
    env.storage()
        .instance()
        .set(&DataKey::EmergencyQueue, queue);
}

fn get_fee_config(env: &Env) -> FeeConfig {
    env.storage()
        .instance()
        .get(&DataKey::FeeConfig)
        .expect("Fee config not set")
}

fn accrue_management_fee(env: &Env) {
    let last_accrual: u64 = env
        .storage()
        .instance()
        .get(&DataKey::LastFeeAccrual)
        .unwrap_or(env.ledger().timestamp());
    let now = env.ledger().timestamp();
    let elapsed = now.saturating_sub(last_accrual);

    if elapsed > 0 {
        let config = get_fee_config(env);
        let total_assets = get_total_assets(env);
        let fee = nester_common::fees::calculate_management_fee(
            total_assets,
            config.management_fee_bps,
            elapsed,
        );

        if fee > 0 {
            let accrued = get_accrued_fees(env);
            set_accrued_fees(env, accrued + fee);
            sync_vault_token_total_assets(env);
        }
        env.storage().instance().set(&DataKey::LastFeeAccrual, &now);
    }
}

fn check_circuit_breaker(env: &Env, amount: i128) {
    let config: CircuitBreakerConfig = env
        .storage()
        .instance()
        .get(&DataKey::CircuitBreakerConfig)
        .expect("CB config missing");
    let mut window: WithdrawalWindow = env
        .storage()
        .instance()
        .get(&DataKey::WithdrawalWindow)
        .unwrap_or(WithdrawalWindow {
            last_update: env.ledger().timestamp(),
            sum: 0,
        });

    let now = env.ledger().timestamp();
    if now >= window.last_update + config.window_seconds {
        window.last_update = now;
        window.sum = amount;
    } else {
        window.sum += amount;
    }

    env.storage()
        .instance()
        .set(&DataKey::WithdrawalWindow, &window);

    let total_assets = get_total_assets(env);
    let threshold = total_assets * config.threshold_bps as i128 / 10000;

    if threshold > 0 && window.sum > threshold {
        env.storage()
            .instance()
            .set(&DataKey::Status, &VaultStatus::Paused);
        emit_event(
            env,
            VAULT,
            CB_TRIGGER,
            env.current_contract_address(),
            CircuitBreakerEventData {
                withdrawal_amount: amount,
                window_sum: window.sum,
                threshold,
            },
        );
    }
}

// ---------------------------------------------------------------------------
// Contract
// ---------------------------------------------------------------------------

#[contract]
pub struct VaultContract;

#[contractimpl]
impl VaultContract {
    /// Initialise the vault, setting `admin` as the sole Admin.
    pub fn initialize(
        env: Env,
        admin: Address,
        token_address: Address,
        vault_token_address: Address,
        treasury: Address,
    ) {
        // AccessControl::initialize handles AlreadyInitialized guard and require_auth
        AccessControl::initialize(&env, &admin);
        env.storage()
            .instance()
            .set(&DataKey::Token, &token_address);
        env.storage()
            .instance()
            .set(&DataKey::VaultToken, &vault_token_address);
        env.storage()
            .instance()
            .set(&DataKey::Status, &VaultStatus::Active);
        env.storage().instance().set(&DataKey::TotalAssets, &0_i128);
        env.storage().instance().set(&DataKey::AccruedFees, &0_i128);
        env.storage()
            .instance()
            .set(&DataKey::LastFeeAccrual, &env.ledger().timestamp());

        let fee_config = FeeConfig {
            performance_fee_bps: 1000,    // 10%
            management_fee_bps: 50,       // 0.5%
            early_withdrawal_fee_bps: 10, // 0.1%
            treasury_address: treasury,
        };
        env.storage()
            .instance()
            .set(&DataKey::FeeConfig, &fee_config);
        env.storage()
            .instance()
            .set(&DataKey::MinLockPeriod, &86400_u64); // 1 day

        // Emergency configs
        env.storage()
            .instance()
            .set(&DataKey::MaxDeposit, &i128::MAX);
        env.storage()
            .instance()
            .set(&DataKey::RebalanceThreshold, &500_u32); // 5%
        env.storage().instance().set(
            &DataKey::CircuitBreakerConfig,
            &CircuitBreakerConfig {
                threshold_bps: 2000,  // 20%
                window_seconds: 7200, // 2h
            },
        );
        env.storage().instance().set(
            &DataKey::WithdrawalWindow,
            &WithdrawalWindow {
                last_update: env.ledger().timestamp(),
                sum: 0,
            },
        );
    }

    pub fn set_max_deposit(env: Env, caller: Address, amount: i128) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);
        env.storage().instance().set(&DataKey::MaxDeposit, &amount);
    }

    pub fn set_rebalance_threshold(env: Env, caller: Address, bps: u32) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);
        env.storage()
            .instance()
            .set(&DataKey::RebalanceThreshold, &bps);
    }

    pub fn set_circuit_breaker_config(env: Env, caller: Address, config: CircuitBreakerConfig) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);
        env.storage()
            .instance()
            .set(&DataKey::CircuitBreakerConfig, &config);
    }

    pub fn set_early_withdrawal_fee(env: Env, caller: Address, bps: u32) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);
        let mut config = get_fee_config(&env);
        config.early_withdrawal_fee_bps = bps;
        env.storage().instance().set(&DataKey::FeeConfig, &config);
    }

    pub fn set_fee_config(env: Env, caller: Address, config: FeeConfig) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);
        env.storage().instance().set(&DataKey::FeeConfig, &config);
    }

    pub fn set_emergency_fee(env: Env, admin: Address, fee_bps: u32) -> Result<(), ContractError> {
        admin.require_auth();
        AccessControl::require_role(&env, &admin, Role::Admin);
        if fee_bps > 500 {
            panic_with_error!(&env, ContractError::InvalidAmount); // Max 500 bps (5%)
        }
        env.storage()
            .instance()
            .set(&DataKey::EmergencyFeeBps, &fee_bps);
        Ok(())
    }

    // -----------------------------------------------------------------------
    // Admin operations
    // -----------------------------------------------------------------------

    /// Pause all vault operations. Requires [`Role::Admin`].
    pub fn pause(env: Env, caller: Address) {
        require_initialized(&env);
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);
        env.storage()
            .instance()
            .set(&DataKey::Status, &VaultStatus::Paused);
        emit_event(
            &env,
            VAULT,
            PAUSE,
            caller.clone(),
            TimestampEventData {
                timestamp: env.ledger().timestamp(),
            },
        );
    }

    /// Resume vault operations. Requires [`Role::Admin`].
    pub fn unpause(env: Env, caller: Address) {
        require_initialized(&env);
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);
        env.storage()
            .instance()
            .set(&DataKey::Status, &VaultStatus::Active);
        emit_event(
            &env,
            VAULT,
            UNPAUSE,
            caller.clone(),
            TimestampEventData {
                timestamp: env.ledger().timestamp(),
            },
        );
    }

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

    // -----------------------------------------------------------------------
    // Core vault operations
    // -----------------------------------------------------------------------

    pub fn report_yield(env: Env, caller: Address, amount: i128) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Manager);

        let total_assets = get_total_assets(&env);
        set_total_assets(&env, total_assets + amount);
        sync_vault_token_total_assets(&env);
    }

    pub fn collect_fees(env: Env, caller: Address) {
        caller.require_auth();
        // Allow ADMIN or MANAGER to collect fees
        if !AccessControl::has_role(&env, &caller, Role::Admin)
            && !AccessControl::has_role(&env, &caller, Role::Manager)
        {
            panic_with_error!(&env, ContractError::Unauthorized);
        }

        accrue_management_fee(&env);
        let fees = get_accrued_fees(&env);
        if fees > 0 {
            let config = get_fee_config(&env);
            let token_address = self::VaultContract::get_token(env.clone());

            token::Client::new(&env, &token_address).transfer(
                &env.current_contract_address(),
                &config.treasury_address,
                &fees,
            );

            // Notify treasury - assuming it has receive_fees method
            // We'll use a raw invoke to avoid dependency on Treasury client here for simplicity
            // Or just rely on token transfer being enough if treasury tracks its own balance.
            // The treasury contract I wrote has receive_fees(amount).
            env.invoke_contract::<()>(
                &config.treasury_address,
                &Symbol::new(&env, "receive_fees"),
                (fees,).into_val(&env),
            );

            set_accrued_fees(&env, 0);

            let total_assets = get_total_assets(&env);
            set_total_assets(&env, total_assets - fees);
            sync_vault_token_total_assets(&env);

            let current_reserves = get_vault_liquid_reserves(&env);
            set_vault_liquid_reserves(&env, current_reserves - fees);
        }
    }

    /// Deposit funds into the vault.
    pub fn deposit(env: Env, user: Address, amount: i128) -> i128 {
        require_initialized(&env);
        require_active(&env);

        let max_deposit: i128 = env
            .storage()
            .instance()
            .get(&DataKey::MaxDeposit)
            .unwrap_or(i128::MAX);
        if amount > max_deposit {
            panic_with_error!(&env, ContractError::ExceedsLimit);
        }

        if amount <= 0 {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }

        user.require_auth();
        accrue_management_fee(&env);

        let token_address = self::VaultContract::get_token(env.clone());
        let contract_address = env.current_contract_address();

        token::Client::new(&env, &token_address).transfer(&user, &contract_address, &amount);

        let total_assets = get_total_assets(&env);
        let shares_to_mint = vault_token_client(&env).mint_for_deposit(&user, &amount);
        let new_user_shares = get_shares(&env, &user);
        set_total_assets(&env, total_assets + amount);

        let current_principal = get_user_principal(&env, &user);
        set_user_principal(&env, &user, current_principal + amount);

        let current_reserves = get_vault_liquid_reserves(&env);
        set_vault_liquid_reserves(&env, current_reserves + amount);

        env.storage().persistent().set(
            &DataKey::DepositTime(user.clone()),
            &env.ledger().timestamp(),
        );

        emit_event(
            &env,
            VAULT,
            DEPOSIT,
            user.clone(),
            DepositEventData {
                amount,
                shares_minted: shares_to_mint,
                new_balance: new_user_shares,
                total_assets: total_assets + amount,
            },
        );

        Self::process_emergency_queue(env.clone());

        new_user_shares
    }

    pub fn process_emergency_queue(env: Env) {
        let queue = get_emergency_queue(&env);
        if queue.is_empty() {
            return;
        }

        let mut liquid_reserves = get_vault_liquid_reserves(&env);
        let token_address = self::VaultContract::get_token(env.clone());
        let contract_address = env.current_contract_address();
        let token_client = token::Client::new(&env, &token_address);

        let mut i = 0;
        while i < queue.len() {
            let req = queue.get(i).unwrap();
            if liquid_reserves >= req.amount {
                token_client.transfer(&contract_address, &req.user, &req.amount);
                liquid_reserves -= req.amount;

                emit_event(
                    &env,
                    VAULT,
                    symbol_short!("ERG_PROC"),
                    req.user.clone(),
                    EmergencyWithdrawProcessedEventData {
                        user: req.user.clone(),
                        amount_returned: req.amount,
                    },
                );
            } else {
                break;
            }
            i += 1;
        }

        let mut new_queue = soroban_sdk::Vec::new(&env);
        while i < queue.len() {
            new_queue.push_back(queue.get(i).unwrap());
            i += 1;
        }

        set_vault_liquid_reserves(&env, liquid_reserves);
        set_emergency_queue(&env, &new_queue);
    }

    /// Withdraw funds from the vault.
    pub fn withdraw(env: Env, user: Address, shares: i128) -> i128 {
        require_initialized(&env);
        require_active(&env);

        if shares <= 0 {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }

        user.require_auth();
        accrue_management_fee(&env);

        let current_shares = get_shares(&env, &user);
        if shares > current_shares {
            panic_with_error!(&env, ContractError::InsufficientBalance);
        }

        let total_assets = get_total_assets(&env);
        let accrued_fees = get_accrued_fees(&env);
        let mut assets_to_withdraw = vault_token_client(&env).amount_for_shares(&shares);

        // Trigger circuit breaker check
        check_circuit_breaker(&env, assets_to_withdraw);

        // Fee logic
        let config = get_fee_config(&env);
        let mut total_fee = 0_i128;

        // 1. Performance fee (10% of yield)
        let yield_part = assets_to_withdraw - shares;
        if yield_part > 0 {
            let perf_fee = nester_common::fees::calculate_performance_fee(
                yield_part,
                config.performance_fee_bps,
            );
            total_fee += perf_fee;
        }

        // 2. Early withdrawal fee (0.1%)
        let deposit_time: u64 = env
            .storage()
            .persistent()
            .get(&DataKey::DepositTime(user.clone()))
            .unwrap_or(0);
        let min_lock: u64 = env
            .storage()
            .instance()
            .get(&DataKey::MinLockPeriod)
            .unwrap_or(0);
        if env.ledger().timestamp() < deposit_time + min_lock {
            let early_fee = nester_common::fees::calculate_withdrawal_fee(
                assets_to_withdraw,
                config.early_withdrawal_fee_bps,
            );
            total_fee += early_fee;
        }

        assets_to_withdraw -= total_fee;
        set_accrued_fees(&env, accrued_fees + total_fee);

        let token_address = self::VaultContract::get_token(env.clone());
        let contract_address = env.current_contract_address();

        token::Client::new(&env, &token_address).transfer(
            &contract_address,
            &user,
            &assets_to_withdraw,
        );

        let _ = vault_token_client(&env).burn_for_withdrawal(&user, &shares);
        let new_user_shares = current_shares - shares;
        set_total_assets(&env, total_assets - assets_to_withdraw);

        let current_principal = get_user_principal(&env, &user);
        let principal_to_remove = if current_shares > 0 {
            current_principal * shares / current_shares
        } else {
            0
        };
        set_user_principal(&env, &user, current_principal - principal_to_remove);

        let current_reserves = get_vault_liquid_reserves(&env);
        set_vault_liquid_reserves(&env, current_reserves - assets_to_withdraw);

        emit_event(
            &env,
            VAULT,
            WITHDRAW,
            user.clone(),
            WithdrawEventData {
                amount: assets_to_withdraw,
                shares_burned: shares,
                new_balance: new_user_shares,
                total_assets: total_assets - assets_to_withdraw,
                fee_deducted: total_fee,
            },
        );

        new_user_shares
    }

    pub fn emergency_withdraw_preview(
        env: Env,
        user: Address,
    ) -> Result<EmergencyPreview, ContractError> {
        let principal = get_user_principal(&env, &user);
        let fee_bps: u32 = env
            .storage()
            .instance()
            .get(&DataKey::EmergencyFeeBps)
            .unwrap_or(0);
        let emergency_fee = principal * (fee_bps as i128) / 10_000;
        let estimated_return = principal - emergency_fee;

        let vault_liquid_reserves = get_vault_liquid_reserves(&env);
        let can_process = vault_liquid_reserves >= estimated_return;

        Ok(EmergencyPreview {
            principal_deposited: principal,
            emergency_fee,
            estimated_return,
            vault_liquid_reserves,
            can_process,
        })
    }

    /// Direct withdrawal bypassing normal logic, only available when paused.
    pub fn emergency_withdraw(env: Env, user: Address) -> Result<i128, ContractError> {
        require_initialized(&env);
        if !is_paused(&env) {
            panic_with_error!(&env, ContractError::InvalidOperation);
        }

        user.require_auth();

        let principal = get_user_principal(&env, &user);
        if principal <= 0 {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }

        let fee_bps: u32 = env
            .storage()
            .instance()
            .get(&DataKey::EmergencyFeeBps)
            .unwrap_or(0);
        let fee = principal * (fee_bps as i128) / 10_000;
        let return_amount = principal - fee;

        let liquid_reserves = get_vault_liquid_reserves(&env);

        let shares = get_shares(&env, &user);
        let total_assets = get_total_assets(&env);
        let burned_assets = if shares > 0 {
            vault_token_client(&env).burn_for_withdrawal(&user, &shares)
        } else {
            0
        };
        set_total_assets(&env, total_assets - burned_assets);
        set_user_principal(&env, &user, 0);

        emit_event(
            &env,
            VAULT,
            symbol_short!("ERG_REQ"),
            user.clone(),
            EmergencyWithdrawRequestedEventData {
                user: user.clone(),
                amount: return_amount,
                fee_applied: fee,
            },
        );

        if liquid_reserves < return_amount {
            let mut queue = get_emergency_queue(&env);
            queue.push_back(EmergencyRequest {
                user: user.clone(),
                amount: return_amount,
            });
            set_emergency_queue(&env, &queue);

            let position = queue.len();
            emit_event(
                &env,
                VAULT,
                symbol_short!("ERG_QUE"),
                user.clone(),
                EmergencyWithdrawQueuedEventData {
                    user: user.clone(),
                    amount: return_amount,
                    position_in_queue: position,
                },
            );

            Ok(0)
        } else {
            let token_address = self::VaultContract::get_token(env.clone());
            token::Client::new(&env, &token_address).transfer(
                &env.current_contract_address(),
                &user,
                &return_amount,
            );

            set_vault_liquid_reserves(&env, liquid_reserves - return_amount);

            emit_event(
                &env,
                VAULT,
                symbol_short!("ERG_PROC"),
                user.clone(),
                EmergencyWithdrawProcessedEventData {
                    user: user.clone(),
                    amount_returned: return_amount,
                },
            );

            Ok(return_amount)
        }
    }

    // -----------------------------------------------------------------------
    // View functions
    // -----------------------------------------------------------------------

    pub fn get_balance(env: Env, user: Address) -> i128 {
        require_initialized(&env);
        let shares = get_shares(&env, &user);
        if shares <= 0 {
            return 0;
        }
        vault_token_client(&env).amount_for_shares(&shares)
    }

    pub fn get_shares(env: Env, user: Address) -> i128 {
        require_initialized(&env);
        get_shares(&env, &user)
    }

    pub fn get_total_deposits(env: Env) -> i128 {
        require_initialized(&env);
        let total_assets = get_total_assets(&env);
        let accrued_fees = get_accrued_fees(&env);
        total_assets - accrued_fees
    }

    pub fn get_status(env: Env) -> VaultStatus {
        require_initialized(&env);
        env.storage()
            .instance()
            .get(&DataKey::Status)
            .unwrap_or(VaultStatus::Paused)
    }

    pub fn get_token(env: Env) -> Address {
        require_initialized(&env);
        env.storage()
            .instance()
            .get(&DataKey::Token)
            .unwrap_or_else(|| panic_with_error!(&env, ContractError::NotInitialized))
    }

    pub fn get_vault_token(env: Env) -> Address {
        require_initialized(&env);
        get_vault_token(&env)
    }

    pub fn is_paused(env: Env) -> bool {
        is_paused(&env)
    }

    pub fn get_fee_config(env: Env) -> FeeConfig {
        get_fee_config(&env)
    }

    pub fn get_accrued_fees(env: Env) -> i128 {
        get_accrued_fees(&env)
    }

    pub fn get_max_deposit(env: Env) -> i128 {
        env.storage()
            .instance()
            .get(&DataKey::MaxDeposit)
            .unwrap_or(i128::MAX)
    }

    pub fn get_rebalance_threshold(env: Env) -> u32 {
        env.storage()
            .instance()
            .get(&DataKey::RebalanceThreshold)
            .unwrap_or(500)
    }

    pub fn get_circuit_breaker_config(env: Env) -> CircuitBreakerConfig {
        env.storage()
            .instance()
            .get(&DataKey::CircuitBreakerConfig)
            .expect("CB config missing")
    }
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod test;
