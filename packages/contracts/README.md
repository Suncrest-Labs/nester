# Nester Soroban Contracts Workspace

Production-grade, scalable Cargo workspace for Soroban smart contracts supporting multi-contract architecture, shared libraries, and unified tooling.

## Workspace Structure

```
packages/contracts/
├── contracts/
│   ├── vault/              # Core vault contract for asset storage
│   ├── vault_token/        # Token contract for vault participation
│   ├── vault_factory/      # Deterministic, registry-tracked vault deployment
│   ├── yield_registry/     # Registry for yield strategies
│   ├── allocation_strategy/ # Dynamic allocation logic
│   ├── access_control/     # Granular role-based access control
│   ├── referral/           # On-chain referral / deposit-incentive program
│   └── timelock/           # Mandatory delay for sensitive admin operations
├── libs/
│   ├── common/             # Shared error types, constants, storage patterns
│   └── test_utils/         # Test environment setup and helpers
├── Cargo.toml              # Workspace configuration
├── Makefile                # Build and deployment tooling
└── README.md               # This file
```

## Prerequisites

- Rust 1.70+ with `wasm32-unknown-unknown` target
- Soroban CLI (for deployment)
- `cargo-fmt` and `clippy` (included in standard Rust installation)

### Install WASM Target

```bash
rustup target add wasm32-unknown-unknown
```

## Quick Start

### Build All Contracts

```bash
make build
```

This compiles all contract crates to WebAssembly.

### Run Tests

```bash
make test
```

Executes all unit and integration tests across the workspace.

> **Important — vault-contract tests require a pre-built WASM artifact.**
> The `vault-contract` test suite depends on `vault-token-contract` being compiled
> to WebAssembly before the tests run. Without this artifact the tests will fail
> or be silently skipped.
>
> Build the artifact first, then run the vault-contract tests:
>
> ```bash
> # 1. Build vault_token.wasm (run from packages/contracts/)
> cargo build --release --target wasm32-unknown-unknown -p vault-token-contract
>
> # 2. Run vault-contract tests
> cargo test -p vault-contract
> ```
>
> CI handles this automatically — the `contracts` job builds `vault_token.wasm`
> before executing `cargo test -p vault-contract`.

### Format Code

```bash
make fmt
```

Applies Rust formatting standards via `cargo fmt`.

### Lint with Clippy

```bash
make clippy
```

Runs the Rust linter with strict warnings-as-errors enforcement.

### Clean Build Artifacts

```bash
make clean
```

Removes `target/` directory and build cache.

### Deploy to Testnet

```bash
make deploy-testnet
```

Deploys all contracts to Stellar testnet (requires proper network configuration).

## Development Workflow

### Adding a New Contract

1. Create a new directory under `contracts/`:
   ```bash
   mkdir -p contracts/my_contract/src
   ```

2. Create `Cargo.toml`:
   ```toml
   [package]
   name = "my-contract"
   version = "0.1.0"
   edition = "2021"
   publish = false

   [lib]
   crate-type = ["cdylib"]
   doctest = false

   [dependencies]
   soroban-sdk = { workspace = true }
   nester-common = { path = "../../libs/common" }

   [dev-dependencies]
   soroban-sdk = { workspace = true, features = ["testutils"] }
   nester-test-utils = { path = "../../libs/test_utils" }
   ```

3. Create `src/lib.rs` with your contract implementation:
   ```rust
   #![no_std]

   use soroban_sdk::{contract, contractimpl, Env};

   #[contract]
   pub struct MyContract;

   #[contractimpl]
   impl MyContract {
       pub fn init(env: Env) {
           // Initialize contract
       }
   }
   ```

4. Add to workspace `Cargo.toml` members:
   ```toml
   members = [
     "contracts/*",
     "libs/common",
     "libs/test_utils",
   ]
   ```

5. Build and test:
   ```bash
   cargo build --release --target wasm32-unknown-unknown
   cargo test --lib
   ```

### Using Shared Libraries

All contracts can import from `nester-common` and `nester-test-utils`:

```rust
use nester_common::{ContractError, constants::*, storage::*};
use nester_test_utils::{setup_test_env, assert_ok};
```

## Contract Descriptions

### Vault (`vault/`)
Core contract managing asset deposits, withdrawals, and vault state. Interfaces with vault tokens and allocation strategies. Protects itself with an autonomous, staged circuit breaker (`breaker.rs`) — share-price-move, yield-sanity, withdrawal-velocity, and source-failure conditions escalate a graded severity (`Normal` → `Throttled` → `DepositsHalted` → `FullHalt`); the emergency withdrawal queue works at every severity level, and recovery is staged and cooled-down. See [`SECURITY.md`](../../SECURITY.md#circuit-breaker-issue-817).

### Vault Token (`vault_token/`)
ERC-20-like token contract representing fractional ownership in the vault. Minted on deposits, burned on withdrawals.

### Yield Registry (`yield_registry/`)
Registry and metadata store for supported yield strategies. Tracks strategy parameters, yields, and performance metrics.

### Allocation Strategy (`allocation_strategy/`)
Dynamic allocation contract that determines how vault assets are distributed across registered yield strategies.

### Access Control (`access_control/`)
Granular role-based access control shared by every contract: `Admin`, `Operator`, `Manager`, `Guardian`, `Upgrader`, `Attester`, `FeeManager`, `RebalanceKeeper`, `Treasurer`, `VaultCreator`. Every role transfer is two-step (`transfer_role`/`accept_role`, cancellable) and roles can be time-bounded via `grant_role_until`. Full role model and the Guardian asymmetry guarantee — a Guardian can always make the protocol safer and never riskier — are documented in [`SECURITY.md`](../../SECURITY.md#on-chain-access-control-model-issue-820).

### Vault Factory (`vault_factory/`)
Deploys new vaults from a single governed WASM hash via the Soroban deployer, with deterministic, pre-computable addresses (`predict_vault_address`) and an on-chain registry (`is_nester_vault`, `get_vault`, `list_vaults`) so any integrator can distinguish a genuine Nester vault from a lookalike. Deployment and initialisation are atomic. Changing the WASM hash goes through the shared `timelock`.

### Referral (`referral/`)
Standalone on-chain referral program — deliberately kept out of the vault's hot path; the vault is the sole trusted caller of `accrue_reward`, mirroring the existing `treasury.receive_fees` trust pattern. Rewards accrue from the protocol's performance-fee slice on a referred user's yield — never from the user's own returns — and are bounded by minimum deposit/tenure gates, per-referrer caps, and a global program budget. See [`EVENTS.md`](EVENTS.md#referral-events-contract-symbol-referral--issue-818) for the full event surface.

## Architecture Principles

- **No Circular Dependencies**: Contracts only depend on shared libraries, not each other
- **Independent Compilation**: Each contract can be built and tested in isolation
- **Idiomatic Rust**: Follows Rust naming conventions and best practices
- **Soroban Compliance**: All code adheres to Soroban contract requirements
- **Minimal Comments**: Code is self-documenting; comments explain "why" not "what"

## Error Handling

All contracts use standardized error types from `nester_common::ContractError`:

```rust
use nester_common::ContractError;

pub enum ContractError {
    AlreadyInitialized,
    NotInitialized,
    Unauthorized,
    InsufficientBalance,
    InvalidAmount,
    StrategyNotFound,
    AllocationError,
    RoleNotFound,
    InvalidOperation,
}
```

## Storage Patterns

Storage keys are defined in `nester_common::storage`:

```rust
use nester_common::storage::*;

admin_key()           // Access control admin
balance_key(account)  // User balance storage
strategy_key(id)      // Strategy metadata
role_key(account, role) // Role assignments
initialized_key()     // Initialization flag
```

## Testing

The workspace includes test utilities in `libs/test_utils`:

```rust
#[cfg(test)]
mod tests {
    use nester_test_utils::*;
    use super::*;

    #[test]
    fn test_deposit() {
        let env = setup_test_env();
        // Test logic
    }
}
```

## Quality Standards

- ✅ All contracts compile to WASM without errors
- ✅ `make test` runs with 100% pass rate
- ✅ `make clippy` passes with zero warnings
- ✅ `make fmt` produces consistent formatting
- ✅ No circular dependency chains
- ✅ Scalable for new contracts without refactoring

## Next Steps

1. Implement contract-specific logic in each `src/lib.rs`
2. Add integration tests in `#[cfg(test)]` modules
3. Document external APIs in `lib.rs` comments
4. Run full QA suite: `make fmt && make clippy && make test && make build`
5. Deploy to testnet: `make deploy-testnet`

## Contract Upgrade Runbook

All upgradeable Nester contracts (`vault`, `yield_registry`, `allocation_strategy`, `treasury`) use a secure, timelock-governed upgrade mechanism.

### 1. Reproducible Build Process
Compile and optimize contract WASM binaries:
```bash
cargo build --release --target wasm32-unknown-unknown -p vault-contract
stellar contract optimize \
  --wasm ./target/wasm32-unknown-unknown/release/vault_contract.wasm \
  --wasm-out ./target/wasm32-unknown-unknown/release/vault_contract_opt.wasm
```

### 2. WASM Hash Computation & Verification
Compute the SHA-256 hash of the optimized WASM:
```bash
sha256sum ./target/wasm32-unknown-unknown/release/vault_contract_opt.wasm
```
Alternatively, install WASM to testnet to obtain the on-chain WASM hash:
```bash
stellar contract install --wasm ./target/wasm32-unknown-unknown/release/vault_contract_opt.wasm --source-account <ACCOUNT> --network testnet
```

### 3. Proposal Process
An account holding the `Upgrader` role proposes the upgrade:
- Minimum timelock delays:
  - Vault: 48 hours (`172,800` seconds)
  - Yield Registry: 48 hours (`172,800` seconds)
  - Allocation Strategy: 48 hours (`172,800` seconds)
  - Treasury: 7 days (`604_800` seconds)
```bash
stellar contract invoke --id <CONTRACT_ID> --source-account upgrader --network testnet -- propose_upgrade \
  --admin <UPGRADER_ADDRESS> \
  --new_wasm_hash <WASM_HASH> \
  --eta <FUTURE_TIMESTAMP>
```

### 4. Multisig Signing & Cancellation
If a proposal needs to be aborted before maturity, an `Upgrader` calls:
```bash
stellar contract invoke --id <CONTRACT_ID> --source-account upgrader --network testnet -- cancel_upgrade \
  --admin <UPGRADER_ADDRESS>
```

### 5. Execution & Migration
Once the ledger timestamp reaches `ETA`, execution is permissionless:
```bash
stellar contract invoke --id <CONTRACT_ID> --source-account any_relayer --network testnet -- execute_upgrade \
  --caller <CALLER_ADDRESS> \
  --wasm_hash <WASM_HASH>
```
Following upgrade execution, run explicit storage migration if schema changes occurred:
```bash
stellar contract invoke --id <CONTRACT_ID> --source-account admin --network testnet -- migrate
```

### 6. Storage Invariant & Rollback Considerations
- **Storage Invariant**: Existing storage keys may never be reused with different data types. New schema modifications must allocate new keys.
- **Rollbacks**: If an executed WASM contains a bug, a new upgrade proposal must be submitted with the previous known-good WASM hash and serve out the timelock delay. Emergency pause state remains available throughout to protect funds.

## References

- [Soroban Documentation](https://soroban.stellar.org/)
- [Soroban SDK Docs](https://docs.rs/soroban-sdk/)
- [Rust Edition 2021](https://doc.rust-lang.org/edition-guide/rust-2021/index.html)

