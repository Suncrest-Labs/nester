# Nester Deployment & Rollback Runbook

## Overview
This document describes release versioning, build metadata stamping, deployment procedures, and the rehearsed rollback protocol for Nester services.

## 1. Build Version and Commit Stamping
All running Nester services expose their build version and git commit hash on the `/health` status endpoint and emit them in structured logs upon startup.

- **Version flag / linker flags:** `go build -ldflags="-X main.Version=v1.0.0 -X main.Commit=abcdef0"`
- **Status Endpoint (`GET /health`):** Returns JSON containing `version`, `commit`, and service status.

## 2. Tagged Releases and Changelog Generation
Releases are tagged in Git following Semantic Versioning (`vMAJOR.MINOR.PATCH`). Release notes are automatically generated using GitHub releases / Conventional Commits.

## 3. Database Migrations & Rollback Safety
Before deploying any database migration, migration safety must be verified:
- **Safe Migrations:** Adding new nullable columns, adding new tables (`CREATE TABLE`), adding indexes concurrently (`CREATE INDEX CONCURRENTLY`).
- **Unsafe Migrations (Require Explicit Planning):** Dropping columns, renaming tables/columns, adding non-nullable columns without defaults, or dropping constraints.
- **Identification:** Any migration that makes rollback unsafe MUST be explicitly documented in the PR and migration file header.

## 4. Rehearsed Rollback Procedure
In the event of a bad deployment or failing health checks:
1. **Identify current version:** Check the `/health` endpoint or log output to confirm the deployed commit/version.
2. **Trigger Rollback:** Revert to the previous stable container image tag or release artifact via the deployment pipeline.
3. **Database Rollback:** If the bad deploy included database migrations that require reversal, execute down-migrations using the migration runner (if safe) or restore from the pre-deployment snapshot.
4. **Verify Health:** Confirm `/health` returns the previous version and all dependent services are stable.
