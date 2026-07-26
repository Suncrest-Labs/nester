//! Yield source adapter interface.
//!
//! One protocol-agnostic trait the vault understands. Each external yield
//! venue (Blend lending pool, Soroswap pair, …) gets a small adapter contract
//! implementing this trait, translating to/from the venue's own interface.
//! Adding a third protocol is "write one small adapter contract" — zero vault
//! changes.
//!
//! Adapters are stateless with respect to depositors: they hold a single
//! aggregate protocol position on behalf of the vault only and never track
//! individual users. They are the least-trusted contracts in the system
//! (they talk to third-party protocol code), so they must stay small and
//! auditable, and callers must treat every adapter call as fallible.

use soroban_sdk::{contractclient, contracttype, Address, Env};

/// Confidence in the APY figure reported by an adapter.
///
/// `Unavailable` is deliberately distinct from a zero APY: a derived APY over
/// a short or new position is noise, and feeding noise into auto-rebalancing
/// churns the vault into fee oblivion. Consumers must ignore the `apy_bps`
/// field whenever confidence is `Unavailable`.
#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum ApyConfidence {
    /// Rate read directly from the protocol (authoritative).
    ProtocolReported,
    /// Rate derived from observed position growth over a sufficient window.
    Derived,
    /// No meaningful rate is available yet — treat APY as unknown, not zero.
    Unavailable,
}

/// APY reading returned by [`YieldAdapter::current_apy`].
#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AdapterApy {
    /// Annualized yield in basis points. Meaningless when
    /// `confidence == Unavailable`.
    pub apy_bps: u32,
    pub confidence: ApyConfidence,
}

/// Minimum protocol-agnostic interface every yield source adapter implements.
///
/// Every value-moving function takes a minimum-output parameter so callers
/// are protected from slippage/manipulation, consistent with the rest of the
/// codebase (`SlippageExceeded`).
#[contractclient(name = "YieldAdapterClient")]
pub trait YieldAdapter {
    /// Deposit `amount` of the underlying asset into the protocol.
    /// Returns position units received. Reverts with `SlippageExceeded`
    /// if units received would be below `min_units_out`.
    fn deposit(env: Env, from: Address, amount: i128, min_units_out: i128) -> i128;

    /// Withdraw `units` of the position, sending underlying to `to`.
    /// Returns assets received. Reverts with `SlippageExceeded` if assets
    /// received would be below `min_out`.
    fn withdraw(env: Env, to: Address, units: i128, min_out: i128) -> i128;

    /// Current asset-denominated value of the adapter's aggregate position.
    fn position_value(env: Env, owner: Address) -> i128;

    /// Current APY with a confidence indicator. See [`AdapterApy`].
    fn current_apy(env: Env) -> AdapterApy;

    /// The underlying asset this adapter accepts and pays out.
    fn underlying(env: Env) -> Address;

    /// Protocol-side deposit ceiling right now. 0 means no capacity.
    fn max_deposit(env: Env) -> i128;

    /// Protocol-side withdrawable maximum right now (in position units).
    fn max_withdraw(env: Env) -> i128;
}
