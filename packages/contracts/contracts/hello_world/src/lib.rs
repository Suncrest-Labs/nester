#![no_std]
use soroban_sdk::{contract, contractimpl, symbol_short, vec, Env, Symbol, Vec};

#[contract]
pub struct NesterContract;

#[contractimpl]
impl NesterContract {
    pub fn hello(env: Env, to: Symbol) -> Vec<Symbol> {
        vec![&env, symbol_short!("Hello"), to]
    }

    pub fn initiate_swap(env: Env, _swap_info: u128) -> Vec<Symbol> {
        vec![&env]
    }
}

/*
- deposit, initiate_swap,
*/
