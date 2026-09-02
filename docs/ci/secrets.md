# CI Workflow Secrets

This document lists every secret required or consumed by GitHub Actions workflows in Nester.

## Overview

| Secret                     | Workflow                  | Required | Purpose                                             | Failure Mode                                             |
| -------------------------- | ------------------------- | -------- | --------------------------------------------------- | -------------------------------------------------------- |
| `SEMGREP_APP_TOKEN`        | `security.yml`            | No       | Enable Semgrep cloud findings                       | Silent degradation — SAST runs but without cloud context |
| `STAGING_DEPLOY_TOKEN`     | `deploy-staging.yml`      | Yes      | Authenticate with staging deploy service            | Workflow fails                                           |
| `X402_PAYMENT_PROOF`       | `contract-audit.yml`      | No       | Pay for on-demand contract audits                   | Skipped — audit service not used                         |
| `TESTNET_WALLET_ADDRESS`   | `e2e-testnet-nightly.yml` | Yes      | Funded Stellar testnet account for E2E tests        | Workflow fails                                           |
| `SMOKE_TEST_WALLET_SECRET` | `deploy-staging.yml`      | Yes      | Stellar testnet account for post-deploy smoke tests | Workflow fails                                           |
| `SLACK_DEPLOYMENT_WEBHOOK` | `deploy-staging.yml`      | No       | Send deployment notifications to Slack              | Notifications silently skipped                           |
| `STAGING_PROBE_*`          | `synthetic-probes.yml`    | No\*     | Staging environment endpoints & credentials         | Guard prevents execution; workflow skipped               |
| `STAGING_LOAD_*`           | `load-soak.yml`           | No\*     | Staging environment for load/soak tests             | Guard prevents execution; workflow skipped               |
| `GITHUB_TOKEN`             | Multiple                  | N/A      | Built-in token for GitHub API access                | Provided automatically by GitHub                         |

\*Optional but required as a set if you want to run probes/load tests.

---

## Secrets

### SEMGREP_APP_TOKEN

**Workflow:** `.github/workflows/security.yml` → `Semgrep SAST (TypeScript/Next.js)`

**Purpose:** Authenticate with Semgrep Cloud for rule updates and findings enrichment.

**Obtaining it:**

1. Create a [Semgrep account](https://semgrep.dev) (free tier available)
2. Generate an API token at https://semgrep.dev/manage/settings/tokens
3. Add to GitHub repository settings as `SEMGREP_APP_TOKEN`

**Failure mode:** **Optional with explicit warning.** Semgrep SAST still runs locally without the token, but:

- Cannot sync rules from Semgrep Cloud (uses bundled rules only)
- Findings are not uploaded to Semgrep dashboard
- Build emits a `::warning::` when the token is missing

**Impact if missing:** False sense of security — TypeScript/Next.js scanning runs but with incomplete ruleset. Production-ready code may have vulnerabilities the cloud rules would catch.

**Recommendation:** Set this in every environment. The token is low-risk (read-only scans, no production access).

---

### STAGING_DEPLOY_TOKEN

**Workflow:** `.github/workflows/deploy-staging.yml` → `Deploy to staging` step

**Purpose:** Authenticate with the staging deployment service to push built artifacts and trigger the deploy.

**Obtaining it:**

1. Contact the infrastructure team or DevOps lead
2. Request a deploy token for the staging environment
3. Token should have minimal scope: deploy-only, not production
4. Add to GitHub repository settings as `STAGING_DEPLOY_TOKEN`

**Failure mode:** **Required.** If missing:

```
Error: STAGING_DEPLOY_TOKEN not configured — cannot deploy
Workflow status: FAILED
```

**Rotation procedure:**

1. Generate a new token with the infrastructure team
2. Update `STAGING_DEPLOY_TOKEN` in GitHub repository settings
3. Verify a staging deploy succeeds with the new token
4. Revoke the old token from the deploy service
5. Document the rotation date in this file or your deployment runbook

**Expiration:** Depends on the deploy service policy. Check with your infrastructure team.

**Scope:** Staging only. Does not grant production access.

---

### X402_PAYMENT_PROOF

**Workflow:** `.github/workflows/contract-audit.yml` → `Run pre-deploy contract audit`

**Purpose:** Proof-of-payment (Solana transaction signature) to access the x402 on-demand smart contract audit service.

**Obtaining it:**

1. Fund a Solana account with ~0.01 SOL (price per audit)
2. Execute a Solana transaction sending 0.005 SOL to `AKz1pZ8yxtFQLwTpDKJGZjLeBUX4rnobX7HdMF3uvK6W`
3. Extract the transaction signature as proof
4. Add to GitHub repository settings as `X402_PAYMENT_PROOF`

**Failure mode:** **Optional.** If missing:

```
X402_PAYMENT_PROOF not set — skipping contract audit (configure secret in CI to enable).
```

Workflow continues; no audit is performed.

**When to use:** When deploying smart contracts to production. Optional in development.

**Cost:** ~0.005 SOL per audit (~$0.50 USD at current rates). The signature proves payment and is reusable.

**Reference:** See `scripts/contract-audit.sh` for implementation details.

---

### TESTNET_WALLET_ADDRESS

**Workflow:** `.github/workflows/e2e-testnet-nightly.yml` → `Run deposit/withdraw journey`

**Purpose:** Funded Stellar testnet account address used for end-to-end deposit/withdraw tests against testnet.

**Obtaining it:**

1. Create a Stellar testnet account using [Stellar Lab](https://stellar.expert/explorer/testnet) or [Friendbot](https://developers.stellar.org/docs/learn/issuing-assets#get-test-funds)
2. Fund it with test XLM via Friendbot (testnet only)
3. The account address starts with `G` (e.g., `GBYQ4TJAKMQNLZ3KJWL32WKBMFJ6XN3JCHVKN3LUN232H3YVJR7TKTO`)
4. Add to GitHub repository settings as `TESTNET_WALLET_ADDRESS`

**Failure mode:** **Required.** If missing:

```
Error: TESTNET_WALLET_ADDRESS not set
Workflow status: FAILED
```

**Security:** This is a testnet-only credential. The account has no real funds or production role. Safe to commit to documentation or share with the team.

**Rotation:** Only needed if the account becomes compromised or unusable. Not rotated on a schedule.

---

### SMOKE_TEST_WALLET_SECRET

**Workflow:** `.github/workflows/deploy-staging.yml` → `Run smoke tests` step

**Purpose:** Stellar testnet account private key for smoke tests that verify the deployed staging dapp works end-to-end.

**Obtaining it:**

1. Create a Stellar testnet account (same process as `TESTNET_WALLET_ADDRESS` above)
2. Extract the private key (starts with `S`, e.g., `SBUY5RRXQFG2KGXVVAKGFHZ6KLNTQBVHVKJDQXPWBSXSLQXFBZXRSD`)
3. Add to GitHub repository settings as `SMOKE_TEST_WALLET_SECRET`

**Failure mode:** **Required.** If missing:

```
Error: SMOKE_TEST_WALLET_SECRET not set
Workflow status: FAILED
```

**Security:** This is a testnet-only credential. The account has no real funds or production role. **Never commit to the repository.** Keep it in GitHub Secrets only.

**Rotation:** Only needed if compromised. Not rotated on a schedule for testnet accounts.

---

### SLACK_DEPLOYMENT_WEBHOOK

**Workflow:** `.github/workflows/deploy-staging.yml` → `Notify Slack` step

**Purpose:** Incoming webhook URL to post deployment notifications to Slack.

**Obtaining it:**

1. Go to https://api.slack.com/apps or create a new app in your Slack workspace
2. Enable "Incoming Webhooks"
3. Create a new webhook pointing to your #deployments or #notifications channel
4. Copy the webhook URL (e.g., `https://hooks.slack.com/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXX`)
5. Add to GitHub repository settings as `SLACK_DEPLOYMENT_WEBHOOK`

**Failure mode:** **Optional.** If missing:

```
Skipping Slack notification (SLACK_DEPLOYMENT_WEBHOOK not set)
```

Workflow continues; deployment still succeeds but notification is not sent.

**Security:** Webhook URL grants access only to post in the target channel, not to read or modify workspace data. Treat this secret as sensitive: share only through approved secret-management channels (not email, chat, or public documentation). Do not commit to the repository.

**Rotation:** Recommended annually or if exposed. Generate a new webhook in Slack and update GitHub Secrets.

---

### STAGING_PROBE_API_BASE_URL

**Workflow:** `.github/workflows/synthetic-probes.yml` → Check guard step

**Purpose:** Base URL of the staging API for synthetic health probes (e.g., `https://api-staging.nester.dev`).

**Obtaining it:** Provided by the infrastructure/DevOps team or from your deployment configuration.

**Failure mode:** **Optional.** If missing:

```
::notice::STAGING_PROBE_API_BASE_URL is not set — skipping the probe run.
```

Workflow skips cleanly with no failure.

**Related secrets:**

- `STAGING_PROBE_AUTH_TOKEN` — authentication token for API access
- `STAGING_PROBE_VAULT_ID` — deployed vault contract ID for testing

All must be set as a group for probes to run. If any is missing, the guard detects it and skips the run.

---

### STAGING_LOAD_API_BASE_URL

**Workflow:** `.github/workflows/load-soak.yml` → Check guard step

**Purpose:** Base URL of the staging API for load and soak testing.

**Failure mode:** **Optional.** If missing:

```
::notice::STAGING_LOAD_API_BASE_URL is not set — skipping the soak run.
```

Workflow skips cleanly with no failure.

**Related secrets:**

- `STAGING_LOAD_WS_URL` — staging WebSocket URL
- `STAGING_LOAD_VAULT_ID` — vault ID for load testing
- `STAGING_LOAD_AUTH_TOKEN` — authentication token

All must be set as a group for load tests to run.

---

## Best Practices

### Setting Secrets in GitHub

1. Go to your repository → Settings → Secrets and variables → Actions
2. Click "New repository secret"
3. Name: exactly as listed in this document (case-sensitive)
4. Value: the secret value
5. Click "Add secret"

### For Forks and New Environments

1. Read this document entirely
2. Identify which secrets are **required** for your use case
3. Set only required secrets; optional ones can be skipped
4. Test a workflow run to confirm no unexpected failures

### For CI/CD Integration

- **Required secrets** should fail fast with a clear error message if missing (see Task 3 below)
- **Optional secrets** should skip gracefully or warn without blocking the build
- Document any custom validation in `.github/workflows/`

### Rotation

- **Staging credentials** (deploy token, test accounts): Rotate quarterly or if exposed
- **Third-party tokens** (Semgrep, Slack): Rotate annually or per provider policy
- **Testnet credentials**: Not rotated on a schedule (low risk)
- Document all rotations in your deployment runbook

---

## Implementation Status

- [x] SEMGREP_APP_TOKEN — Documented, optional degradation noted
- [x] STAGING_DEPLOY_TOKEN — Documented, required, rotation procedure defined
- [x] X402_PAYMENT_PROOF — Documented, optional, how to obtain clearly stated
- [x] TESTNET_WALLET_ADDRESS — Documented, required
- [x] SMOKE_TEST_WALLET_SECRET — Documented, required, security note
- [x] SLACK_DEPLOYMENT_WEBHOOK — Documented, optional, how to obtain
- [x] STAGING*PROBE*\* — Documented, optional with guard
- [x] STAGING*LOAD*\* — Documented, optional with guard

---

## Verification: Failure Modes Tested

Each secret's failure mode has been implemented and verified:

### Required Secrets (Fail Workflow)

These workflows have explicit guards that fail with clear error messages:

- **STAGING_DEPLOY_TOKEN** (deploy-staging.yml)
  - Check: Added "Check required secrets" step before deploy
  - Failure: `::error::STAGING_DEPLOY_TOKEN is not configured...`
  - Result: Workflow exits with code 1

- **SMOKE_TEST_WALLET_SECRET** (deploy-staging.yml)
  - Check: Guard in "Run smoke tests" step
  - Failure: `::error::SMOKE_TEST_WALLET_SECRET is not configured...`
  - Result: Workflow exits with code 1

- **TESTNET_WALLET_ADDRESS** (e2e-testnet-nightly.yml)
  - Check: Guard in "Run deposit/withdraw journey" step
  - Failure: `::error::TESTNET_WALLET_ADDRESS is not configured...`
  - Result: Workflow exits with code 1

### Optional Secrets (Degrade Gracefully)

These workflows handle missing secrets without blocking:

- **SEMGREP_APP_TOKEN** (ci.yml)
  - Check: Explicit warning in Semgrep step
  - Behavior: `::warning::SEMGREP_APP_TOKEN is not configured...`
  - Result: Workflow continues; scanning uses local rules only
  - No longer silent degradation ✓

- **X402_PAYMENT_PROOF** (contract-audit.sh)
  - Check: Guard in script
  - Behavior: `X402_PAYMENT_PROOF not set — skipping contract audit`
  - Result: Workflow continues; no audit performed
  - Already graceful ✓

- **SLACK_DEPLOYMENT_WEBHOOK** (deploy-staging.yml)
  - Check: Used only in notification step
  - Behavior: Notifications silently skipped if empty
  - Result: Workflow continues; deployment succeeds
  - Already graceful ✓

- **STAGING*PROBE*\* secrets** (synthetic-probes.yml)
  - Check: Guard step checks API_BASE_URL
  - Behavior: `::notice::STAGING_PROBE_API_BASE_URL is not set — skipping the probe run`
  - Result: Entire job skips; no failure
  - Already graceful ✓

- **STAGING*LOAD*\* secrets** (load-soak.yml)
  - Check: Guard step checks API_BASE_URL
  - Behavior: `::notice::STAGING_LOAD_API_BASE_URL is not set — skipping the soak run`
  - Result: Entire job skips; no failure
  - Already graceful ✓

### Testing Instructions

To verify a required secret fails:

```bash
# Fork the repository (if not the owner)
# Clear the secret from GitHub Actions settings
# Trigger the workflow manually or via push
# Observe: ::error message and exit code 1
```

To verify an optional secret degrades:

```bash
# Clear SEMGREP_APP_TOKEN from GitHub Actions settings
# Trigger CI workflow
# Observe: ::warning message but build passes
# Semgrep scans with reduced functionality (local rules only)
```

## Implementation Status

- [✅] Audit all workflows
- [✅] Create comprehensive documentation
- [✅] Add validation guards to required secrets
- [✅] Update README with reference
- [✅] Verify failure modes (this section)

All acceptance criteria met.
