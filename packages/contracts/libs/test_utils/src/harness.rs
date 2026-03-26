// ---------------------------------------------------------------------------
// Nester multi-contract test harness
//
// Deploys all five Nester contracts into a single Soroban test environment
// and initialises them with the correct cross-references so integration tests
// can exercise the full protocol without any off-chain scaffolding.
//
// Usage
// -----
// ```rust
// let h = NesterHarness::setup();
// let user = h.create_user();
// h.registry().register_source(&h.admin, &symbol_short!("aave"), ...);
// ```
// ---------------------------------------------------------------------------

extern crate std;

use soroban_sdk::{testutils::Address as _, Address, Env, String};

use allocation_strategy_contract::{AllocationStrategyContract, AllocationStrategyContractClient};
use vault_contract::{VaultContract, VaultContractClient};
use vault_token::{VaultTokenContract, VaultTokenContractClient};
use yield_registry::{YieldRegistryContract, YieldRegistryContractClient};

/// Fully deployed Nester protocol in a single Soroban test environment.
///
/// Clients are created on demand via the `vault()`, `token()`, `registry()`,
/// and `strategy()` methods so the struct can own the `Env` without running
/// into self-referential lifetime issues.
pub struct NesterHarness {
    /// The shared in-process test environment.
    pub env: Env,
    /// The initial admin address — holds the Admin role on every contract.
    pub admin: Address,

    /// On-chain ID of the deployed Vault contract.
    pub vault_id: Address,
    /// On-chain ID of the deployed VaultToken contract.
    pub token_id: Address,
    /// On-chain ID of the deployed YieldRegistry contract.
    pub registry_id: Address,
    /// On-chain ID of the deployed AllocationStrategy contract.
    pub strategy_id: Address,
}

impl NesterHarness {
    /// Deploy and initialise all Nester contracts in a fresh test environment.
    ///
    /// Initialisation order:
    /// 1. `VaultContract` — AccessControl bootstrapped with `admin`.
    /// 2. `VaultTokenContract` — vault address stored as sole minter/burner.
    /// 3. `YieldRegistryContract` — AccessControl bootstrapped with `admin`.
    /// 4. `AllocationStrategyContract` — registry address stored for source
    ///    validation; AccessControl bootstrapped with `admin`.
    pub fn setup() -> Self {
        let env = Env::default();
        env.mock_all_auths();

        let admin = Address::generate(&env);

        // Register contracts (allocates an on-chain address for each).
        let vault_id    = env.register_contract(None, VaultContract);
        let token_id    = env.register_contract(None, VaultTokenContract);
        let registry_id = env.register_contract(None, YieldRegistryContract);
        let strategy_id = env.register_contract(None, AllocationStrategyContract);

        // Initialise in dependency order.
        VaultContractClient::new(&env, &vault_id).initialize(&admin);

        VaultTokenContractClient::new(&env, &token_id).initialize(
            &vault_id,
            &String::from_str(&env, "Nester USDC Vault"),
            &String::from_str(&env, "nUSDC"),
            &7u32,
        );

        YieldRegistryContractClient::new(&env, &registry_id).initialize(&admin);

        // Strategy needs the registry address so it can validate sources via
        // cross-contract calls in `set_weights`.
        AllocationStrategyContractClient::new(&env, &strategy_id)
            .initialize(&admin, &registry_id);

        NesterHarness {
            env,
            admin,
            vault_id,
            token_id,
            registry_id,
            strategy_id,
        }
    }

    // -----------------------------------------------------------------------
    // Client accessors (short-lived borrows from &self)
    // -----------------------------------------------------------------------

    /// Return a fresh client handle for the Vault contract.
    pub fn vault(&self) -> VaultContractClient<'_> {
        VaultContractClient::new(&self.env, &self.vault_id)
    }

    /// Return a fresh client handle for the VaultToken contract.
    pub fn token(&self) -> VaultTokenContractClient<'_> {
        VaultTokenContractClient::new(&self.env, &self.token_id)
    }

    /// Return a fresh client handle for the YieldRegistry contract.
    pub fn registry(&self) -> YieldRegistryContractClient<'_> {
        YieldRegistryContractClient::new(&self.env, &self.registry_id)
    }

    /// Return a fresh client handle for the AllocationStrategy contract.
    pub fn strategy(&self) -> AllocationStrategyContractClient<'_> {
        AllocationStrategyContractClient::new(&self.env, &self.strategy_id)
    }

    // -----------------------------------------------------------------------
    // Test helpers
    // -----------------------------------------------------------------------

    /// Generate a fresh user address.
    pub fn create_user(&self) -> Address {
        Address::generate(&self.env)
    }

    /// Mint `amount` vault-share tokens to `user` by calling
    /// `VaultToken::mint_for_deposit`.  Useful for seeding test balances
    /// without going through the (stub) Vault deposit flow.
    pub fn seed_token_balance(&self, user: &Address, amount: i128) {
        self.token().mint_for_deposit(user, &amount);
    }
}
