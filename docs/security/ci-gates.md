# CI Security Gates

This document describes the security gates that protect the Nester codebase. Each gate must pass for code to merge to main.

## Overview

| Language              | Gate                                | Threshold                                 | Workflow                 |
| --------------------- | ----------------------------------- | ----------------------------------------- | ------------------------ |
| Go                    | `govulncheck`                       | 0 unknown CVEs (accepted list maintained) | `security.yml`, `ci.yml` |
| Go                    | `gosec`                             | medium severity                           | `security.yml`           |
| Rust                  | `cargo-audit`                       | any CVE                                   | `security.yml`           |
| Python                | `pip-audit`                         | high/critical (CVSS ≥ 7.0)                | `security.yml`, `ci.yml` |
| JavaScript/TypeScript | `pnpm audit`                        | critical only                             | `security.yml`, `ci.yml` |
| JavaScript/TypeScript | `eslint` + `eslint-plugin-security` | all violations                            | `ci.yml` dapp job        |
| All                   | `gitleaks`                          | any secret detection                      | `security.yml`           |
| All                   | `semgrep`                           | all violations (XSS, injection, etc.)     | `security.yml`           |
| All                   | `CodeQL`                            | all violations                            | `security.yml`           |

## Go (apps/api)

### govulncheck

**Threshold:** All reported vulnerabilities must be in the accepted list or fixed.

**Accepted CVEs** (in `security.yml`):

- GO-2026-4316
- GO-2026-5004
- GO-2026-5037
- GO-2026-5039

When a new unknown CVE is found:

1. Evaluate if it affects the codebase
2. If yes, fix the dependency or add an accepted exception with justification
3. If no, add to the accepted list with a comment explaining why

**How it works:**

```bash
cd apps/api
govulncheck ./...
```

### gosec

**Threshold:** Medium severity and above. No suppressions without per-line annotation.

**Policy:** Every finding that is not a false positive gets fixed. Findings that are accepted (e.g., hardcoded test credentials with `#nosec` comments) must be annotated at the exact line with reasoning.

**How it works:**

```bash
cd apps/api
gosec -severity medium ./...
```

**Example annotation:**

```go
const testAPIKey = "sk-test-12345" // #nosec G101 -- test fixture, never used in production
```

## Rust (packages/contracts)

### cargo-audit

**Threshold:** Any CVE blocks the build. No exceptions.

**How it works:**

```bash
cd packages/contracts
cargo audit
```

## Python (apps/intelligence)

### pip-audit

**Threshold:** High and critical vulnerabilities (CVSS ≥ 7.0).

**Moderate vulnerabilities** (4.0 ≤ CVSS < 7.0) are warned but do not block the build, allowing time for upstream releases.

**How it works:**

```bash
cd apps/intelligence
pip-audit -r requirements.txt --format json --output report.json
# Then evaluate CVSS scores >= 7.0
```

**Where it runs:**

- `security.yml` — full suite with separate warn/fail gates
- `ci.yml` — intelligence job gates on high/critical only

### bandit

**Threshold:** All violations at `-ll` (high confidence, medium+ severity).

**How it works:**

```bash
cd apps/intelligence
bandit -r app -ll
```

## JavaScript/TypeScript (apps/dapp/frontend, apps/website)

### pnpm audit

**Threshold:** Critical only (0 found today).

**Moderate and high** are reported but do not block. Track in #1025 for upstream fixes to Expo/react-native and Trezor dependencies.

**How it works:**

```bash
pnpm audit --audit-level=critical
```

**Where it runs:**

- `security.yml` — dapp and website jobs
- `ci.yml` — dapp-frontend job

### eslint + eslint-plugin-security

**Threshold:** All violations must pass (part of the build).

**Config:** `apps/dapp/frontend/eslint.config.mjs` includes `security.configs.recommended`.

**How it works:**

```bash
cd apps/dapp/frontend
pnpm run lint
```

**Where it runs:**

- `ci.yml` — dapp-frontend job, now enforced (was `continue-on-error: true`, fixed in #1236)

## All Languages

### gitleaks

**Threshold:** No credentials detected.

**How it works:**

```bash
gitleaks detect --source . --no-git --redact --config .gitleaks.toml
```

**Where it runs:**

- `security.yml` — always

### semgrep

**Threshold:** All violations. Checks TypeScript/Next.js for XSS, injection, secrets, etc.

**How it works:**

```bash
semgrep scan --config p/typescript p/react p/nextjs p/secrets
```

**Where it runs:**

- `security.yml` — dapp changes

### CodeQL

**Threshold:** All violations across Go, TypeScript, Python.

**Where it runs:**

- `security.yml` — always

## CI Workflows

### ci.yml (Per-Job)

Runs on every push/PR, gates specific areas for fast feedback:

- **Dapp Frontend:** Lint (security), build, unit tests, E2E tests, JS audit
- **Intelligence:** Lint, type check, unit tests, Python audit (high/critical)
- **API:** Build, unit tests, database integration tests
- **Contracts:** Build, unit tests, property tests
- **Security:** gitleaks, CodeQL, per-language audits

### security.yml (Full Suite)

Scheduled weekly + triggered by security/\* file changes:

- gitleaks (all branches)
- pnpm audit (critical gate + moderate warning)
- govulncheck (with accepted list)
- cargo-audit
- pip-audit (with CVSS thresholds)
- bandit
- CodeQL
- Semgrep

## When a Gate Fails

1. **Identify the failure** — read the job logs
2. **Evaluate the finding** — is it a real issue or a false positive?
3. **Fix it** — update dependencies, fix code, or add justification
4. **Document** — if it's an accepted exception, add a comment in the code or workflow
5. **Re-run** — push again or re-run the workflow

## Adding New Gates

If you add a new security tool:

1. Run it locally first
2. Fix or justify any findings
3. Add it to the appropriate workflow (ci.yml for fast feedback, security.yml for comprehensive checks)
4. Document the threshold and policy in this file
5. Ensure it blocks the build or clearly state why it doesn't

## Further Reading

- [SECURITY.md](../SECURITY.md) — production security model
- [deploy/](../../deploy) — production security posture
- `.github/workflows/security.yml` — full security job definitions
- `.github/workflows/ci.yml` — per-job gates

## Verification: Testing Each Gate

Each gate has been verified to fail CI when a violation is introduced. Below are the test scenarios:

### Go (govulncheck)

**Fails when:** A dependency with an unknown CVE is added or an existing accepted CVE is resolved (meaning the fix must be applied).

**Example:** Add `github.com/vulnerable-package` at a CVE version → `govulncheck ./...` reports an unknown CVE ID → CI fails unless added to accepted list.

### Go (gosec)

**Fails when:** Medium-severity code patterns are detected without per-line `#nosec` annotations.

**Example:** Hardcoded credentials without `#nosec G101` → gosec reports → CI fails.

```go
// BAD: CI fails
apiKey := "<test-key-placeholder>"

// GOOD: CI passes
apiKey := "<test-key-placeholder>" // #nosec G101 -- test credential only
```

### Rust (cargo-audit)

**Fails when:** Any CVE is detected in the dependency tree.

**Example:** Add a dependency with a known CVE → `cargo audit` finds it → CI fails. No exceptions allowed.

### Python (pip-audit)

**Fails when:** A high or critical vulnerability (CVSS ≥ 7.0) is detected in requirements.

**Example:** Add `vulnerable-package==1.0.0` with CVSS 8.5 → pip-audit detects it → CI fails until dependency is upgraded or removed.

**Moderate vulnerabilities** (CVSS 4.0–7.0) are warned but do not block.

### JavaScript (pnpm audit)

**Fails when:** A critical advisory is found in the dependency tree.

**Example:** A transitive dependency introduces a critical vulnerability → `pnpm audit --audit-level=critical` finds it → CI fails.

**Moderate vulnerabilities** (4.0 ≤ CVSS < 7.0) are reported but do not block (tracked in #1025).

### JavaScript (eslint + security)

**Fails when:** ESLint security rules are violated in dapp code.

**Example:** Unsafe use of `dangerouslySetInnerHTML` without security review → eslint-plugin-security detects it → CI fails.

```tsx
// BAD: CI fails
<div dangerouslySetInnerHTML={{ __html: userInput }} />

// GOOD: CI passes (after security review and comment)
// APPROVED: User input sanitized with DOMPurify before rendering
<div dangerouslySetInnerHTML={{ __html: sanitize(userInput) }} />
```

### gitleaks

**Fails when:** A credential-like string is committed (API keys, passwords, private keys).

**Example:** Commit `AWS_SECRET_ACCESS_KEY=abc123` → gitleaks detects → CI fails. Must use `.env` or remove before committing.

### Semgrep (TypeScript/Next.js)

**Fails when:** OWASP-level violations are detected (XSS, injection, etc.).

**Example:** Direct interpolation of user input into SQL → semgrep detects → CI fails.

```typescript
// BAD: CI fails
const query = `SELECT * FROM users WHERE id = ${userId}`;

// GOOD: CI passes
const query = "SELECT * FROM users WHERE id = ?";
db.query(query, [userId]);
```

### CodeQL

**Fails when:** Static analysis detects potential security issues across Go, TypeScript, or Python.

**Example:** SQL injection pattern, path traversal, or unsafe deserialization → CodeQL detects → CI fails.
