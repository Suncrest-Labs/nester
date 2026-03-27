#![no_std]

pub mod constants;
pub mod errors;
pub mod events;
pub mod storage;
pub mod fees;

pub use constants::*;
pub use errors::ContractError;
pub use events::*;
pub use storage::*;

#[cfg(test)]
mod tests {
    #[test]
    fn test_management_fee_calculation() {
        use super::fees::calculate_management_fee;
        let fee = calculate_management_fee(10_000, 50, 31_536_000);
        assert_eq!(fee, 50);
    }
}
