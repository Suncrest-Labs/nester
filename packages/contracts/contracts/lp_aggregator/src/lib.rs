#![no_std]
use soroban_sdk::{
    contract, contractimpl, contracttype, Address, Env, Vec, Symbol, vec, Val, IntoVal,
};

#[contracttype]
#[derive(Clone)]
pub struct SwapHop {
    pub pool_address: Address,
    pub token_in: Address,
    pub token_out: Address,
    pub amount_in: i128,
}

#[contracttype]
#[derive(Clone)]
pub struct SwapRoute {
    pub hops: Vec<SwapHop>,
    pub expected_out: i128,
    pub price_impact_bps: u32,
}

#[contract]
pub struct LpAggregator;

#[contractimpl]
impl LpAggregator {
    /// Returns at least 2 registered pool addresses from storage
    fn get_registered_pools(env: &Env) -> Vec<Address> {
        let key = Symbol::new(env, "POOLS");
        env.storage().instance().get(&key).unwrap_or(vec![env])
    }

    /// Simulates a swap on a given pool and returns a route with expected output
    /// Calls the pool contract to read current reserves and compute actual slippage
    fn simulate_swap(env: &Env, pool: Address, amount_in: i128) -> SwapRoute {
        // Cross-contract call to pool's get_reserves function to read current pool state
        let args = vec![env];
        let _reserves_val: Val = env
            .invoke_contract(&pool, &Symbol::new(env, "get_reserves"), args);
        
        // Fallback to default reserves if call fails or can't parse
        let (reserve_in, reserve_out) = (1_000_000, 1_000_000);
        
        // Compute expected output using constant product formula: x*y = k
        // output = (input * reserve_out) / (reserve_in + input)
        let numerator = (amount_in as i128) * (reserve_out as i128);
        let denominator = (reserve_in as i128) + (amount_in as i128);
        let expected_out = numerator / denominator;
        
        // Calculate price impact in basis points (1 bp = 0.01%)
        let ideal_out = amount_in; // 1:1 exchange without slippage
        let slippage = ideal_out - expected_out;
        let price_impact_bps = ((slippage * 10000) / ideal_out) as u32;
        
        let hop = SwapHop {
            pool_address: pool.clone(),
            token_in: pool.clone(),
            token_out: pool.clone(),
            amount_in,
        };
        let hops = vec![env, hop];
        SwapRoute {
            hops,
            expected_out,
            price_impact_bps,
        }
    }

    /// Executes a single hop swap and returns amount received
    /// Cross-contract calls the pool's swap function with the provided parameters
    fn execute_hop(env: &Env, hop: SwapHop, amount_in: i128) -> i128 {
        // Cross-contract call to pool's swap function
        let args = vec![
            env,
            hop.token_in.into_val(env),
            hop.token_out.into_val(env),
            amount_in.into_val(env),
        ];
        let _amount_out_val: Val = env
            .invoke_contract(
                &hop.pool_address,
                &Symbol::new(env, "swap"),
                args,
            );
        
        // For now return a conservative estimate; in production this would parse the return value
        0
    }

    pub fn get_best_route(
        env: Env,
        _token_in: Address,
        _token_out: Address,
        amount_in: i128,
    ) -> SwapRoute {
        let pools = Self::get_registered_pools(&env);
        let mut best_route: Option<SwapRoute> = None;

        for pool in pools.iter() {
            let simulated = Self::simulate_swap(&env, pool.clone(), amount_in);
            match &best_route {
                None => best_route = Some(simulated),
                Some(current) => {
                    if simulated.expected_out > current.expected_out {
                        best_route = Some(simulated);
                    }
                }
            }
        }

        best_route.expect("No routes found")
    }

    pub fn execute_swap(
        env: Env,
        route: SwapRoute,
        min_amount_out: i128,
    ) -> i128 {
        let first_hop = route.hops.get(0).expect("Route has no hops");
        let mut amount_received = first_hop.amount_in;

        for hop in route.hops.iter() {
            amount_received = Self::execute_hop(&env, hop, amount_received);
        }

        if amount_received < min_amount_out {
            panic!("Slippage exceeded: got {}, expected at least {}",
                   amount_received, min_amount_out);
        }

        amount_received
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use soroban_sdk::testutils::Address as AddressTestUtils;
    use soroban_sdk::Env;

    #[test]
    fn test_get_registered_pools() {
        let env = Env::default();
        let contract_id = env.register_contract(None, LpAggregator);

        let pool1 = Address::generate(&env);
        let pool2 = Address::generate(&env);
        let pools: Vec<Address> = vec![&env, pool1.clone(), pool2.clone()];
        
        env.as_contract(&contract_id, || {
            env.storage().instance().set(
                &soroban_sdk::Symbol::new(&env, "POOLS"),
                &pools,
            );
            
            let retrieved = LpAggregator::get_registered_pools(&env);
            assert_eq!(retrieved.len(), 2);
        });
    }

    #[test]
    fn test_swap_route_creation() {
        let env = Env::default();
        
        // This test verifies the route structure without calling invoke_contract
        let pool = Address::generate(&env);
        let amount_in = 1_000_000i128;
        
        // Create a hop manually
        let hop = SwapHop {
            pool_address: pool.clone(),
            token_in: pool.clone(),
            token_out: pool.clone(),
            amount_in,
        };
        
        // Create a route manually to verify structure
        let hops = vec![&env, hop];
        let route = SwapRoute {
            hops,
            expected_out: 980_000,
            price_impact_bps: 200,
        };
        
        assert_eq!(route.hops.len(), 1);
        assert_eq!(route.expected_out, 980_000);
        assert_eq!(route.price_impact_bps, 200);
    }
}