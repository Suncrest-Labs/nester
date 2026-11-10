//! Nester Savings Reserve
//!
//! Holder of the performance-fee shares minted by the savings core. The
//! shares live in the core's ledger under this contract's address. This
//! contract never authorizes anything, so its shares cannot be transferred,
//! approved, redeemed or withdrawn; they leave only when the core executes a
//! timelocked `BurnReserve`, which raises the share price for every holder.

#![no_std]

soroban_sdk::contractmeta!(key = "name", val = "SavingsReserve");
include!(concat!(env!("OUT_DIR"), "/contractmeta.rs"));
soroban_sdk::contractmeta!(key = "source_repo", val = "github.com/Suncrest-Labs/nester");

use soroban_sdk::{
    contract, contracterror, contractimpl, contracttype, panic_with_error, Address, Env,
};

#[contracterror]
#[derive(Copy, Clone, Debug, Eq, PartialEq, PartialOrd, Ord)]
#[repr(u32)]
pub enum ReserveError {
    AlreadyInitialized = 1,
}

#[contracttype]
#[derive(Clone)]
enum DataKey {
    Core,
}

#[contract]
pub struct SavingsReserve;

#[contractimpl]
impl SavingsReserve {
    /// Record the savings core this reserve belongs to. Once only; deploy
    /// and initialize in the same transaction.
    pub fn initialize(env: Env, core: Address) {
        let store = env.storage().instance();
        if store.has(&DataKey::Core) {
            panic_with_error!(&env, ReserveError::AlreadyInitialized);
        }
        store.set(&DataKey::Core, &core);
    }

    pub fn core(env: Env) -> Address {
        env.storage().instance().get(&DataKey::Core).unwrap()
    }
}

#[cfg(test)]
mod test {
    use super::{ReserveError, SavingsReserve, SavingsReserveClient};
    use soroban_sdk::{testutils::Address as _, Address, Env};

    #[test]
    fn test_initialize_once() {
        let env = Env::default();
        let id = env.register_contract(None, SavingsReserve);
        let reserve = SavingsReserveClient::new(&env, &id);
        let core = Address::generate(&env);

        reserve.initialize(&core);
        assert_eq!(reserve.core(), core);

        let err = reserve
            .try_initialize(&Address::generate(&env))
            .unwrap_err()
            .unwrap();
        assert_eq!(err, ReserveError::AlreadyInitialized.into());
        assert_eq!(reserve.core(), core);
    }
}
