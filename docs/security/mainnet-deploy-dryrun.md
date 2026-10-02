# Mainnet Contract Deploy Dry-Run Runbook

This runbook documents the exact deploy sequence for Nester smart contracts against an environment matching mainnet's protocol and ledger versions.

## Overview

Before executing any production upgrade or mainnet deployment, engineers must execute the automated dry-run script to validate:
1. WASM compilation and reproducible builds.
2. Contract bytecode deployment via Soroban RPC.
3. Initialization parameters and constructor logic.
4. Access control transfer from the ephemeral deployer key to the production multi-signature coordinator.
5. Post-deployment state verification via read-only contract calls.

## Prerequisites

- Rust toolchain with `wasm32-unknown-unknown` target.
- Stellar CLI installed.
- Local Soroban RPC / Stellar network container running with the target ledger protocol version enabled.

## Running the Dry-Run

```bash
chmod +x scripts/mainnet-deploy-dryrun.sh
./scripts/mainnet-deploy-dryrun.sh
```

## Checklist

- [ ] Artifact checksums match CI expected outputs.
- [ ] Initialization arguments reviewed against audit specs.
- [ ] Multisig public keys confirmed by at least two maintainers.
- [ ] Post-deploy admin inspection returns the expected multisig address.
