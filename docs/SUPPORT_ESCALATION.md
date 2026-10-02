# Mainnet Support & Incident Escalation Tree

Before real money is at stake on mainnet, this document outlines how users can reach support, how reports escalate from front-line support to on-call engineering, and the expected response-time SLAs.

## 1. Support Channels for Users

Users experiencing issues with Nester on mainnet should reach out through official channels only:

- **In-App Problem Report**: Available in the dApp settings / error screens (`#1143`). Automatically attaches sanitized diagnostics and user context.
- **Official Discord Support Channel**: `#support` on the Nester Discord server (community-staffed, moderated by official team members).
- **Support Email**: `support@nester.fi` (monitored during business hours with global coverage for urgent queues).

*Note: Team members will never DM users first asking for private keys, seed phrases, or funds transfers.*

---

## 2. Response-Time SLAs

SLAs are defined based on incident severity and user-impact tiers:

| Severity / Tier | Description | Initial Response SLA | Resolution Target SLA |
|---|---|---|---| 
| **P1 — Critical** | Active fund loss, global pause needed, protocol insolvency risk, bridge/indexer desync affecting multiple users | **15 minutes** (24/7 on-call) | **2 hours** (mitigation/fix) |
| **P2 — Major** | Individual deposit/withdrawal stuck beyond expected time, wallet connection outage for subset of users | **1 hour** (business hours + on-call backup) | **8 hours** |
| **P3 — Moderate** | Incorrect balance display without actual loss, UI rendering glitches, non-blocking feature errors | **4 hours** (business hours) | **24 hours** |
| **P4 — Low** | General questions, feature requests, documentation feedback | **24 hours** | **72 hours** |

---

## 3. Escalation Tree (Support to Engineering)

When a user report cannot be resolved by front-line support using the [Support Triage Guide](SUPPORT_TRIAGE_GUIDE.md), it escalates through the following tiers:

### Tier 1: Front-Line Support / Community Moderators
- **Who**: Support agents and community managers.
- **Action**: Collects user wallet address, transaction hash, approximate timestamp, and screenshots. Uses `GET /api/v1/admin/users/{id}/money-path` to inspect internal state.
- **Escalation Trigger**: Unresolved after initial triage, or matches P1/P2 criteria.

### Tier 2: Escalation Lead / Support Operations
- **Who**: Senior support engineer or designated shift lead.
- **Action**: Verifies indexer and chain health. Checks Prometheus/Grafana dashboards and error tracking (Sentry). Confirms whether the issue is isolated or systemic.
- **Escalation Trigger**: Systemic issue, repeated withdrawal/deposit failure, indexer lag, or suspected contract anomaly.

### Tier 3: On-Call Software Engineer (Backend / Contracts)
- **Who**: Primary on-call engineer via PagerDuty rotation.
- **Action**: Investigates backend service logs, examines raw on-chain state, and coordinates emergency patches. For a system-wide incident, engages the global maintenance mode (`PUT /api/v1/admin/maintenance`), which halts or sets the API read-only across all vaults; for an incident isolated to a single vault, pauses that vault individually instead (`POST /api/v1/admin/vaults/{id}/pause`) rather than halting the whole system.
- **Escalation Trigger**: Confirmed bug in indexer, API, smart contract, or active P1 security/fund safety incident.

### Tier 4: Core Protocol Maintainers & Security Lead
- **Who**: Lead architects and security response team.
- **Action**: Authorizes emergency protocol upgrades, coordinates with audited protocol partners (Blend, Soroswap, etc.), and publishes public incident post-mortems.
- **Escalation Trigger**: Multi-sig intervention required, active exploit, or severe protocol-level failure.
