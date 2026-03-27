# Nester Smart-Contract Testing Guide

This document describes the test strategy for the Nester Soroban contract
workspace.

---

## Test types

| Type | Location | Command |
|------|----------|---------|
| Unit tests | `contracts/*/src/` and `contracts/*/src/test.rs` | `make test` |
| Integration tests | `tests/integration/src/integration/mod.rs` | `make integration-test` |

---

## Running tests

```bash
# All unit tests
make test

# Multi-contract integration tests only
make integration-test

# Everything (unit + integration)
cargo test --lib
```

---

## Unit tests

Each contract has its own inline `#[cfg(test)]` block or a companion
`test.rs` module. Unit tests deploy only the contract under test and verify
its behaviour in isolation using Soroban's in-process test environment
(`soroban_sdk::Env::default()`).

### Contracts with unit tests

| Contract | Test file |
|----------|-----------|
| `vault` | `contracts/vault/src/lib.rs` (inline) |
| `vault_token` | `contracts/vault_token/src/test.rs` |
| `yield_registry` | `contracts/yield_registry/src/test.rs` |
| `allocation_strategy` | `contracts/allocation_strategy/src/test.rs` |
| `access_control` | `contracts/access_control/src/test.rs` |

---

## Integration tests

Multi-contract tests live in the `nester-integration-tests` crate
(`tests/integration/`). They use the `NesterHarness` helper from
`libs/test_utils` to deploy all contracts in a single environment with correct
cross-references.

### `NesterHarness`

`NesterHarness::setup()` deploys all five contracts and initialises them in
dependency order:

1. `VaultContract` — AccessControl bootstrapped with a shared admin.
2. `VaultTokenContract` — vault address stored as sole minter/burner.
3. `YieldRegistryContract` — AccessControl bootstrapped with the same admin.
4. `AllocationStrategyContract` — registry address stored so `set_weights` can
   validate sources via cross-contract calls.

Client handles are available via `h.vault()`, `h.token()`, `h.registry()`,
and `h.strategy()`.

### Integration scenarios

| Scenario | Test function | What it validates |
|----------|---------------|-------------------|
| 1 | `all_contracts_initialise_cleanly` | All five contracts deploy and initialise without error |
| 2 | `strategy_set_weights_validates_sources_via_registry` | `set_weights` performs a live cross-contract call to the registry to confirm each source is active |
| 3 | `strategy_rejects_weights_for_unregistered_source` | Cross-contract validation rejects unknown source IDs |
| 4 | `strategy_rejects_weights_for_paused_source` | Cross-contract validation rejects paused (inactive) sources |
| 5 | `calculate_allocation_distributes_total_proportionally` | Allocation math distributes a total correctly across sources after cross-contract weight validation |
| 6 | `calculate_allocation_assigns_remainder_to_highest_weight_source` | Rounding remainder is fully assigned (no funds lost) |
| 7 | `admin_can_grant_operator_who_can_set_weights` | Admin grants Operator role; operator can call `set_weights` |
| 8 | `non_operator_cannot_set_weights` | Unauthorised address is rejected by `set_weights` |
| 9 | `deposit_is_rejected_when_vault_is_paused` | `deposit()` panics when vault is paused |
| 10 | `withdraw_is_rejected_when_vault_is_paused` | `withdraw()` panics when vault is paused |
| 11 | `vault_accepts_deposit_after_unpause` | Unpause restores deposit functionality |
| 12 | `non_admin_cannot_pause_vault` | Non-admin address is rejected by `pause()` |
| 13 | `two_users_receive_proportional_yield_on_withdrawal` | Two users share yield proportionally via VaultToken share math |
| 14 | `late_depositor_does_not_capture_prior_yield` | A user who deposits after yield accrues does not retroactively earn that yield |

---

## Test utilities (`libs/test_utils`)

| Module | Purpose |
|--------|---------|
| `env` | `setup_test_env()` — creates a default `Env` |
| `assertions` | `assert_error`, `assert_ok`, `assert_eq_balance` helpers |
| `harness` | `NesterHarness` — full-protocol deployment harness for integration tests |

---

## Adding new integration scenarios

1. Open `tests/integration/src/integration/mod.rs`.
2. Add a `#[test]` function that calls `NesterHarness::setup()`.
3. Use `h.vault()`, `h.token()`, `h.registry()`, `h.strategy()` to interact
   with contracts.
4. Run `make integration-test` to verify.
