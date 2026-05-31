#![cfg(test)]

use super::*;
use soroban_sdk::{Env, testutils::Address as _};

#[test]
fn test_find_paths_2hop_and_3hop() {
    let env = Env::default();
    let contract_id = env.register_contract(None, LpAggregator);
    let client = LpAggregatorClient::new(&env, &contract_id);
    
    let token_in = Address::generate(&env);
    let token_out = Address::generate(&env);
    
    let paths = client.find_paths(&token_in, &token_out, &100, &3);
    assert_eq!(paths.len(), 2); // 2-hop and 3-hop
    
    let path_2hop = paths.get(0).unwrap();
    assert_eq!(path_2hop.path.len(), 3); // in -> int -> out
    
    let path_3hop = paths.get(1).unwrap();
    assert_eq!(path_3hop.path.len(), 4); // in -> int1 -> int2 -> out
}

#[test]
#[should_panic(expected = "Error(Contract, #1)")] // SlippageExceeded
fn test_slippage_exceeded_revert() {
    let env = Env::default();
    let contract_id = env.register_contract(None, LpAggregator);
    let client = LpAggregatorClient::new(&env, &contract_id);
    
    let mut path = Vec::new(&env);
    path.push_back(Address::generate(&env));
    path.push_back(Address::generate(&env));
    path.push_back(Address::generate(&env));
    
    // Expected output is amount_in * (path.len() - 1) = 100 * 2 = 200
    // If min_amount_out is 250, it should revert
    client.execute_path_payment(&path, &100, &250);
}

#[test]
fn test_successful_execution() {
    let env = Env::default();
    let contract_id = env.register_contract(None, LpAggregator);
    let client = LpAggregatorClient::new(&env, &contract_id);
    
    let mut path = Vec::new(&env);
    path.push_back(Address::generate(&env));
    path.push_back(Address::generate(&env));
    path.push_back(Address::generate(&env));
    
    let out = client.execute_path_payment(&path, &100, &150);
    assert_eq!(out, 200);
}
