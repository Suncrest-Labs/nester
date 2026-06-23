# Nester Soroban Contracts Workspace

Production-grade, scalable Cargo workspace for Soroban smart contracts supporting multi-contract architecture, shared libraries, and unified tooling.

## Workspace Structure

```
packages/contracts/
├── contracts/
│   ├── vault/              # Core vault contract for asset storage
│   ├── vault_token/        # Token contract for vault participation
│   ├── yield_registry/     # Registry for yield strategies
│   ├── allocation_strategy/ # Dynamic allocation logic
│   └── access_control/     # Role-based access control
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
Core contract managing asset deposits, withdrawals, and vault state. Interfaces with vault tokens and allocation strategies.

### Vault Token (`vault_token/`)
ERC-20-like token contract representing fractional ownership in the vault. Minted on deposits, burned on withdrawals.

### Yield Registry (`yield_registry/`)
Registry and metadata store for supported yield strategies. Tracks strategy parameters, yields, and performance metrics.

### Allocation Strategy (`allocation_strategy/`)
Dynamic allocation contract that determines how vault assets are distributed across registered yield strategies.

### Access Control (`access_control/`)
Role-based access control system for managing permissions across all contracts (admin, manager, user roles).

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

## References

- [Soroban Documentation](https://soroban.stellar.org/)
- [Soroban SDK Docs](https://docs.rs/soroban-sdk/)
- [Rust Edition 2021](https://doc.rust-lang.org/edition-guide/rust-2021/index.html)
