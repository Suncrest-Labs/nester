#![no_std]

pub mod constants;
pub mod errors;
pub mod events;
pub mod fees;
pub mod reentrancy;
pub mod storage;
pub mod upgrade;

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

/// Lifecycle status of a yield source.
#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq, Ord, PartialOrd)]
pub enum SourceStatus {
    Active,
    Paused,
    Deprecated,
    Exploit,
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
