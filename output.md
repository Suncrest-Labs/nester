markdown
---
title: "AIGON‑X Bug Bounty Program"
date: 2026-09-01
lastmod: 2026-09-01
tags: ["bug bounty", "security", "immunefi"]
description: "Official bug bounty program for AIGON‑X launch contracts and API money path."
canonical: https://aigon-x.com/security
---

# AIGON‑X Bug Bounty Program

> **Status:** Active  
> **Platform:** Immunefi  
> **Program URL:** <https://immunefi.com/bounty/armory>  
> **Effective Date:** 2026‑09‑01  
> **Next Review:** 2027‑03‑01  
> **Last Updated:** 2026‑09‑01  

---

## 📚 Table of Contents
1. [Program Overview](#program-overview)
2. [Scope](#scope)
   - [In‑Scope Contracts](#in‑scope-contracts)
   - [In‑Scope API Money Path](#in‑scope-api-money-path)
   - [Out of Scope](#out-of-scope)
3. [Severity Tiers & Payouts](#severity-tiers--payouts)
4. [Triage Process & SLA](#triage-process--sla)
5. [Payout Funds & Execution](#payout-funds--execution)
6. [Rules & Guidelines](#rules--guidelines)
7. [Public Disclosure Template](#public-disclosure-template)
8. [FAQ](#faq)
9. [Contact & Escalation](#contact--escalation)
10. [Legal & Disclaimer](#legal--disclaimer)

---

## 1. Program Overview

The bug bounty program covers the **Armory protocol launch contract set** and the **API money path** (deposit, withdrawal, and accounting endpoints). The program is live **before mainnet deposits open** to ensure white‑hat researchers have a credible, funded channel to report vulnerabilities.

**Total Bounty Pool:** $500,000 USDC (earmarked in multisig `0x7d9…f4A1`)  
**Maximum Single Payout:** Capped at 10 % of TVL at time of report, or tier maximum—whichever is lower.

---

## 2. Scope

### 2.1 In‑Scope Contracts (Launch Set)

| Contract | Address (Mainnet) | Description |
|----------|-------------------|-------------|
| `VaultCore` | `0x123…abc` | Primary vault logic: deposit, withdraw, accounting |
| `StrategyRouter` | `0x456…def` | Routes funds to approved strategies |
| `OracleAdapter` | `0x789…ghi` | Chainlink + TWAP price feeds |
| `FeeManager` | `0xabc…jkl` | Protocol fees, performance fees, referral splits |
| `AccessControl` | `0xdef…mno` | Role‑based access (admin, pauser, strategist) |
| `EmergencyPause` | `0xghi…pqr` | Circuit breaker for critical failures |

**Deployment artifacts:** [GitHub Releases / v1.0.0‑launch](https://github.com/armory-finance/contracts/releases/tag/v1.0.0-launch)  
**Verified sources:** Etherscan (all contracts verified at deployment)

### 2.2 In‑Scope API Money Path

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/v1/deposit` | POST | Initiates deposit, returns calldata + tx hash |
| `/api/v1/withdraw` | POST | Initiates withdrawal request |
| `/api/v1/accounting/snapshot` | GET | Returns user position, accrued yield, fees |
| `/api/v1/strategies/active` | GET | Lists currently allocated strategies |
| `/api/v1/health` | GET | Liveness / readiness for monitoring |

**API repo:** `github.com/armory-finance/api` (tag `v1.0.0‑launch`)  
**Infrastructure:** Cloudflare Workers + Cloudflare D1 (SQLite), rate‑limited, WAF‑enabled.

### 2.3 Out of Scope

* Frontend (React app) — separate program planned post‑launch  
* Governance contracts (Timelock, Governor) — not deployed at launch  
* Testnet / staging environments  
* Social engineering, phishing, or physical access  
* Denial‑of‑service via public RPC / Cloudflare (report to infra team instead)  
* Vulnerabilities in third‑party dependencies (OpenZeppelin, Chainlink) — report upstream

---

## 3. Severity Tiers & Payouts

Payouts scale with **funds at risk (FaR)**. FaR = TVL in affected contract(s) at time of valid report.

| Tier | Definition | Payout Formula | Minimum | Maximum (Cap) |
|------|------------|----------------|----------|----------------|
| **Critical** | Direct theft/loss of user funds, unauthorized withdrawal, mint/burn bypass, oracle manipulation leading to drain | `min(10 % × FaR, $250,000)` | $50,000 | $250,000 |
| **High** | Freeze of user funds > 24 h, incorrect accounting > 1 % TVL, access control bypass (non‑admin), strategy fund misallocation | `min(5 % × FaR, $100,000)` | $15,000 | $100,000 |
| **Medium** | Incorrect accounting 0.1–1 % TVL, temporary freeze < 24 h, fee miscalculation, logic error with user impact | `min(2 % × FaR, $25,000)` | $3,000 | $25,000 |
| **Low** | Minor accounting drift < 0.1 % TVL, info disclosure (non‑PII), gas optimization > 20 %, UI/API mismatch | `min(0.5 % × FaR, $5,000)` | $500 | $5,000 |
| **Informational** | Code quality, best practices, doc gaps | Swag / Hall of Fame | — | — |

**Examples at $10 M TVL:**

| Tier | Formula | Result |
|------|---------|-------|
| Critical | `min(10 % × $10 M, $250k)` | **$250,000** |
| High | `min(5 % × $10 M, $100k)` | **$100,000** |
| Medium | `min(2 % × $10 M, $25k)` | **$25,000** |
| Low | `min(0.5 % × $10 M, $5k)` | **$5,000** |

**Dual submissions:** First valid report receives 100 %; duplicates receive 10 % of tier minimum (good‑faith bonus).

---

## 4. Triage Process & SLA

### 4.1 Triage Owner

| Role | Name | Contact | Backup |
|------|------|---------|--------|
| Lead Triage | Maya Chen (Security Lead) | `mailto:maya@armory.com` | Sam Park |
| Smart Contract Review | Dr. Alex Rivera (Auditor) | `mailto:alex@armory.com` | — |
| API/Infra Review | Priya Shah (Platform Lead) | `mailto:priyaa@armory.com` | — |
| Payout Approver | 3‑of‑5 Multisig (DAO Security Council) | `mailto:security@armory.com` | — |

### 4.2 SLA Timeline

| Stage | Target | Maximum |
|-------|--------|---------|
| Acknowledgment | 4 h | 24 h |
| Initial Triage (valid/invalid/duplicate) | 24 h | 72 h |
| Technical Assessment (severity + FaR calc) | 72 h | 7 days |
| Fix Development | — | 14 days (Critical), 30 days (High), 60 days (Med/Low) |
| Fix Deployment + Verification | — | 7 days after fix ready |
| Payout Execution | 48 h after verification | 7 days |

**Escalation:** If SLA breached, reporter may ping `@security‑council` on Discord or email `security@armory.com`. Council responds within 12 h.

### 4.3 Communication Channel

* **Primary:** Immunefi platform (encrypted, auditable)  
* **Backup (PGP):** `0xABCDEF1234567890` (published on `/security` page)  
* **Discord:** `#security-bounty` (invite‑only after first valid report)

---

## 5. Payout Funds & Execution

### 5.1 Fund Custody

| Item | Details |
|------|---------|
| Wallet | `0x7d9…f4A1` (Gnosis Safe, 3‑of‑5) |
| Signers | Security Lead, CTO, Legal Counsel, DAO Rep, External Auditor |
| Balance | $500,000 USDC (topped up quarterly or when < $100k remaining) |
| Transparency | Balance tracked on [Dune Dashboard](https://dune.com/armory/bounty-pool) |

### 5.2 Payout Process

1. Triage confirms severity + FaR → calculates payout.  
2. Lead Triage drafts payout proposal (Immunefi + internal tracker).  
3. 3‑of‑5 multisig signs → USDC sent to reporter’s Immunefi wallet.  
4. Immunefi handles KYC/AML, tax forms (W‑8BEN / 1099).  
5. Public disclosure coordinated (see §7).

### 5.3 Payout Currency

| Default | USDC on Ethereum mainnet |
|---------|-----------------------------|
| Alternative | USDT, DAI, or ETH (reporter choice, same USD value) |
| Gas | Protocol covers tx fee (separate from bounty) |

---

## 6. Rules & Guidelines

### 6.1 Researcher Conduct

* **No exploitation:** Do not interact with mainnet contracts to prove impact. Use forked mainnet (Foundry/Hardhat) or testnet.  
* **No data exfiltration:** Do not access/modify user data, logs, or infra beyond what is necessary to demonstrate the bug.  
* **No disruption:** Do not DoS, spam, or degrade API/contract availability.  
* **Single report per vulnerability:** Combine related findings in one submission.

### 6.2 Disclosure Policy

* **Coordinated disclosure:** 90‑day embargo from payout date (extendable by mutual agreement for complex fixes).  
* **Early disclosure:** Allowed if fix deployed + verified + 14 days elapsed.  
* **No disclosure:** If exploit actively used in the wild — immediate emergency disclosure to users + authorities.

### 6.3 Eligibility

* Open to all individuals and teams (no geographic restrictions beyond sanctions lists).  
* Armory employees, contractors, auditors (current/past 12 mo) **ineligible** for contracts they authored/audited.  
* Sanctioned entities (OFAC, UN, EU) automatically disqualified.

### 6.4 Safe Harbor

Armory authorizes good‑faith research on in‑scope assets and will not pursue legal action for activities consistent with this policy. This safe harbor does **not** cover:

* Accessing user data or PII  
* Moving funds out of protocol contracts  
* Publishing exploit code before coordinated disclosure window

---

## 7. Public Disclosure Template

After fix + verification, Armory publishes a **Post‑Mortem** including:

1. **Vulnerability summary** (CWE + custom tag)  
2. **Severity** + **FaR** at time of report  
3. **Root cause** + **fix diff** (link to PR)  
4. **Timeline** (report → ack → triage → fix → deploy → payout)  
5. **Payout amount** + **tx hash**  
6. **Researcher credit** (name/handle or “anonymous”)

---

## 8. FAQ

| Question | Answer |
|----------|--------|
| *How do I submit a report?* | Use the Immunefi submission form linked above. |
| *What if I discover a vulnerability after the program ends?* | Contact `security@armory.com`; we may consider a retroactive bounty. |
| *Can I claim a bounty for a vulnerability in a contract not listed?* | No, only in‑scope contracts are eligible. |
| *What if I need to verify a claim on mainnet?* | Provide a forked mainnet replay; mainnet interaction is prohibited. |

---

## 9. Contact & Escalation

| Channel | Details |
|---------|---------|
| Immunefi | <https://immunefi.com/bounty/armory> |
| Email | `security@armory.com` |
| Discord | `#security-bounty` (invite‑only) |
| PGP Key | `0xABCDEF1234567890` (public key on `/security` page) |

---

## 10. Legal & Disclaimer

* This program is governed by the terms of Immunefi and the AIGON‑X legal framework.  
* Participation does **not** grant any ownership or rights to the protocol or its assets.  
* All payouts are subject to local tax regulations; recipients are responsible for compliance.  
* By submitting a report, you agree to the terms outlined in this document.

--- 

**Version:** 1.0.0‑launch  
**Last Updated:** 2026‑09‑01  
**Bounty Page Live:** ✅  
**Payout Funds Earmarked:** ✅  
**Triage Owner & SLA Documented:** ✅