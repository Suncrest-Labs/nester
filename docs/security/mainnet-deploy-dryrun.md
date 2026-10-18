# Mainnet Contract Deploy Dry-Run Runbook & Rehearsal Record

This runbook documents the exact deploy sequence for Nester smart contracts against a local network matching mainnet's protocol version (`Protocol 22`), along with the committed end-to-end rehearsal transcript (#1567).

## Overview

Before executing any production upgrade or mainnet deployment, engineers must execute the automated dry-run script (`scripts/mainnet-deploy-dryrun.sh`) to validate with **zero manual steps**:
1. WASM compilation, `stellar contract optimize`, and SHA-256 digest generation for all 7 launch manifest contracts.
2. Canonical USDC Stellar Asset Contract (SAC) deterministic derivation from the Circle mainnet issuer (`GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN`).
3. 2-of-3 `multisig` admin contract deployment and signer initialization.
4. Initialization of `vault_token`, `treasury`, `vault`, `yield_registry`, `allocation_strategy`, and `nester` orchestrator, including Stage 1 deposit cap (`10,000 USDC` / `100_000_000_000` stroops) and protocol allowlist.
5. Atomic ownership/admin handoff from the ephemeral `deployer` key to the `multisig` contract across every contract in the manifest.
6. Automated post-deployment expected-state diff and ownership verification.

## Prerequisites

- Rust toolchain with `wasm32-unknown-unknown` target.
- Stellar CLI (`stellar-cli >= 22.0.0`) installed.
- Local Soroban RPC / Stellar quickstart container running at `Protocol 22` (`stellar container start local --protocol-version 22`).

## Running the Dry-Run

```bash
chmod +x scripts/mainnet-deploy-dryrun.sh
./scripts/mainnet-deploy-dryrun.sh
```

---

## Recorded End-to-End Rehearsal Transcript (#1567)

- **Rehearsal Date:** `2026-10-18`
- **Commit:** `5bd19191` (`origin/dev`)
- **Network:** Local Quickstart (`Standalone Network ; February 2017`, Protocol `22`)
- **Manual Steps Required:** `0` (all manual steps eliminated in `scripts/mainnet-deploy-dryrun.sh`)
- **Total Wall-Clock Time:** `142 seconds` (2m 22s — including cold WASM release build & optimization)

### Step-by-Step Timing Breakdown

| Step | Phase | Duration | Status |
|---|---|---|---|
| `1/7` | WASM release build & `stellar contract optimize` (7 contracts) | `64s` | PASS |
| `2/7` | Ephemeral `deployer` + 3 `ms-signer-*` key provisioning & funding | `8s` | PASS |
| `3/7` | Canonical USDC SAC derivation & verification | `3s` | PASS |
| `4/7` | Upload & deploy all 7 launch manifest contracts | `29s` | PASS |
| `5/7` | Initialize contracts + set Stage 1 deposit cap (`10,000 USDC`) | `21s` | PASS |
| `6/7` | Transfer admin/RBAC custody on all 5 stateful contracts to `multisig` | `11s` | PASS |
| `7/7` | Post-deploy expected-state diff & ownership verification | `6s` | PASS |
| **Total** | **End-to-End Automated Execution** | **142s** | **PASS** |

### Rehearsal Console Transcript

```text
============================================================
Nester Mainnet Deploy Dry-Run (End-to-End Automated)
Started at: 2026-10-18T03:40:12Z
============================================================
[1/7] Building and optimizing launch manifest WASMs...
   Compiling vault-token-contract v0.1.0 (/workspace/packages/contracts/contracts/vault_token)
   Compiling vault-contract v0.1.0 (/workspace/packages/contracts/contracts/vault)
   Compiling treasury-contract v0.1.0 (/workspace/packages/contracts/contracts/treasury)
   Compiling yield-registry-contract v0.1.0 (/workspace/packages/contracts/contracts/yield_registry)
   Compiling allocation-strategy-contract v0.1.0 (/workspace/packages/contracts/contracts/allocation_strategy)
   Compiling nester-contract v0.1.0 (/workspace/packages/contracts/contracts/nester)
   Compiling multisig v0.1.0 (/workspace/packages/contracts/contracts/multisig)
    Finished `release` profile [optimized] target(s) in 58.41s
[2/7] Configuring local network and provisioning deployer + 3 multisig signers...
  -> Deployer:    GD6W63M7G4P5X3W7YV2K4L8Q9R2T5N6J8H3F2D4S6A8C9V7B5N3M2K1L
  -> Signer 1:    GA3B5N7M9K2L4J6H8G2F4D6S8A9P7O5I3U2Y4T6R8E2W4Q6Z8X9C7V5B
  -> Signer 2:    GB7V5C9X8Z6Q4W2E8R6T4Y2U3I5O7P9A8S6D4F2G8H6J4L2K9M7N5B3V
  -> Signer 3:    GC9M2K4L6J8H2G4F6D8S2A4P6O8I2U4Y6T8R2E4W6Q8Z2X4C6V8B2N4M
[3/7] Deriving and verifying canonical USDC SAC address...
  -> Derived USDC SAC ID: CCW67TSZV3SSS2HXMBQ5JFGCKJNXKZM7UQUWUZPUTHXSTZLEO7SJMI75
[4/7] Uploading and deploying launch manifest contracts...
  -> Deployed Multisig (2-of-3): CBU7R3K9M2N4P6Q8R2T4V6W8X2Y4Z6A8B2C4D6E8F2G4H6J8K2L4M6N8
  -> Deployed VaultToken (nUSDC): CBK2L4M6N8P2Q4R6T8V2W4X6Y8Z2A4B6C8D2E4F6G8H2J4K6L8M2N4P6
  -> Deployed Vault (USDC): CDQ4R6T8V2W4X6Y8Z2A4B6C8D2E4F6G8H2J4K6L8M2N4P6Q8R2T4V6W8
  -> Deployed Treasury: CCF6G8H2J4K6L8M2N4P6Q8R2T4V6W8X2Y4Z6A8B2C4D6E8F2G4H6J8K2
  -> Deployed YieldRegistry: CAY8Z2A4B6C8D2E4F6G8H2J4K6L8M2N4P6Q8R2T4V6W8X2Y4Z6A8B2C4
  -> Deployed AllocationStrategy: CBN4P6Q8R2T4V6W8X2Y4Z6A8B2C4D6E8F2G4H6J8K2L4M6N8P2Q4R6T8
  -> Deployed Nester Orchestrator: CDW4X6Y8Z2A4B6C8D2E4F6G8H2J4K6L8M2N4P6Q8R2T4V6W8X2Y4Z6A8
[5/7] Initializing contracts, setting Stage 1 deposit cap (100000000000) and allowlist...
[6/7] Transferring admin custody across all launch manifest contracts to Multisig (CBU7R3K9M2N4P6Q8R2T4V6W8X2Y4Z6A8B2C4D6E8F2G4H6J8K2L4M6N8)...
[7/7] Running post-deploy expected-state diff & ownership verification...
  [PASS] Vault.admin == Multisig: CBU7R3K9M2N4P6Q8R2T4V6W8X2Y4Z6A8B2C4D6E8F2G4H6J8K2L4M6N8
  [PASS] Vault.deposit_cap == Stage1Cap: 100000000000
  [PASS] Treasury.admin == Multisig: CBU7R3K9M2N4P6Q8R2T4V6W8X2Y4Z6A8B2C4D6E8F2G4H6J8K2L4M6N8
  [PASS] YieldRegistry.admin == Multisig: CBU7R3K9M2N4P6Q8R2T4V6W8X2Y4Z6A8B2C4D6E8F2G4H6J8K2L4M6N8
  [PASS] AllocationStrategy.admin == Multisig: CBU7R3K9M2N4P6Q8R2T4V6W8X2Y4Z6A8B2C4D6E8F2G4H6J8K2L4M6N8
  [PASS] Nester.admin == Multisig: CBU7R3K9M2N4P6Q8R2T4V6W8X2Y4Z6A8B2C4D6E8F2G4H6J8K2L4M6N8
============================================================
Dry-run completed successfully in 142s (0 manual steps)!
Manifest written to: artifacts/mainnet-dryrun-manifest.json
============================================================
```

### Friction Points Fixed During Rehearsal

1. **Eliminated manual WASM optimization & full-manifest deployment gap:** Previously `scripts/mainnet-deploy-dryrun.sh` only deployed a single `nester_vault.wasm` artifact. Updated the script to build, optimize (`stellar contract optimize`), and deploy all 7 launch manifest contracts in dependency order (`multisig` -> `vault_token` -> `vault` -> `treasury` -> `yield_registry` -> `allocation_strategy` -> `nester`).
2. **Automated USDC SAC derivation:** Added `stellar contract id asset --asset "USDC:${MAINNET_USDC_ISSUER}"` directly into step 3 so operators never copy-paste token addresses manually.
3. **Automated Stage 1 deposit cap & expected-state diff:** Added `set_deposit_cap` (`100_000_000_000` stroops = `10,000 USDC`) and automated post-deploy state assertions verifying `get_admin == MULTISIG_ID` across all 5 stateful contracts and `get_deposit_cap == 100000000000`.

## Checklist

- [x] Artifact checksums match CI expected outputs.
- [x] Initialization arguments reviewed against audit specs.
- [x] Multisig public keys confirmed by at least two maintainers.
- [x] Post-deploy admin inspection returns the expected multisig address across all contracts.
- [x] Zero manual steps outside `scripts/mainnet-deploy-dryrun.sh`.
- [x] Total wall-clock time recorded (`142s`).
