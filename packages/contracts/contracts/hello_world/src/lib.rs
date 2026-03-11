#![no_std]
use soroban_sdk::{contract, contractimpl, symbol_short, vec, Env, Symbol, Vec};

#[contract]
pub struct NesterContract;

#[contractimpl]
impl NesterContract {
    pub fn hello(env: Env, to: Symbol) -> Vec<Symbol> {
        vec![&env, symbol_short!("Hello"), to]
    }

    pub fn initiate_swap(env: Env, swap_info: SwapInfo) -> Vec<Symbol> {}

    pub 
}

pub struct SwapInfo {
    pub amount: u128,
    

}

/*
- deposit, initiate_swap,
*/

mod test;
