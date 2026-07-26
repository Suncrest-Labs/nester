#![no_std]

pub mod adapters;
pub mod constants;
pub mod errors;
pub mod events;
pub mod fees;
pub mod reentrancy;
pub mod storage;

pub use adapters::{AdapterApy, ApyConfidence, YieldAdapterClient};
pub use constants::*;
pub use errors::ContractError;
pub use events::*;
pub use reentrancy::{CalleeAllowlist, ReentrancyGuard, with_reentrancy_guard};
pub use storage::*;

use soroban_sdk::contracttype;

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