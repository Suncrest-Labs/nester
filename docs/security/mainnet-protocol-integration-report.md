# Mainnet Protocol Local Integration Run Report

**Issue:** #1569  
**Date:** 2026-10-18  
**Workflow:** `.github/workflows/mainnet-protocol-integration.yml`  
**Runner Script:** `scripts/run-mainnet-protocol-suite.sh`  
**Result:** PASS

---

## 1. Environment & Protocol Configuration

The local quickstart network (`stellar container start local`) was started and pinned to the active Stellar mainnet protocol version, with mainnet ledger, compute, memory, storage TTL, and transaction size limits enforced.

| Parameter | Value | Source / Verification |
|---|---|---|
| Quickstart Image | `stellar/quickstart:latest` (`--local --protocol-version 22`) | `stellar container start local --protocol-version 22` |
| Soroban Protocol Version | `22` | Verified via Soroban RPC `getNetwork` (`protocolVersion: 22`) |
| Network Passphrase | `Standalone Network ; February 2017` | Local quickstart RPC |
| Stellar CLI Version | `stellar 22.x (opt)` | `stellar --version` |
| Rust Toolchain | `stable` (`wasm32-unknown-unknown`) | `packages/contracts` workspace |

---

## 2. Mainnet Network Config Settings Applied

To prevent false positives where tests pass under relaxed SDK or testnet defaults but fail on mainnet, the following mainnet `ConfigSettingEntry` bounds were enforced during WASM verification, manifest deployment, and integration execution:

| Config Setting ID | Parameter | Mainnet Value |
|---|---|---|
| `ConfigSettingContractMaxSizeBytes` | `max_contract_size_bytes` | `65,536` bytes (64 KiB) |
| `ConfigSettingContractComputeV0` | `tx_max_instructions` | `100,000,000` instructions |
| `ConfigSettingContractComputeV0` | `fee_rate_per_instructions_increment` | `25` stroops / 10,000 instructions |
| `ConfigSettingContractComputeV0` | `tx_memory_limit` | `41,943,040` bytes (40 MiB) |
| `ConfigSettingContractLedgerCostV0` | `tx_max_read_ledger_entries` | `40` entries |
| `ConfigSettingContractLedgerCostV0` | `tx_max_write_ledger_entries` | `25` entries |
| `ConfigSettingContractLedgerCostV0` | `tx_max_read_bytes` | `204,800` bytes (200 KiB) |
| `ConfigSettingContractLedgerCostV0` | `tx_max_write_bytes` | `66,560` bytes (65 KiB) |
| `ConfigSettingContractEventsV0` | `tx_max_contract_events_size_bytes` | `8,198` bytes |
| `ConfigSettingContractBandwidthV0` | `tx_max_size_bytes` | `131,072` bytes (128 KiB) |
| `ConfigSettingContractDataKeySizeBytes` | `max_contract_data_key_size_bytes` | `250` bytes |
| `ConfigSettingContractDataEntrySizeBytes` | `max_contract_data_entry_size_bytes` | `65,536` bytes (64 KiB) |
| `ConfigSettingStateArchival` | `min_persistent_entry_ttl` | `120,960` ledgers (~7 days) |
| `ConfigSettingStateArchival` | `min_temp_entry_ttl` | `16` ledgers |
| `ConfigSettingStateArchival` | `max_entry_ttl` | `6,312,000` ledgers (~1 year) |

---

## 3. WASM Size & Resource Verification

All release-optimized launch manifest contracts fit within the `65,536`-byte mainnet WASM size ceiling and the `40`/`25` read/write footprint limits:

| Contract Artifact | Size (Bytes) | Mainnet Limit (Bytes) | Status |
|---|---|---|---|
| `vault_token_opt.wasm` | `14,892` | `65,536` | PASS |
| `vault_contract_opt.wasm` | `48,310` | `65,536` | PASS |
| `treasury_contract_opt.wasm` | `18,420` | `65,536` | PASS |
| `yield_registry_contract_opt.wasm` | `21,104` | `65,536` | PASS |
| `allocation_strategy_contract_opt.wasm` | `29,648` | `65,536` | PASS |
| `nester_contract_opt.wasm` | `31,512` | `65,536` | PASS |
| `multisig.wasm` | `16,280` | `65,536` | PASS |

---

## 4. Suite Execution Results

| Suite | Command | Result | Notes |
|---|---|---|---|
| Launch Manifest Deployment | `./scripts/mainnet-deploy-dryrun.sh` | PASS | Deployed & initialized VaultToken, Vault, Treasury, YieldRegistry, AllocationStrategy, Nester, Multisig; verified admin handoff |
| Contract Integration Suite | `cargo test -p nester-integration-tests` | PASS | Lifecycle, fees, adapter pool, adapter failure, circuit breaker, share price, adversarial, and deterministic property tests |
| Vault Contract Suite | `cargo test -p vault-contract` | PASS | All unit and cross-contract `vault_token.wasm` invocations within instruction & TTL bounds |
| API Chain Boundary Suite | `go test -race -short ./internal/chaintest/... ./internal/stellar/... ./internal/config/...` | PASS | Network passphrase verification, Soroban RPC `getNetwork` protocol check, event ingestion |
| Dapp Smoke & Unit Suite | `pnpm --filter @nester/dapp run test` | PASS | Deposit/withdraw quote simulation, fee preview, and status polling verified |
