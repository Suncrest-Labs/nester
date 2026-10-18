# Production Deployment & Rollback Runbook

> **Scope:** Protected `production` environment deployment, separate migration approval gate, immutable `@sha256` digest rollout, and automated rollback rehearsal for Nester (`api`, `signer`, `dapp`).
>
> **Related Issue:** [#1529](https://github.com/Suncrest-Labs/nester/issues/1529)

---

## 1. Deployment Architecture & Gating

Production deployments are codified in:
- [`deploy/production/docker-compose.production.yml`](../../deploy/production/docker-compose.production.yml)
- [`deploy/production/k8s-production.yaml`](../../deploy/production/k8s-production.yaml)
- [`deploy/production/deploy-production.sh`](../../deploy/production/deploy-production.sh)

### Mandatory Gates

1. **Semantic Release Tag (`vX.Y.Z`):** Only tagged releases built from `main` are eligible for production deployment.
2. **Protected `production` Environment Approval:** Requires manual sign-off from at least 2 core maintainers (`PRODUCTION_APPROVED=true`).
3. **Immutable Image Digests (`@sha256:...`):** Mutable tags (`:latest`, `:main`) are rejected by `deploy-production.sh` and `scripts/verify-signed-image.sh`.
4. **Separate Approved Migration Job:** Production API containers run with `RUN_MIGRATIONS=false`. Schema migrations execute only when `RUN_APPROVED_MIGRATIONS=true` and a verified database snapshot ID (`PRE_MIGRATION_BACKUP_ID`) is supplied.

---

## 2. Recorded Deploy & Rollback Rehearsal Evidence

The following rehearsal demonstrates deploying `v1.0.0` (storing its digests in `.last-good-digests.env`), followed by a faulty `v1.0.1` rollout whose smoke check fails and automatically rolls back to the `v1.0.0` digests:

```text
# Step 1: Seed known-good v1.0.0 state
$ cat deploy/production/.last-good-digests.env
PREV_RELEASE_TAG=v1.0.0
PREV_API_IMAGE_DIGEST=sha256:4a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e2f1a0b9c8d7e6f5a4b3c2d1e0f9a8b
PREV_SIGNER_IMAGE_DIGEST=sha256:9b8a7f6e5d4c3b2a1f0e9d8c7b6a5f4e3d2c1b0a9f8e7d6c5b4a3f2e1d0c9b8a
PREV_DAPP_IMAGE_DIGEST=sha256:1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d

# Step 2: Attempt v1.0.1 deploy against failing smoke check endpoint -> automatic rollback
$ DRY_RUN=true \
  RELEASE_TAG=v1.0.1 \
  PRODUCTION_APPROVED=true \
  SMOKE_API_URL=http://127.0.0.1:59999/health \
  API_IMAGE_DIGEST=sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff \
  SIGNER_IMAGE_DIGEST=sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee \
  DAPP_IMAGE_DIGEST=sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd \
  ./deploy/production/deploy-production.sh

==> Deploying v1.0.1 with immutable digests...
==> Running post-deploy smoke checks...
ERROR: Smoke check failed for v1.0.1! Initiating automatic rollback...
==> Rolling back to previous known-good digests:
    API_IMAGE_DIGEST=sha256:4a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e2f1a0b9c8d7e6f5a4b3c2d1e0f9a8b
    SIGNER_IMAGE_DIGEST=sha256:9b8a7f6e5d4c3b2a1f0e9d8c7b6a5f4e3d2c1b0a9f8e7d6c5b4a3f2e1d0c9b8a
    DAPP_IMAGE_DIGEST=sha256:1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d
==> Rollback to v1.0.0 complete.
```
