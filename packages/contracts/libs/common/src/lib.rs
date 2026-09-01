#![no_std]

pub mod adapters;
pub mod attestation;
pub mod constants;
pub mod errors;
pub mod events;
pub mod fees;
pub mod reentrancy;
pub mod storage;
pub mod upgrade;

pub use adapters::{AdapterApy, ApyConfidence, YieldAdapterClient};
pub use attestation::{
    build_payload_bytes, verify_attestation, Attestation, AttestedField, AttestationPayload,
    FIELD_APY, FIELD_TVL,
};
pub use constants::*;
pub use errors::ContractError;
pub use events::*;
pub use reentrancy::{CalleeAllowlist, ReentrancyGuard, with_reentrancy_guard};
pub use storage::*;
pub use upgrade::*;


use soroban_sdk::{contractclient, contracttype, Address, Env};

/// Standard read-only surface exposed by a Nester tokenised vault.
///
/// Implementations must return zero from `max_*` when the requested action is
/// currently unavailable. Integrators can therefore probe limits without
/// handling a contract failure.
#[contractclient(name = "TokenisedVaultClient")]
pub trait TokenisedVault {
    fn convert_to_shares(env: Env, assets: i128) -> i128;
    fn convert_to_assets(env: Env, shares: i128) -> i128;
    fn total_assets(env: Env) -> i128;
    fn max_deposit(env: Env, user: Address) -> i128;
    fn max_withdraw(env: Env, user: Address) -> i128;
    fn max_redeem(env: Env, user: Address) -> i128;
}

/// Multi-asset vault configuration for a single asset in the basket.
#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AssetConfig {
    /// Token contract address
    pub token: Address,
    /// Target allocation weight in basis points (sum must equal 10,000)
    pub target_weight_bps: u32,
    /// Number of decimals for this asset
    pub decimals: u32,
    /// Oracle feed identifier for price data
    pub price_feed_id: u32,
    /// Maximum deposit amount for this asset
    pub max_deposit_cap: i128,
}

/// Price information from oracle with validation metadata.
#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PriceInfo {
    /// Asset price in USDC (7 decimals)
    pub price: i128,
    /// Timestamp when price was last updated
    pub timestamp: u64,
    /// Confidence level/validity flag
    pub is_valid: bool,
}

/// Result of multi-asset basket valuation.
#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct BasketValuation {
    /// Total value in USDC (7 decimals)
    pub total_value: i128,
    /// Timestamp of the valuation
    pub timestamp: u64,
    /// Whether all prices were fresh and valid
    pub is_valid: bool,
}

/// Lifecycle status of a yield source.
#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq, Ord, PartialOrd)]
pub enum SourceStatus {
    Active,
    Paused,
    Deprecated,
    Exploit,
    /// Automatically set when an adapter exceeds the failure threshold.
    /// Allocation logic treats this like `Paused` (freeze, don't drain).
    /// Recovery requires an explicit admin `update_status` back to `Active`
    /// — never silent auto-recovery.
    Degraded,
}

/// The category of yield-generating protocol.
#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq, Ord, PartialOrd)]
pub enum ProtocolType {
    Lending,
    Staking,
    LP,
}

#[cfg(test)]
mod tests {
    #[test]
    fn test_management_fee_calculation() {
        use super::fees::calculate_management_fee;
        let fee = calculate_management_fee(10_000, 50, 31_536_000).unwrap();
        assert_eq!(fee, 50);
    }
}
