extern crate std;

use soroban_sdk::{contract, contractimpl, contracttype, symbol_short, testutils::Address as _, Address, Env};
use vault_contract::VaultContractClient;

#[contracttype]
#[derive(Clone)]
enum TokenKey {
    Balance(Address),
}

#[contract]
pub struct ReentrantTokenContract;

#[contractimpl]
impl ReentrantTokenContract {
    pub fn initialize(env: Env, admin: Address, vault: Address, attacker: Address) {
        env.storage().instance().set(&symbol_short!("admin"), &admin);
        env.storage().instance().set(&symbol_short!("vault"), &vault);
        env.storage().instance().set(&symbol_short!("att"), &attacker);
    }

    pub fn mint(env: Env, to: Address, amount: i128) {
        let admin: Address = env.storage().instance().get(&symbol_short!("admin")).unwrap();
        admin.require_auth();
        let key = TokenKey::Balance(to.clone());
        let balance: i128 = env.storage().persistent().get(&key).unwrap_or(0);
        env.storage()
            .persistent()
            .set(&key, &(balance + amount));
    }

    pub fn balance(env: Env, id: Address) -> i128 {
        env.storage()
            .persistent()
            .get(&TokenKey::Balance(id))
            .unwrap_or(0)
    }

    pub fn transfer(env: Env, from: Address, to: Address, amount: i128) {
        from.require_auth();
        let from_key = TokenKey::Balance(from.clone());
        let to_key = TokenKey::Balance(to.clone());
        let from_balance: i128 = env.storage().persistent().get(&from_key).unwrap_or(0);
        if from_balance < amount {
            panic!("insufficient balance");
        }
        let to_balance: i128 = env.storage().persistent().get(&to_key).unwrap_or(0);
        env.storage()
            .persistent()
            .set(&from_key, &(from_balance - amount));
        env.storage()
            .persistent()
            .set(&to_key, &(to_balance + amount));

        let vault: Address = env.storage().instance().get(&symbol_short!("vault")).unwrap();
        let attacker: Address = env.storage().instance().get(&symbol_short!("att")).unwrap();
        VaultContractClient::new(&env, &vault).withdraw(&attacker, &1_i128, &0_i128);
    }
}

pub fn register_reentrant_token(env: &Env, vault: &Address, attacker: &Address) -> Address {
    let admin = Address::generate(env);
    let id = env.register_contract(None, ReentrantTokenContract);
    ReentrantTokenContractClient::new(env, &id).initialize(&admin, vault, attacker);
    id
}
