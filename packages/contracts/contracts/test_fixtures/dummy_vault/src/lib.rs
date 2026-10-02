//! Minimal real contract used only as a test fixture for `vault_factory`
//! (issue #816) and `goal_rewards` (issue #819). Its `initialize` accepts a
//! single boolean flag — when `true` it panics, which lets factory tests
//! exercise the atomicity guarantee (a failing `initialize` must leave
//! nothing deployed) against a genuinely-deployed WASM contract rather than
//! a native test double.
//!
//! `set_balance`/`get_balance` were added for goal_rewards' tests, which
//! need a factory-deployed (and therefore `is_known_vault`-recognised)
//! address that also answers the real vault interface's `get_balance(owner)`
//! view — goal_rewards cross-calls this to compute a fake-contribution-proof
//! bonus basis. Storage is a plain per-owner map; nothing here enforces a
//! real balance invariant, since this fixture exists purely to let a test
//! set an owner's "vault balance" to an arbitrary value.
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

    pub fn set_balance(env: Env, owner: Address, amount: i128) {
        env.storage().persistent().set(&owner, &amount);
    }

    pub fn get_balance(env: Env, owner: Address) -> i128 {
        env.storage().persistent().get(&owner).unwrap_or(0)
    }
}
