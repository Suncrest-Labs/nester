# Staging Environment Reset Procedure Runbook

## Overview
This runbook documents the exact sequence of steps required to perform a clean destructive reset of the staging environment, re-apply database migrations, and re-seed funded test accounts against the Stellar testnet configuration.

## Prerequisites
- Access to the staging cluster / Docker host.
- `docker compose` (for containerized staging) or `kubectl` (for Kubernetes staging namespace `nester-staging`).
- Staging environment secrets file.

## Reset Procedure (Docker Compose)

1. Stop all staging services and wipe persistent database and cache volumes:
   ```bash
   cd deploy/staging
   docker compose -f docker-compose.staging.yml down -v
   ```

2. Rebuild and start the staging environment with fresh containers and empty volumes:
   ```bash
   docker compose -f docker-compose.staging.yml up --build -d
   ```

3. Verify database migrations run automatically:
   ```bash
   docker compose -f docker-compose.staging.yml logs -f api_staging
   ```

4. Seed funded test accounts and verify Stellar testnet connectivity:
   ```bash
   docker compose -f docker-compose.staging.yml exec api_staging /app/bootstrap-admin --seed-testnet
   ```

## Reset Procedure (Kubernetes)

1. Delete and recreate the staging namespace or deployment pods:
   ```bash
   kubectl rollout restart deployment/nester-api-staging -n nester-staging
   ```

2. Perform database migration reset job if schema wipe is required:
   ```bash
   kubectl delete job nester-db-reset -n nester-staging --ignore-not-found
   kubectl apply -f deploy/staging/k8s-db-reset-job.yaml -n nester-staging
   ```

## Verification
- Run health probe: `curl https://staging-api.nester.fi/health`
- Verify Stellar testnet horizon queries succeed without rate limits or ledger errors.
