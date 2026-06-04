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
