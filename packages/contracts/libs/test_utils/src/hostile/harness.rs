extern crate std;

use nester_access_control::Role;
use soroban_sdk::{testutils::Address as _, token::StellarAssetClient, Address, Env, String};
use treasury_contract::{TreasuryContract, TreasuryContractClient};
use vault_contract::VaultContractClient;
use vault_token::{VaultTokenContract, VaultTokenContractClient};

use super::token::{register_reentrant_token, ReentrantTokenContractClient};
use super::yield_source::register_reentrant_yield_source;

pub struct HostileVaultHarness {
    pub env: Env,
    pub admin: Address,
    pub vault_id: Address,
    pub token_id: Address,
    pub treasury_id: Address,
    pub deposit_token_id: Address,
}

impl HostileVaultHarness {
    pub fn setup_with_reentrant_token() -> Self {
        let env = Env::default();
        env.mock_all_auths();

        let admin = Address::generate(&env);
        let attacker = Address::generate(&env);
        let vault_id = env.register_contract(None, vault_contract::VaultContract);
        let treasury_id = env.register_contract(None, TreasuryContract);
        let token_id = env.register_contract(None, VaultTokenContract);

        TreasuryContractClient::new(&env, &treasury_id).initialize(&admin, &vault_id);

        let deposit_token_id = register_reentrant_token(&env, &vault_id, &attacker);

        VaultContractClient::new(&env, &vault_id).initialize(
            &admin,
            &deposit_token_id,
            &token_id,
            &treasury_id,
        );

        VaultTokenContractClient::new(&env, &token_id).initialize(
            &vault_id,
            &String::from_str(&env, "Nester USDC Vault"),
            &String::from_str(&env, "nUSDC"),
            &7u32,
        );

        HostileVaultHarness {
            env,
            admin,
            vault_id,
            token_id,
            treasury_id,
            deposit_token_id,
        }
    }

    pub fn setup_with_reentrant_yield_sink() -> Self {
        let env = Env::default();
        env.mock_all_auths();

        let admin = Address::generate(&env);
        let attacker = Address::generate(&env);
        let vault_id = env.register_contract(None, vault_contract::VaultContract);
        let token_id = env.register_contract(None, VaultTokenContract);
        let token_admin = Address::generate(&env);

        let deposit_token_id = env
            .register_stellar_asset_contract_v2(token_admin)
            .address();

        let yield_sink_id = register_reentrant_yield_source(&env, &vault_id, &attacker);

        VaultContractClient::new(&env, &vault_id).initialize(
            &admin,
            &deposit_token_id,
            &token_id,
            &yield_sink_id,
        );

        VaultContractClient::new(&env, &vault_id).register_callee(&admin, &yield_sink_id);

        VaultTokenContractClient::new(&env, &token_id).initialize(
            &vault_id,
            &String::from_str(&env, "Nester USDC Vault"),
            &String::from_str(&env, "nUSDC"),
            &7u32,
        );

        HostileVaultHarness {
            env,
            admin,
            vault_id,
            token_id,
            treasury_id: yield_sink_id,
            deposit_token_id,
        }
    }

    pub fn vault(&self) -> VaultContractClient<'_> {
        VaultContractClient::new(&self.env, &self.vault_id)
    }

    pub fn mint_deposit_tokens(&self, user: &Address, amount: i128) {
        ReentrantTokenContractClient::new(&self.env, &self.deposit_token_id).mint(user, &amount);
    }

    pub fn mint_stellar_deposit_tokens(&self, user: &Address, amount: i128) {
        StellarAssetClient::new(&self.env, &self.deposit_token_id).mint(user, &amount);
    }

    pub fn grant_manager(&self) {
        self.vault()
            .grant_role(&self.admin, &self.admin, &Role::Manager);
    }
}
