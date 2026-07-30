# Security Policy

## Scope

**In scope — please report these:**
- Smart contract vulnerabilities (reentrancy, fund loss, logic errors in vault/rebalance/offramp)
- Authentication or authorization bypass
- Data exposure (user PII, private keys, JWT secrets)
- Injection attacks (SQL, command, XSS with financial impact)
- Privilege escalation in the API or admin endpoints
- Cryptographic weaknesses

**Out of scope — do not report these as security issues:**
- Feature requests or general usability bugs
- UI/styling issues with no security impact
- Rate-limiting on non-sensitive endpoints
- Issues requiring physical access to a user's device
- Self-XSS or social engineering attacks

## Reporting a Vulnerability

**Do NOT open a public GitHub issue for security vulnerabilities.** Doing so exposes users before a fix is available.

Instead, use one of the following:

1. **GitHub Private Vulnerability Reporting** (preferred) — click **"Report a vulnerability"** under the Security tab of this repository.
2. **Email** — send a report to **security@nester.dev** with the subject line `[SECURITY] <brief description>`.

### What to include in your report

- Affected component (smart contract name, API endpoint, service)
- Description of the vulnerability and its potential impact
- Step-by-step reproduction instructions
- Any proof-of-concept code or screenshots (if safe to share)
- Your severity assessment (Critical / High / Medium / Low)

## Response Timeline

| Stage | Target |
|-------|--------|
| Acknowledgment | Within 48 hours |
| Initial assessment | Within 1 week |
| Fix or mitigation | Depends on severity (Critical: ASAP, High: 2 weeks, Medium/Low: next release) |
| Public disclosure | Coordinated with reporter after fix is deployed |

## Severity Classification

| Severity | Examples |
|----------|---------|
| **Critical** | Smart contract funds at risk, private key exposure, complete auth bypass |
| **High** | Privilege escalation, significant PII data breach, DoS of critical services |
| **Medium** | Limited data exposure, partial auth weakness, DoS of non-critical services |
| **Low** | Information disclosure, best-practice violations with minimal impact |

## On-Chain Access Control Model (issue #820)

Every Nester contract shares one role model (`nester_access_control::Role`) rather than a single admin key:

| Role | Can do | Can never do |
|------|--------|---------------|
| `Admin` | Grant/revoke roles, configure non-fund-moving parameters | — (narrower roles below carve fund movement and safety actions out of Admin's day-to-day path) |
| `Guardian` | Pause, halt deposits, trip the circuit breaker | Unpause, upgrade, withdraw, or loosen any threshold |
| `Upgrader` | Propose/execute upgrades through the timelock; run staged breaker recovery | Trip the breaker itself with Guardian's speed |
| `Attester` | Sign APY/TVL/yield reports | Move funds or change fee/role config |
| `FeeManager` | Adjust fees within governed bounds | Grant roles or move treasury funds |
| `RebalanceKeeper` | Trigger rebalances | Change fee/role config |
| `Treasurer` | Authorise treasury outflows | Grant roles or change fee config |
| `VaultCreator` | Deploy new vaults via the factory | Change the governed vault WASM hash (timelocked, Admin-gated) |

**The Guardian asymmetry is the load-bearing guarantee.** A Guardian key can always make a vault *safer* — pause it, halt new deposits, trip the circuit breaker into `FullHalt` — and can *never* make it riskier: it cannot unpause, cannot upgrade, cannot withdraw, and cannot even loosen the breaker thresholds that constrain it. Reversing anything a Guardian does always requires a higher role (`Admin`/`Upgrader`) to run the staged, cooled-down recovery path (`recover_next_stage`). Practically, this means a Guardian key can be held more widely and reacted with faster than an Admin key: a compromised Guardian can at worst halt the protocol — annoying and fully recoverable — never drain it.

Every role transfer is two-step (`transfer_role` / `accept_role`, cancellable via `cancel_role_transfer`) so a mistyped or uncontrolled successor address can never take over in one move. Operational roles can be time-bounded with `grant_role_until` so a forgotten grant to a rotated-away key stops authorising on its own. The full role state is inspectable on-chain via `has_role`, `get_role_members` (bounded), and `role_expires_at`.

**On-chain roles vs application roles:** the RBAC above lives entirely in the Soroban contracts. The `user_roles` tables in the API's Postgres migrations are a separate, application-level authorization layer (who can see what in the dashboard) and do not confer any on-chain authority — do not conflate the two when reasoning about contract security.

## Circuit Breaker (issue #817)

The vault trips itself automatically rather than waiting for an operator to notice a problem. Four independently-configurable conditions escalate a graded severity (`Normal` → `Throttled` → `DepositsHalted` → `FullHalt`):

- **Share-price move** — an implausible change in share price within a rolling window halts new deposits.
- **Yield sanity** — a single `report_yield` call larger than a configured fraction of total assets is rejected (not applied) rather than accepted, and halts new deposits.
- **Withdrawal velocity** — cumulative withdrawal value (not transaction count) within a rolling window throttles the vault. A configurable margin above the raw threshold prevents a dust withdrawal from griefing the vault into a halt for free.
- **Source failure** — consecutive yield-adapter failures halt new deposits.

The emergency withdrawal queue is never gated by severity — it works identically at every level, including `FullHalt`, because a breaker that stops users from leaving is a trap, not a safety device. Recovery only ever moves one severity stage at a time, only after a configurable cooldown, and only for callers holding `Admin`/`Upgrader` — never `Guardian`.

## Encryption at Rest

Nester uses **envelope encryption** with key versioning and rotation for all sensitive user data stored at rest. The scheme is implemented in `apps/api/internal/crypto/account_cipher.go` and reused across all domains.

### Key Hierarchy

```
Master Key (never stored in the application)
    └── Key-Encryption Keys (KEKs) — versioned, configured via env vars
        └── AES-256-GCM Data Key (derived per cipher instance)
            └── Ciphertext stored in `*_encrypted` columns, tagged with `key_version`
```

Key configuration (environment variables):

| Variable | Purpose |
|---|---|
| `BANK_ACCOUNT_ENCRYPTION_KEY` | Legacy single 32-byte base64 key (registers as `v1`) |
| `ACCOUNT_CIPHER_KEYS` | Comma-separated `version:base64key` pairs for multi-key rotation |
| `ACCOUNT_CIPHER_ACTIVE_KEY` | Which version is used for new encryptions |
| `ACCOUNT_CIPHER_FINGERPRINT_KEY` | Independent pepper for blind-index HMAC (required when no `v1` key is in the set) |

### Encrypted Fields

| Table | Encrypted Columns | Blind Index | Key Version |
|---|---|---|---|
| `bank_accounts` | `account_number_encrypted` (BYTEA) | `account_number_fingerprint` (TEXT, unique) | `key_version` (VARCHAR(32)) |
| `kyc_documents` | `id_number_encrypted` (BYTEA), `front_object_key_encrypted` (BYTEA), `back_object_key_encrypted` (BYTEA) | `id_number_fingerprint` (TEXT) | `key_version` (VARCHAR(32)) |

### Blind Indexes

Fields that need exact-match lookup (deduplication, find-by-identifier) use a **blind index**: a keyed HMAC-SHA256 of the normalized plaintext stored in a separate column. The HMAC key is independent of the encryption keys — either the `v1` KEK or the explicit `ACCOUNT_CIPHER_FINGERPRINT_KEY` — so the index cannot be used to attack the ciphertext even if both the index and ciphertext are leaked in the same breach.

Limitations:
- **Exact-match only**: No prefix, range or fuzzy search on blind indexes.
- **Collision resistant**: HMAC-SHA256 produces a 256-bit digest; collisions are not a practical concern.

### Key Rotation

The `cmd/rotate_keys` command re-encrypts stored ciphertext under the active key version. It processes all encrypted domains (bank accounts, KYC documents) in a single run. The rotator is:

- **Idempotent**: Rows already on the active version are skipped.
- **Resumable**: Each row is committed independently; an interrupted run picks up where it left off.
- **Safe**: Only row IDs and key versions are logged — never plaintext, ciphertext, or keys.

### Data Classification

A complete data-classification inventory is maintained at `apps/api/internal/crypto/DATA_CLASSIFICATION.md`. Every column holding user data is classified as HIGH (sensitive, encrypted), MEDIUM (indirect PII), or LOW (non-sensitive).

### Backfill for Existing Data

When encryption is extended to a new domain, existing plaintext rows are backfilled using a dedicated command (e.g. `cmd/backfill_kyc_encryption`). The migration pattern follows:

1. Add encrypted columns alongside existing plaintext columns (rollback-safe).
2. Run a batched, resumable backfill job to populate encrypted values.
3. Verify the backfill is complete.
4. Drop plaintext columns in a separate, later migration after confidence is established.

Plaintext is never dropped in the same step that introduces ciphertext.

## Known and Accepted Vulnerabilities

We maintain a list of known vulnerabilities in our dependency chain that we have assessed and accepted based on their risk profile:

| ID | Module | Severity | Reason | Status |
|----|--------|----------|--------|--------|
| **GO-2026-4316** | `github.com/go-chi/chi` | Medium | Open redirect in unused `RedirectSlashes` middleware. Transitive dependency via `github.com/stellar/go-stellar-sdk`. Middleware not used in codebase. Waiting for upstream fix. | Accepted |

**Mitigation strategy:** Our CI/CD pipeline (`govulncheck` step in `.github/workflows/security.yml`) explicitly allows known accepted vulnerabilities and only fails on new or unreviewed vulnerabilities.

To report a vulnerability not listed here, see [Reporting a Vulnerability](#reporting-a-vulnerability) above.

## Safe Harbor

Nester commits to not pursuing legal action against security researchers who:

- Report vulnerabilities through this policy in good faith
- Avoid accessing, modifying, or deleting user data beyond what is necessary to demonstrate the issue
- Do not disrupt production services or degrade user experience during testing
- Give us reasonable time to resolve the issue before public disclosure

## Recognition

We maintain a Hall of Fame for researchers who responsibly disclose valid security issues. Credit will be given in the relevant release notes and security advisory unless you prefer to remain anonymous.

We do not currently operate a paid bug bounty program, but we appreciate and publicly recognize all valid reports.
