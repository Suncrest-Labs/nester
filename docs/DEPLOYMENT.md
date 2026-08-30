# Nester Deployment & Rollback Runbook

## Overview

This document describes release versioning, build metadata stamping, deployment
procedures, and the rollback protocol for Nester services.

## 1. Build Version and Commit Stamping

The API stamps its build into the binary at link time and reports it in two
places: the first structured log line at startup, and the `/health/detailed`
status endpoint.

**Linker flags.** The variables live in `package main` in `apps/api/cmd/api`
and are lowercase, so the flags are:

```
go build -ldflags="-X main.version=v1.0.0 -X main.commit=$(git rev-parse HEAD)" ./cmd/api
```

The API `Dockerfile` accepts these as build args and applies them for you:

```
docker build --build-arg VERSION=v1.0.0 --build-arg COMMIT=$(git rev-parse HEAD) apps/api
```

**Fallback.** When `commit` is not supplied at link time, the service reads the
VCS revision Go records in the build info, appending `-dirty` for a build made
from a modified tree. A build with no VCS information at all reports `unknown`.
The field is never empty, so "unknown build" is always distinguishable from
"field missing".

**Status endpoint.** `GET /health/detailed` returns JSON including `version`
and `commit` alongside dependency status. Note that `/health` and `/healthz`
are liveness probes and deliberately return a minimal payload — use
`/health/detailed` to identify what is deployed.

**Startup log.** The first line the service logs is:

```
starting nester api version=v1.0.0 commit=abcdef0
```

## 2. Tagged Releases and Changelog Generation

Releases are tagged following Semantic Versioning (`vMAJOR.MINOR.PATCH`).
Release notes are generated from Conventional Commits via GitHub Releases.

The tag and the `VERSION` build arg must match, so that the version reported by
a running service resolves to exactly one tagged commit.

## 3. Database Migrations & Rollback Safety

Every migration ships with a `.down.sql`, and CI applies the full chain up and
back down against a populated database (see `.github/workflows/ci.yml`, job
`migration-safety`). A migration that cannot be applied from scratch, or whose
down migration does not restore the prior schema, fails the build.

**Safe.** Adding nullable columns, adding tables, adding indexes concurrently,
widening a column's precision.

**Unsafe, requires explicit planning.** Dropping columns, renaming tables or
columns, adding non-nullable columns without defaults, dropping constraints,
and *narrowing* a column's type or precision.

**Irreversible migrations must declare themselves.** A migration whose rollback
can lose data must say so in a comment at the top of its `.down.sql`, stating
what makes it unsafe and what has to happen before it can be run. CI checks for
this declaration on any down migration containing a destructive operation.

The widening migrations `103` and `111` are the current examples: rolling
either back on a database holding large balances raises `numeric field
overflow` rather than truncating silently. That failure is intentional — an
amount must never be rounded away by a schema change — so those rows have to be
reconciled before the rollback can proceed.

## 4. Rollback Procedure

1. **Identify the running version.** `curl $HOST/health/detailed` and read
   `version` and `commit`, or check the service's startup log line.
2. **Roll back the code.** Redeploy the previous stable image tag through the
   deployment pipeline. Because the image is stamped, confirm the rollback
   target's `version` before promoting it.
3. **Roll back the database, if required.** Only if the bad deploy included a
   migration. Check that migration's `.down.sql` header first: if it declares
   itself irreversible, restore from the pre-deployment snapshot instead of
   running the down migration.
4. **Verify.** Confirm `/health/detailed` reports the expected previous
   `version` and `commit`, that `status` is `ok`, and that database, Redis,
   Horizon and Soroban RPC all report healthy.

Rehearse this against staging (`docs/observability/runbooks/staging-reset-procedure.md`)
rather than discovering a gap during an incident.
