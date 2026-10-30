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
//! supply, the payout on withdraw, and Blend v2's `get_reserve` (whose
//! `data.b_rate` is the same index at Blend's 1e12 scale). As in Blend, both
//! supply and withdraw request amounts are in the underlying asset: supply
//! mints bTokens rounded down, withdraw burns bTokens rounded up and caps at
//! the whole position. Interest accrual is driven manually by `set_index_bps`
//! so a test can make a position appreciate deterministically.

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

/// Blend v2 `ReserveConfig`. Only `index` and `decimals` are meaningful here.
#[contracttype]
#[derive(Clone)]
pub struct ReserveConfig {
    pub c_factor: u32,
    pub decimals: u32,
    pub enabled: bool,
    pub index: u32,
    pub l_factor: u32,
    pub max_util: u32,
    pub r_base: u32,
    pub r_one: u32,
    pub r_three: u32,
    pub r_two: u32,
    pub reactivity: u32,
    pub supply_cap: i128,
    pub util: u32,
}

/// Blend v2 `ReserveData`. `b_rate` converts bTokens to underlying at 1e12.
#[contracttype]
#[derive(Clone)]
pub struct ReserveData {
    pub b_rate: i128,
    pub b_supply: i128,
    pub backstop_credit: i128,
    pub d_rate: i128,
    pub d_supply: i128,
    pub ir_mod: i128,
    pub last_time: u64,
}

/// Blend v2 `get_reserve` return value.
#[contracttype]
#[derive(Clone)]
pub struct Reserve {
    pub asset: Address,
    pub config: ReserveConfig,
    pub data: ReserveData,
    pub scalar: i128,
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
/// Blend v2's `b_rate` scale (SCALAR_12) per index bps.
const B_RATE_PER_INDEX_BPS: i128 = 100_000_000;
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

    pub fn get_reserve(env: Env, asset: Address) -> Reserve {
        let index: i128 = env.storage().instance().get(&PoolKey::IndexBps).unwrap();
        let reserve_index: u32 = env
            .storage()
            .instance()
            .get(&PoolKey::ReserveIndex)
            .unwrap_or(0);
        Reserve {
            asset,
            config: ReserveConfig {
                c_factor: 0,
                decimals: 7,
                enabled: true,
                index: reserve_index,
                l_factor: 0,
                max_util: 0,
                r_base: 0,
                r_one: 0,
                r_three: 0,
                r_two: 0,
                reactivity: 0,
                supply_cap: i128::MAX,
                util: 0,
            },
            data: ReserveData {
                b_rate: index * B_RATE_PER_INDEX_BPS,
                b_supply: 0,
                backstop_credit: 0,
                d_rate: 1_000_000_000_000,
                d_supply: 0,
                ir_mod: 1_000_000_000_000,
                last_time: env.ledger().timestamp(),
            },
            scalar: 10_000_000,
        }
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
                // Blend: burn ceil(amount / rate); if that exceeds the
                // position, burn it all and pay floor(held * rate).
                let mut units = (request.amount * INDEX_ONE + index - 1) / index;
                let mut assets = request.amount;
                if units > held {
                    units = held;
                    assets = held * index / INDEX_ONE;
                }
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
