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

## Attester Signing Key Separation (issue #820 — signature-attested APY/TVL)

The `update_apy_attested` and `update_tvl_attested` registry functions require every value to
be signed by one or more registered ed25519 attester keys before it is accepted on-chain.
This converts "trust the caller" into "verify the data" and makes every accepted value
independently auditable via the `VAL_ATT` event log.

**The attester signing key MUST be distinct from the Stellar transaction-submitting (fee-paying) key.**

This requirement is not advisory — it is the security boundary the feature is built on:

- The **transaction key** pays Stellar fees and authorises the `update_apy_attested` call. It
  is exposed to the Stellar network on every submission. Compromise of this key lets an attacker
  submit transactions, but without a valid attester signature the on-chain contract will reject
  the value.
- The **attester key** signs the canonical payload (contract address, source id, field, value,
  validity window, nonce). It never touches the Stellar network directly. Compromise of this
  key lets an attacker forge signatures, but without the transaction key no on-chain call can
  be submitted.

If the same key plays both roles, attestation adds nothing — a compromise gives the attacker
both halves simultaneously. The registry can be updated to any value with a single key, exactly
as before.

### Implementation requirements for the backend attester

The Go APY push pipeline (`apps/api/internal/service/apy_service.go`,
`apps/api/internal/service/performance/apy_refresh.go`) is responsible for signing attestations
before submitting registry updates via `apps/api/internal/stellar/invoker.go`.

- The attester private key **must** be stored in the same secret store as the AES cipher keys
  (see `apps/api/internal/crypto/account_cipher.go` and `apps/api/internal/rotation/rotation.go`),
  **never** in a plain environment variable.
- The attester key versioning **must** follow the same rotation scheme used for cipher keys:
  versioned labels (e.g. `attester-v1`, `attester-v2`), non-destructive rotation, with old
  public keys revoked from the registry only after no in-flight attestations using them remain.
- The configuration entry for the attester key lives in `apps/api/internal/config/config.go`
  under the same section as the cipher key configuration. The field name is
  `ATTESTER_SIGNING_KEY` (env: `API_ATTESTER_SIGNING_KEY`). It holds the base64-encoded
  raw ed25519 private key (32 bytes).
- The fee-paying Stellar key is already managed separately in config. Do **not** derive the
  attester key from it or store them together.

### Canonical payload encoding (summary — full spec in `EVENTS.md → VAL_ATT`)

Every attester signature covers:
- The **registry contract address** — so a signature for testnet cannot be replayed on mainnet.
- The **source id** and **field tag** (APY vs TVL) — so a signature for one source/field cannot
  be used for another.
- The **exact value** — so a captured attestation cannot be altered.
- A **validity window** (`valid_from` / `valid_until`) — so a captured-but-old signature has a
  bounded lifetime.
- A **monotonic nonce** per attester — so the same signature cannot be submitted twice.

### Break-glass path

If all attesters become unavailable, the protocol can still mark any source `Paused` or
`Deprecated` via `update_status`, which requires only the normal Admin/Operator role — no
attestation. This means an attester outage can never become a protocol outage: sources can
always be paused to stop capital allocation while the attester infrastructure is restored.

## Circuit Breaker (issue #817)

The vault trips itself automatically rather than waiting for an operator to notice a problem. Four independently-configurable conditions escalate a graded severity (`Normal` → `Throttled` → `DepositsHalted` → `FullHalt`):

- **Share-price move** — an implausible change in share price within a rolling window halts new deposits.
- **Yield sanity** — a single `report_yield` call larger than a configured fraction of total assets is rejected (not applied) rather than accepted, and halts new deposits.
- **Withdrawal velocity** — cumulative withdrawal value (not transaction count) within a rolling window throttles the vault. A configurable margin above the raw threshold prevents a dust withdrawal from griefing the vault into a halt for free.
- **Source failure** — consecutive yield-adapter failures halt new deposits.

The emergency withdrawal queue is never gated by severity — it works identically at every level, including `FullHalt`, because a breaker that stops users from leaving is a trap, not a safety device. Recovery only ever moves one severity stage at a time, only after a configurable cooldown, and only for callers holding `Admin`/`Upgrader` — never `Guardian`.

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
