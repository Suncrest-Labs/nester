use soroban_sdk::{contracttype, panic_with_error, Address, Env};

use crate::{storage, ContractError};

#[contracttype]
#[derive(Clone)]
pub enum ReentrancyState {
    Locked,
}

pub struct ReentrancyGuard;

impl ReentrancyGuard {
    pub fn enter(env: &Env) {
        let key = storage::reentrancy_lock_key();
        if env.storage().temporary().has(&key) {
            panic_with_error!(env, ContractError::ReentrancyDetected);
        }
        env.storage()
            .temporary()
            .set(&key, &ReentrancyState::Locked);
    }

    pub fn exit(env: &Env) {
        env.storage().temporary().remove(&storage::reentrancy_lock_key());
    }

    pub fn is_locked(env: &Env) -> bool {
        env.storage().temporary().has(&storage::reentrancy_lock_key())
    }
}

pub fn with_reentrancy_guard<F, R>(env: Env, f: F) -> R
where
    F: FnOnce(Env) -> R,
{
    let guard_env = env.clone();
    ReentrancyGuard::enter(&guard_env);
    let result = f(env);
    ReentrancyGuard::exit(&guard_env);
    result
}

#[contracttype]
#[derive(Clone)]
pub enum AllowlistKey {
    Member(Address),
}

pub struct CalleeAllowlist;

impl CalleeAllowlist {
    pub fn register(env: &Env, address: &Address) {
        env.storage()
            .persistent()
            .set(&AllowlistKey::Member(address.clone()), &true);
    }

    pub fn unregister(env: &Env, address: &Address) {
        env.storage()
            .persistent()
            .remove(&AllowlistKey::Member(address.clone()));
    }

    pub fn assert_allowed(env: &Env, address: &Address) {
        let allowed: bool = env
            .storage()
            .persistent()
            .get(&AllowlistKey::Member(address.clone()))
            .unwrap_or(false);
        if !allowed {
            panic_with_error!(env, ContractError::CalleeNotAllowed);
        }
    }

    pub fn is_registered(env: &Env, address: &Address) -> bool {
        env.storage()
            .persistent()
            .get(&AllowlistKey::Member(address.clone()))
            .unwrap_or(false)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use soroban_sdk::{contract, contractimpl, testutils::Address as _, Address, Env};

    #[contract]
    struct GuardTestContract;

    #[contractimpl]
    impl GuardTestContract {
        pub fn acquire_and_release(env: Env) {
            ReentrancyGuard::enter(&env);
            ReentrancyGuard::exit(&env);
        }

        pub fn nested_acquire(env: Env) {
            ReentrancyGuard::enter(&env);
            ReentrancyGuard::enter(&env);
        }

        pub fn with_guard_success(env: Env) -> i32 {
            with_reentrancy_guard(env, |_| 42_i32)
        }

        pub fn reacquire_after_release(env: Env) {
            ReentrancyGuard::enter(&env);
            ReentrancyGuard::exit(&env);
            ReentrancyGuard::enter(&env);
            ReentrancyGuard::exit(&env);
        }

        pub fn with_guard_fail(env: Env) -> i32 {
            with_reentrancy_guard(env, |env| {
                panic_with_error!(env, ContractError::InvalidAmount);
            })
        }

        pub fn register_and_check(env: Env, target: Address) {
            CalleeAllowlist::register(&env, &target);
            CalleeAllowlist::assert_allowed(&env, &target);
        }

        pub fn assert_blocked(env: Env, target: Address) {
            CalleeAllowlist::assert_allowed(&env, &target);
        }

        pub fn unregister(env: Env, target: Address) -> bool {
            CalleeAllowlist::register(&env, &target);
            CalleeAllowlist::unregister(&env, &target);
            CalleeAllowlist::is_registered(&env, &target)
        }
    }

    fn setup() -> (Env, Address, GuardTestContractClient<'static>) {
        let env = Env::default();
        let contract_id = env.register_contract(None, GuardTestContract);
        let client = GuardTestContractClient::new(&env, &contract_id);
        (env, contract_id, client)
    }

    #[test]
    fn reentrancy_guard_acquire_and_release() {
        let (_env, _id, client) = setup();
        client.acquire_and_release();
    }

    #[test]
    #[should_panic(expected = "Error(Contract, #22)")]
    fn reentrancy_guard_blocks_nested_entry() {
        let (_env, _id, client) = setup();
        client.nested_acquire();
    }

    #[test]
    fn reentrancy_guard_releases_after_with_guard() {
        let (_env, _id, client) = setup();
        assert_eq!(client.with_guard_success(), 42);
    }

    #[test]
    fn reentrancy_guard_reacquires_after_exit_on_simulated_revert() {
        let (_env, _id, client) = setup();
        client.reacquire_after_release();
    }

    #[test]
    fn reentrancy_lock_clears_after_guarded_call_reverts() {
        let (_env, _id, client) = setup();
        assert!(client.try_with_guard_fail().is_err());
        client.acquire_and_release();
    }

    #[test]
    fn callee_allowlist_registers_and_checks() {
        let (env, _id, client) = setup();
        let allowed = Address::generate(&env);
        client.register_and_check(&allowed);
    }

    #[test]
    #[should_panic(expected = "Error(Contract, #23)")]
    fn callee_allowlist_rejects_unregistered_address() {
        let (env, _id, client) = setup();
        let blocked = Address::generate(&env);
        client.assert_blocked(&blocked);
    }

    #[test]
    fn callee_allowlist_unregister_removes_member() {
        let (env, _id, client) = setup();
        let target = Address::generate(&env);
        assert!(!client.unregister(&target));
    }
}
