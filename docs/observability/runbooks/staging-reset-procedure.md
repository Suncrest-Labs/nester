# Runbook — Staging Environment Reset

## Overview

How to wipe the staging environment back to a known-empty state, re-apply the
migration chain, and re-seed funded testnet accounts.

Staging exists so failures can be induced and recovery rehearsed without
touching the only working environment (issue #1114). That is only true if
resetting it is routine, so this procedure is meant to be run often rather than
carefully.

## Prerequisites

- Access to the staging Docker host, or `kubectl` against the `nester-staging`
  namespace.
- `deploy/staging/.env` populated. The compose file has no defaults for the
  contract addresses or the funded test account secret and will refuse to start
  without them:

  ```
  CONTRACT_ADDRESS_VAULT_FACTORY=C...
  CONTRACT_ADDRESS_YIELD_REGISTRY=C...
  FUNDED_TEST_ACCOUNT_SECRET=S...
  ```

  The file is gitignored. Never commit a real secret, and do not substitute a
  placeholder to get past the check — the service will start and then fail at
  its first chain call, which is harder to diagnose than a refusal to boot.

## Reset (Docker Compose)

1. Stop everything and destroy the volumes. `-v` is what makes this a reset
   rather than a restart; without it the old database survives.

   ```bash
   cd deploy/staging
   docker compose -f docker-compose.staging.yml down -v
   ```

2. Rebuild and start against empty volumes. `RUN_MIGRATIONS=true` is required:
   it defaults to false, so without it the API comes up against a database with
   no schema.

   ```bash
   RUN_MIGRATIONS=true docker compose -f docker-compose.staging.yml up --build -d
   ```

3. Confirm the migration chain applied and the service reports healthy:

   ```bash
   docker compose -f docker-compose.staging.yml logs api_staging | grep -i migration
   curl -s localhost:8081/health/detailed | jq '{status, version, commit, database}'
   ```

   `/health/detailed` is the endpoint that carries the build stamp; `/health`
   and `/healthz` are liveness probes with a minimal payload.

4. Fund the test account from the testnet friendbot. Friendbot is the funding
   mechanism for Stellar testnet — there is no in-repo seeding command, and
   `bootstrap-admin` grants an admin role rather than funding anything.

   ```bash
   curl "https://friendbot.stellar.org/?addr=<PUBLIC_KEY_OF_FUNDED_TEST_ACCOUNT>"
   ```

   Verify it landed:

   ```bash
   curl -s "https://horizon-testnet.stellar.org/accounts/<PUBLIC_KEY>" \
     | jq '.balances'
   ```

5. Optionally load fixture rows for a populated environment:

   ```bash
   docker compose -f docker-compose.staging.yml exec -T postgres_staging \
     psql -U nester -d nester_staging < ../../scripts/seed.sql
   ```

## Reset (Kubernetes)

1. Scale the API down so nothing writes while the schema is being replaced:

   ```bash
   kubectl scale deployment/nester-api-staging -n nester-staging --replicas=0
   ```

2. Drop and recreate the schema:

   ```bash
   kubectl exec -n nester-staging deploy/postgres-staging -- \
     psql -U nester -d nester_staging \
     -c 'DROP SCHEMA public CASCADE; CREATE SCHEMA public;'
   ```

3. Scale back up with migrations enabled:

   ```bash
   kubectl set env deployment/nester-api-staging -n nester-staging RUN_MIGRATIONS=true
   kubectl scale deployment/nester-api-staging -n nester-staging --replicas=1
   kubectl rollout status deployment/nester-api-staging -n nester-staging
   ```

4. Fund the test account with the friendbot call from step 4 above.

## Verification

The reset is complete when all of the following hold:

- `curl -s $STAGING_API/health/detailed | jq .status` returns `"ok"`.
- The same payload's `database.ok` is `true`, and `version` and `commit` match
  the build that was deployed.
- The funded test account shows a non-zero XLM balance on Horizon testnet.
- A deposit and a withdrawal complete end to end against the staging dApp.

That last check is the one that matters — the first three can pass while the
money path is broken.
