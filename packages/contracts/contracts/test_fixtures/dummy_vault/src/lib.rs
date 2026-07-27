//! Minimal real contract used only as a test fixture for `vault_factory`
//! (issue #816). Its `initialize` accepts a single boolean flag — when
//! `true` it panics, which lets factory tests exercise the atomicity
//! guarantee (a failing `initialize` must leave nothing deployed) against a
//! genuinely-deployed WASM contract rather than a native test double.
#![no_std]

use soroban_sdk::{contract, contractimpl, Address, Env};

#[contract]
pub struct DummyVault;

#[contractimpl]
impl DummyVault {
    pub fn initialize(env: Env, admin: Address, should_fail: bool) {
        if should_fail {
            panic!("dummy vault init failure (test fixture)");
        }
        env.storage().instance().set(&0u32, &admin);
    }

    pub fn get_admin(env: Env) -> Address {
        env.storage().instance().get(&0u32).unwrap()
    }
}
