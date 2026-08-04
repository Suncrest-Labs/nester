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

## Supply-Chain Security

Nester implements defence-in-depth for software supply-chain security, covering all ecosystems in the monorepo (Go, npm, Rust Cargo, Python pip, and CI/containers).

### Software Bill of Materials (SBOM)

Every CI build generates a CycloneDX-format SBOM for each deployable artifact affected by the change set:
- **Go API**: `cyclonedx-gomod` generates the Go module SBOM
- **Frontend (npm/pnpm)**: `syft` generates the pnpm dependency tree SBOM (catalogs `pnpm-lock.yaml` directly)
- **Intelligence (Python)**: `syft` generates the Python package SBOM
- **Contracts (Rust)**: `syft` generates the Rust crate SBOM

SBOM generation is mandatory for the artifacts touched by a build: a generator failure fails the job, and the artifact upload is configured with `if-no-files-found: error` so a build with missing SBOMs cannot go green. SBOMs are uploaded as build artifacts and retained per build, so any deployed version has an exact, queryable record of its dependency contents.

### Dependency Pinning and Integrity

All package dependencies are pinned to exact versions with integrity verification:
- **Go**: `go.sum` with checksum database (`GOSUMDB`) verification
- **npm**: Lockfile (`pnpm-lock.yaml`) with integrity hashes; CI uses `--frozen-lockfile` to reject unrecorded changes
- **Rust**: `Cargo.lock` with verified crate checksums
- **Python**: `requirements.txt` version pins audited by `pip-audit` in CI

CI fails the build if integrity verification fails for any ecosystem. CI tooling is pinned as well: third-party GitHub Actions are pinned to immutable commit SHAs (see below) and the SBOM generators (`cyclonedx-gomod`, `syft`) are installed from exact release versions, with downloaded binaries verified against published checksums.

### CI Action Pinning

All third-party GitHub Actions are pinned to immutable commit SHAs rather than mutable version tags. A `@v4` tag can be repointed by the action owner; a SHA cannot. Each SHA is annotated with the equivalent semver tag for readability.

The following actions are pinned across all workflows:
- `actions/checkout`, `actions/setup-node`, `actions/setup-go`, `actions/setup-python`
- `pnpm/action-setup`, `dorny/paths-filter`, `dtolnay/rust-toolchain`
- `github/codeql-action/*`, `semgrep/semgrep-action`, `dependabot/fetch-metadata`
- `actions/upload-artifact`

### Provenance Verification

Where available, dependencies and actions with build provenance/attestations are preferred and verified:
- **GitHub Actions**: Official `actions/*` and `github/*` actions publish signed attestations via `attestations.write` permission
- **npm packages**: Packages with provenance attestations (via npm registry) are preferred
- **Go modules**: Go checksum database (`sum.golang.org`) provides transparency log verification

Coverage is documented and reviewed as part of new-dependency introduction.

### Vulnerability Scanning Gates

Automated vulnerability scanning runs on every PR and push across all ecosystems:

| Scanner | Ecosystem | Policy |
|---------|-----------|--------|
| `gitleaks` | Secrets | No secrets in any commit |
| `govulncheck` | Go (api) | Known critical/high blocks; accepted vulns must be waived in `.vulnignore` with a future review date, owner, and status |
| `gosec` | Go (api) | Medium+ severity; warnings only |
| `cargo-audit` | Rust (contracts) | All advisories; warnings only |
| `pnpm audit` | npm (dapp/website) | Moderate+ fails; warnings only |
| `pip-audit` | Python (intelligence) | High/critical fails with JSON report |
| `bandit` | Python (intelligence) | High-confidence issues |
| `semgrep` | TypeScript/Next.js | OSS + Pro rules (TypeScript, React, Next.js, secrets) |
| `CodeQL` | Go, JS/TS, Python | All queries, security-extended suite |

A known critical or high vulnerability in a production dependency blocks the merge/release until remediated or explicitly waived.

### Typosquat Detection

New npm dependencies are automatically checked for typosquatting patterns (Levenshtein distance ≤ 2 from known popular packages) during CI. Suspicious matches are surfaced as warnings for manual review before merge.

### Dependency Introduction Control

Adding a new dependency follows this gated process:
1. Dependabot or developer proposes the change in a PR
2. CI runs vulnerability scans across all ecosystems
3. Typosquat detection checks the new package name
4. For significant additions, a brief review of the package's provenance and health is required
5. The PR must pass all CI gates before merge

### Waiver Process

`.vulnignore` is the single source of truth for accepted vulnerabilities and is enforced by CI:
- Every entry requires a documented justification, a time-bounded review date (`Review date: YYYY-MM-DD`), an owner, and a status
- CI parses `.vulnignore` when filtering vulnerability-scan results and fails the build if any waiver is expired, malformed, or ownerless
- Waivers are auditable and reviewed as part of the release process; permanent silent suppressions are not permitted

## Known and Accepted Vulnerabilities

We maintain a list of known vulnerabilities in our dependency chain that we have assessed and accepted based on their risk profile:

| ID | Module | Severity | Reason | Status |
|----|--------|----------|--------|--------|
| **GO-2026-4316** | `github.com/go-chi/chi` | Medium | Open redirect in unused `RedirectSlashes` middleware. Transitive dependency via `github.com/stellar/go-stellar-sdk`. Middleware not used in codebase. Waiting for upstream fix. | Accepted |

**Mitigation strategy:** Our CI/CD pipeline (`govulncheck` steps in `.github/workflows/ci.yml` and `.github/workflows/security.yml`) reads the waiver list from `.vulnignore`, only allows vulnerabilities explicitly waived there, fails on any new or unreviewed vulnerability, and fails the build if a waiver is expired, malformed, or ownerless.

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

---

## Tamper-Evident Audit Log (Issue #834)

### Overview

Nester maintains a cryptographically hash-chained audit log for all security-relevant actions (admin operations, role changes, fund movements, configuration changes, authentication events). The goal is to make silent modification or deletion of log entries **detectable** even by a compromised database or malicious insider.

### Integrity Model

Each audit log entry includes four chain fields in addition to the standard event payload:

| Field | Description |
|-------|-------------|
| `sequence` | Strictly monotone integer assigned under a serializable transaction + mutex. No gaps are allowed. |
| `prev_hash` | SHA-256 of the canonical serialization of the **previous** entry (empty for entry #1). |
| `entry_hash` | SHA-256 of this entry's canonical serialization, including `prev_hash`. |
| `anchored` / `anchor_tx_hash` | Whether the entry's hash has been recorded outside the DB (file log today; Stellar chain in production). |

**Hash input canonical form** — deterministic JSON serialization (sorted keys):
```
SHA256( JSON({sequence, prev_hash, actor, action, target, detail, timestamp}) )
```

### Chain Construction

Entries are written inside a **`SERIALIZABLE`** isolation-level transaction and guarded by an in-process `sync.Mutex`. This ensures:

1. No two goroutines can race on the sequence counter.
2. The database rejects non-serial concurrent writes.
3. Every new entry's `prev_hash` exactly matches the `entry_hash` of the previous entry.

### Verification

The `AuditService.VerifyChain(ctx, fromSeq, toSeq)` method:
1. Fetches entries in sequence order from `fromSeq` to `toSeq`.
2. Detects **sequence gaps** (a gap means an entry was deleted).
3. Recomputes each `entry_hash` from the raw fields and compares it to the stored value. A mismatch means the entry was modified.
4. Checks `prev_hash` linkage across entries.

Returns `(ok bool, brokenAtSeq int64, err error)` — when `ok=false`, `brokenAtSeq` is the sequence number where the first break was detected.

### Background Verification Job

`scheduler.AuditChainVerifier` runs on a configurable interval (default: 30 minutes). On each tick it calls `VerifyChain` over the entire table. If a break is found, it calls `ChainBreakAlerter.AlertChainBreak` — in production this should page on-call.

**Environment variables:**
```
AUDIT_VERIFIER_ENABLED=true
AUDIT_VERIFIER_INTERVAL_MINUTES=30
```

### On-Demand Operator Verification

Administrators can trigger a one-shot integrity check via:
```
GET /api/v1/admin/audit/verify
Authorization: Bearer <admin JWT>
```

**Response (intact chain):**
```json
{ "chain_ok": true, "broken_at_sequence": 0 }
```

**Response (break detected):**
```json
{ "chain_ok": false, "broken_at_sequence": 7, "error": "entry_hash mismatch at sequence 7" }
```

### Redaction

Sensitive fields (e.g. plaintext tokens inadvertently logged) can be redacted via `AuditService.RedactEntry`. Redaction:
- Replaces the targeted field value with `"[REDACTED]"` in `detail`.
- Sets `redacted = true` on the entry.
- Appends a **new** `REDACTION` audit entry (which is itself chained), so the redaction event is itself tamper-evident.
- The **original `entry_hash` is preserved** so historical chains that reference this entry remain verifiable.

### Anchoring

The latest entry's hash can be anchored outside the database via `AuditService.AnchorLatestEntry`:
- **MVP**: Written to an append-only local file (`AUDIT_ANCHOR_FILE_PATH`).
- **Production target**: Published to a Stellar ledger transaction via `apps/api/internal/stellar/invoker.go`.

Once anchored, the `anchored = true` and `anchor_tx_hash` are set. Any later modification of the anchored entry can be proved fraudulent by comparing against the external anchor.

### Threat Model and Limitations

| Threat | Mitigation |
|--------|------------|
| DBA deletes a row | Sequence gap detected on next `VerifyChain` call |
| DBA modifies a field | Hash mismatch detected on next `VerifyChain` call |
| DBA recomputes hash after modification | `prev_hash` chain breaks on the following entry; or anchored hash in Stellar is contradicted |
| Attacker reconstructs entire chain | Requires control of all external anchors and all on-chain Stellar transactions |
| In-process race / dual-write | Prevented by `sync.Mutex` + `SERIALIZABLE` transaction |

> **Note:** The hash-chained model provides **tamper evidence**, not full **tamper prevention**. A sufficiently privileged attacker who can also rewrite the anchor store could still forge entries. Full prevention requires HSM-backed signing, which is tracked as a future enhancement.
