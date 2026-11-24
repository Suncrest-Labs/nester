# Pre-Production Launch Rehearsal Environment

`deploy/preprod/` provides a dedicated pre-production environment (`nester-preprod`) that mirrors mainnet's exact infrastructure shape (`deploy/production/`) while targeting Stellar testnet on completely isolated infrastructure.

## Why a Dedicated Pre-Production Environment Exists

Standard `staging` (`deploy/staging/`) uses a simplified topology (single API replica, mutable `:latest` image, no isolated signer deployment, no separate migration Job, `/health` readiness probe). Rehearsing a mainnet launch by only swapping contract addresses on staging does not validate the real production launch sequence.

`preprod` mirrors `production` 1:1:

| Dimension | `deploy/production/` | `deploy/preprod/` |
| --- | --- | --- |
| Namespace | `nester-production` | `nester-preprod` (isolated) |
| API replicas | `2` | `2` |
| Image pinning | Immutable `@sha256:...` digest | Immutable `@sha256:...` digest |
| Signer isolation | Separate `nester-signer-production` Deployment (no exposed port) | Separate `nester-signer-preprod` Deployment (no exposed port) |
| Migrations | Separate one-shot `batch/v1` Job (`RUN_MIGRATIONS=true`), API has `RUN_MIGRATIONS=false` | Separate one-shot `batch/v1` Job (`RUN_MIGRATIONS=true`), API has `RUN_MIGRATIONS=false` |
| Readiness / Liveness | `/readyz` / `/health` | `/readyz` / `/health` |
| Postgres / Redis | Isolated containers, no host port mappings | Isolated containers, no host port mappings |
| Stellar network | `mainnet` (`https://horizon.stellar.org`) | `testnet` (`https://horizon-testnet.stellar.org`) |

## Required GitHub Environment Configuration

Create a `preprod` GitHub Environment (Settings -> Environments -> `preprod`) with required reviewers (matching the `production` approval gate) and configure:

- **Secrets**
  - `PREPROD_KUBE_CONFIG` — base64-encoded kubeconfig scoped to the `nester-preprod` cluster/namespace
  - `PREPROD_SMOKE_TEST_WALLET_SECRET` — funded testnet wallet secret for smoke checks
- **Variables**
  - `PREPROD_API_URL` (default: `https://api-preprod.nester.dev/api/v1`)
  - `PREPROD_URL` (default: `https://preprod.nester.dev`)

## Running a Full Launch-Sequence Rehearsal

1. **Verify manifest parity** before cutting a release candidate:
   ```bash
   python3 scripts/verify-preprod-parity.py
   ```
2. **Trigger the preprod launch workflow** (`.github/workflows/deploy-preprod.yml`) by pushing a release-candidate tag (`vX.Y.Z-rc.N`) or via `workflow_dispatch`.
3. **Validate every stage of the mainnet sequence**:
   - `build-images`: builds and pushes both `nester-api` and `nester-signer` images to GHCR.
   - `resolve-digests`: resolves immutable `@sha256:...` digests and captures the currently-deployed preprod digests for rollback.
   - `migrate`: runs the approved `nester-api-migrate` Job in `nester-preprod` and waits for `"database migrations complete"`.
   - `preprod-deploy`: applies the pinned API (2 replicas) and isolated signer Deployments, waits for rollout completion, and polls `/readyz`.
   - `preprod-smoke-test`: executes `tests/smoke.spec.ts` against `PREPROD_URL`.
   - `rollback`: automatically rolls both API and signer back to the previously recorded digests if smoke checks fail.
