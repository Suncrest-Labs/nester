#![no_std]

mod breaker;
pub mod conversion;

use soroban_sdk::{
    contract, contractimpl, contracttype, panic_with_error, symbol_short, token, Address, BytesN, Env,
    IntoVal, Symbol, Val, Vec,
};

pub use breaker::{BreakerConfig, BreakerStatus, Severity, TripReason};

struct VaultTokenContractClient<'a> {
    env: &'a Env,
    address: Address,
}

impl<'a> VaultTokenContractClient<'a> {
    fn new(env: &'a Env, address: &Address) -> Self {
        Self {
            env,
            address: address.clone(),
        }
    }

    fn call<R>(&self, name: &str, args: Vec<Val>) -> R
    where
        R: soroban_sdk::TryFromVal<Env, Val>,
    {
        CalleeAllowlist::assert_allowed(self.env, &self.address);
        self.env
            .invoke_contract(&self.address, &Symbol::new(self.env, name), args)
    }

    fn balance(&self, id: &Address) -> i128 {
        self.call(
            "balance",
            soroban_sdk::vec![self.env, id.clone().into_val(self.env)],
        )
    }

    fn set_total_assets(&self, new_total: &i128) {
        self.call(
            "set_total_assets",
            soroban_sdk::vec![self.env, (*new_total).into_val(self.env)],
        )
    }

    fn shares_for_deposit(&self, amount: &i128) -> i128 {
        self.call(
            "shares_for_deposit",
            soroban_sdk::vec![self.env, (*amount).into_val(self.env)],
        )
    }

    fn mint_for_deposit(&self, to: &Address, amount: &i128) -> i128 {
        self.call(
            "mint_for_deposit",
            soroban_sdk::vec![
                self.env,
                to.clone().into_val(self.env),
                (*amount).into_val(self.env)
            ],
        )
    }

    fn amount_for_shares(&self, shares: &i128) -> i128 {
        self.call(
            "amount_for_shares",
            soroban_sdk::vec![self.env, (*shares).into_val(self.env)],
        )
    }

    fn get_deposit_time(&self, user: &Address) -> u64 {
        self.call(
            "get_deposit_time",
            soroban_sdk::vec![self.env, user.clone().into_val(self.env)],
        )
    }

    fn burn_for_withdrawal(&self, from: &Address, shares: &i128) -> i128 {
        self.call(
            "burn_for_withdrawal",
            soroban_sdk::vec![
                self.env,
                from.clone().into_val(self.env),
                (*shares).into_val(self.env)
            ],
        )
    }

    fn share_price(&self) -> i128 {
        self.call("share_price", soroban_sdk::vec![self.env])
    }

    fn total_supply(&self) -> i128 {
        self.call("total_supply", soroban_sdk::vec![self.env])
    }
}

mod queue;
mod rebalance;

use nester_access_control::{AccessControl, Role};
use nester_common::{emit_event, with_reentrancy_guard, CalleeAllowlist, ContractError};
use queue::{QueueEntry, QueuePosition, QueueStats};
pub use rebalance::RebalanceLeg;

const VAULT: Symbol = symbol_short!("VAULT");
const DEPOSIT: Symbol = symbol_short!("DEPOSIT");
const WITHDRAW: Symbol = symbol_short!("WITHDRAW");
const PAUSE: Symbol = symbol_short!("PAUSE");
const UNPAUSE: Symbol = symbol_short!("UNPAUSE");
const REBALANCE: Symbol = symbol_short!("REBAL");
const HARVEST: Symbol = symbol_short!("HARVEST");
const HARVEST_VLT: Symbol = symbol_short!("HARV_VLT");
const MIN_REBALANCE_AMOUNT: i128 = 1;
const DEFAULT_REBALANCE_COOLDOWN: u64 = 3600;
/// Default rebalance slippage tolerance: 50 bps (0.5%) — issue #638.
const DEFAULT_REBALANCE_SLIPPAGE_BPS: u32 = 50;
/// Upper bound on the configurable rebalance slippage tolerance (50%).
const MAX_REBALANCE_SLIPPAGE_BPS: u32 = 5_000;
const FEE_CONFIG_UPDATED: Symbol = symbol_short!("FEE_CFG");
const EMRG_REQD: Symbol = symbol_short!("EMRG_REQD");
const EMRG_FILL: Symbol = symbol_short!("EMRG_FILL");
const EMRG_CANCL: Symbol = symbol_short!("EMRG_CANC");
const REBAL_LEG: Symbol = symbol_short!("REBAL_LEG");
const REBAL_CMP: Symbol = symbol_short!("REBAL_CMP");
const DEFAULT_MAX_REBALANCE_VALUE_BPS: u32 = rebalance::DEFAULT_MAX_REBALANCE_VALUE_BPS;
const DEFAULT_MAX_LEG_SLIPPAGE_BPS: u32 = rebalance::MAX_LEG_SLIPPAGE_BPS_CEILING;
const PNLTY_CHG: Symbol = symbol_short!("PNLTY_CHG");
const PNLTY_DST: Symbol = symbol_short!("PNLTY_DST");
/// Default split: 70% of every penalty compensates remaining depositors,
/// 30% is protocol revenue — within the compile-time treasury cap.
const DEFAULT_DEPOSITOR_SHARE_BPS: u32 = 10_000 - nester_common::MAX_TREASURY_SHARE_BPS + 2_000;
const DEFAULT_MIN_PENALTY_DISTRIBUTION_AMOUNT: i128 = 1_000_000;
const DEFAULT_PENALTY_DISTRIBUTION_COOLDOWN: u64 = 3_600;

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
pub struct FeeConfigUpdatedEventData {
    pub old_config: FeeConfig,
    pub new_config: FeeConfig,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct CircuitBreakerConfig {
    pub threshold_bps: u32,  // e.g., 2000 = 20%
    pub window_seconds: u64, // e.g., 7200 = 2h
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct WithdrawalEntry {
    pub timestamp: u64,
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

/// Fair-ordering emergency withdrawal queue events (issue #814). Distinct
/// from the legacy `ERG_REQ`/`ERG_QUE`/`ERG_PROC` events emitted by
/// `emergency_withdraw`, which remains a separate, simpler paused-only exit
/// path. `request_emergency_withdrawal` / `process_emergency_queue` /
/// `cancel_emergency_request` are the fairness-hardened primitives intended
/// for general withdrawal-under-stress scenarios.
#[contracttype]
#[derive(Clone, Debug)]
pub struct EmergencyRequestedEventData {
    pub user: Address,
    pub seq: u64,
    pub shares_requested: i128,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct EmergencyFilledEventData {
    pub user: Address,
    pub seq: u64,
    pub fill_shares: i128,
    pub fill_assets: i128,
    pub fully_filled: bool,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct EmergencyCancelledEventData {
    pub user: Address,
    pub seq: u64,
    pub shares_returned: i128,
}

/// A single yield-source position unwound by an emergency exit.
#[contracttype]
#[derive(Clone, Debug)]
pub struct PositionWithdrawal {
    pub protocol: Symbol,
    pub amount: i128,
}

/// Outcome of [`VaultContract::emergency_withdraw_all`]: positions that were
/// successfully unwound and those that could not be (failures are logged, not
/// fatal, so a single bad position can't block the rest from exiting).
#[contracttype]
#[derive(Clone, Debug)]
pub struct EmergencyWithdrawAllResult {
    pub succeeded: Vec<PositionWithdrawal>,
    pub failed: Vec<PositionWithdrawal>,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct PositionEmergencyWithdrawEventData {
    pub user: Address,
    pub protocol: Symbol,
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

#[contracttype]
#[derive(Clone, Debug)]
pub struct WithdrawalFeePreview {
    pub gross_asset_value: i128,
    pub management_fee_deducted: i128,
    pub performance_fee_deducted: i128,
    pub early_withdrawal_fee_deducted: i128,
    pub net_amount_received: i128,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct HarvestResult {
    pub gross_yield: i128,
    pub performance_fee: i128,
    pub net_yield: i128,
    pub compounded: bool, // true if net_yield was reinvested into vault shares
    pub new_share_balance: i128, // user's vault-token balance after harvest
    pub user: Address,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct VaultHarvestResult {
    pub total_gross_yield: i128,
    pub total_fee_collected: i128,
    pub total_net_yield: i128,
    pub positions_harvested: u32,
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
    MinDeposit,
    RebalanceThreshold,
    CircuitBreakerConfig,
    WithdrawalHistory,
    UserPrincipal(Address),
    EmergencyFeeBps,
    VaultLiquidReserves,
    EmergencyQueue,
    LiquidReserved, // total amount committed to the emergency queue but not yet paid
    AllocationStrategy,
    SourceAllocation(Symbol),
    AllocatedSources,
    LastRebalanceAt,
    RebalanceCooldown,
    RebalanceSlippageBps,
    UserYield(Address),
    TotalReportedYield,
    LastHarvestAt(Address),
    FirstDepositAt(Address),
    PerformanceFeeTiers,
    ExitFeeTiers,
    ManagementFeeTiers,
    MaxLegSlippageBps,
    MaxRebalanceValueBps,
    PenaltyEscrow,
    DepositorShareBps,
    LastPenaltyDistributionAt,
    MinPenaltyDistributionAmount,
    PenaltyDistributionCooldown,
    // --- Circuit breaker v2 (issue #817) ---
    Severity,
    LastTripReason,
    LastObservedValue,
    LastThreshold,
    NextRecoveryAllowedAt,
    SharePriceBaseline,
    SharePriceBaselineAt,
    SourceFailureCount,
    BreakerConfigV2,
    // --- Referral integration (issue #818) ---
    ReferralContract,
}

/// Why a penalty was charged (issue #805). `LockBreak` and `WeightDeviation`
/// are reserved for the time-lock and multi-asset-deposit features
/// respectively — neither exists in this vault yet, so no path currently
/// emits them, but the discriminant is defined up front so those features
/// can adopt the same escrow without an event-schema change later.
#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum PenaltyReason {
    EarlyWithdrawal,
    LockBreak,
    EmergencyExit,
    WeightDeviation,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct PenaltyChargedEventData {
    pub user: Address,
    pub amount: i128,
    pub shares_burned: i128,
    pub reason: PenaltyReason,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct PenaltyDistributedEventData {
    pub depositor_amount: i128,
    pub treasury_amount: i128,
    pub retained_dust: i128,
    pub timestamp: u64,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct PenaltyConfig {
    pub depositor_share_bps: u32,
    pub min_distribution_amount: i128,
    pub distribution_cooldown: u64,
}

/// One (threshold, rate) breakpoint in an on-chain fee schedule (issue
/// #813). `threshold` is a tenure in seconds for performance/exit tiers, or
/// a TVL amount for the management tier.
#[contracttype]
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct VaultFeeTier {
    pub threshold: i128,
    pub rate_bps: u32,
}

#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum FeeTierKind {
    Performance,
    Exit,
    Management,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct FeeSchedulePreview {
    pub tenure_secs: u64,
    pub current_exit_fee_bps: u32,
    pub current_performance_fee_bps: u32,
    pub next_boundary_secs: Option<u64>,
    pub next_boundary_exit_fee_bps: u32,
    pub next_boundary_perf_fee_bps: u32,
}

#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CurrentAllocationView {
    pub source_id: Symbol,
    pub amount: i128,
}

#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AllocationDeltaView {
    pub source_id: Symbol,
    pub delta: i128,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct RebalancedEventData {
    pub source_deltas: Vec<AllocationDeltaView>,
    pub timestamp: u64,
}

/// Return shape of `plan_rebalance`: the plan plus its `plan_hash`, so
/// callers never have to compute the checksum themselves off-chain.
#[contracttype]
#[derive(Clone, Debug)]
pub struct RebalancePlan {
    pub legs: Vec<RebalanceLeg>,
    pub plan_hash: u64,
}

/// Per-leg execution record for `execute_rebalance` (issue #810).
#[contracttype]
#[derive(Clone, Debug)]
pub struct RebalanceLegExecutedEventData {
    pub source_id: Symbol,
    pub delta: i128,
    pub amount_out: i128,
    pub min_out: i128,
}

/// Summary emitted once per `execute_rebalance` call.
#[contracttype]
#[derive(Clone, Debug)]
pub struct RebalanceCompletedEventData {
    pub plan_hash: u64,
    pub total_value_moved: i128,
    pub realized_slippage_bps: u32,
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

/// Slippage-safe minimum proceeds for withdrawing `gross` assets during a
/// rebalance (#638): the net-of-fees withdrawal preview reduced by
/// `slippage_bps`. Uses `preview_withdraw_net` (never the gross
/// `preview_withdraw`) so fees are already accounted for. Returns 0 when the
/// gross is too small to map to any shares.
fn rebalance_min_assets_out(env: &Env, gross: i128, slippage_bps: u32) -> i128 {
    let shares_equiv = vault_token_client(env).shares_for_deposit(&gross);
    if shares_equiv <= 0 {
        return 0;
    }
    let net = VaultContract::preview_withdraw_net(env.clone(), shares_equiv);
    nester_common::fees::mul_div(net, (10_000 - slippage_bps) as i128, 10_000).unwrap_or(0)
}

/// Reverts with `SlippageExceeded` when realised proceeds fall below the floor.
fn enforce_rebalance_slippage(env: &Env, min_assets_out: i128, actual_received: i128) {
    if actual_received < min_assets_out {
        panic_with_error!(env, ContractError::SlippageExceeded);
    }
}

fn invoke_allowed<R>(env: &Env, address: &Address, fn_name: &Symbol, args: Vec<Val>) -> R
where
    R: soroban_sdk::TryFromVal<Env, Val>,
{
    CalleeAllowlist::assert_allowed(env, address);
    env.invoke_contract(address, fn_name, args)
}

fn transfer_tokens(env: &Env, token: &Address, from: &Address, to: &Address, amount: &i128) {
    CalleeAllowlist::assert_allowed(env, token);
    token::Client::new(env, token).transfer(from, to, amount);
}

fn bootstrap_callee_allowlist(
    env: &Env,
    token: &Address,
    vault_token: &Address,
    treasury: &Address,
) {
    CalleeAllowlist::register(env, token);
    CalleeAllowlist::register(env, vault_token);
    CalleeAllowlist::register(env, treasury);
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

/// Assets backing shares after already-accrued vault fees.
fn get_net_total_assets(env: &Env) -> i128 {
    get_total_assets(env).saturating_sub(get_accrued_fees(env))
}

/// Remaining assets that can be withdrawn without crossing the configured
/// rolling circuit-breaker threshold. This helper is read-only and deliberately
/// uses saturating arithmetic so `max_*` queries never fail.
fn circuit_breaker_headroom(env: &Env) -> i128 {
    let Some(config) = env
        .storage()
        .instance()
        .get::<_, CircuitBreakerConfig>(&DataKey::CircuitBreakerConfig)
    else {
        return 0;
    };

    let threshold = nester_common::fees::mul_div(
        get_total_assets(env),
        config.threshold_bps as i128,
        10_000,
    )
    .unwrap_or(0);
    // A zero threshold disables the check in `check_circuit_breaker`.
    if threshold == 0 {
        return i128::MAX;
    }

    let window_start = env
        .ledger()
        .timestamp()
        .saturating_sub(config.window_seconds);
    let history: Vec<WithdrawalEntry> = env
        .storage()
        .instance()
        .get(&DataKey::WithdrawalHistory)
        .unwrap_or(Vec::new(env));
    let mut used = 0_i128;
    for entry in history.iter() {
        if entry.timestamp >= window_start && entry.sum > 0 {
            used = used.saturating_add(entry.sum);
        }
    }
    threshold.saturating_sub(used)
}

/// Maximum gross assets the holder can presently redeem. Unlike the mutating
/// withdrawal path this function never panics.
fn max_withdrawable_assets(env: &Env, user: &Address) -> i128 {
    if !env.storage().instance().has(&DataKey::Token) || is_paused(env) {
        return 0;
    }

    let shares = get_shares(env, user);
    let total_shares = vault_token_client(env).total_supply();
    let total_assets = get_net_total_assets(env);
    let holder_assets =
        conversion::shares_to_assets_down(shares, total_assets, total_shares).unwrap_or(0);
    let liquid = get_vault_liquid_reserves(env).saturating_sub(get_liquid_reserved(env));
    holder_assets.min(liquid).min(circuit_breaker_headroom(env))
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

fn get_liquid_reserved(env: &Env) -> i128 {
    env.storage()
        .instance()
        .get(&DataKey::LiquidReserved)
        .unwrap_or(0)
}

fn set_liquid_reserved(env: &Env, amount: i128) {
    env.storage()
        .instance()
        .set(&DataKey::LiquidReserved, &amount);
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

// ---------------------------------------------------------------------------
// Tiered fee schedule helpers (issue #813)
// ---------------------------------------------------------------------------

fn has_first_deposit_at(env: &Env, user: &Address) -> bool {
    env.storage()
        .persistent()
        .has(&DataKey::FirstDepositAt(user.clone()))
}

fn get_first_deposit_at(env: &Env, user: &Address) -> u64 {
    env.storage()
        .persistent()
        .get(&DataKey::FirstDepositAt(user.clone()))
        .unwrap_or(0)
}

fn set_first_deposit_at(env: &Env, user: &Address, ts: u64) {
    env.storage()
        .persistent()
        .set(&DataKey::FirstDepositAt(user.clone()), &ts);
}

fn clear_first_deposit_at(env: &Env, user: &Address) {
    env.storage()
        .persistent()
        .remove(&DataKey::FirstDepositAt(user.clone()));
}

/// Seconds since `user`'s first deposit into their current position. Reset
/// only by a full exit (see [`clear_first_deposit_at`]); unaffected by
/// partial withdrawals or additional deposits.
///
/// Existence is checked explicitly rather than comparing the stored
/// timestamp to zero — a deposit genuinely made at ledger timestamp 0 (the
/// start of any fresh test/dev chain) is a legitimate value, not a sentinel
/// for "unset".
fn tenure_secs(env: &Env, user: &Address) -> u64 {
    if !has_first_deposit_at(env, user) {
        return 0;
    }
    let first = get_first_deposit_at(env, user);
    env.ledger().timestamp().saturating_sub(first)
}

fn get_performance_tiers(env: &Env) -> Vec<VaultFeeTier> {
    env.storage()
        .instance()
        .get(&DataKey::PerformanceFeeTiers)
        .unwrap_or(Vec::new(env))
}

fn set_performance_tiers(env: &Env, tiers: &Vec<VaultFeeTier>) {
    env.storage()
        .instance()
        .set(&DataKey::PerformanceFeeTiers, tiers);
}

fn get_exit_tiers(env: &Env) -> Vec<VaultFeeTier> {
    env.storage()
        .instance()
        .get(&DataKey::ExitFeeTiers)
        .unwrap_or(Vec::new(env))
}

fn set_exit_tiers(env: &Env, tiers: &Vec<VaultFeeTier>) {
    env.storage().instance().set(&DataKey::ExitFeeTiers, tiers);
}

fn get_management_tiers(env: &Env) -> Vec<VaultFeeTier> {
    env.storage()
        .instance()
        .get(&DataKey::ManagementFeeTiers)
        .unwrap_or(Vec::new(env))
}

fn set_management_tiers(env: &Env, tiers: &Vec<VaultFeeTier>) {
    env.storage()
        .instance()
        .set(&DataKey::ManagementFeeTiers, tiers);
}

/// Converts a bounded on-chain `Vec<VaultFeeTier>` into a fixed-size array so
/// it can be handed to `nester_common::fees`'s pure, Soroban-free tier math.
/// Bounded by `MAX_FEE_TIERS`, so this is cheap regardless of on-chain size
/// (which is itself validated to never exceed that bound on write).
fn to_fee_tier_array(
    tiers: &Vec<VaultFeeTier>,
) -> (
    [nester_common::fees::FeeTier; nester_common::fees::MAX_FEE_TIERS],
    usize,
) {
    let mut arr = [nester_common::fees::FeeTier {
        threshold: 0,
        rate_bps: 0,
    }; nester_common::fees::MAX_FEE_TIERS];
    let mut n = 0;
    for t in tiers.iter() {
        if n >= arr.len() {
            break;
        }
        arr[n] = nester_common::fees::FeeTier {
            threshold: t.threshold,
            rate_bps: t.rate_bps,
        };
        n += 1;
    }
    (arr, n)
}

/// Tenure-tiered performance fee rate, falling back to the flat
/// `FeeConfig::performance_fee_bps` when no tiers have been configured
/// (empty table = feature not opted into, fully backward compatible).
fn effective_performance_fee_bps(env: &Env, user: &Address, config: &FeeConfig) -> u32 {
    let tiers = get_performance_tiers(env);
    let (arr, n) = to_fee_tier_array(&tiers);
    if n == 0 {
        config.performance_fee_bps
    } else {
        nester_common::fees::rate_at(&arr[..n], tenure_secs(env, user) as i128)
    }
}

/// Tenure-tiered exit (early withdrawal) fee rate, falling back to the flat
/// `FeeConfig::early_withdrawal_fee_bps` when no tiers are configured.
fn effective_exit_fee_bps(env: &Env, user: &Address, config: &FeeConfig) -> u32 {
    let tiers = get_exit_tiers(env);
    let (arr, n) = to_fee_tier_array(&tiers);
    if n == 0 {
        config.early_withdrawal_fee_bps
    } else {
        nester_common::fees::rate_at(&arr[..n], tenure_secs(env, user) as i128)
    }
}

/// TVL-tiered management fee rate, falling back to the flat
/// `FeeConfig::management_fee_bps` when no tiers are configured.
fn effective_management_fee_bps(env: &Env, total_assets: i128, config: &FeeConfig) -> u32 {
    let tiers = get_management_tiers(env);
    let (arr, n) = to_fee_tier_array(&tiers);
    if n == 0 {
        config.management_fee_bps
    } else {
        nester_common::fees::rate_at(&arr[..n], total_assets)
    }
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
    let elapsed_full = now.saturating_sub(last_accrual);
    // Cap the per-call accrual window. If collection has been delayed for
    // longer than the cap, the remainder is picked up on subsequent calls
    // by advancing the cursor only by the capped interval. This bounds the
    // intermediate values in the fee math and prevents a single delayed
    // collection from triggering an overflow that locks fees forever.
    let elapsed = elapsed_full.min(nester_common::fees::MAX_FEE_ACCRUAL_INTERVAL_SECONDS);

    if elapsed > 0 {
        let config = get_fee_config(env);
        let total_assets = get_total_assets(env);
        let management_fee_bps = effective_management_fee_bps(env, total_assets, &config);
        let fee = nester_common::fees::calculate_management_fee(
            total_assets,
            management_fee_bps,
            elapsed,
        )
        .unwrap_or_else(|e| panic_with_error!(env, e));

        if fee > 0 {
            let accrued = get_accrued_fees(env);
            let new_accrued = accrued
                .checked_add(fee)
                .unwrap_or_else(|| panic_with_error!(env, ContractError::ArithmeticOverflow));
            set_accrued_fees(env, new_accrued);
            sync_vault_token_total_assets(env);
        }
        let next_cursor = last_accrual.saturating_add(elapsed);
        env.storage()
            .instance()
            .set(&DataKey::LastFeeAccrual, &next_cursor);
    }
}

fn get_allocation_strategy(env: &Env) -> Address {
    env.storage()
        .instance()
        .get(&DataKey::AllocationStrategy)
        .unwrap_or_else(|| panic_with_error!(env, ContractError::NotInitialized))
}

fn get_allocated_sources(env: &Env) -> Vec<Symbol> {
    env.storage()
        .instance()
        .get(&DataKey::AllocatedSources)
        .unwrap_or(Vec::new(env))
}

fn set_allocated_sources(env: &Env, sources: &Vec<Symbol>) {
    env.storage()
        .instance()
        .set(&DataKey::AllocatedSources, sources);
}

fn get_source_allocation(env: &Env, source_id: &Symbol) -> i128 {
    env.storage()
        .persistent()
        .get(&DataKey::SourceAllocation(source_id.clone()))
        .unwrap_or(0)
}

fn set_source_allocation(env: &Env, source_id: &Symbol, amount: i128) {
    env.storage()
        .persistent()
        .set(&DataKey::SourceAllocation(source_id.clone()), &amount);
    let mut sources = get_allocated_sources(env);
    let mut found = false;
    for existing in sources.iter() {
        if existing == *source_id {
            found = true;
            break;
        }
    }
    if !found {
        sources.push_back(source_id.clone());
        set_allocated_sources(env, &sources);
    }
}

fn current_allocations_vec(env: &Env) -> Vec<CurrentAllocationView> {
    let sources = get_allocated_sources(env);
    let mut out = Vec::new(env);
    for source_id in sources.iter() {
        out.push_back(CurrentAllocationView {
            source_id: source_id.clone(),
            amount: get_source_allocation(env, &source_id),
        });
    }
    out
}

fn get_user_yield(env: &Env, user: &Address) -> i128 {
    env.storage()
        .persistent()
        .get(&DataKey::UserYield(user.clone()))
        .unwrap_or(0)
}

fn set_user_yield(env: &Env, user: &Address, amount: i128) {
    env.storage()
        .persistent()
        .set(&DataKey::UserYield(user.clone()), &amount);
}

fn get_total_reported_yield(env: &Env) -> i128 {
    env.storage()
        .instance()
        .get(&DataKey::TotalReportedYield)
        .unwrap_or(0)
}

fn set_total_reported_yield(env: &Env, amount: i128) {
    env.storage()
        .instance()
        .set(&DataKey::TotalReportedYield, &amount);
}

fn get_last_harvest_at(env: &Env, user: &Address) -> u64 {
    env.storage()
        .persistent()
        .get(&DataKey::LastHarvestAt(user.clone()))
        .unwrap_or(0)
}

fn set_last_harvest_at(env: &Env, user: &Address, ts: u64) {
    env.storage()
        .persistent()
        .set(&DataKey::LastHarvestAt(user.clone()), &ts);
}

/// Fee adjustments are gated to Admin or the narrower [`Role::FeeManager`]
/// (issue #820) — an operational key can be granted just this role instead
/// of full Admin.
fn require_admin_or_fee_manager(env: &Env, caller: &Address) {
    if !AccessControl::has_role(env, caller, Role::Admin)
        && !AccessControl::has_role(env, caller, Role::FeeManager)
    {
        panic_with_error!(env, ContractError::Unauthorized);
    }
}

fn get_referral_contract(env: &Env) -> Option<Address> {
    env.storage().instance().get(&DataKey::ReferralContract)
}

fn get_effective_deposit_time(env: &Env, user: &Address) -> u64 {
    let vault_deposit_time: u64 = env
        .storage()
        .persistent()
        .get(&DataKey::DepositTime(user.clone()))
        .unwrap_or(0);
    let vt_deposit_time: u64 = vault_token_client(env).get_deposit_time(user);
    vault_deposit_time.max(vt_deposit_time)
}

/// Fire-and-forget hook into the referral contract (issue #818), if one is
/// configured. The referral contract independently re-checks eligibility
/// (minimum deposit size and tenure) before crediting anything — the vault
/// only supplies the facts it already tracks (principal, first-deposit time)
/// and the fee slice; it never decides eligibility itself.
fn notify_referral_of_fee(env: &Env, user: &Address, performance_fee: i128, principal: i128) {
    if performance_fee <= 0 {
        return;
    }
    if let Some(referral) = get_referral_contract(env) {
        let deposit_time = get_effective_deposit_time(env, user);
        invoke_allowed::<()>(
            env,
            &referral,
            &Symbol::new(env, "accrue_reward"),
            (user.clone(), performance_fee, principal, deposit_time).into_val(env),
        );
    }
}

/// Withdrawal-velocity trip condition (issue #817). Historically this
/// function paused the whole vault the instant the rolling window crossed
/// `threshold_bps`. It now feeds the staged [`breaker`] severity machine
/// instead: a velocity breach escalates to [`Severity::Throttled`] (vault
/// stays open, limits tighten) rather than an immediate hard stop, and the
/// withdrawal that triggered it still completes — only *subsequent*
/// deposits/withdrawals are affected by the new severity. This still uses
/// the legacy [`CircuitBreakerConfig`]/`set_circuit_breaker_config` knobs so
/// existing integrations keep working; the anti-griefing margin lives in
/// [`BreakerConfig::margin_bps`].
fn check_circuit_breaker(env: &Env, amount: i128) {
    let config: CircuitBreakerConfig = env
        .storage()
        .instance()
        .get(&DataKey::CircuitBreakerConfig)
        .expect("CB config missing");
    let now = env.ledger().timestamp();
    let window_start = now.saturating_sub(config.window_seconds);
    let history: Vec<WithdrawalEntry> = env
        .storage()
        .instance()
        .get(&DataKey::WithdrawalHistory)
        .unwrap_or(Vec::new(env));

    let mut rolling_history: Vec<WithdrawalEntry> = Vec::new(env);
    let mut window_sum = amount;
    for entry in history.iter() {
        if entry.timestamp >= window_start {
            window_sum += entry.sum;
            rolling_history.push_back(entry.clone());
        }
    }

    let total_assets = get_total_assets(env);
    let threshold = nester_common::fees::mul_div(total_assets, config.threshold_bps as i128, 10000)
        .unwrap_or_else(|e| panic_with_error!(env, e));

    if threshold > 0 {
        breaker::check_withdraw_velocity(env, window_sum, threshold);
    }

    rolling_history.push_back(WithdrawalEntry {
        timestamp: now,
        sum: amount,
    });

    env.storage()
        .instance()
        .set(&DataKey::WithdrawalHistory, &rolling_history);
}

// ---------------------------------------------------------------------------
// Slippage-safe multi-hop rebalance helpers (issue #810)
// ---------------------------------------------------------------------------

fn get_max_leg_slippage_bps(env: &Env) -> u32 {
    env.storage()
        .instance()
        .get(&DataKey::MaxLegSlippageBps)
        .unwrap_or(DEFAULT_MAX_LEG_SLIPPAGE_BPS)
}

fn get_max_rebalance_value_bps(env: &Env) -> u32 {
    env.storage()
        .instance()
        .get(&DataKey::MaxRebalanceValueBps)
        .unwrap_or(DEFAULT_MAX_REBALANCE_VALUE_BPS)
}

/// Recomputes the rebalance plan the contract would produce right now:
/// fetches current allocations, asks the allocation strategy for deltas, and
/// builds a fresh, slippage-safe leg plan. Used by both `plan_rebalance`
/// (as-is, for previewing) and `execute_rebalance` (to check the submitted
/// plan hasn't gone stale).
fn compute_fresh_rebalance_plan(env: &Env) -> (Vec<RebalanceLeg>, i128, i128) {
    let total_assets = get_total_assets(env) - get_accrued_fees(env);
    let strategy = get_allocation_strategy(env);
    let current = current_allocations_vec(env);

    let mut deployed_total: i128 = 0;
    for a in current.iter() {
        deployed_total = deployed_total
            .checked_add(a.amount)
            .unwrap_or_else(|| panic_with_error!(env, ContractError::ArithmeticOverflow));
    }

    let deltas: Vec<AllocationDeltaView> = invoke_allowed(
        env,
        &strategy,
        &Symbol::new(env, "calculate_rebalance_deltas"),
        (current, deployed_total).into_val(env),
    );

    let mut delta_inputs = Vec::new(env);
    for d in deltas.iter() {
        delta_inputs.push_back(rebalance::DeltaInput {
            source_id: d.source_id.clone(),
            delta: d.delta,
        });
    }

    let slippage_bps = env
        .storage()
        .instance()
        .get(&DataKey::RebalanceSlippageBps)
        .unwrap_or(DEFAULT_REBALANCE_SLIPPAGE_BPS);
    let effective_slippage = slippage_bps.min(get_max_leg_slippage_bps(env));

    let (plan, total_moved) = rebalance::build_plan(env, &delta_inputs, |gross| {
        rebalance_min_assets_out(env, gross, effective_slippage)
    });

    (plan, total_moved, total_assets)
}

// ---------------------------------------------------------------------------
// Early-exit penalty escrow helpers (issue #805)
// ---------------------------------------------------------------------------

fn get_penalty_escrow(env: &Env) -> i128 {
    env.storage()
        .instance()
        .get(&DataKey::PenaltyEscrow)
        .unwrap_or(0)
}

fn set_penalty_escrow(env: &Env, amount: i128) {
    env.storage()
        .instance()
        .set(&DataKey::PenaltyEscrow, &amount);
}

fn get_depositor_share_bps(env: &Env) -> u32 {
    env.storage()
        .instance()
        .get(&DataKey::DepositorShareBps)
        .unwrap_or(DEFAULT_DEPOSITOR_SHARE_BPS)
}

fn get_min_penalty_distribution_amount(env: &Env) -> i128 {
    env.storage()
        .instance()
        .get(&DataKey::MinPenaltyDistributionAmount)
        .unwrap_or(DEFAULT_MIN_PENALTY_DISTRIBUTION_AMOUNT)
}

fn get_penalty_distribution_cooldown(env: &Env) -> u64 {
    env.storage()
        .instance()
        .get(&DataKey::PenaltyDistributionCooldown)
        .unwrap_or(DEFAULT_PENALTY_DISTRIBUTION_COOLDOWN)
}

/// Route a penalty into the escrow and emit `penalty_charged`. The amount is
/// value that stays inside the vault's real token balance without being
/// reflected in `TotalAssets` (mirroring how the fee is already excluded
/// from the amount transferred back to the exiting user) — `distribute_penalties`
/// later either folds the depositor slice back into `TotalAssets` (raising
/// share price) or transfers the treasury slice out.
fn charge_penalty(
    env: &Env,
    user: &Address,
    amount: i128,
    reason: PenaltyReason,
    shares_burned: i128,
) {
    if amount <= 0 {
        return;
    }
    let escrow = get_penalty_escrow(env)
        .checked_add(amount)
        .unwrap_or_else(|| panic_with_error!(env, ContractError::ArithmeticOverflow));
    set_penalty_escrow(env, escrow);

    emit_event(
        env,
        VAULT,
        PNLTY_CHG,
        user.clone(),
        PenaltyChargedEventData {
            user: user.clone(),
            amount,
            shares_burned,
            reason,
        },
    );
}

// ---------------------------------------------------------------------------
// Contract
// ---------------------------------------------------------------------------

#[contract]
pub struct VaultContract;

#[contractimpl]
impl VaultContract {
    /// Initialise the vault, setting `admin` as the sole Admin.
    ///
    /// # Token immutability
    /// `token_address` and `vault_token_address` are written once here and
    /// never updated again.  No admin function exists to change either address
    /// after initialization.  This guarantees that withdrawals always redeem
    /// the same token that was deposited, preventing an admin key compromise
    /// from swapping the token to steal deposited funds.  Any future need to
    /// migrate tokens must go through a governance-approved upgrade with a
    /// timelock so depositors can exit before the change takes effect.
    pub fn initialize(
        env: Env,
        admin: Address,
        token_address: Address,
        vault_token_address: Address,
        treasury: Address,
    ) {
        // AccessControl::initialize handles AlreadyInitialized guard and require_auth
        AccessControl::initialize(&env, &admin);
        nester_common::Upgrade::init_schema_version(&env, 1);
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
            treasury_address: treasury.clone(),
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
        let history: Vec<WithdrawalEntry> = Vec::new(&env);
        env.storage()
            .instance()
            .set(&DataKey::WithdrawalHistory, &history);

        bootstrap_callee_allowlist(&env, &token_address, &vault_token_address, &treasury);
    }

    pub fn set_max_deposit(env: Env, caller: Address, amount: i128) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);
        if amount <= 0 {
            panic_with_error!(&env, ContractError::ConfigOutOfRange);
        }
        env.storage().instance().set(&DataKey::MaxDeposit, &amount);
    }

    pub fn set_min_deposit(env: Env, caller: Address, amount: i128) {
        require_initialized(&env);
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);
        if amount < 0 {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }
        env.storage().instance().set(&DataKey::MinDeposit, &amount);
    }

    pub fn get_min_deposit(env: Env) -> i128 {
        env.storage()
            .instance()
            .get(&DataKey::MinDeposit)
            .unwrap_or(0)
    }

    pub fn set_rebalance_threshold(env: Env, caller: Address, bps: u32) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);
        if !(100..=5000).contains(&bps) {
            panic_with_error!(&env, ContractError::ConfigOutOfRange);
        }
        env.storage()
            .instance()
            .set(&DataKey::RebalanceThreshold, &bps);
    }

    /// Configure this vault's rebalance slippage tolerance, in basis points
    /// (default 50 = 0.5%). Admin/owner only. Capped at
    /// [`MAX_REBALANCE_SLIPPAGE_BPS`]. Issue #638.
    pub fn set_rebalance_slippage(env: Env, caller: Address, bps: u32) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);
        if bps > MAX_REBALANCE_SLIPPAGE_BPS {
            panic_with_error!(&env, ContractError::ConfigOutOfRange);
        }
        env.storage()
            .instance()
            .set(&DataKey::RebalanceSlippageBps, &bps);
    }

    /// Current rebalance slippage tolerance in bps (defaults to 50).
    pub fn get_rebalance_slippage(env: Env) -> u32 {
        env.storage()
            .instance()
            .get(&DataKey::RebalanceSlippageBps)
            .unwrap_or(DEFAULT_REBALANCE_SLIPPAGE_BPS)
    }

    pub fn set_circuit_breaker_config(env: Env, caller: Address, config: CircuitBreakerConfig) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);
        if config.window_seconds == 0 || config.threshold_bps < 1000 || config.threshold_bps > 10000
        {
            panic_with_error!(&env, ContractError::ConfigOutOfRange);
        }
        env.storage()
            .instance()
            .set(&DataKey::CircuitBreakerConfig, &config);
    }

    pub fn set_early_withdrawal_fee(env: Env, caller: Address, bps: u32) {
        caller.require_auth();
        require_admin_or_fee_manager(&env, &caller);
        if bps > nester_common::MAX_EARLY_WITHDRAWAL_FEE_BPS {
            panic_with_error!(&env, ContractError::FeeTooHigh);
        }
        let mut config = get_fee_config(&env);
        let old_config = config.clone();
        config.early_withdrawal_fee_bps = bps;
        env.storage().instance().set(&DataKey::FeeConfig, &config);
        emit_event(
            &env,
            VAULT,
            FEE_CONFIG_UPDATED,
            caller.clone(),
            FeeConfigUpdatedEventData {
                old_config,
                new_config: config,
            },
        );
    }

    pub fn set_fee_config(env: Env, caller: Address, config: FeeConfig) {
        caller.require_auth();
        require_admin_or_fee_manager(&env, &caller);
        if config.management_fee_bps > nester_common::MAX_MANAGEMENT_FEE_BPS
            || config.performance_fee_bps > nester_common::MAX_PERFORMANCE_FEE_BPS
            || config.early_withdrawal_fee_bps > nester_common::MAX_EARLY_WITHDRAWAL_FEE_BPS
        {
            panic_with_error!(&env, ContractError::FeeTooHigh);
        }
        let old_config = get_fee_config(&env);
        env.storage().instance().set(&DataKey::FeeConfig, &config);
        emit_event(
            &env,
            VAULT,
            FEE_CONFIG_UPDATED,
            caller.clone(),
            FeeConfigUpdatedEventData {
                old_config,
                new_config: config,
            },
        );
    }

    /// Admin: replace one of the three tiered fee schedules (issue #813).
    /// Validated for: bounded tier count, strictly ascending thresholds,
    /// every rate at or below the relevant compile-time ceiling, and — the
    /// grandfathering rule — no point on the new curve exceeding the old
    /// curve by more than `fees::MAX_SCHEDULE_RATE_INCREASE_BPS`, so an
    /// admin cannot retroactively spike an existing depositor's rate.
    pub fn set_fee_tiers(env: Env, caller: Address, kind: FeeTierKind, tiers: Vec<VaultFeeTier>) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);

        let ceiling = match kind {
            FeeTierKind::Performance => nester_common::MAX_PERFORMANCE_FEE_BPS,
            FeeTierKind::Exit => nester_common::MAX_EARLY_WITHDRAWAL_FEE_BPS,
            FeeTierKind::Management => nester_common::MAX_MANAGEMENT_FEE_BPS,
        };

        let (new_arr, new_n) = to_fee_tier_array(&tiers);
        nester_common::fees::validate_tiers(&new_arr[..new_n], ceiling)
            .unwrap_or_else(|e| panic_with_error!(&env, e));

        let old_tiers = match kind {
            FeeTierKind::Performance => get_performance_tiers(&env),
            FeeTierKind::Exit => get_exit_tiers(&env),
            FeeTierKind::Management => get_management_tiers(&env),
        };
        let (old_arr, old_n) = to_fee_tier_array(&old_tiers);
        nester_common::fees::validate_no_adverse_increase(
            &old_arr[..old_n],
            &new_arr[..new_n],
            nester_common::fees::MAX_SCHEDULE_RATE_INCREASE_BPS,
        )
        .unwrap_or_else(|e| panic_with_error!(&env, e));

        match kind {
            FeeTierKind::Performance => set_performance_tiers(&env, &tiers),
            FeeTierKind::Exit => set_exit_tiers(&env, &tiers),
            FeeTierKind::Management => set_management_tiers(&env, &tiers),
        }
    }

    pub fn get_fee_tiers(env: Env, kind: FeeTierKind) -> Vec<VaultFeeTier> {
        match kind {
            FeeTierKind::Performance => get_performance_tiers(&env),
            FeeTierKind::Exit => get_exit_tiers(&env),
            FeeTierKind::Management => get_management_tiers(&env),
        }
    }

    /// A user's current tenure, the exit/performance rates that apply to
    /// them right now, and the next tier boundary — lets the frontend show
    /// "wait N more days and your exit fee drops to X%".
    pub fn fee_schedule_preview(env: Env, user: Address) -> FeeSchedulePreview {
        require_initialized(&env);
        let config = get_fee_config(&env);
        let tenure = tenure_secs(&env, &user);

        let perf_tiers = get_performance_tiers(&env);
        let (parr, pn) = to_fee_tier_array(&perf_tiers);
        let exit_tiers = get_exit_tiers(&env);
        let (earr, en) = to_fee_tier_array(&exit_tiers);

        let current_performance_fee_bps = if pn == 0 {
            config.performance_fee_bps
        } else {
            nester_common::fees::rate_at(&parr[..pn], tenure as i128)
        };
        let current_exit_fee_bps = if en == 0 {
            config.early_withdrawal_fee_bps
        } else {
            nester_common::fees::rate_at(&earr[..en], tenure as i128)
        };

        let perf_next = if pn == 0 {
            None
        } else {
            nester_common::fees::next_boundary(&parr[..pn], tenure as i128)
        };
        let exit_next = if en == 0 {
            None
        } else {
            nester_common::fees::next_boundary(&earr[..en], tenure as i128)
        };

        let next_boundary_secs = match (perf_next, exit_next) {
            (Some(a), Some(b)) => Some(a.threshold.min(b.threshold) as u64),
            (Some(a), None) => Some(a.threshold as u64),
            (None, Some(b)) => Some(b.threshold as u64),
            (None, None) => None,
        };

        FeeSchedulePreview {
            tenure_secs: tenure,
            current_exit_fee_bps,
            current_performance_fee_bps,
            next_boundary_secs,
            next_boundary_exit_fee_bps: exit_next
                .map(|t| t.rate_bps)
                .unwrap_or(current_exit_fee_bps),
            next_boundary_perf_fee_bps: perf_next
                .map(|t| t.rate_bps)
                .unwrap_or(current_performance_fee_bps),
        }
    }

    pub fn set_emergency_fee(env: Env, admin: Address, fee_bps: u32) -> Result<(), ContractError> {
        admin.require_auth();
        require_admin_or_fee_manager(&env, &admin);
        if fee_bps > nester_common::MAX_EMERGENCY_FEE_BPS {
            panic_with_error!(&env, ContractError::FeeTooHigh);
        }
        env.storage()
            .instance()
            .set(&DataKey::EmergencyFeeBps, &fee_bps);
        Ok(())
    }

    /// Bind this vault to an AllocationStrategy contract whose targets drive
    /// rebalancing. Must be called by Admin before `rebalance` will succeed.
    pub fn set_allocation_strategy(env: Env, caller: Address, strategy: Address) {
        require_initialized(&env);
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);
        env.storage()
            .instance()
            .set(&DataKey::AllocationStrategy, &strategy);
    }

    pub fn register_callee(env: Env, caller: Address, callee: Address) {
        require_initialized(&env);
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);
        CalleeAllowlist::register(&env, &callee);
    }

    pub fn unregister_callee(env: Env, caller: Address, callee: Address) {
        require_initialized(&env);
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);
        CalleeAllowlist::unregister(&env, &callee);
    }

    pub fn get_allocation_strategy(env: Env) -> Address {
        get_allocation_strategy(&env)
    }

    pub fn set_rebalance_cooldown(env: Env, caller: Address, seconds: u64) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);
        env.storage()
            .instance()
            .set(&DataKey::RebalanceCooldown, &seconds);
    }

    pub fn get_rebalance_cooldown(env: Env) -> u64 {
        env.storage()
            .instance()
            .get(&DataKey::RebalanceCooldown)
            .unwrap_or(DEFAULT_REBALANCE_COOLDOWN)
    }

    pub fn last_rebalance_at(env: Env) -> u64 {
        env.storage()
            .instance()
            .get(&DataKey::LastRebalanceAt)
            .unwrap_or(0)
    }

    // -----------------------------------------------------------------------
    // Admin operations
    // -----------------------------------------------------------------------

    /// Pause all vault operations. Requires [`Role::Admin`] or
    /// [`Role::Guardian`] — the Guardian asymmetry (issue #820): a Guardian
    /// can always make the vault safer, so pausing is open to it, but
    /// [`Self::unpause`] deliberately is not.
    pub fn pause(env: Env, caller: Address) {
        require_initialized(&env);
        caller.require_auth();
        if !AccessControl::has_role(&env, &caller, Role::Admin)
            && !AccessControl::has_role(&env, &caller, Role::Guardian)
        {
            panic_with_error!(&env, ContractError::Unauthorized);
        }
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
    // Circuit breaker (issue #817)
    // -----------------------------------------------------------------------

    /// Full trip status: current severity, the last firing condition and its
    /// observed value/threshold, and the earliest timestamp the next
    /// recovery step is permitted.
    pub fn get_breaker_status(env: Env) -> BreakerStatus {
        breaker::status(&env)
    }

    /// Configure the automatic trip conditions. Admin only — a Guardian must
    /// never be able to loosen the very thresholds that constrain it.
    pub fn set_breaker_config(env: Env, caller: Address, config: BreakerConfig) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);
        breaker::set_config(&env, &config);
    }

    pub fn get_breaker_config_v2(env: Env) -> BreakerConfig {
        breaker::get_config(&env)
    }

    /// Guardian (or Admin) manual escalation: block new deposits while
    /// leaving withdrawals open. This can only make the vault safer.
    pub fn guardian_halt_deposits(env: Env, caller: Address) {
        require_initialized(&env);
        caller.require_auth();
        if !AccessControl::has_role(&env, &caller, Role::Admin)
            && !AccessControl::has_role(&env, &caller, Role::Guardian)
        {
            panic_with_error!(&env, ContractError::Unauthorized);
        }
        breaker::guardian_escalate(&env, Severity::DepositsHalted);
    }

    /// Guardian (or Admin) manual escalation to `FullHalt`: blocks
    /// everything except the emergency withdrawal queue. Still only ever
    /// makes the vault safer — reversing it requires [`Self::recover_next_stage`],
    /// which Guardian cannot call.
    pub fn guardian_trip_breaker(env: Env, caller: Address) {
        require_initialized(&env);
        caller.require_auth();
        if !AccessControl::has_role(&env, &caller, Role::Admin)
            && !AccessControl::has_role(&env, &caller, Role::Guardian)
        {
            panic_with_error!(&env, ContractError::Unauthorized);
        }
        breaker::guardian_escalate(&env, Severity::FullHalt);
    }

    /// Move severity exactly one stage down (never skips a stage), enforcing
    /// the recovery cooldown. Requires Admin or Upgrader — deliberately
    /// excludes Guardian, so reversing a Guardian action always requires a
    /// higher role.
    pub fn recover_next_stage(env: Env, caller: Address) -> Severity {
        require_initialized(&env);
        caller.require_auth();
        if !AccessControl::has_role(&env, &caller, Role::Admin)
            && !AccessControl::has_role(&env, &caller, Role::Upgrader)
        {
            panic_with_error!(&env, ContractError::Unauthorized);
        }
        breaker::recover_next_stage(&env, &caller)
    }

    /// Record a yield-source adapter failure against the source-failure trip
    /// condition. Callable by Admin, Operator, or RebalanceKeeper — whichever
    /// integration observes the adapter fault.
    pub fn record_source_failure(env: Env, caller: Address) {
        caller.require_auth();
        if !AccessControl::has_role(&env, &caller, Role::Admin)
            && !AccessControl::has_role(&env, &caller, Role::Operator)
            && !AccessControl::has_role(&env, &caller, Role::RebalanceKeeper)
        {
            panic_with_error!(&env, ContractError::Unauthorized);
        }
        breaker::note_source_failure(&env);
    }

    pub fn reset_source_failures(env: Env, caller: Address) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);
        breaker::reset_source_failures(&env);
    }

    // -----------------------------------------------------------------------
    // Referral integration (issue #818)
    // -----------------------------------------------------------------------

    /// Bind a referral contract that receives an `accrue_reward` call after
    /// every harvest that collects a performance fee. Admin only. Also
    /// registers the referral contract in the callee allowlist so the
    /// cross-contract call is permitted.
    pub fn set_referral_contract(env: Env, caller: Address, referral: Address) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);
        CalleeAllowlist::register(&env, &referral);
        env.storage()
            .instance()
            .set(&DataKey::ReferralContract, &referral);
    }

    pub fn get_referral_contract_address(env: Env) -> Option<Address> {
        get_referral_contract(&env)
    }

    // -----------------------------------------------------------------------
    // Core vault operations
    // -----------------------------------------------------------------------

    pub fn report_yield(env: Env, caller: Address, amount: i128) {
        caller.require_auth();
        if !AccessControl::has_role(&env, &caller, Role::Manager)
            && !AccessControl::has_role(&env, &caller, Role::Attester)
        {
            panic_with_error!(&env, ContractError::Unauthorized);
        }

        let total_assets = get_total_assets(&env);

        // Yield-sanity trip (#817): an implausible single report is not
        // applied at all — the transaction still succeeds (so a panic can't
        // undo the escalation it triggers), but total_assets/user_yield are
        // left untouched and the breaker escalates.
        if breaker::check_yield_sanity(&env, amount, total_assets) {
            return;
        }

        let new_total = total_assets
            .checked_add(amount)
            .unwrap_or_else(|| panic_with_error!(&env, ContractError::ArithmeticOverflow));
        set_total_assets(&env, new_total);
        sync_vault_token_total_assets(&env);

        // Track per-caller pending yield and aggregate reported yield for harvest.
        // Only accumulate positive yield; losses (negative amount) reduce
        // pending yield down to zero and reduce the aggregate counter.
        if amount > 0 {
            let user_yield = get_user_yield(&env, &caller);
            let new_user_yield = user_yield
                .checked_add(amount)
                .unwrap_or_else(|| panic_with_error!(&env, ContractError::ArithmeticOverflow));
            set_user_yield(&env, &caller, new_user_yield);

            let total_reported = get_total_reported_yield(&env);
            let new_total_reported = total_reported
                .checked_add(amount)
                .unwrap_or_else(|| panic_with_error!(&env, ContractError::ArithmeticOverflow));
            set_total_reported_yield(&env, new_total_reported);
        } else if amount < 0 {
            // For impairments, reduce the user's pending yield (floor at zero).
            let user_yield = get_user_yield(&env, &caller);
            let reduced = user_yield.saturating_add(amount).max(0);
            set_user_yield(&env, &caller, reduced);

            let total_reported = get_total_reported_yield(&env);
            let reduced_total = total_reported.saturating_add(amount).max(0);
            set_total_reported_yield(&env, reduced_total);
        }
    }

    /// Claim the pending yield accumulated for `user` by previous `report_yield`
    /// calls (where the caller was `user`).
    ///
    /// Steps (issue #518):
    ///  1. Calculate accrued yield since last harvest.
    ///  2. Deduct performance fee — only on net positive yield, never on impairment.
    ///  3. Send the fee portion to the treasury contract.
    ///  4. Compound the net yield: mint new vault-token shares at the current price
    ///     and credit them to `user`, then increase TotalAssets accordingly.
    ///  5. Update `LastHarvestAt` timestamp for `user`.
    ///
    /// Returns a zero-filled `HarvestResult` with `compounded: false` when the
    /// user has no pending yield, so callers can always call this safely.
    pub fn harvest(env: Env, user: Address) -> HarvestResult {
        with_reentrancy_guard(env, |env| Self::harvest_internal(env, user))
    }

    fn harvest_internal(env: Env, user: Address) -> HarvestResult {
        require_initialized(&env);
        require_active(&env);
        breaker::require_not_full_halt(&env);
        user.require_auth();

        let shares = get_shares(&env, &user);
        let redeemable = vault_token_client(&env).amount_for_shares(&shares);
        let principal = get_user_principal(&env, &user);
        let gross_yield = redeemable.saturating_sub(principal);
        let now = env.ledger().timestamp();

        if gross_yield <= 0 {
            set_last_harvest_at(&env, &user, now);
            let new_share_balance = get_shares(&env, &user);
            return HarvestResult {
                gross_yield,
                performance_fee: 0,
                net_yield: 0,
                compounded: false,
                new_share_balance,
                user,
            };
        }

        let config = get_fee_config(&env);
        // Rate is taken at the user's tenure *now*, at harvest time, and
        // immediately netted out of the compounded shares below — this is
        // the "incremental, already-netted" choice from issue #813: a
        // schedule change can only affect yield the user hasn't harvested
        // yet, never yield already compounded into their principal under an
        // earlier rate. Users who want to lock in today's rate before a
        // tier boundary can simply harvest first.
        let performance_fee_bps = effective_performance_fee_bps(&env, &user, &config);
        let performance_fee =
            nester_common::fees::calculate_performance_fee(gross_yield, performance_fee_bps)
                .unwrap_or_else(|e| panic_with_error!(&env, e));

        let net_yield = gross_yield
            .checked_sub(performance_fee)
            .unwrap_or_else(|| panic_with_error!(&env, ContractError::ArithmeticOverflow));

        // Transfer performance fee to treasury.
        if performance_fee > 0 {
            let token_address = self::VaultContract::get_token(env.clone());
            transfer_tokens(
                &env,
                &token_address,
                &env.current_contract_address(),
                &config.treasury_address,
                &performance_fee,
            );
            invoke_allowed::<()>(
                &env,
                &config.treasury_address,
                &Symbol::new(&env, "receive_fees"),
                (performance_fee,).into_val(&env),
            );
            // Reduce TotalAssets by the fee sent out.
            let total_assets = get_total_assets(&env);
            let post_fee_assets = total_assets
                .checked_sub(performance_fee)
                .unwrap_or_else(|| panic_with_error!(&env, ContractError::ArithmeticOverflow));
            set_total_assets(&env, post_fee_assets);
            sync_vault_token_total_assets(&env);

            // Referral hook (#818): the referrer's reward is carved out of
            // the protocol's performance-fee slice, never out of the
            // referred user's own yield — `net_yield`/the user's redemption
            // is computed above and is unaffected by this call. Silently a
            // no-op when no referral contract is configured.
            notify_referral_of_fee(&env, &user, performance_fee, principal);
        }

        // Compound net yield: mint new shares for the user at the current price.
        // The gross yield was already added to TotalAssets by report_yield, so
        // only the fee reduction above affects TotalAssets here.
        let new_shares = if net_yield > 0 {
            let s = vault_token_client(&env).mint_for_deposit(&user, &net_yield);
            // mint_for_deposit increments vault token's total_assets by net_yield, but
            // that amount was already tracked by report_yield — sync back to the correct value.
            sync_vault_token_total_assets(&env);
            s
        } else {
            0
        };
        let _ = new_shares; // shares minted internally; user balance updated by vault token

        // Reset per-user pending yield to zero and record harvest timestamp.
        set_user_yield(&env, &user, 0);
        set_last_harvest_at(&env, &user, now);

        // Add compounded yield to principal to prevent double-charging on future harvests
        if net_yield > 0 {
            set_user_principal(&env, &user, principal + net_yield);
        }

        let new_share_balance = get_shares(&env, &user);

        let result = HarvestResult {
            gross_yield,
            performance_fee,
            net_yield,
            compounded: net_yield > 0,
            new_share_balance,
            user: user.clone(),
        };

        emit_event(&env, VAULT, HARVEST, user, result.clone());

        result
    }

    /// Admin-level vault-wide harvest: reads the aggregate yield reported since
    /// the last vault harvest, extracts the performance fee portion, transfers
    /// it to the treasury, and resets the `TotalReportedYield` counter to zero.
    /// Suitable for periodic treasury collection without enumerating individual
    /// user positions on-chain (Soroban does not support unbounded iteration).
    pub fn harvest_vault(env: Env, admin: Address) -> VaultHarvestResult {
        with_reentrancy_guard(env, |env| Self::harvest_vault_internal(env, admin))
    }

    fn harvest_vault_internal(env: Env, admin: Address) -> VaultHarvestResult {
        require_initialized(&env);
        require_active(&env);
        admin.require_auth();
        AccessControl::require_role(&env, &admin, Role::Admin);

        let total_gross_yield = get_total_reported_yield(&env);

        if total_gross_yield == 0 {
            return VaultHarvestResult {
                total_gross_yield: 0,
                total_fee_collected: 0,
                total_net_yield: 0,
                positions_harvested: 0,
            };
        }

        let config = get_fee_config(&env);
        let total_fee_collected = nester_common::fees::calculate_performance_fee(
            total_gross_yield,
            config.performance_fee_bps,
        )
        .unwrap_or_else(|e| panic_with_error!(&env, e));

        let total_net_yield = total_gross_yield
            .checked_sub(total_fee_collected)
            .unwrap_or_else(|| panic_with_error!(&env, ContractError::ArithmeticOverflow));

        // Transfer performance fee to treasury.
        if total_fee_collected > 0 {
            let token_address = self::VaultContract::get_token(env.clone());
            transfer_tokens(
                &env,
                &token_address,
                &env.current_contract_address(),
                &config.treasury_address,
                &total_fee_collected,
            );
            invoke_allowed::<()>(
                &env,
                &config.treasury_address,
                &Symbol::new(&env, "receive_fees"),
                (total_fee_collected,).into_val(&env),
            );
            // Reduce TotalAssets by the fee sent to treasury.
            let total_assets = get_total_assets(&env);
            let post_fee_assets = total_assets
                .checked_sub(total_fee_collected)
                .unwrap_or_else(|| panic_with_error!(&env, ContractError::ArithmeticOverflow));
            set_total_assets(&env, post_fee_assets);
            sync_vault_token_total_assets(&env);
        }

        // Reset aggregate yield counter; per-user UserYield entries are left
        // in place — they are swept individually by each user's own harvest() call.
        set_total_reported_yield(&env, 0);

        // positions_harvested reflects the aggregate sweep (one vault-wide sweep).
        let result = VaultHarvestResult {
            total_gross_yield,
            total_fee_collected,
            total_net_yield,
            positions_harvested: 1,
        };

        emit_event(&env, VAULT, HARVEST_VLT, admin, result.clone());

        result
    }

    /// Read-only check: does the live allocation drift exceed the strategy's
    /// `rebalance_threshold_bps`? Returns false when no strategy is set or the
    /// vault has no assets yet.
    pub fn check_rebalance_needed(env: Env) -> bool {
        if !env.storage().instance().has(&DataKey::AllocationStrategy) {
            return false;
        }

        let total_assets = get_total_assets(&env) - get_accrued_fees(&env);
        if total_assets <= 0 {
            return false;
        }

        let strategy = get_allocation_strategy(&env);
        let allocations = current_allocations_vec(&env);

        let in_spec: bool = invoke_allowed(
            &env,
            &strategy,
            &Symbol::new(&env, "validate_allocations"),
            (allocations, total_assets).into_val(&env),
        );

        !in_spec
    }

    /// Per-source amounts currently deployed across yield sources.
    pub fn get_current_allocations(env: Env) -> Vec<CurrentAllocationView> {
        require_initialized(&env);
        current_allocations_vec(&env)
    }

    /// Move funds between yield sources to match strategy targets.
    ///
    /// Bookkeeping-only in this contract: actual on-chain transfers to
    /// yield-source adapters are appended once those adapters land. The
    /// rebalance is atomic — either every delta applies or the call panics.
    pub fn rebalance(env: Env, caller: Address) -> Vec<AllocationDeltaView> {
        with_reentrancy_guard(env, |env| Self::rebalance_internal(env, caller))
    }

    fn rebalance_internal(env: Env, caller: Address) -> Vec<AllocationDeltaView> {
        require_initialized(&env);
        require_active(&env);
        breaker::require_not_full_halt(&env);
        caller.require_auth();
        if !AccessControl::has_role(&env, &caller, Role::Admin)
            && !AccessControl::has_role(&env, &caller, Role::Operator)
            && !AccessControl::has_role(&env, &caller, Role::RebalanceKeeper)
        {
            panic_with_error!(&env, ContractError::Unauthorized);
        }

        let now = env.ledger().timestamp();
        let cooldown: u64 = env
            .storage()
            .instance()
            .get(&DataKey::RebalanceCooldown)
            .unwrap_or(DEFAULT_REBALANCE_COOLDOWN);
        let last: u64 = env
            .storage()
            .instance()
            .get(&DataKey::LastRebalanceAt)
            .unwrap_or(0);
        if last != 0 && now < last + cooldown {
            panic_with_error!(&env, ContractError::InvalidOperation);
        }

        accrue_management_fee(&env);

        let total_assets = get_total_assets(&env) - get_accrued_fees(&env);
        if total_assets <= 0 {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }

        let strategy = get_allocation_strategy(&env);
        let current = current_allocations_vec(&env);

        // Rebalance only redistributes capital already deployed to sources.
        // Passing the deployed sum ensures delta conservation (sum == 0) in
        // the allocation strategy; undeployed vault buffer is not touched.
        let mut deployed_total: i128 = 0;
        for a in current.iter() {
            deployed_total = deployed_total
                .checked_add(a.amount)
                .unwrap_or_else(|| panic_with_error!(&env, ContractError::ArithmeticOverflow));
        }
        if deployed_total <= 0 {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }

        // Fetch deltas from the allocation strategy.
        let deltas: Vec<AllocationDeltaView> = invoke_allowed(
            &env,
            &strategy,
            &Symbol::new(&env, "calculate_rebalance_deltas"),
            (current, deployed_total).into_val(&env),
        );

        // Per-vault slippage tolerance applied to each withdrawal step (#638).
        let slippage_bps = Self::get_rebalance_slippage(env.clone());

        // Apply each delta to source-allocation bookkeeping. Min-rebalance
        // skip is per-source so we don't pay tx fees for dust adjustments.
        let mut applied = Vec::new(&env);
        let mut total_delta: i128 = 0;
        for d in deltas.iter() {
            if d.delta.abs() < MIN_REBALANCE_AMOUNT {
                continue;
            }

            let current_amount = get_source_allocation(&env, &d.source_id);
            let new_amount = current_amount
                .checked_add(d.delta)
                .unwrap_or_else(|| panic_with_error!(&env, ContractError::ArithmeticOverflow));

            if new_amount < 0 {
                // Indicates the target wants to withdraw more than the source holds —
                // refuse the entire rebalance (atomicity).
                panic_with_error!(&env, ContractError::AllocationError);
            }

            // #638: slippage guard on each withdrawal step (negative delta).
            // min_assets_out is the net-of-fees withdrawal preview reduced by
            // the configured tolerance; the realised proceeds must not fall
            // below it or the rebalance reverts with SlippageExceeded. In the
            // current accounting model the full gross is moved internally
            // (actual == gross); once rebalance performs real LP/protocol
            // withdrawals, pass the call's returned amount as `actual_received`.
            if d.delta < 0 {
                let gross = -d.delta;
                let min_assets_out = rebalance_min_assets_out(&env, gross, slippage_bps);
                let actual_received = gross;
                enforce_rebalance_slippage(&env, min_assets_out, actual_received);
            }

            set_source_allocation(&env, &d.source_id, new_amount);
            total_delta += d.delta;
            applied.push_back(d);
        }

        if total_delta < 0 {
            let current_reserves = get_vault_liquid_reserves(&env);
            set_vault_liquid_reserves(&env, current_reserves - total_delta);
        }

        env.storage()
            .instance()
            .set(&DataKey::LastRebalanceAt, &now);

        emit_event(
            &env,
            VAULT,
            REBALANCE,
            caller,
            RebalancedEventData {
                source_deltas: applied.clone(),
                timestamp: now,
            },
        );

        applied
    }

    // -----------------------------------------------------------------------
    // Slippage-safe multi-hop rebalance: plan/execute split (issue #810)
    // -----------------------------------------------------------------------

    /// Pure read: the rebalance plan the contract would execute right now,
    /// with every leg's `min_out` already computed, plus the exact
    /// `plan_hash` `execute_rebalance` expects for it. Callable by anyone —
    /// a caller previews with this, then passes the returned `legs` and
    /// `plan_hash` straight through to `execute_rebalance` unmodified.
    pub fn plan_rebalance(env: Env) -> RebalancePlan {
        require_initialized(&env);
        let (plan, _total_moved, _total_assets) = compute_fresh_rebalance_plan(&env);
        let plan_hash = rebalance::checksum(&plan);
        RebalancePlan {
            legs: plan,
            plan_hash,
        }
    }

    /// Executes a previously planned rebalance. `plan_hash` must equal the
    /// checksum of `legs` (integrity — the caller can't submit legs that
    /// don't match what they committed to), and `legs` must match a
    /// freshly recomputed plan within [`rebalance::PLAN_STALENESS_TOLERANCE_BPS`]
    /// (freshness — rejects a plan built against prices that have since
    /// moved too far, reverting with `PlanStale`). Every leg's `min_out` is
    /// enforced individually: one under-delivering leg reverts the entire
    /// call, so a manipulated leg can never hide inside an aggregate
    /// tolerance. The total value moved is capped at
    /// `max_rebalance_value_bps` of vault assets.
    pub fn execute_rebalance(
        env: Env,
        caller: Address,
        plan_hash: u64,
        legs: Vec<RebalanceLeg>,
    ) -> Vec<RebalanceLeg> {
        with_reentrancy_guard(env, |env| {
            Self::execute_rebalance_internal(env, caller, plan_hash, legs)
        })
    }

    fn execute_rebalance_internal(
        env: Env,
        caller: Address,
        plan_hash: u64,
        legs: Vec<RebalanceLeg>,
    ) -> Vec<RebalanceLeg> {
        require_initialized(&env);
        require_active(&env);
        caller.require_auth();
        if !AccessControl::has_role(&env, &caller, Role::Admin)
            && !AccessControl::has_role(&env, &caller, Role::Operator)
        {
            panic_with_error!(&env, ContractError::Unauthorized);
        }

        let now = env.ledger().timestamp();
        let cooldown: u64 = env
            .storage()
            .instance()
            .get(&DataKey::RebalanceCooldown)
            .unwrap_or(DEFAULT_REBALANCE_COOLDOWN);
        let last: u64 = env
            .storage()
            .instance()
            .get(&DataKey::LastRebalanceAt)
            .unwrap_or(0);
        if last != 0 && now < last + cooldown {
            panic_with_error!(&env, ContractError::InvalidOperation);
        }

        accrue_management_fee(&env);

        if rebalance::checksum(&legs) != plan_hash {
            panic_with_error!(&env, ContractError::PlanStale);
        }

        let (fresh_plan, _fresh_total_moved, total_assets) = compute_fresh_rebalance_plan(&env);
        if total_assets <= 0 {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }
        if !rebalance::plan_matches_fresh(
            &fresh_plan,
            &legs,
            rebalance::PLAN_STALENESS_TOLERANCE_BPS,
        ) {
            panic_with_error!(&env, ContractError::PlanStale);
        }

        let max_value_bps = get_max_rebalance_value_bps(&env);
        let value_cap = nester_common::fees::mul_div(total_assets, max_value_bps as i128, 10_000)
            .unwrap_or(total_assets);
        let mut total_moved: i128 = 0;
        let mut net_delta: i128 = 0;
        for leg in fresh_plan.iter() {
            total_moved = total_moved.saturating_add(leg.delta.abs());
            net_delta += leg.delta;
        }
        if total_moved > value_cap {
            panic_with_error!(&env, ContractError::RebalanceValueCapExceeded);
        }

        // Execute against the freshly recomputed plan, not the
        // caller-submitted `legs` — `legs`/`plan_hash` only establish that
        // the caller's view was within tolerance of live state (checked
        // above). Enforcing `min_out` from the caller's own input would let
        // a malicious caller submit deltas that pass the freshness check
        // but a hollowed-out `min_out`, defeating the whole point of
        // per-leg slippage protection.
        let mut applied = Vec::new(&env);
        for leg in fresh_plan.iter() {
            let current_amount = get_source_allocation(&env, &leg.source_id);
            let new_amount = current_amount
                .checked_add(leg.delta)
                .unwrap_or_else(|| panic_with_error!(&env, ContractError::ArithmeticOverflow));
            if new_amount < 0 {
                panic_with_error!(&env, ContractError::AllocationError);
            }

            // Bookkeeping-only today: the full amount is moved internally
            // (actual == gross). Once rebalance performs real yield-source
            // withdrawals, pass the call's returned amount here instead —
            // `min_out` is already the real security boundary either way.
            let actual_received = if leg.delta < 0 { -leg.delta } else { 0 };
            if leg.delta < 0 && actual_received < leg.min_out {
                panic_with_error!(&env, ContractError::LegSlippageExceeded);
            }

            set_source_allocation(&env, &leg.source_id, new_amount);

            emit_event(
                &env,
                VAULT,
                REBAL_LEG,
                caller.clone(),
                RebalanceLegExecutedEventData {
                    source_id: leg.source_id.clone(),
                    delta: leg.delta,
                    amount_out: actual_received,
                    min_out: leg.min_out,
                },
            );

            applied.push_back(leg);
        }

        if net_delta < 0 {
            let current_reserves = get_vault_liquid_reserves(&env);
            set_vault_liquid_reserves(&env, current_reserves - net_delta);
        }

        env.storage()
            .instance()
            .set(&DataKey::LastRebalanceAt, &now);

        emit_event(
            &env,
            VAULT,
            REBAL_CMP,
            caller,
            RebalanceCompletedEventData {
                plan_hash,
                total_value_moved: total_moved,
                // Realised slippage is 0 in the current bookkeeping-only
                // model (actual == gross always). Once live yield-source
                // withdrawals are wired in, compute this from the gap
                // between expected and actual `amount_out` per leg.
                realized_slippage_bps: 0,
            },
        );

        applied
    }

    /// Admin: tighten the per-leg slippage ceiling. Bounded by the
    /// compile-time [`rebalance::MAX_LEG_SLIPPAGE_BPS_CEILING`] — stored
    /// configuration can only ever be equal to or stricter than that
    /// ceiling, never looser.
    pub fn set_max_leg_slippage_bps(env: Env, caller: Address, bps: u32) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);
        if bps == 0 || bps > rebalance::MAX_LEG_SLIPPAGE_BPS_CEILING {
            panic_with_error!(&env, ContractError::ConfigOutOfRange);
        }
        env.storage()
            .instance()
            .set(&DataKey::MaxLegSlippageBps, &bps);
    }

    pub fn get_max_leg_slippage_bps(env: Env) -> u32 {
        get_max_leg_slippage_bps(&env)
    }

    /// Admin: cap the fraction of vault assets a single `execute_rebalance`
    /// call may move.
    pub fn set_max_rebalance_value_bps(env: Env, caller: Address, bps: u32) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);
        if bps == 0 || bps > 10_000 {
            panic_with_error!(&env, ContractError::ConfigOutOfRange);
        }
        env.storage()
            .instance()
            .set(&DataKey::MaxRebalanceValueBps, &bps);
    }

    pub fn get_max_rebalance_value_bps(env: Env) -> u32 {
        get_max_rebalance_value_bps(&env)
    }

    /// Compute the `plan_hash` `execute_rebalance` expects for an arbitrary
    /// `legs` vector. `plan_rebalance` already returns this for the current
    /// live plan; this exists for callers constructing or inspecting a plan
    /// independently (e.g. off-chain tooling, tests).
    pub fn compute_plan_checksum(_env: Env, legs: Vec<RebalanceLeg>) -> u64 {
        rebalance::checksum(&legs)
    }

    /// Operator hook used by deposit/yield-routing flows to record that a
    /// known amount has been deployed to a specific yield source. Keeps the
    /// vault's per-source bookkeeping in sync with off-chain settlement.
    pub fn record_source_allocation(env: Env, caller: Address, source_id: Symbol, amount: i128) {
        require_initialized(&env);
        caller.require_auth();
        if !AccessControl::has_role(&env, &caller, Role::Admin)
            && !AccessControl::has_role(&env, &caller, Role::Operator)
        {
            panic_with_error!(&env, ContractError::Unauthorized);
        }
        if amount < 0 {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }
        set_source_allocation(&env, &source_id, amount);
    }

    pub fn collect_fees(env: Env, caller: Address) {
        with_reentrancy_guard(env, |env| {
            Self::collect_fees_internal(env, caller);
        })
    }

    fn collect_fees_internal(env: Env, caller: Address) {
        caller.require_auth();
        if !AccessControl::has_role(&env, &caller, Role::Admin)
            && !AccessControl::has_role(&env, &caller, Role::Manager)
        {
            panic_with_error!(&env, ContractError::Unauthorized);
        }

        accrue_management_fee(&env);
        let fees = get_accrued_fees(&env);
        if fees > 0 {
            // Only transfer the portion of liquid reserves that is not already
            // committed to the emergency queue, preventing over-drawing funds
            // that are owed to queued withdrawal requests.
            let current_reserves = get_vault_liquid_reserves(&env);
            let reserved = get_liquid_reserved(&env);
            let available = current_reserves.saturating_sub(reserved);
            let collectable = fees.min(available);

            if collectable == 0 {
                return;
            }

            let config = get_fee_config(&env);
            let token_address = self::VaultContract::get_token(env.clone());

            transfer_tokens(
                &env,
                &token_address,
                &env.current_contract_address(),
                &config.treasury_address,
                &collectable,
            );

            invoke_allowed::<()>(
                &env,
                &config.treasury_address,
                &Symbol::new(&env, "receive_fees"),
                (collectable,).into_val(&env),
            );

            set_accrued_fees(&env, fees - collectable);

            let total_assets = get_total_assets(&env);
            set_total_assets(&env, total_assets - collectable);
            sync_vault_token_total_assets(&env);

            set_vault_liquid_reserves(&env, current_reserves - collectable);
        }
    }

    // -----------------------------------------------------------------------
    // Early-exit penalty escrow distribution (issue #805)
    // -----------------------------------------------------------------------

    /// Permissionlessly split the accumulated penalty escrow between
    /// remaining depositors and the treasury by `depositor_share_bps`.
    /// Gated by a minimum amount (anti-spam) and a cooldown (rate limit),
    /// mirroring the existing rebalance cooldown pattern. Atomic: if the
    /// treasury transfer fails, the whole call reverts and the escrow is
    /// left untouched (Soroban aborts all state changes from this
    /// invocation on panic, so there is no way to zero the escrow without
    /// the transfer having already succeeded).
    pub fn distribute_penalties(env: Env, _caller: Address) -> PenaltyDistributedEventData {
        with_reentrancy_guard(env, Self::distribute_penalties_internal)
    }

    fn distribute_penalties_internal(env: Env) -> PenaltyDistributedEventData {
        require_initialized(&env);

        let escrow = get_penalty_escrow(&env);
        let min_amount = get_min_penalty_distribution_amount(&env);
        if escrow < min_amount {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }

        let now = env.ledger().timestamp();
        let cooldown = get_penalty_distribution_cooldown(&env);
        let last: u64 = env
            .storage()
            .instance()
            .get(&DataKey::LastPenaltyDistributionAt)
            .unwrap_or(0);
        if last != 0 && now < last + cooldown {
            panic_with_error!(&env, ContractError::InvalidOperation);
        }

        let depositor_share_bps = get_depositor_share_bps(&env);
        let (depositor_amount, treasury_amount, dust) =
            nester_common::fees::split_penalty(escrow, depositor_share_bps);

        if treasury_amount > 0 {
            let config = get_fee_config(&env);
            let token_address = self::VaultContract::get_token(env.clone());
            transfer_tokens(
                &env,
                &token_address,
                &env.current_contract_address(),
                &config.treasury_address,
                &treasury_amount,
            );
            invoke_allowed::<()>(
                &env,
                &config.treasury_address,
                &Symbol::new(&env, "receive_fees"),
                (treasury_amount,).into_val(&env),
            );
        }

        if depositor_amount > 0 {
            // Credit remaining depositors through the same mechanism that
            // already prices every share (`TotalAssets` / vault-token share
            // price) rather than a bespoke bump — anyone who withdraws
            // before this call is unaffected (they were paid out against
            // the pre-distribution price), and anyone still holding shares
            // benefits proportionally the next time their balance is priced.
            let total_assets = get_total_assets(&env);
            let new_total_assets = total_assets
                .checked_add(depositor_amount)
                .unwrap_or_else(|| panic_with_error!(&env, ContractError::ArithmeticOverflow));
            set_total_assets(&env, new_total_assets);
            sync_vault_token_total_assets(&env);
        }

        // Dust from independent rounding of both slices is retained in the
        // escrow rather than force-assigned to either side — never lost,
        // just carried into the next round.
        set_penalty_escrow(&env, dust);
        env.storage()
            .instance()
            .set(&DataKey::LastPenaltyDistributionAt, &now);

        let result = PenaltyDistributedEventData {
            depositor_amount,
            treasury_amount,
            retained_dust: dust,
            timestamp: now,
        };

        emit_event(
            &env,
            VAULT,
            PNLTY_DST,
            env.current_contract_address(),
            result.clone(),
        );

        result
    }

    pub fn get_penalty_escrow(env: Env) -> i128 {
        get_penalty_escrow(&env)
    }

    pub fn get_penalty_config(env: Env) -> PenaltyConfig {
        PenaltyConfig {
            depositor_share_bps: get_depositor_share_bps(&env),
            min_distribution_amount: get_min_penalty_distribution_amount(&env),
            distribution_cooldown: get_penalty_distribution_cooldown(&env),
        }
    }

    /// Admin: set the depositor slice of every future penalty distribution.
    /// The treasury's slice (`10_000 - depositor_share_bps`) is hard-capped
    /// at the compile-time [`nester_common::MAX_TREASURY_SHARE_BPS`], so
    /// `depositor_share_bps` must be at least `10_000 -
    /// MAX_TREASURY_SHARE_BPS` — no admin configuration can send more than
    /// that ceiling to the treasury.
    pub fn set_depositor_share_bps(env: Env, caller: Address, bps: u32) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);
        let min_depositor_share = 10_000u32.saturating_sub(nester_common::MAX_TREASURY_SHARE_BPS);
        if bps < min_depositor_share || bps > 10_000 {
            panic_with_error!(&env, ContractError::ConfigOutOfRange);
        }
        env.storage()
            .instance()
            .set(&DataKey::DepositorShareBps, &bps);
    }

    pub fn set_min_penalty_dist_amount(env: Env, caller: Address, amount: i128) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);
        if amount < 0 {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }
        env.storage()
            .instance()
            .set(&DataKey::MinPenaltyDistributionAmount, &amount);
    }

    pub fn set_penalty_dist_cooldown(env: Env, caller: Address, seconds: u64) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);
        env.storage()
            .instance()
            .set(&DataKey::PenaltyDistributionCooldown, &seconds);
    }

    /// Deposit funds into the vault.
    pub fn deposit(env: Env, user: Address, amount: i128, min_shares_out: i128) -> i128 {
        with_reentrancy_guard(env, |env| {
            Self::deposit_internal(env, user, amount, min_shares_out)
        })
    }

    fn deposit_internal(env: Env, user: Address, amount: i128, min_shares_out: i128) -> i128 {
        require_initialized(&env);
        require_active(&env);
        breaker::require_deposits_allowed(&env);

        let max_deposit: i128 = env
            .storage()
            .instance()
            .get(&DataKey::MaxDeposit)
            .unwrap_or(i128::MAX);
        if amount > max_deposit {
            panic_with_error!(&env, ContractError::ExceedsLimit);
        }

        let min_deposit: i128 = env
            .storage()
            .instance()
            .get(&DataKey::MinDeposit)
            .unwrap_or(0);
        if amount < min_deposit {
            panic_with_error!(&env, ContractError::BelowMinDeposit);
        }

        if amount < nester_common::MIN_DEPOSIT_AMOUNT {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }
        if min_shares_out < 0 {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }

        user.require_auth();
        accrue_management_fee(&env);

        // Validate the exchange-rate state before moving funds. In particular,
        // a live share supply backed by zero assets is insolvent and must not
        // fall back to the bootstrap 1:1 rate.
        let _validated_conversion = conversion::assets_to_shares_down(
            amount,
            get_total_assets(&env),
            vault_token_client(&env).total_supply(),
        )
        .unwrap_or_else(|e| panic_with_error!(&env, e));

        let token_address = self::VaultContract::get_token(env.clone());
        let contract_address = env.current_contract_address();

        transfer_tokens(&env, &token_address, &user, &contract_address, &amount);

        let total_assets = get_total_assets(&env);
        // Mint deposit shares against gross assets (pre-fee) so new depositors
        // do not pay for uncollected accrued fees.
        vault_token_client(&env).set_total_assets(&total_assets);
        let shares_to_mint = vault_token_client(&env).shares_for_deposit(&amount);
        if shares_to_mint < min_shares_out {
            panic_with_error!(&env, ContractError::SlippageExceeded);
        }
        let _ = vault_token_client(&env).mint_for_deposit(&user, &amount);
        let new_user_shares = get_shares(&env, &user);
        let new_total_assets = total_assets
            .checked_add(amount)
            .unwrap_or_else(|| panic_with_error!(&env, ContractError::ArithmeticOverflow));
        set_total_assets(&env, new_total_assets);
        sync_vault_token_total_assets(&env);

        let current_principal = get_user_principal(&env, &user);
        let new_principal = current_principal
            .checked_add(amount)
            .unwrap_or_else(|| panic_with_error!(&env, ContractError::ArithmeticOverflow));
        set_user_principal(&env, &user, new_principal);

        let current_reserves = get_vault_liquid_reserves(&env);
        let new_reserves = current_reserves
            .checked_add(amount)
            .unwrap_or_else(|| panic_with_error!(&env, ContractError::ArithmeticOverflow));
        set_vault_liquid_reserves(&env, new_reserves);

        env.storage().persistent().set(
            &DataKey::DepositTime(user.clone()),
            &env.ledger().timestamp(),
        );

        // Tenure starts on a user's first deposit and is never reset by
        // subsequent deposits — only a full exit resets it (see
        // `withdraw_internal`). This rewards long-tenured behaviour instead
        // of punishing it every time a user tops up their position.
        if !has_first_deposit_at(&env, &user) {
            set_first_deposit_at(&env, &user, env.ledger().timestamp());
        }

        emit_event(
            &env,
            VAULT,
            DEPOSIT,
            user.clone(),
            DepositEventData {
                amount,
                shares_minted: shares_to_mint,
                new_balance: new_user_shares,
                total_assets: new_total_assets,
            },
        );

        Self::process_emergency_queue_internal(env.clone());

        let post_price = vault_token_client(&env).share_price();
        breaker::check_share_price_move(&env, post_price);

        new_user_shares
    }

    /// Drains the legacy asset-amount queue used by [`Self::emergency_withdraw`]'s
    /// insufficient-liquidity fallback. Distinct from the fair, share-based
    /// queue added in issue #814 (see [`Self::process_emergency_queue`]);
    /// kept as-is since `emergency_withdraw` is a separate, simpler
    /// paused-only exit path with its own tests.
    pub fn drain_legacy_emergency_queue(env: Env) {
        with_reentrancy_guard(env, Self::process_emergency_queue_internal)
    }

    fn process_emergency_queue_internal(env: Env) {
        let queue = get_emergency_queue(&env);
        if queue.is_empty() {
            return;
        }

        let mut liquid_reserves = get_vault_liquid_reserves(&env);
        let mut liquid_reserved = get_liquid_reserved(&env);
        let token_address = self::VaultContract::get_token(env.clone());
        let contract_address = env.current_contract_address();

        let mut i = 0;
        while i < queue.len() {
            let req = queue.get(i).unwrap();
            if liquid_reserves >= req.amount {
                transfer_tokens(
                    &env,
                    &token_address,
                    &contract_address,
                    &req.user,
                    &req.amount,
                );
                liquid_reserves -= req.amount;
                // Release the reservation now that the payment has been made.
                liquid_reserved = liquid_reserved.saturating_sub(req.amount);

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
        set_liquid_reserved(&env, liquid_reserved);
        set_emergency_queue(&env, &new_queue);
    }

    /// Withdraw funds from the vault.
    pub fn withdraw(env: Env, user: Address, shares: i128, min_assets_out: i128) -> i128 {
        with_reentrancy_guard(env, |env| {
            Self::withdraw_internal(env, user, shares, min_assets_out)
        })
    }

    fn withdraw_internal(env: Env, user: Address, shares: i128, min_assets_out: i128) -> i128 {
        require_initialized(&env);
        require_active(&env);
        breaker::require_withdrawals_allowed(&env);

        if shares <= 0 {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }
        if min_assets_out < 0 {
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
        let current_principal = get_user_principal(&env, &user);
        let principal_to_remove =
            nester_common::fees::mul_div(current_principal, shares, current_shares)
                .unwrap_or_else(|e| panic_with_error!(&env, e));

        // Trigger circuit breaker check
        check_circuit_breaker(&env, assets_to_withdraw);

        // Fee logic
        let config = get_fee_config(&env);
        let mut total_fee = 0_i128;

        // 1. Performance fee applies only to realized gain above user cost basis.
        // Rate is tenure-tiered when a schedule is configured (issue #813),
        // evaluated at the user's tenure *now* — the same "take it when you
        // touch it" timing the vault already uses for withdrawals, just with
        // a tiered rate instead of a flat one. See `harvest_internal` for the
        // equivalent choice on the claim-yield path.
        let yield_part = assets_to_withdraw - principal_to_remove;
        if yield_part > 0 {
            let performance_fee_bps = effective_performance_fee_bps(&env, &user, &config);
            let perf_fee =
                nester_common::fees::calculate_performance_fee(yield_part, performance_fee_bps)
                    .unwrap_or_else(|e| panic_with_error!(&env, e));
            total_fee = total_fee
                .checked_add(perf_fee)
                .unwrap_or_else(|| panic_with_error!(&env, ContractError::ArithmeticOverflow));
        }

        // 2. Early withdrawal fee (0.1%)
        // Use the most recent deposit timestamp: either the direct-deposit record
        // stored in the vault or the transfer-derived timestamp stored in the
        // vault token.  Taking the maximum prevents a user who received shares
        // via transfer from inheriting an old timestamp and skipping the fee.
        let vault_deposit_time: u64 = env
            .storage()
            .persistent()
            .get(&DataKey::DepositTime(user.clone()))
            .unwrap_or(0);
        let vt_deposit_time: u64 = vault_token_client(&env).get_deposit_time(&user);
        let deposit_time = vault_deposit_time.max(vt_deposit_time);
        let min_lock: u64 = env
            .storage()
            .instance()
            .get(&DataKey::MinLockPeriod)
            .unwrap_or(0);
        // A tenure-tiered exit schedule (issue #813) is a continuous curve
        // over the whole tenure domain and supersedes the binary min-lock
        // gate below — its own long-tenure tiers taper to a low floor, so it
        // doesn't need the gate to avoid over-charging long-held positions.
        // With no tiers configured, behaviour is unchanged: the flat fee
        // only applies inside the lock window.
        let exit_tiers_configured = !get_exit_tiers(&env).is_empty();
        if exit_tiers_configured || env.ledger().timestamp() < deposit_time + min_lock {
            let exit_fee_bps = effective_exit_fee_bps(&env, &user, &config);
            let early_fee =
                nester_common::fees::calculate_withdrawal_fee(assets_to_withdraw, exit_fee_bps)
                    .unwrap_or_else(|e| panic_with_error!(&env, e));
            total_fee = total_fee
                .checked_add(early_fee)
                .unwrap_or_else(|| panic_with_error!(&env, ContractError::ArithmeticOverflow));
        }

        assets_to_withdraw -= total_fee;
        if assets_to_withdraw < min_assets_out {
            panic_with_error!(&env, ContractError::SlippageExceeded);
        }
        let new_accrued = accrued_fees
            .checked_add(total_fee)
            .unwrap_or_else(|| panic_with_error!(&env, ContractError::ArithmeticOverflow));
        set_accrued_fees(&env, new_accrued);

        let token_address = self::VaultContract::get_token(env.clone());
        let contract_address = env.current_contract_address();

        transfer_tokens(
            &env,
            &token_address,
            &contract_address,
            &user,
            &assets_to_withdraw,
        );

        let _ = vault_token_client(&env).burn_for_withdrawal(&user, &shares);
        let new_user_shares = current_shares - shares;
        set_total_assets(&env, total_assets - assets_to_withdraw);

        set_user_principal(&env, &user, current_principal - principal_to_remove);

        // A full exit resets tenure; a partial withdrawal keeps it — a user
        // who fully exits and later returns starts the tiered schedule
        // fresh, but topping up an existing position never costs them their
        // accumulated tenure discount (issue #813).
        if new_user_shares == 0 {
            clear_first_deposit_at(&env, &user);
        }

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

        let post_price = vault_token_client(&env).share_price();
        breaker::check_share_price_move(&env, post_price);

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
        let emergency_fee = nester_common::fees::mul_div(principal, fee_bps as i128, 10_000)?;
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
        with_reentrancy_guard(env, |env| Self::emergency_withdraw_internal(env, user))
    }

    fn emergency_withdraw_internal(env: Env, user: Address) -> Result<i128, ContractError> {
        require_initialized(&env);
        // Emergency exit is available whenever the vault is in a
        // restricted state — the legacy admin-triggered pause, or the new
        // auto/guardian-triggered FullHalt severity (#817). This path is
        // deliberately never gated by severity beyond that check: a breaker
        // that stops users from leaving is a trap, not a safety device.
        if !is_paused(&env) && breaker::severity(&env) != Severity::FullHalt {
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
        let fee = nester_common::fees::mul_div(principal, fee_bps as i128, 10_000)?;
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

        // The emergency fee was never transferred to the user nor reflected
        // as a reduction to `TotalAssets` beyond `burned_assets` — it was
        // simply left inside the vault's real balance, untracked. Escrow it
        // explicitly (issue #805) so it can be split between remaining
        // depositors and the treasury instead of quietly inflating nobody's
        // accounted balance.
        charge_penalty(&env, &user, fee, PenaltyReason::EmergencyExit, shares);

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

            // Reserve these funds so collect_fees cannot draw them away
            // before the queued request is processed.
            let currently_reserved = get_liquid_reserved(&env);
            set_liquid_reserved(&env, currently_reserved + return_amount);

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
            transfer_tokens(
                &env,
                &token_address,
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

    /// Emergency exit from **all** active yield-source positions in a single
    /// transaction.
    ///
    /// Iterates every source that currently holds a live allocation, pulls the
    /// deployed funds back into the vault's liquid reserves, and emits an
    /// `EmergencyWithdraw` event per position carrying the amount and protocol.
    /// Inactive positions (zero allocation) are skipped.
    ///
    /// Partial success is allowed: if unwinding a position fails it is recorded
    /// in `failed` and iteration continues, so one bad position cannot block the
    /// rest from exiting. The returned [`EmergencyWithdrawAllResult`] lists both
    /// the succeeded and failed withdrawals.
    ///
    /// Authorization: callable only by the position owner (`user`).
    pub fn emergency_withdraw_all(env: Env, user: Address) -> EmergencyWithdrawAllResult {
        with_reentrancy_guard(env, |env| Self::emergency_withdraw_all_internal(env, user))
    }

    fn emergency_withdraw_all_internal(env: Env, user: Address) -> EmergencyWithdrawAllResult {
        require_initialized(&env);
        user.require_auth();

        let sources = get_allocated_sources(&env);
        let mut succeeded = Vec::new(&env);
        let mut failed = Vec::new(&env);

        for source_id in sources.iter() {
            let amount = get_source_allocation(&env, &source_id);
            // Only active positions have something to unwind.
            if amount <= 0 {
                continue;
            }

            let reserves = get_vault_liquid_reserves(&env);
            match reserves.checked_add(amount) {
                Some(new_reserves) => {
                    // Move the deployed funds back into liquid reserves so they
                    // become available for withdrawal, and clear the position.
                    set_source_allocation(&env, &source_id, 0);
                    set_vault_liquid_reserves(&env, new_reserves);

                    emit_event(
                        &env,
                        VAULT,
                        symbol_short!("EMRG_WD"),
                        user.clone(),
                        PositionEmergencyWithdrawEventData {
                            user: user.clone(),
                            protocol: source_id.clone(),
                            amount,
                        },
                    );

                    succeeded.push_back(PositionWithdrawal {
                        protocol: source_id.clone(),
                        amount,
                    });
                }
                None => {
                    // Unwinding this position would overflow the vault's
                    // reserves — log it as failed and keep going.
                    failed.push_back(PositionWithdrawal {
                        protocol: source_id.clone(),
                        amount,
                    });
                }
            }
        }

        EmergencyWithdrawAllResult { succeeded, failed }
    }

    // -----------------------------------------------------------------------
    // Fair-ordering emergency withdrawal queue (issue #814)
    // -----------------------------------------------------------------------

    /// Queue a fair, FIFO-ordered emergency withdrawal for `shares`. Calling
    /// this again while a request is already open extends it rather than
    /// creating a second queue position (see [`queue::request`]).
    pub fn request_emergency_withdrawal(env: Env, user: Address, shares: i128) -> QueueEntry {
        require_initialized(&env);
        user.require_auth();

        let balance = get_shares(&env, &user);
        let entry = queue::request(&env, &user, shares, balance);

        emit_event(
            &env,
            VAULT,
            EMRG_REQD,
            user.clone(),
            EmergencyRequestedEventData {
                user,
                seq: entry.seq,
                shares_requested: entry.shares_requested,
            },
        );

        entry
    }

    /// Cancel `user`'s open emergency-queue request. Any unfilled shares were
    /// never burned, so they are already sitting in the user's balance —
    /// this call is bookkeeping only, no asset transfer.
    pub fn cancel_emergency_request(env: Env, user: Address) -> i128 {
        require_initialized(&env);
        user.require_auth();

        let seq_before = queue::position(&env, &user).seq;
        let shares_returned = queue::cancel(&env, &user);

        emit_event(
            &env,
            VAULT,
            EMRG_CANCL,
            user.clone(),
            EmergencyCancelledEventData {
                user,
                seq: seq_before,
                shares_returned,
            },
        );

        shares_returned
    }

    /// Permissionlessly drive the fair emergency queue forward. Anyone may
    /// call this — exits do not depend on operator liveness. `max_entries`
    /// is capped by [`queue::MAX_PROCESS_ENTRIES`] regardless of the value
    /// requested, so a single call can never exceed the tx resource budget.
    pub fn process_emergency_queue(env: Env, caller: Address, max_entries: u32) -> u32 {
        with_reentrancy_guard(env, |env| {
            Self::process_fair_queue_internal(env, caller, max_entries)
        })
    }

    fn process_fair_queue_internal(env: Env, _caller: Address, max_entries: u32) -> u32 {
        require_initialized(&env);

        let available_liquidity = get_vault_liquid_reserves(&env);
        let plan = queue::plan_fills(&env, max_entries, available_liquidity, |shares| {
            vault_token_client(&env).amount_for_shares(&shares)
        });

        let token_address = self::VaultContract::get_token(env.clone());
        let contract_address = env.current_contract_address();
        let mut processed: u32 = 0;

        for planned in plan.iter() {
            // Self-heal against a user whose balance shrank after enqueuing
            // (e.g. a transfer of vault-token shares elsewhere): never burn
            // more than they currently hold.
            let current_balance = get_shares(&env, &planned.user);
            let fill_shares = planned.fill_shares.min(current_balance);
            if fill_shares <= 0 {
                continue;
            }

            let burned_assets =
                vault_token_client(&env).burn_for_withdrawal(&planned.user, &fill_shares);
            transfer_tokens(
                &env,
                &token_address,
                &contract_address,
                &planned.user,
                &burned_assets,
            );

            let reserves = get_vault_liquid_reserves(&env);
            set_vault_liquid_reserves(&env, reserves - burned_assets);

            let total_assets = get_total_assets(&env);
            set_total_assets(&env, total_assets - burned_assets);

            let fully_filled = queue::apply_fill(&env, planned.seq, fill_shares);
            processed += 1;

            emit_event(
                &env,
                VAULT,
                EMRG_FILL,
                planned.user.clone(),
                EmergencyFilledEventData {
                    user: planned.user.clone(),
                    seq: planned.seq,
                    fill_shares,
                    fill_assets: burned_assets,
                    fully_filled,
                },
            );
        }

        sync_vault_token_total_assets(&env);
        processed
    }

    /// Caller's position in the fair emergency queue: sequence number,
    /// entries/shares strictly ahead, and how much of their own request has
    /// already been filled. Bounded-cost — see [`queue::MAX_POSITION_SCAN`].
    pub fn get_queue_position(env: Env, user: Address) -> QueuePosition {
        queue::position(&env, &user)
    }

    /// O(1) aggregate queue stats backed by running counters, not iteration.
    pub fn get_queue_stats(env: Env) -> QueueStats {
        let available_liquidity = get_vault_liquid_reserves(&env);
        queue::stats(&env, available_liquidity)
    }

    /// Admin: configure the per-entry fill cap (bps of a round's available
    /// liquidity) that any single queue entry may absorb per
    /// `process_emergency_queue` call. Bounded to (0, 10_000].
    pub fn set_max_fill_share_bps(env: Env, caller: Address, bps: u32) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);
        queue::set_max_fill_share_bps(&env, bps);
    }

    pub fn get_max_fill_share_bps(env: Env) -> u32 {
        queue::max_fill_share_bps(&env)
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

    pub fn preview_deposit(env: Env, amount: i128) -> i128 {
        require_initialized(&env);
        if amount <= 0 {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }
        conversion::assets_to_shares_down(
            amount,
            get_net_total_assets(&env),
            vault_token_client(&env).total_supply(),
        )
        .unwrap_or_else(|e| panic_with_error!(&env, e))
    }

    /// Returns the **gross**, pre-fee asset value of `shares` (the raw
    /// share-price conversion, like an EIP-4626 `previewRedeem` of the
    /// underlying price).
    ///
    /// ⚠️ Do **not** pass this value straight through as `min_assets_out` to
    /// [`VaultContract::withdraw`]. A fee-bearing withdrawal deducts a
    /// performance fee (on realized yield) and/or an early-withdrawal fee, so
    /// the amount actually transferred is *less* than this gross figure and the
    /// call reverts with `ContractError::SlippageExceeded` (see #448). For a
    /// slippage-safe floor that reflects the fees deducted on withdrawal, use
    /// [`VaultContract::preview_withdraw_net`] or
    /// [`VaultContract::withdrawal_fee_preview`].
    pub fn preview_withdraw(env: Env, shares: i128) -> i128 {
        require_initialized(&env);
        if shares <= 0 {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }
        conversion::shares_to_assets_down(
            shares,
            get_net_total_assets(&env),
            vault_token_client(&env).total_supply(),
        )
        .unwrap_or_else(|e| panic_with_error!(&env, e))
    }

    /// Convert assets to shares at the current exchange rate. Since this is the
    /// amount the caller would receive, a non-zero remainder rounds down.
    pub fn convert_to_shares(env: Env, assets: i128) -> i128 {
        require_initialized(&env);
        conversion::assets_to_shares_down(
            assets,
            get_net_total_assets(&env),
            vault_token_client(&env).total_supply(),
        )
        .unwrap_or_else(|e| panic_with_error!(&env, e))
    }

    /// Convert shares to assets at the current exchange rate. Since this is the
    /// amount the caller would receive, a non-zero remainder rounds down.
    pub fn convert_to_assets(env: Env, shares: i128) -> i128 {
        require_initialized(&env);
        conversion::shares_to_assets_down(
            shares,
            get_net_total_assets(&env),
            vault_token_client(&env).total_supply(),
        )
        .unwrap_or_else(|e| panic_with_error!(&env, e))
    }

    /// Total underlying assets currently backing outstanding shares.
    pub fn total_assets(env: Env) -> i128 {
        require_initialized(&env);
        get_net_total_assets(&env)
    }

    /// Maximum assets accepted by one deposit. Returns zero rather than
    /// reverting when deposits are unavailable.
    pub fn max_deposit(env: Env, _user: Address) -> i128 {
        if !env.storage().instance().has(&DataKey::Token) || is_paused(&env) {
            return 0;
        }
        let cap: i128 = env
            .storage()
            .instance()
            .get(&DataKey::MaxDeposit)
            .unwrap_or(i128::MAX);
        if cap <= 0 {
            return 0;
        }
        // Refuse deposits in the insolvent live-supply state.
        let total_shares = vault_token_client(&env).total_supply();
        if total_shares > 0 && get_net_total_assets(&env) == 0 {
            return 0;
        }
        cap
    }

    /// Maximum gross assets currently withdrawable by `user`.
    pub fn max_withdraw(env: Env, user: Address) -> i128 {
        max_withdrawable_assets(&env, &user)
    }

    /// Maximum shares currently redeemable by `user`.
    pub fn max_redeem(env: Env, user: Address) -> i128 {
        let max_assets = max_withdrawable_assets(&env, &user);
        if max_assets == 0 {
            return 0;
        }
        let balance = get_shares(&env, &user);
        conversion::assets_to_shares_down(
            max_assets,
            get_net_total_assets(&env),
            vault_token_client(&env).total_supply(),
        )
        .unwrap_or(0)
        .min(balance)
    }

    /// Returns the amount the caller actually receives after all fees —
    /// safe to use directly as `min_assets_out` in [`VaultContract::withdraw`].
    ///
    /// Worst-case scenario: assumes the entire gross amount is yield (maximum
    /// performance fee) and that the lock period is still active (early-withdrawal
    /// fee applies). Callers that know the user's cost basis or lock status can
    /// use [`VaultContract::withdrawal_fee_preview`] for a tighter estimate.
    pub fn preview_withdraw_net(env: Env, shares: i128) -> i128 {
        require_initialized(&env);
        if shares <= 0 {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }
        let gross = vault_token_client(&env).amount_for_shares(&shares);
        let config = get_fee_config(&env);

        // Worst-case: treat the full gross as yield.
        let perf_fee =
            nester_common::fees::calculate_performance_fee(gross, config.performance_fee_bps)
                .unwrap_or(0);

        // Worst-case: assume still within lock period.
        let early_fee =
            nester_common::fees::calculate_withdrawal_fee(gross, config.early_withdrawal_fee_bps)
                .unwrap_or(0);

        let total_fee = perf_fee.saturating_add(early_fee);
        gross.saturating_sub(total_fee)
    }

    pub fn get_shares(env: Env, user: Address) -> i128 {
        require_initialized(&env);
        get_shares(&env, &user)
    }

    pub fn get_principal(env: Env, user: Address) -> i128 {
        require_initialized(&env);
        get_user_principal(&env, &user)
    }

    pub fn get_total_deposits(env: Env) -> i128 {
        require_initialized(&env);
        let total_assets = get_total_assets(&env);
        let accrued_fees = get_accrued_fees(&env);
        total_assets - accrued_fees
    }

    pub fn share_price(env: Env) -> i128 {
        require_initialized(&env);
        vault_token_client(&env).share_price()
    }

    pub fn total_shares(env: Env) -> i128 {
        require_initialized(&env);
        vault_token_client(&env).total_supply()
    }

    pub fn estimated_fees(env: Env) -> i128 {
        require_initialized(&env);
        let mut fees = get_accrued_fees(&env);
        let last_accrual: u64 = env
            .storage()
            .instance()
            .get(&DataKey::LastFeeAccrual)
            .unwrap_or(env.ledger().timestamp());
        let now = env.ledger().timestamp();
        // Match the on-chain accrual cap so the estimate reflects what would
        // actually be collected on the next call rather than an unbounded
        // figure that can't be realised in a single transaction.
        let elapsed = now
            .saturating_sub(last_accrual)
            .min(nester_common::fees::MAX_FEE_ACCRUAL_INTERVAL_SECONDS);
        if elapsed > 0 {
            let config = get_fee_config(&env);
            let total_assets = get_total_assets(&env);
            let pending = nester_common::fees::calculate_management_fee(
                total_assets,
                config.management_fee_bps,
                elapsed,
            )
            .unwrap_or(0);
            fees = fees.saturating_add(pending);
        }
        fees
    }

    pub fn pending_yield(env: Env) -> i128 {
        require_initialized(&env);
        let token_address = self::VaultContract::get_token(env.clone());
        let contract_balance =
            token::Client::new(&env, &token_address).balance(&env.current_contract_address());
        let liquid_reserves = get_vault_liquid_reserves(&env);
        let accrued_fees = get_accrued_fees(&env);

        let gross = if contract_balance > liquid_reserves {
            contract_balance - liquid_reserves
        } else {
            0
        };
        // Return net yield after subtracting accrued management fees so the
        // caller sees the amount actually distributable to depositors.
        gross.saturating_sub(accrued_fees)
    }

    pub fn withdrawal_fee_preview(env: Env, user: Address, shares: i128) -> WithdrawalFeePreview {
        require_initialized(&env);
        let current_shares = get_shares(&env, &user);
        let mut preview = WithdrawalFeePreview {
            gross_asset_value: 0,
            management_fee_deducted: 0,
            performance_fee_deducted: 0,
            early_withdrawal_fee_deducted: 0,
            net_amount_received: 0,
        };
        if shares <= 0 || shares > current_shares {
            return preview;
        }

        let assets_to_withdraw = vault_token_client(&env).amount_for_shares(&shares);
        preview.gross_asset_value = assets_to_withdraw;

        let current_principal = get_user_principal(&env, &user);
        let principal_to_remove = current_principal * shares / current_shares;

        let config = get_fee_config(&env);
        let yield_part = assets_to_withdraw - principal_to_remove;
        if yield_part > 0 {
            let performance_fee_bps = effective_performance_fee_bps(&env, &user, &config);
            preview.performance_fee_deducted =
                nester_common::fees::calculate_performance_fee(yield_part, performance_fee_bps)
                    .unwrap_or(0);
        }

        let vault_deposit_time: u64 = env
            .storage()
            .persistent()
            .get(&DataKey::DepositTime(user.clone()))
            .unwrap_or(0);
        let vt_deposit_time: u64 = vault_token_client(&env).get_deposit_time(&user);
        let deposit_time = vault_deposit_time.max(vt_deposit_time);
        let min_lock: u64 = env
            .storage()
            .instance()
            .get(&DataKey::MinLockPeriod)
            .unwrap_or(0);
        let exit_tiers_configured = !get_exit_tiers(&env).is_empty();
        if exit_tiers_configured || env.ledger().timestamp() < deposit_time + min_lock {
            let exit_fee_bps = effective_exit_fee_bps(&env, &user, &config);
            preview.early_withdrawal_fee_deducted =
                nester_common::fees::calculate_withdrawal_fee(assets_to_withdraw, exit_fee_bps)
                    .unwrap_or(0);
        }

        preview.net_amount_received = assets_to_withdraw
            - preview.performance_fee_deducted
            - preview.early_withdrawal_fee_deducted;
        preview
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

    pub fn get_last_harvest_at(env: Env, user: Address) -> u64 {
        get_last_harvest_at(&env, &user)
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

    // -----------------------------------------------------------------------
    // Upgradeability & Schema Migration
    // -----------------------------------------------------------------------

    /// Proposes a new WASM upgrade for the vault.
    ///
    /// Requires Upgrader role and enforces MIN_UPGRADE_DELAY_VAULT (48 hours).
    pub fn propose_upgrade(env: Env, admin: Address, new_wasm_hash: BytesN<32>, eta: u64) {
        AccessControl::require_role(&env, &admin, Role::Upgrader);
        nester_common::Upgrade::propose_upgrade(
            &env,
            &admin,
            new_wasm_hash,
            nester_common::MIN_UPGRADE_DELAY_VAULT,
            eta,
        );
    }

    /// Cancels a pending WASM upgrade for the vault.
    ///
    /// Requires Upgrader role.
    pub fn cancel_upgrade(env: Env, admin: Address) {
        AccessControl::require_role(&env, &admin, Role::Upgrader);
        nester_common::Upgrade::cancel_upgrade(&env, &admin);
    }

    /// Executes a matured WASM upgrade for the vault.
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
}


// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod test;
