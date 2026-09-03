//! Mock Blend lending pool.
//!
//! This mirrors Blend's real entry point rather than a convenient one. The
//! existing `MockLendingProtocol` exposes `deposit`/`withdraw`, which is the
//! shape `adapter_lending` assumes — so that adapter passes its tests while
//! reverting against a real Blend pool, which has no `deposit` function at
//! all. A mock that agrees with the adapter instead of the protocol certifies
//! nothing, so this one takes `submit(from, spender, to, requests)` and
//! returns `Positions`, exactly as the deployed contract does.
//!
//! Fidelity is deliberately limited to what the adapter depends on: request
//! dispatch, bToken accounting against an exchange index, the token pull on
//! supply, and the payout on withdraw. Interest accrual is driven manually by
//! `set_index_bps` so a test can make a position appreciate deterministically.

use soroban_sdk::{contract, contractimpl, contracttype, token, Address, Env, Map, Vec};

#[contracttype]
#[derive(Clone)]
pub struct Request {
    pub address: Address,
    pub amount: i128,
    pub request_type: u32,
}

#[contracttype]
#[derive(Clone)]
pub struct Positions {
    pub collateral: Map<u32, i128>,
    pub liabilities: Map<u32, i128>,
    pub supply: Map<u32, i128>,
}

#[contracttype]
#[derive(Clone)]
enum PoolKey {
    Token,
    /// Exchange index in bps: assets = units * index / 10_000.
    IndexBps,
    /// Reserve index this pool serves.
    ReserveIndex,
    /// Supplied bToken units per address.
    Supply(Address),
}

const INDEX_ONE: i128 = 10_000;
const REQUEST_TYPE_SUPPLY: u32 = 0;
const REQUEST_TYPE_WITHDRAW: u32 = 1;

#[contract]
pub struct MockBlendPool;

#[contractimpl]
impl MockBlendPool {
    pub fn initialize(env: Env, token: Address, reserve_index: u32) {
        env.storage().instance().set(&PoolKey::Token, &token);
        env.storage().instance().set(&PoolKey::IndexBps, &INDEX_ONE);
        env.storage()
            .instance()
            .set(&PoolKey::ReserveIndex, &reserve_index);
    }

    /// Moves the exchange index so supplied positions appreciate, standing in
    /// for interest accrual.
    pub fn set_index_bps(env: Env, index_bps: i128) {
        env.storage().instance().set(&PoolKey::IndexBps, &index_bps);
    }

    pub fn get_positions(env: Env, address: Address) -> Positions {
        Self::positions_for(&env, &address)
    }

    /// Blend's single value-moving entry point.
    pub fn submit(
        env: Env,
        from: Address,
        spender: Address,
        to: Address,
        requests: Vec<Request>,
    ) -> Positions {
        let token_addr: Address = env.storage().instance().get(&PoolKey::Token).unwrap();
        let index: i128 = env.storage().instance().get(&PoolKey::IndexBps).unwrap();
        let me = env.current_contract_address();

        for request in requests.iter() {
            let held: i128 = env
                .storage()
                .instance()
                .get(&PoolKey::Supply(from.clone()))
                .unwrap_or(0);

            if request.request_type == REQUEST_TYPE_SUPPLY {
                // The real pool pulls the tokens itself, which is why the
                // adapter has to approve it first.
                token::Client::new(&env, &token_addr).transfer_from(
                    &me,
                    &spender,
                    &me,
                    &request.amount,
                );
                let units = request.amount * INDEX_ONE / index;
                env.storage()
                    .instance()
                    .set(&PoolKey::Supply(from.clone()), &(held + units));
            } else if request.request_type == REQUEST_TYPE_WITHDRAW {
                let units = if request.amount > held { held } else { request.amount };
                let assets = units * index / INDEX_ONE;
                env.storage()
                    .instance()
                    .set(&PoolKey::Supply(from.clone()), &(held - units));
                token::Client::new(&env, &token_addr).transfer(&me, &to, &assets);
            }
        }

        Self::positions_for(&env, &from)
    }

    fn positions_for(env: &Env, address: &Address) -> Positions {
        let reserve: u32 = env
            .storage()
            .instance()
            .get(&PoolKey::ReserveIndex)
            .unwrap_or(0);
        let held: i128 = env
            .storage()
            .instance()
            .get(&PoolKey::Supply(address.clone()))
            .unwrap_or(0);

        let mut supply = Map::new(env);
        supply.set(reserve, held);
        Positions {
            collateral: Map::new(env),
            liabilities: Map::new(env),
            supply,
        }
    }
}
