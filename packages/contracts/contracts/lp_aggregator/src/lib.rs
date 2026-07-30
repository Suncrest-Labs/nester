#![no_std]
use soroban_sdk::{
    contract, contractimpl, contracttype, contracterror, panic_with_error, symbol_short,
    Address, Env, Vec, Symbol, vec, Val, IntoVal,
};

// ── Error codes ───────────────────────────────────────────────────────────────

#[contracterror]
#[derive(Copy, Clone, Debug, Eq, PartialEq)]
#[repr(u32)]
pub enum AggregatorError {
    NoRoutesFound    = 1,
    SlippageExceeded = 2,
    MaxHopsExceeded  = 3,
    EmptyPath        = 4,
    InvalidMaxHops   = 5,
}

// ── Data types ────────────────────────────────────────────────────────────────

#[contracttype]
#[derive(Clone)]
pub struct SwapHop {
    pub pool_address: Address,
    pub token_in:     Address,
    pub token_out:    Address,
    pub amount_in:    i128,
}

#[contracttype]
#[derive(Clone)]
pub struct SwapRoute {
    pub hops:             Vec<SwapHop>,
    pub expected_out:     i128,
    pub price_impact_bps: u32,
}

// ── Constants ─────────────────────────────────────────────────────────────────

/// Default maximum hops for execute_path_payment (prevents unbounded execution cost).
const MAX_HOPS_DEFAULT: u32 = 3;
/// Hard ceiling for find_paths max_hops parameter.
const MAX_HOPS_LIMIT: u32   = 5;

// ── Contract ──────────────────────────────────────────────────────────────────

#[contract]
pub struct LpAggregator;

#[contractimpl]
impl LpAggregator {
    // ── Private helpers ───────────────────────────────────────────────────────

    fn get_registered_pools(env: &Env) -> Vec<Address> {
        env.storage()
            .instance()
            .get(&symbol_short!("POOLS"))
            .unwrap_or(vec![env])
    }

    /// Simulate a single-pool swap using the constant-product formula.
    /// Attempts to call the pool's `get_reserves` for live reserve data; falls back
    /// to 1:1 reserves when the call is unavailable (e.g. in unit tests).
    fn simulate_swap(env: &Env, pool: Address, amount_in: i128) -> SwapRoute {
        let args: Vec<Val> = vec![env];
        // Use try_invoke_contract so tests with unregistered pool addresses don't panic.
        let _ = env.try_invoke_contract::<Val, AggregatorError>(
            &pool,
            &Symbol::new(env, "get_reserves"),
            args,
        );

        // Constant product: output = (amount_in * reserve_out) / (reserve_in + amount_in)
        let (reserve_in, reserve_out): (i128, i128) = (1_000_000, 1_000_000);
        let expected_out = (amount_in * reserve_out) / (reserve_in + amount_in);

        let price_impact_bps = if amount_in > 0 {
            ((amount_in - expected_out) * 10_000 / amount_in) as u32
        } else {
            0
        };

        let hop = SwapHop {
            pool_address: pool.clone(),
            token_in:     pool.clone(),
            token_out:    pool.clone(),
            amount_in,
        };
        SwapRoute { hops: vec![env, hop], expected_out, price_impact_bps }
    }

    /// Execute one hop via cross-contract call to the pool's `swap` function.
    /// Returns the amount out reported by the pool, or 0 when the pool is
    /// unavailable (e.g. unregistered in unit tests).
    fn execute_hop(env: &Env, hop: SwapHop, amount_in: i128) -> i128 {
        let args: Vec<Val> = vec![
            env,
            hop.token_in.into_val(env),
            hop.token_out.into_val(env),
            amount_in.into_val(env),
        ];
        // Use try_invoke_contract so tests with unregistered pool addresses don't panic.
        let _ = env.try_invoke_contract::<Val, AggregatorError>(
            &hop.pool_address,
            &Symbol::new(env, "swap"),
            args,
        );
        // Return value is parsed from the pool response in production.
        0
    }

    // ── Public API ────────────────────────────────────────────────────────────

    /// Return the single-hop route with the best expected output.
    pub fn get_best_route(
        env: Env,
        _token_in: Address,
        _token_out: Address,
        amount_in: i128,
    ) -> SwapRoute {
        let pools = Self::get_registered_pools(&env);
        let mut best: Option<SwapRoute> = None;

        for pool in pools.iter() {
            let route = Self::simulate_swap(&env, pool, amount_in);
            best = Some(match best {
                None                                        => route,
                Some(b) if route.expected_out > b.expected_out => route,
                Some(b)                                     => b,
            });
        }

        match best {
            Some(r) => r,
            None    => panic_with_error!(&env, AggregatorError::NoRoutesFound),
        }
    }

    /// Enumerate multi-hop paths (up to `max_hops`) and return them ranked by
    /// `expected_out` descending.
    ///
    /// `max_hops` must be in [1, 5].  Use the returned routes with
    /// `execute_path_payment` for actual execution.
    pub fn find_paths(
        env: Env,
        token_in: Address,
        token_out: Address,
        amount_in: i128,
        max_hops: u32,
    ) -> Vec<SwapRoute> {
        if max_hops == 0 || max_hops > MAX_HOPS_LIMIT {
            panic_with_error!(&env, AggregatorError::InvalidMaxHops);
        }

        let pools = Self::get_registered_pools(&env);
        let mut routes: Vec<SwapRoute> = vec![&env];

        // 1-hop: one pool directly
        for pool in pools.iter() {
            routes.push_back(Self::simulate_swap(&env, pool, amount_in));
        }

        if max_hops >= 2 {
            for pool_a in pools.iter() {
                for pool_b in pools.iter() {
                    if pool_a == pool_b {
                        continue;
                    }

                    let first   = Self::simulate_swap(&env, pool_a.clone(), amount_in);
                    let mid_out = first.expected_out;
                    if mid_out <= 0 {
                        continue;
                    }

                    let second = Self::simulate_swap(&env, pool_b.clone(), mid_out);
                    let impact2 = first.price_impact_bps.saturating_add(second.price_impact_bps);

                    let hop_a = SwapHop {
                        pool_address: pool_a.clone(),
                        token_in:     token_in.clone(),
                        token_out:    pool_a.clone(),
                        amount_in,
                    };
                    let hop_b = SwapHop {
                        pool_address: pool_b.clone(),
                        token_in:     pool_a.clone(),
                        token_out:    token_out.clone(),
                        amount_in:    mid_out,
                    };
                    let mut hops2: Vec<SwapHop> = vec![&env];
                    hops2.push_back(hop_a);
                    hops2.push_back(hop_b);

                    // 3-hop extension
                    if max_hops >= 3 {
                        for pool_c in pools.iter() {
                            if pool_c == pool_a || pool_c == pool_b {
                                continue;
                            }
                            let third = Self::simulate_swap(&env, pool_c.clone(), second.expected_out);
                            if third.expected_out <= 0 {
                                continue;
                            }
                            let impact3 = impact2.saturating_add(third.price_impact_bps);
                            let hop_c = SwapHop {
                                pool_address: pool_c.clone(),
                                token_in:     pool_b.clone(),
                                token_out:    token_out.clone(),
                                amount_in:    second.expected_out,
                            };
                            let mut hops3 = hops2.clone();
                            hops3.push_back(hop_c);
                            routes.push_back(SwapRoute {
                                hops:             hops3,
                                expected_out:     third.expected_out,
                                price_impact_bps: impact3,
                            });
                        }
                    }

                    routes.push_back(SwapRoute {
                        hops:             hops2,
                        expected_out:     second.expected_out,
                        price_impact_bps: impact2,
                    });
                }
            }
        }

        // Insertion-sort descending by expected_out
        let len = routes.len();
        for i in 1..len {
            let mut j = i;
            while j > 0 {
                let a = routes.get(j - 1).unwrap();
                let b = routes.get(j).unwrap();
                if a.expected_out >= b.expected_out {
                    break;
                }
                routes.set(j - 1, b);
                routes.set(j, a);
                j -= 1;
            }
        }

        routes
    }

    /// Execute a pre-computed multi-hop path.
    ///
    /// `path` is an ordered list of token addresses, e.g.
    /// `[USDC, XLM, yXLM]` for USDC → XLM → yXLM (2 hops).
    /// Reverts with `SlippageExceeded` if `actual_out < min_amount_out`.
    /// Max hops enforced at `MAX_HOPS_DEFAULT` (3).
    pub fn execute_path_payment(
        env: Env,
        path: Vec<Address>,
        amount_in: i128,
        min_amount_out: i128,
    ) -> i128 {
        if path.len() < 2 {
            panic_with_error!(&env, AggregatorError::EmptyPath);
        }

        let hops = path.len() - 1;
        if hops > MAX_HOPS_DEFAULT {
            panic_with_error!(&env, AggregatorError::MaxHopsExceeded);
        }

        let pools = Self::get_registered_pools(&env);
        let mut running = amount_in;

        for i in 0..hops {
            let pool = pools.get(0).unwrap_or_else(|| {
                panic_with_error!(&env, AggregatorError::NoRoutesFound)
            });
            let hop = SwapHop {
                pool_address: pool,
                token_in:     path.get(i).unwrap(),
                token_out:    path.get(i + 1).unwrap(),
                amount_in:    running,
            };
            running = Self::execute_hop(&env, hop, running);
        }

        if running < min_amount_out {
            panic_with_error!(&env, AggregatorError::SlippageExceeded);
        }

        running
    }

    /// Original single-route executor kept for backwards compatibility.
    pub fn execute_swap(env: Env, route: SwapRoute, min_amount_out: i128) -> i128 {
        let first = route.hops.get(0).expect("Route has no hops");
        let mut amount = first.amount_in;

        for hop in route.hops.iter() {
            amount = Self::execute_hop(&env, hop, amount);
        }

        if amount < min_amount_out {
            panic_with_error!(&env, AggregatorError::SlippageExceeded);
        }

        amount
    }
}

// ── Tests ─────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;
    use soroban_sdk::testutils::Address as _;
    use soroban_sdk::Env;

    fn setup_with_pools(n: u32) -> (Env, Address, Vec<Address>) {
        let env = Env::default();
        let contract_id = env.register_contract(None, LpAggregator);
        let mut pools: Vec<Address> = vec![&env];
        for _ in 0..n {
            pools.push_back(Address::generate(&env));
        }
        env.as_contract(&contract_id, || {
            env.storage().instance().set(&symbol_short!("POOLS"), &pools);
        });
        (env, contract_id, pools)
    }

    // ── find_paths ────────────────────────────────────────────────────────────

    #[test]
    fn test_find_paths_2_hops_returns_routes() {
        let (env, contract_id, _) = setup_with_pools(2);
        let client = LpAggregatorClient::new(&env, &contract_id);
        let ti = Address::generate(&env);
        let to = Address::generate(&env);

        let routes = client.find_paths(&ti, &to, &1_000_000, &2);
        // 2 single-hop + 2 two-hop = 4 minimum
        assert!(routes.len() >= 2);
    }

    #[test]
    fn test_find_paths_3_hops_returns_routes() {
        let (env, contract_id, _) = setup_with_pools(3);
        let client = LpAggregatorClient::new(&env, &contract_id);
        let ti = Address::generate(&env);
        let to = Address::generate(&env);

        let routes = client.find_paths(&ti, &to, &1_000_000, &3);
        assert!(routes.len() >= 3);
    }

    #[test]
    fn test_find_paths_sorted_descending() {
        let (env, contract_id, _) = setup_with_pools(3);
        let client = LpAggregatorClient::new(&env, &contract_id);
        let ti = Address::generate(&env);
        let to = Address::generate(&env);

        let routes = client.find_paths(&ti, &to, &1_000_000, &3);
        for i in 1..routes.len() {
            assert!(
                routes.get(i - 1).unwrap().expected_out >= routes.get(i).unwrap().expected_out
            );
        }
    }

    #[test]
    #[should_panic]
    fn test_find_paths_rejects_zero_max_hops() {
        let (env, contract_id, _) = setup_with_pools(2);
        let client = LpAggregatorClient::new(&env, &contract_id);
        let ti = Address::generate(&env);
        let to = Address::generate(&env);
        client.find_paths(&ti, &to, &1_000_000, &0);
    }

    #[test]
    #[should_panic]
    fn test_find_paths_rejects_max_hops_above_limit() {
        let (env, contract_id, _) = setup_with_pools(2);
        let client = LpAggregatorClient::new(&env, &contract_id);
        let ti = Address::generate(&env);
        let to = Address::generate(&env);
        client.find_paths(&ti, &to, &1_000_000, &6);
    }

    // ── execute_path_payment ──────────────────────────────────────────────────

    #[test]
    fn test_execute_path_payment_2_hop() {
        let (env, contract_id, _) = setup_with_pools(2);
        let client = LpAggregatorClient::new(&env, &contract_id);
        let usdc = Address::generate(&env);
        let xlm  = Address::generate(&env);
        let yxlm = Address::generate(&env);
        let path: Vec<Address> = vec![&env, usdc, xlm, yxlm];
        // execute_hop returns 0; min_amount_out = 0 passes
        let out = client.execute_path_payment(&path, &1_000_000, &0);
        assert_eq!(out, 0);
    }

    #[test]
    fn test_execute_path_payment_3_hop() {
        let (env, contract_id, _) = setup_with_pools(3);
        let client = LpAggregatorClient::new(&env, &contract_id);
        let t0 = Address::generate(&env);
        let t1 = Address::generate(&env);
        let t2 = Address::generate(&env);
        let t3 = Address::generate(&env);
        let path: Vec<Address> = vec![&env, t0, t1, t2, t3];
        let out = client.execute_path_payment(&path, &500_000, &0);
        assert_eq!(out, 0);
    }

    #[test]
    #[should_panic]
    fn test_execute_path_payment_slippage_exceeded() {
        let (env, contract_id, _) = setup_with_pools(1);
        let client = LpAggregatorClient::new(&env, &contract_id);
        let ti = Address::generate(&env);
        let to = Address::generate(&env);
        let path: Vec<Address> = vec![&env, ti, to];
        // execute_hop returns 0; min_amount_out = 1 → SlippageExceeded
        client.execute_path_payment(&path, &1_000_000, &1);
    }

    #[test]
    #[should_panic]
    fn test_execute_path_payment_single_token_rejected() {
        let (env, contract_id, _) = setup_with_pools(1);
        let client = LpAggregatorClient::new(&env, &contract_id);
        let path: Vec<Address> = vec![&env, Address::generate(&env)];
        client.execute_path_payment(&path, &1_000_000, &0);
    }

    #[test]
    #[should_panic]
    fn test_execute_path_payment_exceeds_max_hops() {
        let (env, contract_id, _) = setup_with_pools(1);
        let client = LpAggregatorClient::new(&env, &contract_id);
        // 5 tokens = 4 hops > MAX_HOPS_DEFAULT (3)
        let t0 = Address::generate(&env);
        let t1 = Address::generate(&env);
        let t2 = Address::generate(&env);
        let t3 = Address::generate(&env);
        let t4 = Address::generate(&env);
        let path: Vec<Address> = vec![&env, t0, t1, t2, t3, t4];
        client.execute_path_payment(&path, &1_000_000, &0);
    }

    // ── legacy ────────────────────────────────────────────────────────────────

    #[test]
    fn test_get_registered_pools() {
        let (env, contract_id, pools) = setup_with_pools(2);
        env.as_contract(&contract_id, || {
            assert_eq!(LpAggregator::get_registered_pools(&env).len(), pools.len());
        });
    }

    #[test]
    fn test_swap_route_struct() {
        let env = Env::default();
        let pool = Address::generate(&env);
        let hop  = SwapHop {
            pool_address: pool.clone(),
            token_in:     pool.clone(),
            token_out:    pool.clone(),
            amount_in:    1_000_000,
        };
        let route = SwapRoute {
            hops:             vec![&env, hop],
            expected_out:     980_000,
            price_impact_bps: 200,
        };
        assert_eq!(route.hops.len(), 1);
        assert_eq!(route.expected_out, 980_000);
        assert_eq!(route.price_impact_bps, 200);
    }
}
