// ---------------------------------------------------------------------------
// Mock external-protocol contracts for adapter tests.
//
// These stand in for third-party yield venues (a Blend-style lending market
// and a Soroswap-style AMM pair) so adapter unit tests and the
// failure-isolation integration test can run without any real protocol code.
//
// Each mock lives in its own module: `#[contractimpl]` emits module-level
// symbols, so two contracts sharing a module would collide on `deposit`,
// `withdraw`, and friends.
// ---------------------------------------------------------------------------

pub mod blend_pool;
pub mod failing;
pub mod lending;
pub mod pool;

pub use blend_pool::{MockBlendPool, MockBlendPoolClient};
pub use failing::{MockFailingAdapter, MockFailingAdapterClient};
pub use lending::{MockLendingProtocol, MockLendingProtocolClient};
pub use pool::{MockAmmPool, MockAmmPoolClient};
