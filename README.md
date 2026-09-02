# Nester

**Decentralized Savings & Yield Investment Protocol**

Nester is a crypto-first savings and investment app on Stellar. Deposits are diversified across multiple on-chain yield sources, tracked in a live portfolio, and grown with automated recurring deposits — self-custodial from end to end.

> Your keys. Your yield. Your portfolio.

---

## The Problem

Holding stablecoins today means choosing between two bad options: let your money sit idle losing value to inflation, or navigate the complex world of DeFi protocols yourself — juggling pools, APYs, and rebalancing by hand.

Nester turns that into one decision: pick a risk profile, deposit, and let the protocol do the work.

---

## How It Works

| Piece | Function | Outcome |
| ----- | -------- | ------- |
| **Smart Vaults** | Diversify deposits across lending & staking protocols | Optimized yields, one deposit |
| **Portfolio** | Live positions, P&L, performance history | Full visibility, on-chain truth |
| **Auto-invest** | Recurring on-chain deposit mandates | Dollar-cost averaging into yield |

---

## Smart Vaults

The yield engine. Deposits are automatically allocated across battle-tested DeFi protocols to generate consistent returns without manual management.

**Smart Vaults** let users choose their risk profile:

| Vault        | Risk   | Target APY | Strategy                  |
| ------------ | ------ | ---------- | ------------------------- |
| Conservative | Low    | 6-8%       | Stablecoin lending only   |
| Balanced     | Medium | 8-12%      | Mixed lending + staking   |
| Growth       | Higher | 12-18%     | Aggressive multi-protocol |

The protocol continuously monitors APYs and risk metrics — including signature-attested APY/TVL data on-chain — automatically rebalancing to maintain optimal performance while minimizing exposure to underperforming pools. A circuit breaker and emergency withdrawal queue protect deposits when a source degrades.

---

## Portfolio & Auto-invest

Every position is visible in a live portfolio: holdings, realized yield, performance charts, and a unified activity feed. Recurring deposit mandates run on-chain, so auto-invest schedules execute without trusting a server with your funds.

Savings goals sit on top: set a target, attach a schedule, watch streaks and milestones — all backed by the same vault engine.

---

## Technical Architecture

![System Architecture]

**Smart Contracts (Soroban/Stellar)** — Vault management, deposit routing, yield distribution, rebalancing logic, recurring deposit mandates, and savings goals.

**Backend Services (Go)** — Real-time APY/TVL monitoring, a reorg-safe event indexer, a double-entry ledger, and portfolio valuation.

**Client Applications** — Web app (Next.js) and API for integrations.

---

## Security Model

Nester is non-custodial. Users maintain full ownership of assets through smart contracts—the protocol cannot freeze, seize, or redirect funds.

**Audit Status:** [Audit package and report](/audits)

**Risk Mitigations:** Multi-protocol diversification limits single-point-of-failure exposure. Real-time exploit monitoring with automatic pause mechanisms. Insurance fund for covered events. Rate limiting and withdrawal delays for large transactions.

**Automated Security Scanning (CI)**

Every pull request runs the following automated security checks:

| Language / Layer     | Tool                                                                                 | Coverage                                        |
| -------------------- | ------------------------------------------------------------------------------------ | ----------------------------------------------- |
| TypeScript / Next.js | [eslint-plugin-security](https://github.com/eslint-community/eslint-plugin-security) | Injection, object-injection, unsafe regex, eval |
| TypeScript / Next.js | [Semgrep](https://semgrep.dev) (`p/typescript`, `p/react`, `p/nextjs`, `p/secrets`)  | XSS, secrets, Next.js misconfigs                |
| TypeScript / Next.js | [CodeQL](https://codeql.github.com) (`javascript-typescript`)                        | Broad static analysis                           |
| Go                   | gosec + govulncheck                                                                  | SAST + CVE audit                                |
| Rust                 | cargo-audit                                                                          | CVE audit                                       |
| All languages        | gitleaks                                                                             | Secret / credential detection                   |

---

## Getting Started

The fastest way to run the full stack locally is Docker Compose. You only need Docker and Docker Compose installed — no Go, Node, or Python required on your host.

### Prerequisites

- [Docker Desktop](https://www.docker.com/products/docker-desktop/) (includes Docker Compose v2)

### 1. Clone and configure

```bash
git clone https://github.com/suncrestlabs/nester.git
cd nester
cp .env.example .env
```

### 2. Start all services

```bash
make dev
```

This builds and starts PostgreSQL, Redis, the Go API, and the Next.js frontend. On first run Docker pulls base images and compiles everything — expect 2–5 minutes.

| Service      | URL                           |
| ------------ | ----------------------------- |
| Frontend     | http://localhost:3001         |
| API          | http://localhost:8080         |
| API health   | http://localhost:8080/healthz |
| PostgreSQL   | localhost:5432                |

The database is seeded automatically with a test user, two vaults, and allocations.

### 3. Useful commands

```bash
make dev-logs    # tail logs from all services
make dev-db      # open a psql shell in the dev database
make dev-down    # stop all services
make dev-reset   # wipe volumes and restart fresh
```

### Hot reload

- **Go API** — [air](https://github.com/air-verse/air) watches `apps/api/` and recompiles on every `.go` file save.
- **Next.js** — standard Next.js fast refresh works via the volume mount.

### Connecting to the database manually

```bash
make dev-db
# or
docker compose exec postgres psql -U nester nester_dev
```

Test credentials: user `550e8400-e29b-41d4-a716-446655440001` / `testuser@nester.dev`.

### Health endpoints

**`/healthz` is the canonical liveness endpoint.** Point orchestrator liveness
probes, load-balancer checks, and uptime monitors at it. `/health` is a
permanent alias — same handler, same response — kept for the probes and
runbooks that already reference it; neither path will be removed.

| Path | Purpose | Healthy | Unhealthy |
|------|---------|---------|-----------|
| `GET /healthz` | Liveness (canonical). Reflects the drain flag only, never dependency state, so a database outage never gets the process restarted. | `200` `ok` | `503` `draining` |
| `GET /health` | Permanent alias for `/healthz`. | `200` `ok` | `503` `draining` |
| `GET /readyz` | Readiness. Fails closed on PostgreSQL and — when `REDIS_ADDR` is set — Redis, which backs the token-revocation cache and the distributed rate limiters. | `200` `ok` | `503` `draining` \| `database unavailable` \| `redis unavailable` |
| `GET /health/detailed` | JSON diagnostics: per-dependency status for PostgreSQL (with pool counters), Redis, Horizon, and Soroban RPC. `503` when draining, or when PostgreSQL or a configured Redis is down; `200` with `"status":"degraded"` when only Horizon or Soroban RPC is down. | `200` | `503` |

All four are unauthenticated, so dependency failures are reported as coarse
reasons (`unavailable`, `timeout`) rather than driver errors — a pgx or
go-redis error carries connection details that must not be published. The full
error stays in the service logs.

Liveness and readiness are exempt from rate limiting and cost quotas so an
orchestrator can always reach them. The internal metrics listener serves its
own `GET /healthz` on `METRICS_ADDR`, so "the scrape target is down" reads
differently from "the API is down".

---

## Development Stack Security

### Default: Loopback Binding

By default, `make dev` binds all services to `127.0.0.1` (loopback) only. This means:

- **Postgres** on `127.0.0.1:5432` — accessible only from your machine
- **Redis** on `127.0.0.1:6379` — accessible only from your machine
- **API** on `127.0.0.1:8080` — accessible only from your machine
- **Frontend** on `127.0.0.1:3001` — accessible only from your machine

This prevents accidental network exposure when developing on shared WiFi (airports, cafés, offices). Loopback binding reduces accidental remote exposure of development credentials and services.

### Development Credentials

The dev stack uses placeholder credentials committed in the repository:

| Service    | User     | Password                                     | Context                        |
| ---------- | -------- | -------------------------------------------- | ------------------------------ |
| PostgreSQL | `nester` | `nester_dev_password`                        | Development only, known-bad    |
| Redis      | (none)   | (none)                                       | No authentication in dev       |
| JWT Secret | (varies) | `dev-nester-jwt-secret-change-in-production` | Rejected by production startup |

**These are development placeholders, never for production use.** The API's startup validation explicitly rejects the dev JWT secret in production (see `internal/config/config.go`).

### External Network Access

If you need to expose the dev stack to other machines (multi-machine development), use:

```bash
make dev-external
```

This composes `docker-compose.external.yml` on top of the base config, exposing services on `0.0.0.0` (all interfaces).

**Security warning:** Only use this on **trusted networks**. Development credentials are known; on public WiFi, anyone in range can access your database.

### Production Posture

Production deployments are handled separately and are not affected by docker-compose configuration:

- Secrets (JWT key, database credentials) are injected at deployment time and never committed
- Services bind to specific interfaces (not `0.0.0.0`)
- The API rejects the development JWT secret on startup
- Postgres and Redis require strong authentication

See [SECURITY.md](/SECURITY.md) and the [deployment docs](/deploy) for production security details.

---

## Roadmap

| Phase  | Focus                                                           | Status      |
| ------ | --------------------------------------------------------------- | ----------- |
| **v1** | Yield vaults, portfolio dashboard, recurring on-chain deposits  | In Progress |
| **v2** | In-app swaps (DEX routing) and recurring token buys             | Planned     |
| **v3** | Fiat onramp widget, MPC embedded wallets, curated asset baskets | Planned     |
| **v4** | Multi-region expansion and deeper protocol integrations         | Future      |

---

## How to Contribute

Nester is being built in the open. We welcome contributions from developers, designers, researchers, and DeFi enthusiasts.

### Getting Started

1. **Explore the codebase** — Familiarize yourself with the monorepo structure and existing patterns
2. **Check open issues** — Look for issues tagged `good-first-issue` or `help-wanted`
3. **Join the conversation** — Reach out before starting major work to ensure alignment

### CI/CD Secrets

If you're setting up CI/CD (GitHub Actions, forks, or new environments), see [docs/ci/secrets.md](docs/ci/secrets.md) for:

- What secrets are required vs optional
- How to obtain each secret
- What happens if a secret is missing
- Rotation procedures for credentials

### Contribution Areas

| Area            | Looking For                                  | Skills                  |
| --------------- | -------------------------------------------- | ----------------------- |
| Smart Contracts | Vault logic, rebalancing, yield adapters     | Soroban, Rust, Stellar  |
| Backend         | Indexing, ledger, portfolio valuation        | Go, PostgreSQL          |
| Frontend        | Web UI, dashboards, charts                   | React, Next.js          |
| Documentation   | Guides, API docs, tutorials                  | Technical writing       |
| Security        | Audits, penetration testing, threat modeling | Smart contract security |

### Process

1. Fork the repository
2. Create a feature branch (`feat/your-feature`)
3. Make your changes with clear commit messages
4. Open a PR with description of changes and motivation
5. Respond to review feedback

### Code Standards

Follow existing patterns and conventions in the codebase. Write tests for new functionality. Keep PRs focused and reasonably sized. Document public APIs and complex logic.

### Contact

- **GitHub:** [github.com/suncrestlabs/nester](https://github.com/suncrestlabs/nester)
- **Twitter:** [@TheNesterHQ](https://x.com/TheNesterHQ)

---

## Links

- [Website](https://nesterhq.netlify.app/)
- [GitHub](https://github.com/suncrestlabs/nester)

---

**Built by [Suncrest Labs](https://suncrestlabs.com)**

_Nester is in active development. Features and specifications may change._
