export interface DocSection {
    title: string;
    slug: string;
    icon?: string;
    children?: { title: string; slug: string }[];
}

export const docsNav: DocSection[] = [
    {
        title: "GETTING STARTED",
        slug: "getting-started",
        children: [
            { title: "Introduction", slug: "introduction" },
            { title: "Architecture Overview", slug: "architecture" },
            { title: "Quick Start", slug: "quick-start" },
        ],
    },
    {
        title: "CORE CONCEPTS",
        slug: "concepts",
        children: [
            { title: "Savings Layer", slug: "savings-layer" },
            { title: "Yield Layer", slug: "yield-layer" },
        ],
    },
    {
        title: "SMART CONTRACTS",
        slug: "contracts",
        children: [
            { title: "Overview", slug: "contracts-overview" },
            { title: "Vault Contract", slug: "vault-contract" },
            { title: "Vault Share Token", slug: "vault-token" },
            { title: "Yield Adapters", slug: "yield-adapters" },
        ],
    },
    {
        title: "BACKEND API",
        slug: "api",
        children: [
            { title: "Overview", slug: "api-overview" },
            { title: "Vault Endpoints", slug: "vault-api" },
            { title: "Position Endpoints", slug: "position-api" },
        ],
    },
    {
        title: "FRONTEND SDK",
        slug: "frontend",
        children: [
            { title: "Wallet Integration", slug: "wallet-integration" },
            { title: "Transaction Signing", slug: "transaction-signing" },
        ],
    },
    {
        title: "DEPLOYMENT",
        slug: "deployment",
        children: [
            { title: "Testnet", slug: "testnet" },
            { title: "CI / CD Pipeline", slug: "ci-cd" },
        ],
    },
];

export const docsContent: Record<string, { title: string; content: string }> = {
    introduction: {
        title: "Introduction",
        content: `# What is Nester?

Nester is a **decentralized, crypto-first savings and yield investment protocol** built on Stellar/Soroban. It automates crypto savings by diversifying vault deposits across multiple yield sources, with a live portfolio view and on-chain auto-invest.

> A decentralized protocol that optimizes user deposits through diversified yield strategies — giving everyday savers institutional-grade returns without ever giving up custody of their funds.

## The Problem

Most people who hold stablecoins face a frustrating dilemma:

- Leave USDC/USDT idle in a wallet earning **0%**
- Navigate the bewildering maze of DeFi protocols — learning about smart contracts, managing gas fees, tracking APYs across dozens of platforms
- Manually rebalancing between protocols is tedious, error-prone, and easy to abandon

For 99% of people, this complexity means their money sits idle, losing value to inflation.

## The Solution

Nester rests on **three integrated pillars**:

| Pillar | Purpose | Status |
|--------|---------|--------|
| **Smart Vaults** | Tiered, vault-based yield on stablecoins: Conservative (6–8% APY), Balanced (8–12%), Growth (12–18%) | v1 |
| **Live Portfolio** | Positions, P&L, and performance tracking in real time | v1 |
| **Auto-Invest** | On-chain recurring deposit mandates that execute automatically | v1 |

## Non-Custodial by Design

Nester never takes custody of user funds. Deposits sit in Soroban smart contracts, and users connect with their own Stellar wallets — **Freighter**, **LOBSTR**, or **xBull**. Withdrawals go straight back to the user's wallet at any time.

## Roadmap

| Version | Scope |
|---------|-------|
| **v1** | Smart Vaults, live portfolio, auto-invest mandates |
| **v2** | In-app swaps and recurring buys |
| **v3** | Fiat onramp widget, MPC embedded wallets, curated baskets |

## Tech Stack

\`\`\`
Blockchain:    Stellar / Soroban (Rust smart contracts)
Backend:       Go + Chi router + PostgreSQL (Supabase)
Frontend:      Next.js 16 + Tailwind v4 + StellarWalletsKit
Mobile:        React Native (future)
\`\`\`
`,
    },

    architecture: {
        title: "Architecture Overview",
        content: `# Architecture Overview

Nester follows a layered architecture separating concerns across clients, backend services, and the blockchain.

## System Diagram

\`\`\`
┌─────────────────────────────────────────────────────────────┐
│                         CLIENTS                             │
│                                                             │
│   Website (Next.js)    DApp (Next.js 16)    Mobile (RN)    │
│   Marketing/Landing    Stellar Wallets      iOS/Android    │
│                              │              (future)       │
└──────────────────────────────┼──────────────────────────────┘
                               │ REST API
┌──────────────────────────────┼──────────────────────────────┐
│                       BACKEND LAYER                         │
│                              │                              │
│  ┌───────────────────────────▼───────────────────────────┐  │
│  │           API Gateway (Go + Chi Router)                │  │
│  │                                                       │  │
│  │  ┌─────────────┐ ┌──────────────┐ ┌───────────────┐  │  │
│  │  │ Vault       │ │ Portfolio    │ │ User / Auth   │  │  │
│  │  │ Manager     │ │ Service      │ │ Service       │  │  │
│  │  └─────────────┘ └──────────────┘ └───────────────┘  │  │
│  │  ┌─────────────┐ ┌──────────────┐ ┌───────────────┐  │  │
│  │  │ Yield       │ │ Auto-Invest  │ │ Event         │  │  │
│  │  │ Router      │ │ Scheduler    │ │ Listener      │  │  │
│  │  └─────────────┘ └──────────────┘ └───────────────┘  │  │
│  │  ┌─────────────┐                                     │  │
│  │  │ Rebalancer  │                                     │  │
│  │  │ Service     │                                     │  │
│  │  └─────────────┘                                     │  │
│  └───────────────────────────────────────────────────────┘  │
│                              │                              │
│  ┌───────────────────────────▼───────────────────────────┐  │
│  │              PostgreSQL (Supabase)                     │  │
│  │  users │ vaults │ positions │ shares │ yield_snapshots │  │
│  │  transactions │ events │ auto_invest_mandates          │  │
│  └───────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
                               │
┌──────────────────────────────┼──────────────────────────────┐
│              BLOCKCHAIN LAYER (Stellar / Soroban)           │
│                              │                              │
│  ┌───────────────────────────▼───────────────────────────┐  │
│  │  Smart Contracts (Rust / Soroban SDK)                  │  │
│  │                                                       │  │
│  │  VaultFactory          Vault Contracts                 │  │
│  │  Deploy/manage         deposit / withdraw / position   │  │
│  │  vault instances       nVault share token mint/burn    │  │
│  │                        maturity & penalty logic        │  │
│  │  Auto-Invest Mandates  Yield Source Adapters           │  │
│  │  recurring deposit     Blend / Kamino / Aave           │  │
│  │  authorization         deposit / withdraw / balanceOf  │  │
│  └───────────────────────────────────────────────────────┘  │
│                                                             │
│  Stellar Network: Horizon API / Soroban RPC                 │
└─────────────────────────────────────────────────────────────┘
\`\`\`

## Data Flow: Deposit

\`\`\`
User clicks "Deposit 1000 USDC into Balanced Vault"
    │
    ▼
┌─────────────────┐     ┌──────────────────┐
│ Frontend         │────▶│ Go Backend       │
│ Build Soroban TX │     │ POST /deposit    │
└────────┬────────┘     │ Validate + build │
         │              │ TX envelope      │
         ▼              └────────┬─────────┘
┌─────────────────┐              │
│ Wallet Extension │◀────────────┘
│ Sign TX          │     TX envelope returned
└────────┬────────┘
         │ Signed TX
         ▼
┌─────────────────┐     ┌──────────────────┐
│ Stellar Network  │────▶│ Event Listener   │
│ Execute TX       │     │ Index deposit    │
│ Vault.deposit()  │     │ Update positions │
└─────────────────┘     └──────────────────┘
\`\`\`

## Data Flow: Auto-Invest

\`\`\`
User authorizes "Deposit 200 USDC into Balanced Vault weekly"
    │
    ▼
┌──────────────┐    ┌────────────────┐    ┌─────────────┐
│ Frontend     │───▶│ Go Backend     │───▶│ Scheduler   │
│ POST         │    │ Validate +     │    │ Registers   │
│ /mandates    │    │ store mandate  │    │ next run    │
└──────────────┘    └───────┬────────┘    └──────┬──────┘
                            │                     │ every cycle
                            ▼                     ▼
                   ┌────────────────┐    ┌─────────────┐
                   │ Soroban        │───▶│ Vault       │
                   │ Mandate        │    │ deposit()   │
                   │ authorization  │    │ shares mint │
                   └────────────────┘    └──────┬──────┘
                                                │
                   ┌────────────────┐           │
                   │ Event Listener │◀──────────┘
                   │ Index deposit  │
                   │ Update position│
                   └────────────────┘
\`\`\`

## Project Structure

\`\`\`bash
nester/
├── apps/
│   ├── website/              # Marketing site (Next.js + pnpm)
│   ├── dapp/
│   │   ├── frontend/         # DApp (Next.js 16 + npm)
│   │   └── contracts/        # Soroban smart contracts (Rust)
│   └── api/                  # API Gateway (Go + Chi + PostgreSQL)
├── packages/
│   └── contracts/            # Shared contract types/ABIs
└── mobile/                   # React Native app (future)
\`\`\`

## Database Schema

\`\`\`sql
-- Core tables (PostgreSQL / Supabase)
CREATE TABLE users (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    wallet_addr  TEXT UNIQUE NOT NULL,
    created_at   TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE vaults (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name         TEXT NOT NULL,         -- "Conservative", "Balanced", etc.
    vault_type   TEXT NOT NULL,         -- conservative | balanced | growth
    contract_id  TEXT NOT NULL,         -- Soroban contract address
    total_shares NUMERIC DEFAULT 0,
    total_assets NUMERIC DEFAULT 0,
    apy_current  NUMERIC DEFAULT 0,
    status       TEXT DEFAULT 'active', -- active | paused
    created_at   TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE positions (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID REFERENCES users(id),
    vault_id     UUID REFERENCES vaults(id),
    shares       NUMERIC NOT NULL,
    deposited_at TIMESTAMPTZ DEFAULT now(),
    maturity_at  TIMESTAMPTZ,
    UNIQUE(user_id, vault_id)
);

CREATE TABLE yield_snapshots (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    vault_id     UUID REFERENCES vaults(id),
    apy          NUMERIC NOT NULL,
    tvl          NUMERIC NOT NULL,
    recorded_at  TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE auto_invest_mandates (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID REFERENCES users(id),
    vault_id     UUID REFERENCES vaults(id),
    amount_usd   NUMERIC NOT NULL,
    cadence      TEXT NOT NULL,          -- daily | weekly | monthly
    status       TEXT DEFAULT 'active',  -- active | paused | cancelled
    next_run_at  TIMESTAMPTZ NOT NULL,
    created_at   TIMESTAMPTZ DEFAULT now()
);
\`\`\`
`,
    },

    "quick-start": {
        title: "Quick Start",
        content: `# Quick Start

Get the Nester monorepo running locally in under 5 minutes.

## Prerequisites

- **Node.js** ≥ 20 (22 for dapp frontend)
- **pnpm** ≥ 8 (for website)
- **npm** ≥ 9 (for dapp frontend)
- **Rust** + \`soroban-cli\` (for smart contracts)
- **Go** ≥ 1.22 (for API backend)

## Clone & Install

\`\`\`bash
git clone https://github.com/Suncrest-Labs/nester.git
cd nester

# Website (pnpm)
pnpm install

# Go API Backend (Go)
cd apps/api && go mod download && cd ../..

# DApp frontend (npm)
cd apps/dapp/frontend && npm install && cd ../../..
\`\`\`

## Run Development Servers

\`\`\`bash
# Website — runs on localhost:3000
pnpm --filter @nester/website dev

# Go API Backend — runs on localhost:8080
cd apps/api && go run cmd/api/main.go

# DApp frontend — runs on localhost:3001
cd apps/dapp/frontend && npm run dev
\`\`\`

## Build & Verify

\`\`\`bash
# Must pass before pushing — CI runs these exact commands
pnpm --filter @nester/website build && pnpm --filter @nester/website lint
cd apps/dapp/frontend && npm run build && npm run lint && cd ../../..
cd apps/api && go test ./... && cd ../..
\`\`\`

## Smart Contracts (Soroban)

\`\`\`bash
cd packages/contracts

# Build all contracts
soroban contract build

# Run tests
cargo test

# Deploy to testnet
soroban contract deploy \\
  --wasm target/wasm32-unknown-unknown/release/vault.wasm \\
  --source <ACCOUNT> \\
  --network testnet
\`\`\`

## Environment Variables

\`\`\`bash
# apps/api/.env
DATABASE_URL=postgresql://user:pass@host:5432/nester
SOROBAN_RPC_URL=https://soroban-testnet.stellar.org
\`\`\`
`,
    },

    "savings-layer": {
        title: "Savings Layer",
        content: `# Stablecoin Savings Layer

The Savings Layer is the core of Nester's MVP. Users deposit stablecoins (USDC, USDT, DAI) into protocol-managed vaults that auto-allocate across multiple yield sources.

## How It Works

\`\`\`
User deposits 1,000 USDC
        │
        ▼
┌───────────────────────────────────────────────┐
│            Balanced Vault Contract             │
│                                               │
│  User receives 1,000 nVault share tokens      │
│                                               │
│  Allocation strategy:                         │
│  ┌─────────┐  ┌─────────┐  ┌─────────┐      │
│  │ Blend   │  │ Kamino  │  │ Aave    │      │
│  │ 40%     │  │ 35%     │  │ 25%     │      │
│  │ 400 USDC│  │ 350 USDC│  │ 250 USDC│      │
│  │ 8.5% APY│  │ 10% APY │  │ 9% APY  │      │
│  └─────────┘  └─────────┘  └─────────┘      │
│                                               │
│  Blended APY: 9.08%                          │
│  After 1 year: 1,000 → 1,090.80 USDC        │
└───────────────────────────────────────────────┘
\`\`\`

## Vault Types

| Vault | Target APY | Risk Profile | Strategy |
|-------|-----------|--------------|----------|
| **Conservative** | 6–8% | Minimal | Battle-tested lending protocols only |
| **Balanced** | 8–12% | Moderate | Lending + selected liquidity pools |
| **Growth** | 12–18% | Higher | Newer strategies with strict risk controls |
| **DeFi500** | Variable | Diversified | Index fund of top DeFi protocols, monthly rebalanced |

## Share Token Model

When you deposit into a vault, you receive **nVault** tokens proportional to your share:

\`\`\`
shares_minted = deposit_amount × total_shares / total_assets

Example:
  Vault has 10,000 USDC (total_assets) and 10,000 shares (total_shares)
  User deposits 1,000 USDC
  shares = 1,000 × 10,000 / 10,000 = 1,000 nVault tokens

After yield accrual (vault now has 11,000 USDC, still 11,000 shares):
  User's 1,000 shares = 1,000 / 11,000 × 11,000 = 1,000 USDC + yield
\`\`\`

## Automated Rebalancing

The rebalancer runs continuously, monitoring:

- **APY drift** — if a protocol's APY drops below threshold, funds migrate
- **Risk metrics** — TVL changes, exploit history, smart contract health
- **Liquidity depth** — ensures withdrawals can be processed instantly

\`\`\`
Rebalance trigger conditions:
  1. APY delta > 2% between current and optimal allocation
  2. Protocol TVL drops > 20% in 24 hours
  3. Security alert from monitoring systems
  4. Scheduled rebalance (weekly for DeFi500)
\`\`\`

## Maturity & Penalties

Vaults can optionally enforce time-locks:

| Lock Period | Early Withdrawal Penalty |
|-------------|-------------------------|
| No lock     | 0% |
| 30 days     | 1% of withdrawn amount |
| 90 days     | 2.5% |
| 180 days    | 5% |

Penalties are redistributed to remaining vault depositors, increasing their share value.
`,
    },

    "yield-layer": {
        title: "Yield Layer",
        content: `# Automated Yield Layer (Multi-Asset)

Separate from the stablecoin savings, the Automated Yield Layer lets users earn on volatile assets like XLM, BTC, and ETH while maintaining full price exposure.

## Key Difference from Savings

| | Savings Layer | Yield Layer |
|---|---|---|
| **Assets** | USDC, USDT, DAI | XLM, BTC, ETH, etc. |
| **Price exposure** | None (stablecoins) | Full (up and down) |
| **Yield source** | Lending protocols | Staking, LP, lending |
| **Risk** | Low (capital preserved) | Medium-High (price volatility) |
| **Use case** | Predictable savings | Growth + yield |

## Per-Asset Strategies

\`\`\`
XLM  → Auto-stake (8% base) + Liquidity provision (12% with IL risk)
       Strategy engine allocates: 60% staking, 40% LP based on market conditions

BTC  → Wrapped to Stellar-compatible token
       Deployed to Bitcoin-backed lending protocols (4–6% APY)
       100% BTC exposure maintained

ETH  → Liquid staking derivatives (e.g., stETH)
       3–5% staking yield + DeFi composability
\`\`\`

## Portfolio Allocation

Users can select multiple tokens simultaneously:

\`\`\`
Portfolio example:
  40% XLM  — auto-staked + liquidity provision
  30% BTC  — wrapped and deployed to lending
  20% ETH  — liquid staking derivatives
  10% USDC — Balanced Vault (stability anchor)
\`\`\`

## Automated Rebalancing

The rebalancer monitors drift from target allocations and surfaces alerts in your portfolio:

\`\`\`
Alert: "XLM now represents 45% of your portfolio (target: 30%).
        XLM rallied 50% this month.
        Consider rebalancing to reduce concentration risk.
        Suggested action: Sell 30% of XLM position → Balanced Vault"

[Approve]  [Dismiss]
\`\`\`
`,
    },

    "contracts-overview": {
        title: "Smart Contracts Overview",
        content: `# Smart Contracts Overview

Nester's on-chain logic runs on **Stellar's Soroban** platform using Rust smart contracts.

## Contract Architecture

\`\`\`
┌────────────────────────────────────────────────────────┐
│                    VaultFactory                         │
│  deploy_vault() → creates new Vault instance           │
│  list_vaults() → returns all active vaults             │
│  pause_vault() / unpause_vault()                       │
└────────────────────────┬───────────────────────────────┘
                         │ deploys
         ┌───────────────┼───────────────┐
         ▼               ▼               ▼
┌──────────────┐ ┌──────────────┐ ┌──────────────┐
│ Conservative │ │   Balanced   │ │    Growth    │
│    Vault     │ │    Vault     │ │    Vault     │
│              │ │              │ │              │
│ deposit()    │ │ deposit()    │ │ deposit()    │
│ withdraw()   │ │ withdraw()   │ │ withdraw()   │
│ get_position │ │ get_position │ │ get_position │
│ rebalance()  │ │ rebalance()  │ │ rebalance()  │
└──────┬───────┘ └──────┬───────┘ └──────┬───────┘
       │                │                │
       └────────────────┼────────────────┘
                        │ uses
         ┌──────────────┼──────────────┐
         ▼              ▼              ▼
┌──────────────┐ ┌────────────┐ ┌────────────┐
│ Blend        │ │ Kamino     │ │ Aave       │
│ Adapter      │ │ Adapter    │ │ Adapter    │
│              │ │            │ │            │
│ deposit()    │ │ deposit()  │ │ deposit()  │
│ withdraw()   │ │ withdraw() │ │ withdraw() │
│ balance_of() │ │ balance_of │ │ balance_of │
└──────────────┘ └────────────┘ └────────────┘
\`\`\`

## Workspace Structure

\`\`\`bash
packages/contracts/
├── Cargo.toml            # Workspace manifest
├── vault/
│   ├── Cargo.toml
│   └── src/lib.rs        # Core vault logic
├── vault_token/
│   ├── Cargo.toml
│   └── src/lib.rs        # nVault share token (SEP-41)
├── vault_factory/
│   ├── Cargo.toml
│   └── src/lib.rs        # Factory for deploying vaults
├── yield_registry/
│   ├── Cargo.toml
│   └── src/lib.rs        # Approved yield source registry
├── strategy/
│   ├── Cargo.toml
│   └── src/lib.rs        # Allocation strategy engine
└── adapters/
    ├── blend/src/lib.rs   # Blend protocol adapter
    └── ...
\`\`\`
`,
    },

    "vault-contract": {
        title: "Vault Contract",
        content: `# Vault Contract

The Vault contract is the core primitive of Nester's savings layer. It accepts stablecoin deposits, tracks share ownership, and manages yield source allocations.

## Interface

\`\`\`rust
#![no_std]
use soroban_sdk::{contract, contractimpl, Address, Env, Vec};

#[contract]
pub struct Vault;

#[contractimpl]
impl Vault {
    /// Initialize a new vault with admin, asset token, and strategy
    pub fn initialize(
        env: Env,
        admin: Address,
        asset: Address,       // USDC token contract
        share_token: Address, // nVault token contract
        strategy: Address,    // Allocation strategy contract
    );

    /// Deposit stablecoins and receive share tokens
    pub fn deposit(
        env: Env,
        user: Address,
        amount: i128,
    ) -> i128 {
        user.require_auth();

        let total_assets = Self::total_assets(&env);
        let total_shares = Self::total_shares(&env);

        // Calculate shares to mint
        let shares = if total_shares == 0 {
            amount  // First depositor gets 1:1 shares
        } else {
            amount * total_shares / total_assets
        };

        // Transfer USDC from user to vault
        token::transfer(&env, &user, &env.current_contract_address(), amount);

        // Mint nVault share tokens to user
        share_token::mint(&env, &user, shares);

        // Update vault totals
        Self::set_total_assets(&env, total_assets + amount);
        Self::set_total_shares(&env, total_shares + shares);

        // Emit deposit event
        env.events().publish(
            (symbol_short!("deposit"), user.clone()),
            (amount, shares),
        );

        shares
    }

    /// Withdraw stablecoins by burning share tokens
    pub fn withdraw(
        env: Env,
        user: Address,
        shares: i128,
    ) -> i128 {
        user.require_auth();

        let total_assets = Self::total_assets(&env);
        let total_shares = Self::total_shares(&env);

        // Calculate underlying amount
        let amount = shares * total_assets / total_shares;

        // Check maturity and apply penalty if early
        let penalty = Self::calculate_penalty(&env, &user, amount);
        let net_amount = amount - penalty;

        // Burn user's share tokens
        share_token::burn(&env, &user, shares);

        // Transfer USDC to user
        token::transfer(&env, &env.current_contract_address(), &user, net_amount);

        // Update vault totals (penalty stays in vault, boosting other holders)
        Self::set_total_assets(&env, total_assets - net_amount);
        Self::set_total_shares(&env, total_shares - shares);

        env.events().publish(
            (symbol_short!("withdraw"), user.clone()),
            (net_amount, shares, penalty),
        );

        net_amount
    }

    /// Get user's current position
    pub fn get_position(env: Env, user: Address) -> Position {
        let shares = share_token::balance(&env, &user);
        let total_assets = Self::total_assets(&env);
        let total_shares = Self::total_shares(&env);
        let value = shares * total_assets / total_shares;

        Position { shares, value }
    }

    /// Get vault info (for frontend display)
    pub fn get_info(env: Env) -> VaultInfo {
        VaultInfo {
            total_assets: Self::total_assets(&env),
            total_shares: Self::total_shares(&env),
            apy_estimate: Self::current_apy(&env),
            allocations: Self::get_allocations(&env),
        }
    }

    /// Rebalance vault allocations (admin only)
    pub fn rebalance(env: Env, admin: Address, new_allocations: Vec<Allocation>) {
        admin.require_auth();
        Self::verify_admin(&env, &admin);
        // Execute rebalance across yield sources
    }

    /// Emergency pause (admin only)
    pub fn pause(env: Env, admin: Address) {
        admin.require_auth();
        Self::verify_admin(&env, &admin);
        Self::set_paused(&env, true);
    }
}
\`\`\`

## Events

| Event | Data | When |
|-------|------|------|
| \`deposit\` | \`(amount, shares)\` | User deposits into vault |
| \`withdraw\` | \`(net_amount, shares, penalty)\` | User withdraws from vault |
| \`rebalance\` | \`(new_allocations)\` | Admin triggers rebalance |
| \`pause\` | \`(admin)\` | Vault paused for emergency |

## Storage Layout

\`\`\`rust
// Persistent storage keys
enum DataKey {
    Admin,          // Address
    Asset,          // Address (USDC token)
    ShareToken,     // Address (nVault token)
    Strategy,       // Address (allocation strategy)
    TotalAssets,    // i128
    TotalShares,    // i128
    Paused,         // bool
    Maturity(Address), // Timestamp per user
}
\`\`\`
`,
    },

    "vault-token": {
        title: "Vault Share Token (nVault)",
        content: `# Vault Share Token (nVault)

The nVault token is a Soroban token contract (SEP-41 compliant) that represents a user's proportional ownership of a vault's total assets.

## Interface

\`\`\`rust
#[contract]
pub struct VaultToken;

#[contractimpl]
impl VaultToken {
    pub fn initialize(env: Env, admin: Address, name: String, symbol: String, decimals: u32);

    /// Mint shares to user (called by vault on deposit)
    pub fn mint(env: Env, to: Address, amount: i128) {
        Self::require_vault_caller(&env);  // Only vault contract can mint
        // ... mint logic
    }

    /// Burn shares from user (called by vault on withdraw)
    pub fn burn(env: Env, from: Address, amount: i128) {
        Self::require_vault_caller(&env);
        // ... burn logic
    }

    // Standard SEP-41 token interface
    pub fn balance(env: Env, id: Address) -> i128;
    pub fn transfer(env: Env, from: Address, to: Address, amount: i128);
    pub fn allowance(env: Env, from: Address, spender: Address) -> i128;
    pub fn approve(env: Env, from: Address, spender: Address, amount: i128);
    pub fn total_supply(env: Env) -> i128;
    pub fn name(env: Env) -> String;
    pub fn symbol(env: Env) -> String;
    pub fn decimals(env: Env) -> u32;
}
\`\`\`

## DeFi500 Token (nDEFI)

The DeFi500 vault uses a special \`nDEFI\` token that represents diversified exposure to an index of top DeFi protocols. Monthly rebalancing adjusts the underlying allocation to track the best performers.

\`\`\`
nDEFI token value tracks:
  Top 5 DeFi protocols by risk-adjusted yield
  Monthly rebalance adjusts weights
  User holds 1 token = exposure to entire index
\`\`\`
`,
    },

    "yield-adapters": {
        title: "Yield Source Adapters",
        content: `# Yield Source Adapters

Each yield source (Blend, Kamino, Aave) has a standardized adapter contract that the vault uses to deposit, withdraw, and check balances.

## Adapter Interface

\`\`\`rust
/// Standard interface all yield adapters must implement
pub trait YieldAdapter {
    /// Deposit assets into the yield source
    fn deposit(env: Env, amount: i128) -> i128;

    /// Withdraw assets from the yield source
    fn withdraw(env: Env, amount: i128) -> i128;

    /// Check current balance in the yield source
    fn balance_of(env: Env) -> i128;

    /// Get current APY (in basis points, e.g., 950 = 9.50%)
    fn current_apy(env: Env) -> u32;
}
\`\`\`

## Blend Adapter (Native Stellar)

\`\`\`rust
#[contract]
pub struct BlendAdapter;

#[contractimpl]
impl BlendAdapter {
    pub fn deposit(env: Env, amount: i128) -> i128 {
        let blend_pool = Self::get_pool_address(&env);
        // Call Blend's lending pool deposit
        blend_pool::supply(&env, &env.current_contract_address(), amount);
        amount
    }

    pub fn withdraw(env: Env, amount: i128) -> i128 {
        let blend_pool = Self::get_pool_address(&env);
        blend_pool::withdraw(&env, &env.current_contract_address(), amount);
        amount
    }

    pub fn balance_of(env: Env) -> i128 {
        let blend_pool = Self::get_pool_address(&env);
        blend_pool::get_balance(&env, &env.current_contract_address())
    }

    pub fn current_apy(env: Env) -> u32 {
        // Fetch from Blend's rate oracle
        850 // 8.50% example
    }
}
\`\`\`

## Yield Source Registry

The registry contract tracks which adapters are approved, their risk scores, and maximum allocation limits.

\`\`\`rust
pub struct YieldSource {
    pub adapter: Address,     // Adapter contract address
    pub name: String,         // "Blend", "Kamino", etc.
    pub risk_score: u32,      // 1-10 (1 = lowest risk)
    pub max_allocation: u32,  // Max % of vault this source can hold
    pub is_active: bool,
}
\`\`\`
`,
    },

    "api-overview": {
        title: "Backend API Overview",
        content: `# Backend API Overview

The Nester backend is built with **Go + Chi router**, serving as the API gateway between the frontend and blockchain/external services.

## Base Configuration

\`\`\`go
package main

import (
    "log/slog"
    "net/http"
    "os"

    "github.com/go-chi/chi/v5"
    "github.com/go-chi/chi/v5/middleware"
    "github.com/go-chi/cors"
)

func main() {
    logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

    r := chi.NewRouter()

    // Middleware
    r.Use(middleware.RequestID)
    r.Use(middleware.RealIP)
    r.Use(middleware.Logger)
    r.Use(middleware.Recoverer)
    r.Use(cors.Handler(cors.Options{
        AllowedOrigins:   []string{"https://nester.finance", "http://localhost:3001"},
        AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
        AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
        AllowCredentials: true,
    }))
    r.Use(middleware.Timeout(30 * time.Second))

    // Routes
    r.Route("/api/v1", func(r chi.Router) {
        r.Route("/vaults", vaultRoutes)
        r.Route("/positions", positionRoutes)
        r.Route("/yields", yieldRoutes)
        r.Route("/users", userRoutes)
    })

    // Health check
    r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
        w.Write([]byte(\`{"status":"ok"}\`))
    })

    logger.Info("starting server", "port", 8080)
    http.ListenAndServe(":8080", r)
}
\`\`\`

## API Response Envelope

All API responses follow a consistent envelope:

\`\`\`go
type APIResponse struct {
    Success bool        \`json:"success"\`
    Data    interface{} \`json:"data,omitempty"\`
    Error   *APIError   \`json:"error,omitempty"\`
    Meta    *Meta       \`json:"meta,omitempty"\`
}

type APIError struct {
    Code    string \`json:"code"\`
    Message string \`json:"message"\`
}

// Example success response
{
    "success": true,
    "data": { "vault_id": "abc-123", "shares": 1000 }
}

// Example error response
{
    "success": false,
    "error": { "code": "INSUFFICIENT_BALANCE", "message": "Not enough USDC" }
}
\`\`\`
`,
    },

    "vault-api": {
        title: "Vault Endpoints",
        content: `# Vault API Endpoints

## GET /api/v1/vaults

List all available vaults with current APY and TVL.

\`\`\`bash
curl https://api.nester.finance/api/v1/vaults
\`\`\`

\`\`\`json
{
    "success": true,
    "data": [
        {
            "id": "vault_conservative_01",
            "name": "Conservative",
            "type": "conservative",
            "apy": 7.2,
            "tvl": 250000,
            "total_depositors": 45,
            "status": "active",
            "allocations": [
                { "source": "Blend", "percentage": 60, "apy": 7.0 },
                { "source": "Aave", "percentage": 40, "apy": 7.5 }
            ]
        },
        {
            "id": "vault_balanced_01",
            "name": "Balanced",
            "type": "balanced",
            "apy": 9.5,
            "tvl": 580000,
            "total_depositors": 120,
            "status": "active",
            "allocations": [
                { "source": "Blend", "percentage": 40, "apy": 8.5 },
                { "source": "Kamino", "percentage": 35, "apy": 10.0 },
                { "source": "Aave", "percentage": 25, "apy": 9.0 }
            ]
        }
    ]
}
\`\`\`

## GET /api/v1/vaults/:id/allocations

Get detailed allocation breakdown for a specific vault.

## POST /api/v1/vaults/{id}/deposit

Prepare and record a deposit to a vault.

\`\`\`bash
curl -X POST https://api.nester.finance/api/v1/vaults/vault_balanced_01/deposit \\
  -H "Content-Type: application/json" \\
  -d '{
    "amount": "1000.00",
    "asset": "USDC"
  }'
\`\`\`

\`\`\`json
{
    "success": true,
    "data": {
        "id": "vault_balanced_01",
        "user_id": "8c7d86f0-0b73-45cd-95f3-c5b9bf10e4a7",
        "contract_address": "CB...",
        "total_deposited": "6000.000000",
        "current_balance": "6250.500000",
        "currency": "USDC",
        "status": "active",
        "yield_earned": "250.500000",
        "fees_paid": "0.000000",
        "allocations": [
            {
                "id": "alloc_01_uuid",
                "vault_id": "vault_balanced_01",
                "protocol": "Blend",
                "amount": "2400.00",
                "apy": "8.5",
                "status": "active",
                "allocated_at": "2026-01-15T10:30:00Z"
            }
        ],
        "created_at": "2026-01-15T10:30:00Z",
        "updated_at": "2026-06-02T12:00:00Z"
    }
}
\`\`\`

## POST /api/v1/vaults/{id}/withdraw

Prepare and record a withdrawal from a vault.

\`\`\`bash
curl -X POST https://api.nester.finance/api/v1/vaults/vault_balanced_01/withdraw \\
  -H "Content-Type: application/json" \\
  -d '{
    "amount": "500.00",
    "asset": "USDC"
  }'
\`\`\`

\`\`\`json
{
    "success": true,
    "data": {
        "id": "vault_balanced_01",
        "user_id": "8c7d86f0-0b73-45cd-95f3-c5b9bf10e4a7",
        "contract_address": "CB...",
        "total_deposited": "5500.000000",
        "current_balance": "5750.500000",
        "currency": "USDC",
        "status": "active",
        "yield_earned": "250.500000",
        "fees_paid": "0.000000",
        "created_at": "2026-01-15T10:30:00Z",
        "updated_at": "2026-06-02T12:00:00Z"
    }
}
\`\`\`
`,
    },

    "position-api": {
        title: "Position Endpoints",
        content: `# Position & Portfolio API Endpoints

## GET /api/v1/portfolio/summary

Get aggregated portfolio summary and positions for the authenticated user.

\`\`\`bash
curl https://api.nester.finance/api/v1/portfolio/summary \\
  -H "Authorization: Bearer <token>"
\`\`\`

\`\`\`json
{
    "success": true,
    "data": {
        "total_deposited_usdc": "5000.000000",
        "total_current_value_usdc": "5250.500000",
        "total_yield_earned_usdc": "250.500000",
        "positions": [
            {
                "vault_id": "vault_balanced_01",
                "vault_name": "Balanced",
                "deposited": "5000.000000",
                "current_value": "5250.500000",
                "shares": "0.000000",
                "apy_7d": "9.500000"
            }
        ]
    }
}
\`\`\`

## GET /api/v1/vaults/{id}/my-position

Get authenticated user's position in a specific vault.

\`\`\`bash
curl https://api.nester.finance/api/v1/vaults/vault_balanced_01/my-position \\
  -H "Authorization: Bearer <token>"
\`\`\`

## GET /api/v1/vaults/{id}/performance/history

Get historical performance and yield data for charting.

\`\`\`bash
curl "https://api.nester.finance/api/v1/vaults/vault_balanced_01/performance/history?period=30d"
\`\`\`

\`\`\`json
{
    "success": true,
    "data": [
        { "recorded_at": "2026-02-20T12:00:00Z", "apy": 9.2, "tvl": 520000 },
        { "recorded_at": "2026-02-21T12:00:00Z", "apy": 9.5, "tvl": 535000 },
        { "recorded_at": "2026-02-22T12:00:00Z", "apy": 9.3, "tvl": 548000 }
    ]
}
\`\`\`
`,
    },

    "wallet-integration": {
        title: "Wallet Integration",
        content: `# Wallet Integration

Nester uses **StellarWalletsKit** to support multiple Stellar wallets via a unified adapter.

## Setup

\`\`\`bash
npm install @creit.tech/stellar-wallets-kit @stellar/stellar-sdk
\`\`\`

## Provider Implementation

\`\`\`typescript
import { StellarWalletsKit } from "@creit.tech/stellar-wallets-kit";
import { defaultModules } from "@creit.tech/stellar-wallets-kit/modules/utils";

// Initialize kit with all default wallet modules
const kit = new StellarWalletsKit({
    network: "TESTNET",
    selectedWalletId: "freighter",
    modules: defaultModules(),
});

// Supported wallets (via defaultModules):
// Freighter, Lobstr, xBull, Hana, Rabet, Albedo,
// WalletConnect, XDEFI, Hot Wallet
\`\`\`

## Connection Flow

\`\`\`typescript
async function connectWallet(walletId: string) {
    // 1. Set the active wallet
    kit.setWallet(walletId);

    // 2. Get address from the wallet extension directly
    const module = kit.selectedModule;
    const { address } = await module.getAddress();

    // 3. Sync the kit's internal state
    const { activeAddress } = await import(
        "@creit.tech/stellar-wallets-kit/state"
    );
    activeAddress.value = address;

    // 4. Store session for persistence
    localStorage.setItem("nester_wallet_id", walletId);
    localStorage.setItem("nester_wallet_addr", address);

    return address;
}
\`\`\`

> **Important:** Call \`kit.selectedModule.getAddress()\`, NOT \`kit.getAddress()\`. The latter only reads from memory and will throw "No wallet connected" if \`activeAddress\` hasn't been set.

## Handling Uninstalled Wallets

\`\`\`typescript
const INSTALL_URLS: Record<string, string> = {
    freighter: "https://chromewebstore.google.com/detail/freighter/bcacfldlkkdogcmkkibnjlakofdplcbk",
    lobstr: "https://chromewebstore.google.com/detail/lobstr-signer/ldiagbjmlmfagcjpbbkobgjgkihfkiab",
    xbull: "https://chromewebstore.google.com/detail/xbull-wallet/omajpeaffjgmlpmhbfdmmaplefklipcb",
    // ... more wallets
};

async function connect(walletId: string) {
    try {
        return await connectWallet(walletId);
    } catch (error) {
        // Wallet not installed — redirect to Chrome Web Store
        const installUrl = INSTALL_URLS[walletId];
        if (installUrl) {
            window.open(installUrl, "_blank");
        }
    }
}
\`\`\`
`,
    },

    "transaction-signing": {
        title: "Transaction Signing",
        content: `# Transaction Signing

After building a Soroban transaction via the backend API, the frontend signs it with the user's wallet.

## Deposit Transaction Flow

\`\`\`typescript
import * as StellarSdk from "@stellar/stellar-sdk";

async function executeDeposit(vaultId: string, amount: number) {
    const wallet = getConnectedWallet(); // from wallet provider

    // 1. Get transaction envelope from backend
    const res = await fetch("/api/v1/deposit", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
            vault_id: vaultId,
            amount,
            wallet_address: wallet.address,
        }),
    });
    const { data } = await res.json();

    // 2. Sign with wallet extension
    const { signedTxXdr } = await kit.selectedModule.signTransaction(
        data.transaction_xdr,
        {
            networkPassphrase: StellarSdk.Networks.TESTNET,
        }
    );

    // 3. Submit to Stellar network
    const server = new StellarSdk.SorobanRpc.Server(
        "https://soroban-testnet.stellar.org"
    );
    const tx = StellarSdk.TransactionBuilder.fromXDR(
        signedTxXdr,
        StellarSdk.Networks.TESTNET
    );
    const result = await server.sendTransaction(tx);

    // 4. Poll for confirmation
    if (result.status === "PENDING") {
        const confirmed = await pollTransaction(server, result.hash);
        return confirmed;
    }

    return result;
}
\`\`\`

## Error Handling

\`\`\`typescript
// StellarWalletsKit throws IKitError objects, not Error instances
interface IKitError {
    code: number;
    message: string;
}

function extractErrorMessage(err: unknown): string {
    if (err && typeof err === "object") {
        if ("message" in err) return String((err as IKitError).message);
    }
    if (err instanceof Error) return err.message;
    return "Unknown error";
}
\`\`\`
`,
    },

    testnet: {
        title: "Testnet Deployment",
        content: `# Testnet Deployment

Nester deploys to Stellar's Soroban testnet for beta testing before mainnet launch.

## Network Configuration

\`\`\`bash
# Stellar Testnet
NETWORK=testnet
SOROBAN_RPC_URL=https://soroban-testnet.stellar.org
HORIZON_URL=https://horizon-testnet.stellar.org
NETWORK_PASSPHRASE="Test SDF Network ; September 2015"
\`\`\`

## Deploying Contracts

\`\`\`bash
# 1. Build WASM binaries
cd packages/contracts
soroban contract build

# 2. Deploy VaultFactory
soroban contract deploy \\
  --wasm target/wasm32-unknown-unknown/release/vault_factory.wasm \\
  --source alice \\
  --network testnet

# Output: CONTRACT_ID (e.g., CDLZFC...)

# 3. Initialize VaultFactory
soroban contract invoke \\
  --id CDLZFC... \\
  --source alice \\
  --network testnet \\
  -- initialize \\
  --admin alice

# 4. Deploy a vault through the factory
soroban contract invoke \\
  --id CDLZFC... \\
  --source alice \\
  --network testnet \\
  -- deploy_vault \\
  --name "Balanced" \\
  --vault-type balanced \\
  --asset USDC_TOKEN_ID
\`\`\`

## Getting Testnet Tokens

\`\`\`bash
# Fund account with testnet XLM
curl "https://friendbot.stellar.org?addr=GBXYZ..."

# Mint testnet USDC (using test token contract)
soroban contract invoke \\
  --id TEST_USDC_CONTRACT \\
  --source alice \\
  --network testnet \\
  -- mint \\
  --to GBXYZ... \\
  --amount 10000
\`\`\`

## Explorers

- **Stellar Expert:** \`stellar.expert/explorer/testnet\`
- **StellarChain:** \`stellarchain.io\`
`,
    },

    "ci-cd": {
        title: "CI / CD Pipeline",
        content: `# CI / CD Pipeline

CI runs on every push/PR to \`main\` and \`dev\`. All jobs must pass before merging.

## GitHub Actions Workflow

\`\`\`yaml
# .github/workflows/ci.yml
name: CI

on:
  push:
    branches: [main, dev]
  pull_request:
    branches: [main, dev]

jobs:
  website:
    name: Website (Next.js)
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: pnpm/action-setup@v2
        with: { version: 8 }
      - uses: actions/setup-node@v4
        with: { node-version: 20, cache: pnpm }
      - run: pnpm install --frozen-lockfile
      - run: pnpm --filter @nester/website build
      - run: pnpm --filter @nester/website lint

  dapp-frontend:
    name: Dapp Frontend (Next.js)
    runs-on: ubuntu-latest
    defaults:
      run: { working-directory: apps/dapp/frontend }
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with: { node-version: 22 }
      - run: npm install
      - run: npm run build

\`\`\`

## Pre-Push Checklist

\`\`\`bash
# Run ALL of these from repo root before pushing:

# 1. Website
pnpm --filter @nester/website build && pnpm --filter @nester/website lint

# 2. Dapp Frontend
cd apps/dapp/frontend && npm run build && cd ../../..
\`\`\`

## Common CI Failures

| Failure | Fix |
|---------|-----|
| \`npm ci\` sync error | Run \`npm install\` in affected directory, commit lock file |
| \`pnpm --frozen-lockfile\` fails | Run \`pnpm install\` at root, commit lock file |
| Build error | Fix TypeScript/import errors locally |
| Lint error | Run lint command locally, fix violations |
`,
    },
};
