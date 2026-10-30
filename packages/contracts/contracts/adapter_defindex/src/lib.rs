#![no_std]

//! DeFindex vault adapter.
//!
//! Deposits the core's USDC into a single-asset DeFindex vault and holds the
//! vault's shares. Units are those shares: `max_withdraw` returns the shares
//! held (read live from the vault's SEP-41 `balance`), `withdraw` redeems
//! shares, and `position_value` is the vault's own
//! `get_asset_amounts_per_shares(shares)[0]`, which DeFindex rounds down.
//!
//! Interface verified against `defindex-io/stellar-contracts`
//! `vault/src/lib.rs` (commit `529f7c50`) and the deployed testnet vault WASM
//! (hash `f345228d…16be`, the `defindex_vault` hash in that repo's
//! `public/testnet.contracts.json`):
//! - `deposit(amounts_desired: Vec<i128>, amounts_min: Vec<i128>, from: Address, invest: bool)`
//! - `withdraw(withdraw_shares: i128, min_amounts_out: Vec<i128>, from: Address) -> Vec<i128>`
//! - `get_asset_amounts_per_shares(vault_shares: i128) -> Vec<i128>`
//! - `balance(id: Address) -> i128`
//!
//! Testnet: `usdc_paltalabs_vault`
//! `CBMVK2JK6NTOT2O4HNQAIQFJY232BHKGLIMXDVQVHIIZKDACXDFZDWHN` (same file). Its
//! asset is Blend's testnet USDC
//! (`CAQCFVLOBK5GIULPNZRGATJJMIZL5BSP7X5YJVMGCPTUEPFM4AVSRCJU`), not the
//! Circle testnet USDC SAC, so on testnet the savings core must be deployed
//! with that asset to route to this vault.

soroban_sdk::contractmeta!(key = "name", val = "AdapterDefindex");
include!(concat!(env!("OUT_DIR"), "/contractmeta.rs"));
soroban_sdk::contractmeta!(key = "source_repo", val = "github.com/Suncrest-Labs/nester");

use nester_common::adapters::{AdapterApy, ApyConfidence, YieldAdapter};
use nester_common::{emit_event, ContractError};
use soroban_sdk::{
    auth::{ContractContext, InvokerContractAuthEntry, SubContractInvocation},
    contract, contractimpl, contracttype, panic_with_error, symbol_short, token, vec, Address, Env,
    IntoVal, Symbol, Val, Vec,
};

const ADAPTER: Symbol = symbol_short!("ADAPTER");
const DEPOSITED: Symbol = symbol_short!("DEPOSIT");
const WITHDRAWN: Symbol = symbol_short!("WITHDRAW");

#[contracttype]
#[derive(Clone, Debug)]
pub struct AdapterMovedEventData {
    pub amount: i128,
    pub units: i128,
}

#[contracttype]
#[derive(Clone)]
enum DataKey {
    /// Savings core: the only caller allowed to move value.
    Core,
    /// DeFindex vault contract, also its share token.
    Vault,
    /// The vault's single asset.
    Underlying,
}

#[contract]
pub struct DefindexAdapterContract;

fn read(env: &Env, key: &DataKey) -> Address {
    env.storage()
        .instance()
        .get(key)
        .unwrap_or_else(|| panic_with_error!(env, ContractError::NotInitialized))
}

/// Vault shares held by this adapter.
fn shares_held(env: &Env) -> i128 {
    env.invoke_contract(
        &read(env, &DataKey::Vault),
        &Symbol::new(env, "balance"),
        vec![env, env.current_contract_address().into_val(env)],
    )
}

#[contractimpl]
impl DefindexAdapterContract {
    /// `defindex_vault` must hold `underlying` as its only asset; any other
    /// vault fails every deposit (wrong asset count, or a transfer of a token
    /// the adapter did not authorize), so a misconfiguration cannot move funds.
    pub fn initialize(env: Env, core: Address, defindex_vault: Address, underlying: Address) {
        core.require_auth();
        if env.storage().instance().has(&DataKey::Core) {
            panic_with_error!(&env, ContractError::AlreadyInitialized);
        }
        env.storage().instance().set(&DataKey::Core, &core);
        env.storage()
            .instance()
            .set(&DataKey::Vault, &defindex_vault);
        env.storage()
            .instance()
            .set(&DataKey::Underlying, &underlying);
    }

    pub fn get_core(env: Env) -> Address {
        read(&env, &DataKey::Core)
    }

    pub fn get_defindex_vault(env: Env) -> Address {
        read(&env, &DataKey::Vault)
    }

    pub fn version(env: Env) -> soroban_sdk::String {
        soroban_sdk::String::from_str(&env, env!("CARGO_PKG_VERSION"))
    }
}

#[contractimpl]
impl YieldAdapter for DefindexAdapterContract {
    fn deposit(env: Env, from: Address, amount: i128, min_units_out: i128) -> i128 {
        from.require_auth();
        if from != read(&env, &DataKey::Core) {
            panic_with_error!(&env, ContractError::Unauthorized);
        }
        if amount <= 0 {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }

        let underlying = read(&env, &DataKey::Underlying);
        let vault = read(&env, &DataKey::Vault);
        let me = env.current_contract_address();

        // Authorized pull from the core; a revert below rolls it back.
        token::Client::new(&env, &underlying).transfer(&from, &me, &amount);

        // Units are the shares actually received. DeFindex's returned share
        // count includes the MINIMUM_LIQUIDITY it locks to itself on a
        // vault's first deposit, so it is not used.
        let before = shares_held(&env);

        // DeFindex's `deposit` pulls `amount` from `from` (this adapter) with
        // a token transfer it invokes itself. That call is nested below the
        // vault, so the adapter authorizes exactly it and nothing else. Invoker
        // authorizations cover only the next call, so this must directly
        // precede `deposit`.
        env.authorize_as_current_contract(vec![
            &env,
            InvokerContractAuthEntry::Contract(SubContractInvocation {
                context: ContractContext {
                    contract: underlying,
                    fn_name: Symbol::new(&env, "transfer"),
                    args: (me.clone(), vault.clone(), amount).into_val(&env),
                },
                sub_invocations: Vec::new(&env),
            }),
        ]);

        let _: Val = env.invoke_contract(
            &vault,
            &Symbol::new(&env, "deposit"),
            vec![
                &env,
                vec![&env, amount].into_val(&env),
                vec![&env, amount].into_val(&env),
                me.into_val(&env),
                true.into_val(&env),
            ],
        );
        let units = shares_held(&env) - before;
        if units < min_units_out {
            panic_with_error!(&env, ContractError::SlippageExceeded);
        }

        emit_event(
            &env,
            ADAPTER,
            DEPOSITED,
            from,
            AdapterMovedEventData { amount, units },
        );
        units
    }

    fn withdraw(env: Env, to: Address, units: i128, min_out: i128) -> i128 {
        read(&env, &DataKey::Core).require_auth();
        if units <= 0 {
            panic_with_error!(&env, ContractError::InvalidAmount);
        }
        if units > shares_held(&env) {
            panic_with_error!(&env, ContractError::InsufficientBalance);
        }

        let underlying = token::Client::new(&env, &read(&env, &DataKey::Underlying));
        let me = env.current_contract_address();
        let before = underlying.balance(&me);

        // The vault pays floor(total * units / supply) to `from`. min_out is
        // enforced on the measured amount below, so the vault's own minimum
        // is left at zero.
        let _: Val = env.invoke_contract(
            &read(&env, &DataKey::Vault),
            &Symbol::new(&env, "withdraw"),
            vec![
                &env,
                units.into_val(&env),
                vec![&env, 0i128].into_val(&env),
                me.into_val(&env),
            ],
        );
        let assets = underlying.balance(&me) - before;
        if assets < min_out {
            panic_with_error!(&env, ContractError::SlippageExceeded);
        }
        if assets > 0 {
            underlying.transfer(&me, &to, &assets);
        }

        emit_event(
            &env,
            ADAPTER,
            WITHDRAWN,
            to,
            AdapterMovedEventData {
                amount: assets,
                units,
            },
        );
        assets
    }

    fn position_value(env: Env, _owner: Address) -> i128 {
        let shares = shares_held(&env);
        if shares == 0 {
            return 0;
        }
        let amounts: Vec<i128> = env.invoke_contract(
            &read(&env, &DataKey::Vault),
            &Symbol::new(&env, "get_asset_amounts_per_shares"),
            vec![&env, shares.into_val(&env)],
        );
        amounts
            .get(0)
            .unwrap_or_else(|| panic_with_error!(&env, ContractError::InvalidOperation))
    }

    fn current_apy(_env: Env) -> AdapterApy {
        // DeFindex exposes no rate, and deriving one needs a checkpoint this
        // adapter does not keep. Consumers ignore apy_bps when Unavailable.
        AdapterApy {
            apy_bps: 0,
            confidence: ApyConfidence::Unavailable,
        }
    }

    fn underlying(env: Env) -> Address {
        read(&env, &DataKey::Underlying)
    }

    fn max_deposit(_env: Env) -> i128 {
        // DeFindex vaults have no deposit cap.
        i128::MAX
    }

    fn max_withdraw(env: Env) -> i128 {
        shares_held(&env)
    }
}

#[cfg(test)]
mod test;

#[cfg(test)]
mod core_test;

#[cfg(test)]
mod spec_test;
