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

## Known and Accepted Vulnerabilities

We maintain a list of known vulnerabilities in our dependency chain that we have assessed and accepted based on their risk profile. The canonical waiver list lives in **`.vulnignore.yml`** with structured fields (id, ecosystem, package, severity, justification, mitigation, expires_on, approved_by). A flat ID-only mirror is kept in `.vulnignore` for backward compatibility with legacy scanners.

| ID | Module | Severity | Reason | Status |
|----|--------|----------|--------|--------|
| **GO-2026-4316** | `github.com/go-chi/chi` | Medium | Open redirect in unused `RedirectSlashes` middleware. Transitive dependency via `github.com/stellar/go-stellar-sdk`. Middleware not used in codebase. Waiver expires 2026-10-15. | Accepted (waivered) |

**Mitigation strategy:** Our CI/CD pipeline (`govulncheck` step in `.github/workflows/security.yml` and `.github/workflows/ci.yml`) reads `.vulnignore.yml` and explicitly allows known accepted vulnerabilities, only failing on new or unreviewed vulnerabilities. The `action-pinning` job fails the build when any waiver in `.vulnignore.yml` has expired without renewal, so suppressed risks cannot silently persist forever.

To report a vulnerability not listed here, see [Reporting a Vulnerability](#reporting-a-vulnerability) above.

## Supply-Chain Security (issue #867)

Nester's code is a small fraction of what actually runs in production — the rest is dependencies (Go modules, npm packages, Rust crates, Python packages, container base images, and the CI actions that build and ship everything). A compromised package version, a typosquatted dependency, or a malicious update to a transitive dependency can inject code through the front door of the build where ordinary code review never looks. The following controls bring that invisible attack surface under the same scrutiny as the code itself.

### Software Bill of Materials (SBOM)

Every deployable artifact is accompanied by a **CycloneDX** SBOM in JSON format, generated on every CI build and retained per build:

| Artifact | SBOM generator | Output |
|----------|----------------|--------|
| Go API (`apps/api`) | `cyclonedx-gomod` | `api-sbom.cdx.json` |
| dApp frontend (`apps/dapp/frontend`) | `@cyclonedx/cyclonedx-npm` | `dapp-frontend-sbom.cdx.json` |
| Website (`apps/website`) | `@cyclonedx/cyclonedx-npm` | `website-sbom.cdx.json` |
| Contracts (`packages/contracts`) | `cargo-cyclonedx` | `contracts-sbom.cdx.json` |
| Intelligence service (`apps/intelligence`) | `cyclonedx-bom` | `intelligence-sbom.cdx.json` |

The jobs live in `.github/workflows/security.yml` and the SBOM is uploaded as a CI artifact. When a new vulnerability is disclosed, the SBOM answers "are we affected, and where?" in seconds instead of a frantic manual audit.

### Dependency pinning and integrity verification

- **npm** — `pnpm-lock.yaml` is committed, CI installs with `--frozen-lockfile`, and the build runs `npm audit --audit-level=high` against the lockfile's integrity hashes.
- **Go** — `go.sum` is committed, and `govulncheck ./...` runs against the locked module graph.
- **Rust** — `Cargo.lock` is committed, and `cargo audit` runs against the locked dependency tree.
- **Python** — `requirements.txt` is hashed at audit time and re-checked by `pip-audit`.
- **CI actions** — every third-party `uses:` is pinned to a commit SHA, never a mutable tag like `@v4`. The `.github/workflows/security.yml` `action-pinning` job enforces this on every push and PR with a regex check; `dtolnay/rust-toolchain@stable` is pinned to the `master` branch HEAD SHA and is reviewed with each Rust toolchain bump.

### Provenance verification

We prefer dependencies and CI actions that publish build attestations (npm provenance, Sigstore, GitHub artifact attestations) and verify them where our tooling supports it. The coverage matrix is:

| Source | Provenance coverage |
|--------|---------------------|
| npm registry (default) | npm provenance flag enabled for publishers; verified when available |
| PyPI | Sigstore attestation verified when available |
| Rust crates.io | cargo `--locked` flag rejects unexpected lockfile mutations |
| GitHub Actions marketplace | SHA pinning makes the immutable commit the trusted identifier |
| Container base images | Pinned by digest in production (see `apps/api/Dockerfile`, `apps/dapp/frontend/Dockerfile.dev`) |

### Vulnerability scanning with policy gates

`.github/workflows/security.yml` and `.github/workflows/ci.yml` run a `*-audit` step for every ecosystem on every PR:

- A **high** or **critical** vulnerability in a production dependency blocks the merge.
- A **moderate** vulnerability produces a warning annotation but does not block.
- Waivers (`.vulnignore.yml`) are auditable, time-bounded (≤ 6 months by default), and require a `justification`, `mitigation`, `expires_on` date, and an `approved_by` GitHub handle. A waiver with `expires_on` in the past fails the build until renewed or removed.

### Typosquat / suspicious-package detection

`.github/workflows/security.yml` → `typosquat-check` runs `scripts/typosquat_check.py` on every PR. The script flags:

1. New packages whose Levenshtein distance is ≤ 1 from a popular package on any ecosystem (catches `reqests` vs `requests`, `reactt` vs `react`).
2. Names that mix Latin and Cyrillic/Greek scripts (homoglyph attack).
3. Names whose shape is wrong for their declared ecosystem (uppercase letters in pip, scoped npm without target).

### New-dependency review

Adding a new top-level dependency is a security decision, not a routine change. The PR that introduces it must:

- Pass the typosquat check and the vulnerability audit.
- Include a one-paragraph justification: what does this dependency do that we cannot reasonably implement ourselves, and how is it maintained (bus factor, last release, governance)?
- Have a SHA-pinned lockfile entry (no `latest` or floating ranges).
- For significant additions, a brief maintainer-level review (security team tag) before merge.

### Audit trail

The following artifacts and commands let an auditor trace any deployed version's supply chain back to source:

- The SBOM artifact for that build (per `GitHub Actions → Run → Artifacts`).
- The CI run logs that produced the build (per `GitHub Actions → Run → Logs`).
- The `.vulnignore.yml` history (`git log -- .vulnignore.yml`).
- The `gitleaks` scan results (per `GitHub Actions → Security → Code scanning`).

### What this policy does NOT cover

- Application-level vulnerabilities in our own code — those are covered by CodeQL and Semgrep SAST (the `security` job in `.github/workflows/ci.yml` and the `codeql` job in `.github/workflows/security.yml`).
- On-chain contract vulnerabilities — those are covered by the dedicated audit workflow `.github/workflows/contract-audit.yml` and `scripts/contract-audit.sh`.

## Safe Harbor

Nester commits to not pursuing legal action against security researchers who:

- Report vulnerabilities through this policy in good faith
- Avoid accessing, modifying, or deleting user data beyond what is necessary to demonstrate the issue
- Do not disrupt production services or degrade user experience during testing
- Give us reasonable time to resolve the issue before public disclosure

## Recognition

We maintain a Hall of Fame for researchers who responsibly disclose valid security issues. Credit will be given in the relevant release notes and security advisory unless you prefer to remain anonymous.

We do not currently operate a paid bug bounty program, but we appreciate and publicly recognize all valid reports.
