# CI Security Gates

This document describes the security gates that protect the Nester codebase. Each gate must pass for code to merge to main.

## Overview

Each tool runs exactly once per change.

| Language              | Gate                                | Threshold                                 | Workflow / job              |
| --------------------- | ----------------------------------- | ----------------------------------------- | --------------------------- |
| Go                    | `govulncheck`                       | 0 unknown CVEs (accepted list maintained) | `security.yml` / `Security` |
| Go                    | `gosec`                             | medium severity                           | `security.yml` / `Security` |
| Rust                  | `cargo-audit`                       | any CVE                                   | `security.yml` / `Security` |
| JavaScript/TypeScript | `pnpm audit`                        | critical only                             | `security.yml` / `Security` |
| JavaScript/TypeScript | `eslint` + `eslint-plugin-security` | all violations                            | `ci.yml` / `JS`             |
| All                   | `gitleaks`                          | any secret detection                      | `security.yml` / `Security` |
| All                   | `semgrep`                           | all violations (XSS, injection, etc.)     | `security.yml` / `Security` |
| All                   | `CodeQL`                            | all violations (push to dev/main, weekly) | `security.yml` / `Security` |
| Contracts             | `scripts/contract-audit.sh`         | audit service findings                    | `security.yml` / `Security` |
| Container images      | `trivy` (api, signer)               | HIGH/CRITICAL with a fix available        | `security.yml` / `Security` |

Container image scanning (#1532) covers the two production Dockerfiles that exist today: `apps/api/Dockerfile` and
`apps/api/Dockerfile.signer`. The dapp has no production Dockerfile (only `Dockerfile.dev`, it deploys as a built
Next.js artifact, not a container), so there is nothing to scan for it yet. Release images are pushed to GHCR only by
`deploy.yml` on a `v*.*.*` tag. Image signing (cosign, keyless OIDC) and signature verification at deploy time are not
implemented yet; the `build-images` job in `deploy.yml` is where signing would attach.

## Required status checks

Branch protection on `dev` and `main` requires exactly these checks, which are also every check a pull request shows
(apart from external apps such as CodeRabbit):

| Check              | Workflow        | What it covers                                                                      |
| ------------------ | --------------- | ----------------------------------------------------------------------------------- |
| `JS`               | `ci.yml`        | website build/lint; dapp security lint, build, unit tests, Playwright E2E           |
| `API`              | `ci.yml`        | migrations, build, vet, staticcheck, generated-file checks, tests; observability    |
| `Contracts`        | `ci.yml`        | WASM builds, tests, fmt, clippy, integration tests, WASM hashes                     |
| `Security`         | `security.yml`  | gitleaks, dependency audits, gosec, semgrep, typosquat, Trivy, SBOM, contract audit |
| `Contracts freeze` | `pr-policy.yml` | contracts change-control rule after `audit-candidate-1`                             |

Each workflow has one job per check. Each job runs the path filter as its first step and skips the steps a change does
not need, so a check is never left pending and an untouched area passes in seconds. A pull request therefore always
shows five checks; adding a job adds a check, so new work goes into an existing job.

Playwright E2E is the one non-blocking part (`continue-on-error`) until the runner's browser install is fixed; it was
never part of the merge gate. No other required step uses `continue-on-error`.

## Workflow budget

`.github/workflows/` holds at most five files: `ci.yml`, `security.yml`, `scheduled.yml`, `deploy.yml` and
`pr-policy.yml`. New work goes into whichever of these owns its trigger; adding a sixth file requires removing one, and
the PR must say why. Every workflow declares top-level `permissions: contents: read` (jobs that need more raise it themselves), and
every action is pinned to a commit SHA. Go, Rust and Node/pnpm setup lives in the `.github/actions/setup` composite
action.

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

## JavaScript/TypeScript (apps/dapp/frontend, apps/website)

### pnpm audit

**Threshold:** Critical only (0 found today).

**Moderate and high** are reported but do not block. Track in #1025 for upstream fixes to Expo/react-native and Trezor dependencies.

**How it works:**

```bash
pnpm audit --audit-level=critical
```

**Where it runs:**

- `security.yml` — `Security` job, when JS files change

### eslint + eslint-plugin-security

**Threshold:** All violations must pass (part of the build).

**Config:** `apps/dapp/frontend/eslint.config.mjs` includes `security.configs.recommended`.

**How it works:**

```bash
cd apps/dapp/frontend
pnpm run lint
```

**Where it runs:**

- `ci.yml` — `JS` job (`lint:security`), enforced (was `continue-on-error: true`, fixed in #1236)

## All Languages

### gitleaks

**Threshold:** No credentials detected.

**How it works:**

```bash
gitleaks detect --source . --no-git --redact --config .gitleaks.toml
```

**Where it runs:**

- `security.yml` — `Security` job, always

### semgrep

**Threshold:** All violations. Checks TypeScript/Next.js for XSS, injection, secrets, etc.

**How it works:**

```bash
semgrep scan --config p/typescript p/react p/nextjs p/secrets
```

**Where it runs:**

- `security.yml` — `Security` job, dapp changes

### CodeQL

**Threshold:** All violations across Go, TypeScript and Python.

**Where it runs:**

- `security.yml` — `Security` job on pushes to `dev`/`main`, weekly and on demand. Not on pull requests, where it would
  add a check of its own; findings surface on the merge commit.

## CI Workflows

### ci.yml

Runs on pull requests and pushes to `dev`/`main`. Three jobs, each path-filtered step by step
(`.github/path-filters.yml`):

- **JS:** website build and lint; dapp security lint, build, unit tests, Playwright E2E (non-blocking)
- **API:** migration checks, build, vet, staticcheck, OpenAPI and config-doc checks, unit, integration and property
  tests; promtool/amtool checks, burn-rate and probe tests, dashboard drift when observability files change
- **Contracts:** build, tests, fmt, clippy, integration tests, WASM hashes

### security.yml

Runs on pull requests, pushes to `dev`/`main`, weekly, and on demand, as a single `Security` job. Language-specific
scans are path-filtered the same way; scheduled and manual runs scan everything. Every scan step runs even after an
earlier one fails, so one finding never hides another:

- gitleaks over full history
- `pnpm audit`, `govulncheck`, `cargo-audit`
- gosec, Semgrep, typosquat check
- Trivy on the api and signer images
- CycloneDX/syft SBOMs
- `scripts/contract-audit.sh`
- CodeQL (not on pull requests)

### scheduled.yml

Every 15 minutes: synthetic probes against staging. Daily 03:00 UTC: contracts property tests and fuzzing, API property
tests, testnet deposit/withdraw journey. Weekly (Sunday 02:00 UTC): staging load soak. Each cron runs only its own jobs;
a manual run picks one with the `task` input (`nightly`, `soak` or `probes`). A failed nightly or soak run opens or
updates one `nightly-failure` tracking issue; probe failures alert through the pushed metrics instead.

### deploy.yml

`workflow_dispatch` deploys to staging (build from source, deploy, readiness wait, smoke tests). Pushing a `v*.*.*` tag
deploys to production: build and push the API and signer images, pin them by digest, run migrations and deploy behind
the `production` environment approval, smoke-test, and roll back automatically if the smoke tests fail.

### pr-policy.yml

Checks on pull request metadata rather than code; today the `Contracts freeze` check. Separate from `ci.yml` because it
reruns on every label or description edit.

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
3. Add it to the one workflow that should own it (ci.yml for build/test, security.yml for scanners); a tool must not run in two
4. Document the threshold and policy in this file
5. Ensure it blocks the build or clearly state why it doesn't

## Further Reading

- [SECURITY.md](../SECURITY.md) — production security model
- [deploy/](../../deploy) — production security posture
- `.github/workflows/security.yml` — full security job definitions
- `.github/workflows/ci.yml` — build and test gates
- `.github/workflows/scheduled.yml` — probes, property tests, fuzzing, E2E and soak

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

**Fails when:** Static analysis detects potential security issues across Go or TypeScript.

**Example:** SQL injection pattern, path traversal, or unsafe deserialization → CodeQL detects → CI fails.
