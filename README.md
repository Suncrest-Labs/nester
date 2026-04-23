# Nester

**Decentralized Savings & Liquidity Protocol**

Nester automates DeFi savings by diversifying deposits across multiple yield sources, while enabling instant crypto-to-fiat settlements. Built for emerging markets where people need both: high-yield crypto savings and fast access to local currency.

> Earn on-chain. Spend locally. Live freely—anywhere in the world.

---

## The Problem

Holding stablecoins today means choosing between two bad options: let your money sit idle losing value to inflation, or navigate the complex world of DeFi protocols yourself. And even if you manage to earn yield, converting crypto to spendable fiat remains slow, expensive, and unreliable—especially in emerging markets.

Nester solves both problems in one protocol.

---

## How It Works

![Nester Architecture Overview]

Nester operates through three integrated layers that work together seamlessly:

| Layer | Function | Outcome |
|-------|----------|---------|
| **Savings Layer** | Diversifies deposits across lending & staking protocols | Optimized yields (8-15% APY) |
| **Offramp Layer** | Aggregates liquidity and settles to local fiat | ~3 second settlements |
| **AI Layer** | Analyzes markets and portfolios | Personalized guidance |

---

## Savings Layer

The yield engine. Deposits are automatically allocated across battle-tested DeFi protocols (Aave, Blend, Compound) to generate consistent returns without manual management.

![Savings Flow]

**Smart Vaults** let users choose their risk profile:

| Vault | Risk | Target APY | Strategy |
|-------|------|------------|----------|
| Conservative | Low | 6-8% | Stablecoin lending only |
| Balanced | Medium | 8-12% | Mixed lending + staking |
| Growth | Higher | 12-18% | Aggressive multi-protocol |

The protocol continuously monitors APYs and risk metrics, automatically rebalancing to maintain optimal performance while minimizing exposure to underperforming pools.

---

## Offramp Layer

The bridge to real-world spending. Unlike P2P marketplaces where you wait hours for a counterparty, Nester uses pre-funded liquidity nodes that enable instant settlement.

![Offramp Architecture]

**How settlement works:**

1. User initiates withdrawal (USDC → NGN)
2. LP Aggregator finds optimal swap route
3. Pre-funded node executes fiat transfer
4. Bank/mobile money receives funds (~3 seconds)
5. Automatic refund if settlement fails

Supported rails include bank transfers, mobile money (M-Pesa, MTN MoMo), and card withdrawals across African markets.

---

## AI Intelligence Layer (Prometheus)

An intelligent advisor that analyzes market conditions and user portfolios to provide data-driven recommendations. Prometheus never executes transactions automatically—it only suggests, users decide.

![AI Layer]

**Capabilities:**

- **Vault Analyzer** — Evaluates historical performance, risk metrics, and market conditions to recommend optimal vaults
- **Portfolio Tracker** — Monitors holdings across vaults and wallets, identifies concentration risks, suggests rebalancing
- **Market Intelligence** — Integrates DeFiLlama, CoinGecko, and on-chain data for real-time sentiment analysis
- **Conversational Interface** — Natural language queries: "Should I move funds to Growth Vault?" or "What's safest right now?"

All AI suggestions include reasoning and confidence levels. Disclaimer always present: recommendations are informational, not financial advice.

---

## Technical Architecture

![System Architecture]

**Smart Contracts (Soroban/Stellar)** — Vault management, deposit routing, yield distribution, rebalancing logic, LP aggregation, and swap execution.

**Backend Services** — Real-time APY monitoring, fiat settlement orchestration, and AI inference pipeline.

**Client Applications** — Web app (Next.js), mobile app (React Native), and API for integrations.

---

## Security Model

Nester is non-custodial. Users maintain full ownership of assets through smart contracts—the protocol cannot freeze, seize, or redirect funds.

**Audit Status:** [Pending — link to audits when complete]

**Risk Mitigations:** Multi-protocol diversification limits single-point-of-failure exposure. Real-time exploit monitoring with automatic pause mechanisms. Insurance fund for covered events. Rate limiting and withdrawal delays for large transactions.

---

## Getting Started

The fastest way to run the full stack locally is Docker Compose. You only need Docker and Docker Compose installed — no Go, Node, or Python required on your host.

### Prerequisites

- [Docker Desktop](https://www.docker.com/products/docker-desktop/) (includes Docker Compose v2)
- An Anthropic API key (optional — intelligence service falls back to a placeholder)

### 1. Clone and configure

```bash
git clone https://github.com/suncrestlabs/nester.git
cd nester
cp .env.example .env
# Edit .env and set ANTHROPIC_API_KEY if you want the intelligence service
```

### 2. Start all services

```bash
make dev
```

This builds and starts PostgreSQL, the Go API, the Next.js frontend, and the FastAPI intelligence service. On first run Docker pulls base images and compiles everything — expect 2–5 minutes.

| Service | URL |
|---------|-----|
| Frontend | http://localhost:3001 |
| API | http://localhost:8080 |
| API health | http://localhost:8080/health |
| Intelligence | http://localhost:8000 |
| PostgreSQL | localhost:5432 |

The database is seeded automatically with a test user, two vaults, allocations, and settlements in various states.

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

---

## Roadmap

| Phase | Focus | Status |
|-------|-------|--------|
| **Phase 1** | Core savings vaults + manual rebalancing | In Progress |
| **Phase 2** | Automated rebalancing + LP aggregator | Planned |
| **Phase 3** | Fiat offramp integration (Nigeria first) | Planned |
| **Phase 4** | AI Intelligence Layer (Prometheus) | Planned |
| **Phase 5** | Multi-region expansion | Future |

---

## How to Contribute

Nester is being built in the open. We welcome contributions from developers, designers, researchers, and DeFi enthusiasts.

### Getting Started

1. **Explore the codebase** — Familiarize yourself with the monorepo structure and existing patterns
2. **Check open issues** — Look for issues tagged `good-first-issue` or `help-wanted`
3. **Join the conversation** — Reach out before starting major work to ensure alignment

### Contribution Areas

| Area | Looking For | Skills |
|------|-------------|--------|
| Smart Contracts | Vault logic, rebalancing, LP routing | Soroban, Rust, Stellar |
| Backend | Settlement orchestration, AI pipeline | Node.js, Python, PostgreSQL |
| Frontend | Web/mobile UI, dashboards | React, Next.js, React Native |
| AI/ML | Market analysis models, risk scoring | Python, ML frameworks |
| Documentation | Guides, API docs, tutorials | Technical writing |
| Security | Audits, penetration testing, threat modeling | Smart contract security |

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

- [Website]https://nesterhq.netlify.app/)
- [GitHub](https://github.com/suncrestlabs/nester)

---

**Built by [Suncrest Labs](https://suncrestlabs.com)**

*Nester is in active development. Features and specifications may change.*
