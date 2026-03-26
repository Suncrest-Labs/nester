#![no_std]

use soroban_sdk::{contract, contractimpl, contracttype, symbol_short, Address, Env};

use nester_access_control::{AccessControl, Role};
use nester_common::ContractError;

// ---------------------------------------------------------------------------
// Storage
// ---------------------------------------------------------------------------

#[contracttype]
#[derive(Clone)]
enum DataKey {
    Paused,
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
    /// Must be called once before any other function.
    pub fn initialize(env: Env, admin: Address) {
        AccessControl::initialize(&env, &admin);
        env.storage().instance().set(&DataKey::Paused, &false);
    }

    // -----------------------------------------------------------------------
    // Admin operations
    // -----------------------------------------------------------------------

    /// Pause all vault operations. Requires [`Role::Admin`].
    pub fn pause(env: Env, caller: Address) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);
        env.storage().instance().set(&DataKey::Paused, &true);
        env.events().publish((symbol_short!("paused"), caller), ());
    }

    /// Resume vault operations. Requires [`Role::Admin`].
    pub fn unpause(env: Env, caller: Address) {
        caller.require_auth();
        AccessControl::require_role(&env, &caller, Role::Admin);
        env.storage().instance().set(&DataKey::Paused, &false);
        env.events()
            .publish((symbol_short!("unpaused"), caller), ());
    }

    /// Grant `role` to `grantee`. Requires caller to be an Admin.
    pub fn grant_role(env: Env, grantor: Address, grantee: Address, role: Role) {
        AccessControl::grant_role(&env, &grantor, &grantee, role);
    }

    /// Revoke `role` from `target`. Requires caller to be an Admin.
    pub fn revoke_role(env: Env, revoker: Address, target: Address, role: Role) {
        AccessControl::revoke_role(&env, &revoker, &target, role);
    }

    /// Propose an admin transfer (step 1). Requires caller to be an Admin.
    pub fn transfer_admin(env: Env, current_admin: Address, new_admin: Address) {
        AccessControl::transfer_admin(&env, &current_admin, &new_admin);
    }

    /// Accept a proposed admin transfer (step 2). Caller must be the pending new admin.
    pub fn accept_admin(env: Env, new_admin: Address) {
        AccessControl::accept_admin(&env, &new_admin);
    }

    // -----------------------------------------------------------------------
    // Core vault operations
    // -----------------------------------------------------------------------

    /// Deposit funds into the vault.
    pub fn deposit(env: Env) {
        Self::require_not_paused(&env);
        // TODO: deposit logic
    }

    /// Withdraw funds from the vault.
    pub fn withdraw(env: Env) {
        Self::require_not_paused(&env);
        // TODO: withdrawal logic
    }

    /// Return the vault balance.
    pub fn balance(_env: Env) -> u64 {
        // TODO: balance logic
        0
    }

    // -----------------------------------------------------------------------
    // Helpers
    // -----------------------------------------------------------------------

    pub fn is_paused(env: Env) -> bool {
        env.storage()
            .instance()
            .get(&DataKey::Paused)
            .unwrap_or(false)
    }

    fn require_not_paused(env: &Env) {
        let paused: bool = env
            .storage()
            .instance()
            .get(&DataKey::Paused)
            .unwrap_or(false);
        if paused {
            soroban_sdk::panic_with_error!(env, ContractError::InvalidOperation);
        }
    }
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    extern crate std;

    use soroban_sdk::{contract, contractimpl, testutils::Address as _, Address, Env};

    use super::{VaultContract, VaultContractClient};

    #[contract]
    struct VaultObserverContract;

    #[contractimpl]
    impl VaultObserverContract {
        pub fn pause_target(env: Env, target: Address, caller: Address) {
            caller.require_auth();
            let client = VaultContractClient::new(&env, &target);
            client.pause(&caller);
        }

        pub fn is_target_paused(env: Env, target: Address) -> bool {
            let client = VaultContractClient::new(&env, &target);
            client.is_paused()
        }
    }

    fn setup() -> (Env, soroban_sdk::Address) {
        let env = Env::default();
        env.mock_all_auths();
        let admin = soroban_sdk::Address::generate(&env);
        let contract_id = env.register_contract(None, VaultContract);
        let client = VaultContractClient::new(&env, &contract_id);
        client.initialize(&admin);
        (env, admin)
    }

    #[test]
    fn vault_initializes_and_is_not_paused() {
        let (env, admin) = setup();
        let contract_id = env.register_contract(None, VaultContract);
        let client = VaultContractClient::new(&env, &contract_id);
        client.initialize(&admin);
        assert!(!client.is_paused());
    }

    #[test]
    fn admin_can_pause_and_unpause() {
        let env = Env::default();
        env.mock_all_auths();
        let admin = soroban_sdk::Address::generate(&env);
        let contract_id = env.register_contract(None, VaultContract);
        let client = VaultContractClient::new(&env, &contract_id);
        client.initialize(&admin);

        client.pause(&admin);
        assert!(client.is_paused());

        client.unpause(&admin);
        assert!(!client.is_paused());
    }

    #[test]
    fn pause_and_unpause_are_idempotent() {
        let env = Env::default();
        env.mock_all_auths();
        let admin = soroban_sdk::Address::generate(&env);
        let contract_id = env.register_contract(None, VaultContract);
        let client = VaultContractClient::new(&env, &contract_id);
        client.initialize(&admin);

        client.pause(&admin);
        client.pause(&admin);
        assert!(client.is_paused());

        client.unpause(&admin);
        client.unpause(&admin);
        assert!(!client.is_paused());
    }

    #[test]
    #[should_panic]
    fn non_admin_cannot_pause() {
        let env = Env::default();
        env.mock_all_auths();
        let admin = soroban_sdk::Address::generate(&env);
        let outsider = soroban_sdk::Address::generate(&env);
        let contract_id = env.register_contract(None, VaultContract);
        let client = VaultContractClient::new(&env, &contract_id);
        client.initialize(&admin);
        client.pause(&outsider);
    }

    #[test]
    #[should_panic]
    fn deposit_fails_when_paused() {
        let env = Env::default();
        env.mock_all_auths();
        let admin = soroban_sdk::Address::generate(&env);
        let contract_id = env.register_contract(None, VaultContract);
        let client = VaultContractClient::new(&env, &contract_id);
        client.initialize(&admin);
        client.pause(&admin);
        client.deposit(); // must panic
    }

    #[test]
    #[should_panic]
    fn withdraw_fails_when_paused() {
        let env = Env::default();
        env.mock_all_auths();
        let admin = soroban_sdk::Address::generate(&env);
        let contract_id = env.register_contract(None, VaultContract);
        let client = VaultContractClient::new(&env, &contract_id);
        client.initialize(&admin);
        client.pause(&admin);
        client.withdraw();
    }

    #[test]
    fn cross_contract_pause_state_is_visible() {
        let env = Env::default();
        env.mock_all_auths();
        let admin = soroban_sdk::Address::generate(&env);
        let vault_id = env.register_contract(None, VaultContract);
        let observer_id = env.register_contract(None, VaultObserverContract);

        let vault = VaultContractClient::new(&env, &vault_id);
        let observer = VaultObserverContractClient::new(&env, &observer_id);

        vault.initialize(&admin);
        assert!(!observer.is_target_paused(&vault_id));

        vault.pause(&admin);
        assert!(observer.is_target_paused(&vault_id));
    }

    #[test]
    fn cross_contract_pause_requires_admin_in_target_contract() {
        let env = Env::default();
        env.mock_all_auths();
        let admin = soroban_sdk::Address::generate(&env);
        let outsider = soroban_sdk::Address::generate(&env);
        let vault_id = env.register_contract(None, VaultContract);
        let observer_id = env.register_contract(None, VaultObserverContract);

        let vault = VaultContractClient::new(&env, &vault_id);
        let observer = VaultObserverContractClient::new(&env, &observer_id);

        vault.initialize(&admin);
        observer.pause_target(&vault_id, &admin);
        assert!(vault.is_paused());

        vault.unpause(&admin);
        let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
            observer.pause_target(&vault_id, &outsider);
        }));

        assert!(result.is_err());
        assert!(!vault.is_paused());
    }
}
