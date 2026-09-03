//! Mock Soroswap router.
//!
//! Split from the pair mock because `#[contractimpl]` emits module-level
//! symbols: two contracts in one module collide on `initialize`.

use soroban_sdk::{contract, contractimpl, contracttype, token, Address, Env, Vec};

use super::soroswap_pair::MockSoroswapPairClient;

#[contracttype]
#[derive(Clone)]
enum RouterKey {
    Pair,
}

#[contract]
pub struct MockSoroswapRouter;

#[contractimpl]
impl MockSoroswapRouter {
    pub fn initialize(env: Env, pair: Address) {
        env.storage().instance().set(&RouterKey::Pair, &pair);
    }

    pub fn add_liquidity(
        env: Env,
        token_a: Address,
        token_b: Address,
        amount_a_desired: i128,
        amount_b_desired: i128,
        _amount_a_min: i128,
        _amount_b_min: i128,
        to: Address,
        _deadline: u64,
    ) -> (i128, i128, i128) {
        let pair: Address = env.storage().instance().get(&RouterKey::Pair).unwrap();

        let minted = MockSoroswapPairClient::new(&env, &pair).mint_for(
            &to,
            &amount_a_desired,
            &amount_b_desired,
        );
        (amount_a_desired, amount_b_desired, minted)
    }

    pub fn remove_liquidity(
        env: Env,
        token_a: Address,
        token_b: Address,
        liquidity: i128,
        _amount_a_min: i128,
        _amount_b_min: i128,
        to: Address,
        _deadline: u64,
    ) -> (i128, i128) {
        let pair: Address = env.storage().instance().get(&RouterKey::Pair).unwrap();
        let (amount_a, amount_b) =
            MockSoroswapPairClient::new(&env, &pair).burn_for(&to, &liquidity);

        let pair_client = MockSoroswapPairClient::new(&env, &pair);
        pair_client.pay_out(&token_a, &to, &amount_a);
        pair_client.pay_out(&token_b, &to, &amount_b);
        (amount_a, amount_b)
    }

    pub fn swap_exact_tokens_for_tokens(
        env: Env,
        amount_in: i128,
        amount_out_min: i128,
        path: Vec<Address>,
        to: Address,
        _deadline: u64,
    ) -> Vec<i128> {
        let pair: Address = env.storage().instance().get(&RouterKey::Pair).unwrap();
        let token_in = path.first().unwrap();
        let token_out = path.last().unwrap();
        let pair_client = MockSoroswapPairClient::new(&env, &pair);
        let zero_for_one = pair_client.token_0() == token_in;

        let out = pair_client.swap_reserves(&zero_for_one, &amount_in);
        if out < amount_out_min {
            panic!("insufficient output amount");
        }

        pair_client.pay_out(&token_out, &to, &out);

        soroban_sdk::vec![&env, amount_in, out]
    }
}
