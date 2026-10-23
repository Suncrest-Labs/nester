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

## Adapter Interface Specification

All yield protocol integrations (such as Blend lending pools or Soroswap AMM liquidity) must implement a uniform adapter contract interface. This enables vaults to interact with external protocols without protocol-specific logic in core contracts.

### Interface Methods

Every adapter contract must expose the following Soroban methods: