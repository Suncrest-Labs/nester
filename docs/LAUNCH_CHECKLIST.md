# Mainnet Launch Checklist (Go / No-Go Gating)

This document defines the strict, mandatory checklist required for the Nester mainnet launch. **Every item below must be fully checked and verified** before the mainnet switch is flipped.

## Launch Gate Status

- **Overall Status:** Blocked (Pending final sign-offs)
- **Target Network:** Stellar Mainnet
- **Decision Makers:** Core Maintainers & Security Council

---

## Checklist

- [ ] **1. Audit Complete**
  - All smart contracts (Vault, Vault Factory, Adapters, Timelock, Treasury, Access Control, etc.) have undergone independent third-party security audits.
  - All critical and high findings have been fully resolved, verified, and re-tested.
  - Formal audit reports are published in `docs/security/` and linked publicly.

- [ ] **2. Multisig Live**
  - Protocol administrative keys, upgrade authorities, and treasury controls are transitioned to a secure multisig (e.g., Stellar multisig / Stellar Enterprise Fund standard with a threshold of $M$-of-$N$).
  - Timelock contracts are deployed and configured with the required delay for parameter updates and contract upgrades.
  - Multisig signers are distributed across independent parties and hardware keys are verified.
  - Signer set, key custody standards, and the rotation procedure are documented in [`docs/security/multisig-signers.md`](security/multisig-signers.md) with the signer registry filled in.

- [ ] **3. Monitoring Live**
  - Prometheus scraping, Grafana dashboards, and alert receivers (PagerDuty / Slack / webhooks) are fully deployed and operational for mainnet endpoints.
  - Synthetic probes (`synthetic-probes.yml`) and critical SLO alerts (`SLOTargetDown`, error budget burn rates, high latency) are verified and firing correctly in staging.
  - Metrics listeners and ledger/event indexer lag tracking are active.
  - The [30-day heightened-monitoring plan](POST_LAUNCH_MONITORING.md) is staffed with a named daily-reviewer rota for Day 0–30.

- [ ] **4. Runbooks Written**
  - All operational runbooks under `docs/observability/runbooks/` (including API availability, database failover, monitoring down, and incident response) are reviewed and tested in staging drills.
  - Emergency pause / money path switches (`moneypath` switches for deposits and withdrawals) are documented and verified to take effect immediately.

- [ ] **5. Load Test Passed**
  - k6 load tests (`tests/load/`) executed against an isolated staging environment meeting or exceeding mainnet targets (Vault reads 500 RPS, writes 50 RPS, portfolio aggregation 200 RPS, WebSocket connections).
  - p95 latency remains below 500 ms under steady-state load with an error rate under 1%.
  - Load test baseline report attached in `docs/LOAD_TESTING_BASELINE.md` with zero unresolved memory leaks or connection pool exhaustion.

- [ ] **6. Legal Review Done**
  - Terms of Service, Privacy Policy, and disclaimer documentation finalized and published on the website.
  - Compliance, regulatory, and token-structure reviews completed by legal counsel for target jurisdictions.

---

## Go / No-Go Decision Sign-off

| Item | Signer / Role | Date | Signature / Hash |
|---|---|---|---|
| Smart Contract Security | Lead Auditor / Security Lead | — | — |
| Infrastructure & Monitoring | DevOps Lead | — | — |
| Operations & Runbooks | SRE Lead | — | — |
| Legal & Compliance | Legal Counsel | — | — |
| Final Go / No-Go Verdict | Core Maintainer Multisig | — | — |
