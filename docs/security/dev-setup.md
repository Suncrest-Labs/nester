# Local development: what the dev stack exposes

`docker-compose.yml` binds every service port to `127.0.0.1` rather than to all
interfaces. This document explains why, and how to opt out when you genuinely
need to.

Referenced from `docker-compose.external.yml` and the `dev-external` target in
the `Makefile`.

## The default: loopback only

`make dev` publishes Postgres, Redis, the API, the frontend, the intelligence
service and Jaeger on `127.0.0.1`. They are reachable from your machine and
from nothing else.

This matters because the dev stack ships with credentials that are committed to
the repository — the Postgres password and `AUTH_JWT_SECRET` are both literals
in `docker-compose.yml`. Publishing those ports on `0.0.0.0` means anyone on the
same network as you can reach a database whose password they can read on GitHub,
and mint tokens with a signing key they can read on GitHub.

The API itself refuses to start in staging or production with the dev JWT
secret (there is a deny-list in `internal/config/config.go`), so this is a
local-only hazard. Locally is where most people run it on café and hotel
networks.

## When you need external access

Two cases come up in practice: testing from a phone on the same LAN, and
sharing a running stack with someone during a debugging session.

```bash
make dev-external
```

This layers `docker-compose.external.yml` over the base file, rebinding the
published ports to `0.0.0.0` and rewriting the frontend's API and WebSocket
URLs to use your machine's hostname:

```bash
EXTERNAL_HOSTNAME=192.168.1.42 make dev-external
```

`EXTERNAL_HOSTNAME` defaults to `localhost`, which is not useful for external
access — set it to the address the other device will actually reach you on.

### What to know before you do

- **The committed credentials are now reachable by everyone on the network.**
  Treat any data in that database as public for the duration.
- **Do not leave it running.** `make dev-down` when the session is over.
- **Do not do it on an untrusted network** — public Wi-Fi, conference networks,
  co-working spaces.
- **Never point a production or staging database at this stack.**

If you need external access regularly, generate a per-environment secret rather
than relying on the committed default: set `AUTH_JWT_SECRET` and the Postgres
password in a local `.env` that git ignores.

## Why the override file repeats the environment block

`docker-compose.external.yml` uses `!override` on the frontend's `environment`
map. Compose replaces the whole map rather than merging into it, so every key
the base file sets has to be repeated in the override — including
`NEXT_PUBLIC_NETWORK` and `INTELLIGENCE_SERVICE_URL`, which are not themselves
hostname-dependent.

Dropping one of them does not fail loudly; the frontend comes up and then
misroutes API calls or loses the intelligence rewrites. If you add a variable to
the frontend service in `docker-compose.yml`, add it to the override too, and
verify with:

```bash
docker compose -f docker-compose.yml -f docker-compose.external.yml config
```

That prints the merged result, which is the only reliable way to see what the
container will actually receive.
