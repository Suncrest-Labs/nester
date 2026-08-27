# Smoke Test Runbook

Operational guide for monitoring, debugging, and responding to smoke test failures in the Nester deployment pipeline.

**Related Documentation**:
- Test implementation: `apps/dapp/frontend/tests/smoke/README.md`
- Workflow definition: `.github/workflows/deploy-staging.yml`

## Quick Reference

| Scenario | Action |
|----------|--------|
| Smoke test fails | Check artifact → debug step → re-run workflow |
| Persistent failures | Check infrastructure health → escalate to platform team |
| Transient failure (1 of last 3 passes) | Override and promote (document in PR) |
| Multiple consecutive failures | Page on-call → investigate infrastructure |

---

## Understanding Smoke Test Failures

### What Is a Smoke Test?

A smoke test is a minimal, fast validation of the critical happy path:
1. User registration
2. Wallet connection
3. Deposit transaction
4. Balance verification
5. Withdrawal transaction
6. Settlement confirmation

**Purpose**: Gate every staging deployment to catch regressions early without running full e2e suite (which takes 30+ minutes).

**Scope**: Testnet only. Real user flows simulated with deterministic test accounts and mock wallets.

**Runtime**: 6-8 minutes target (max 10 minutes in CI).

### Failure Categories

#### 1. Test Failure (Smoke Test Code/Logic Issue)

**Symptoms**:
- Specific step fails consistently (same step every run)
- Error message points to test code (e.g., "selector not found")
- Other changes in the same commit may have broken the UI

**Investigation**:
1. Download `smoke-test-results-<run-id>` artifact from failed workflow
2. Open `playwright-report/index.html` in browser → view failed step screenshots/logs
3. Check console logs for the specific error message
4. If UI changed, check if test selectors need updating

**Resolution**:
```bash
# 1. Fix test code or selectors
# 2. Commit to the PR branch
# 3. Re-run the workflow

git add apps/dapp/frontend/tests/smoke/...
git commit -m "fix(smoke): update selector for new UI element"
git push origin <branch>

# Then manually trigger:
# GitHub Actions > Deploy to Staging > Run workflow > <branch>
```

#### 2. Infrastructure Failure (Backend/Network Issue)

**Symptoms**:
- Timeout waiting for balance update or transaction confirmation
- "Horizon timeout" or "RPC unavailable" errors
- Fails at different steps on different runs (non-deterministic)
- Staging services are up but smoke tests hang

**Investigation**:
1. Check staging service health:
   ```bash
   curl -I https://staging.nester.dev/health
   curl -I https://api-staging.nester.dev/health
   ```

2. Check Stellar testnet health:
   ```bash
   curl -I https://soroban-testnet.stellar.org/
   curl -I https://horizon-testnet.stellar.org/
   ```

3. Check recent infrastructure changes:
   - Recent deployments?
   - Database migrations?
   - API changes that affect smoke test?

4. Review artifact logs for specific error:
   - Look for HTTP error codes
   - RPC/Horizon timeouts
   - Contract failures

**Resolution**:
```bash
# For transient network issues, simple retry often works:
# GitHub Actions > Deploy to Staging > Run workflow > (re-run failed job)

# For backend issues, coordinate with API/infrastructure team:
# - Check for recent API changes affecting smoke test
# - Verify contract addresses are correct for testnet
# - Check database/migration status
```

#### 3. Faucet Failure (Testnet Funding Issue)

**Symptoms**:
- Fails at "faucet" or funding step early in test
- Error: "Friendbot unavailable" or "insufficient funds"
- Testnet wallet has 0 XLM balance

**Investigation**:
1. Check Friendbot availability:
   ```bash
   curl https://friendbot.stellar.org/
   ```

2. If Friendbot is down, check alternatives:
   - Use private testnet faucet (if configured)
   - Manually fund test accounts
   - Skip funding by pre-provisioning accounts

3. If rate-limited:
   - Friendbot may have daily/hourly limits
   - Space out smoke test runs or use alternate faucet

**Resolution**:
```bash
# Option 1: Wait for Friendbot recovery (usually quick)
# Option 2: Use private faucet (requires config update)
# Option 3: Pre-fund test accounts with manual top-up
# Option 4: Override and skip faucet for this run
```

#### 4. Wallet/Signing Failure

**Symptoms**:
- Fails at "connect-wallet" step
- Error: "Wallet connection timeout" or "Mock wallet injection failed"
- Browser logs show wallet provider errors

**Investigation**:
1. Check that `SMOKE_TEST_WALLET_SECRET` is set in CI secrets:
   ```bash
   # GitHub Settings > Secrets > verify SMOKE_TEST_WALLET_SECRET exists
   ```

2. Verify secret format:
   - Must be a valid Stellar testnet keypair seed (starts with 'S')
   - Must be funded with at least 50 XLM for transaction fees
   - Must not be a production/mainnet key (security risk)

3. Check if wallet provider code changed:
   - Recent updates to `components/wallet-provider.tsx`?
   - Mock wallet injection logic in `wallet-harness.ts` compatible?

**Resolution**:
```bash
# 1. Regenerate test keypair if expired/compromised
npx @stellar/stellar-sdk randomKeypair
# Output: {"publicKey": "G...", "secret": "S..."}

# 2. Fund new keypair with testnet XLM
# Use friendbot or faucet

# 3. Update secret in GitHub:
# Settings > Secrets > SMOKE_TEST_WALLET_SECRET > Update

# 4. Re-run workflow
```

---

## Responding to Failures

### Step 1: Assess Impact

```
Is this a blocking issue?
├─ Yes, smoke tests fail every run
│  └─ Block promotion, escalate
├─ No, transient (1-2 of last 5 runs pass)
│  └─ Can override with documentation
└─ Maybe, inconsistent
   └─ Investigate root cause first
```

### Step 2: Collect Data

Always gather these before escalating:

1. **Artifact**: Download `smoke-test-results-<run-id>` from failed workflow
2. **Screenshots**: View Playwright report for visual state at failure
3. **Logs**: Check both test logs and staging service logs
4. **Recent changes**: Review PR/commits since last successful smoke test
5. **Timing**: When did failures start? (helps identify root cause)

```bash
# Example data gathering script
cd nester
git log --oneline -10 --decorate
curl https://staging.nester.dev/health
curl https://api-staging.nester.dev/health
```

### Step 3: Attempt Resolution

**For test code issues**:
```bash
git checkout -b fix/smoke-test-selector
# Edit test file to fix selector
git commit -m "fix(smoke): update balance element selector"
git push origin fix/smoke-test-selector
# Re-run workflow
```

**For infrastructure issues**:
```bash
# Coordinate with on-call infrastructure engineer
# Provide: artifact, error details, timing, recent changes
```

**For transient failures**:
```bash
# Document in PR that this is a known transient issue
# Re-run the job, document the override decision
# If becomes persistent, investigate root cause
```

### Step 4: Override (If Necessary)

**When to override**:
- Transient network blip (1-2 hour window)
- Known external service issue (Friendbot down, RPC maintenance)
- Non-blocking test infrastructure change (not smoke code itself)

**How to override**:

1. Manual approval (recommended):
   ```
   GitHub Actions > Deploy to Staging > Run workflow
   → Inputs: skip_smoke_tests = true
   → Run Workflow
   
   ⚠️  Document in deployment PR:
   "Skipped smoke tests due to [reason]. 
    Issue #1234 tracks. Manual testing performed."
   ```

2. Automatic override (not recommended):
   ```
   Only authorized for emergency hotfixes.
   Requires 2 approvals and incident ticket.
   ```

**Audit trail**:
- All overrides logged in GitHub Actions
- PR comments document reason
- Incident tracking system has ticket
- Notify #nester-incidents channel

---

## Monitoring & Alerting

### Key Metrics

Track these in observability system:

1. **Smoke test success rate**: Target 99%+ (alerts if <95%)
2. **Smoke test duration**: Target <8 min (alerts if >9 min)
3. **Staging service health**: Target 99.5% uptime
4. **RPC/Horizon availability**: Monitor from StatusPage

### Alert Configuration

**Slack alerts** (if configured):

```
When: Smoke tests fail
Then: Post to #nester-deploys
Contains: Failed step, run link, artifact link
```

**PagerDuty** (optional):

```
When: 3+ consecutive failures OR smoke test timeout
Then: Page on-call platform engineer
Severity: SEV-4 (low, investigation ticket)
```

### Dashboard

Create Grafana dashboard showing:
- Smoke test pass/fail over time
- Per-step durations
- Infrastructure health
- RPC/Horizon latency

---

## Escalation Path

### Level 1: Self-Help (Smoke Test Owner)

- Review artifact and logs
- Check infrastructure health
- Identify if test code vs infrastructure issue
- Attempt fix if code issue

**Time**: 5-10 minutes

### Level 2: Infrastructure Team

If issue is infrastructure-related:
- Page on-call (if in business hours, ping #nester-dev)
- Provide artifact, error details, timing
- Investigate staging service health
- Coordinate API/database changes

**Time**: 15-30 minutes

### Level 3: Incident Management

If blocks all deploys for >30 minutes:
- Create SEV-3 incident ticket
- Page SRE team
- Post update to #nester-incidents
- Coordinate rollback or emergency fix

**Time**: Continuous monitoring

---

## Common Issues & Fixes

### Issue: "Balance did not update"

**Cause**: Backend WebSocket not sending balance updates, or UI not consuming them.

**Fix**:
1. Check WebSocket connection in browser dev tools
2. Verify API is emitting `balance_updated` events
3. Check `balance-monitor.ts` polling interval (increase if necessary)
4. Increase timeout in `smoke.spec.ts` (STEP_TIMEOUT_MS)

```typescript
// In balance-monitor.ts
await waitForBalanceUpdate(page, {
  expectedMinimumBalance: 49,
  timeout: 90_000, // Increase from 60_000 if network is slow
  pollIntervalMs: 5000, // Increase from 3000 for less aggressive polling
});
```

### Issue: "Transaction timed out"

**Cause**: On-chain confirmation taking >90 seconds (slow testnet block times, RPC lag).

**Fix**:
1. Check testnet health: `https://status.stellar.org`
2. Increase per-step timeout (currently 90s, can go to 120s)
3. Reduce polling interval to detect faster (may increase RPC calls)
4. If testnet is slow, wait for network recovery

```typescript
// In smoke.spec.ts
const STEP_TIMEOUT_MS = 120_000; // 2 minutes max per step
```

### Issue: "Wallet connection failed"

**Cause**: `SMOKE_TEST_WALLET_SECRET` not available or invalid.

**Fix**:
1. Verify secret exists in GitHub Secrets
2. Test keypair locally:
   ```bash
   npx stellar account <public-key>
   ```
3. If account doesn't exist, fund it:
   ```bash
   curl "https://friendbot.stellar.org?addr=<public-key>"
   ```
4. Regenerate if compromised

### Issue: "Faucet unavailable"

**Cause**: Friendbot down or rate-limited.

**Fix**:
1. Check Friendbot status: `curl https://friendbot.stellar.org/`
2. If down, wait for recovery (usually <1 hour)
3. Use private faucet if available (requires configuration)
4. Pre-fund test accounts manually

### Issue: "Flaky tests" (fails ~50% of time)

**Cause**: Race conditions, timing issues, or intermittent infrastructure.

**Fix**:
1. Increase polling intervals (slower, more reliable)
2. Increase timeouts (allows more time for slow networks)
3. Add waits between steps
4. Review logs for pattern of failures

```typescript
// Conservative settings for reliability
const STEP_TIMEOUT_MS = 120_000; // 2 min
const POLL_INTERVAL_MS = 3000; // 3 sec
```

---

## Manual Re-runs

### Re-run from GitHub Actions UI

```
GitHub > Actions > Deploy to Staging
→ Click failed run
→ "Re-run failed jobs" button
→ Watch for results
```

### Re-run specific job

```
GitHub > Actions > Deploy to Staging
→ Click failed run
→ Jobs > smoke-test > Re-run job
```

### Manual CLI re-run (local dev)

```bash
cd nester/apps/dapp/frontend

# Set up environment
export SMOKE_TEST_WALLET_SECRET="S..."
export STAGING_URL="https://staging.nester.dev"

# Run smoke tests
npx playwright test tests/smoke.spec.ts --run

# View report
npx playwright show-trace playwright-report/trace.zip
```

---

## Performance Tuning

If smoke tests are consistently slow (approaching 10-minute limit):

### Measure Current Performance

```bash
# Run test 3 times, capture durations
npx playwright test tests/smoke.spec.ts --run -g "full-stack" --repeat-each 3

# Check smoke-result.json for per-step times
cat smoke-result.json | jq '.steps[] | "\(.name): \(.durationMs)ms"'
```

### Optimize Slow Steps

1. **If deposit is slow**: 
   - Reduce polling interval (2s → 1s)
   - But may increase RPC load

2. **If balance-update is slow**:
   - Reduce expected balance threshold (more frequent polling)
   - Check backend WebSocket latency

3. **If settlement is slow**:
   - Increase settlement timeout (80% of total budget)
   - May indicate slow backend processing

4. **If wallet connection is slow**:
   - Check browser extension/mock wallet initialization
   - May indicate framework/dependency issue

### Total Budget

```
Register: 5s
Connect Wallet: 3s
Deposit: 20s
Balance Update: 10s
Withdraw: 20s
Settle: 15s
─────────────────
Total: ~70s (well under 10-min limit)
```

---

## Debugging with Playwright Inspector

### Local Debug Session

```bash
# Run with browser visible and debugger paused at each step
PWDEBUG=1 npx playwright test tests/smoke.spec.ts --run --headed

# Use step-through controls in inspector
# View current DOM, network traffic, console logs
```

### Trace Inspection

```bash
# After test run, inspect trace
npx playwright show-trace playwright-report/trace.zip

# Navigate to failed step, view:
# - DOM state
# - Screenshot
# - Network requests
# - Browser console logs
```

### Remote Debugging

For CI failures that can't be reproduced locally:

```bash
# Add debug logging to test
console.log(`DEBUG: Current balance = ${await page.textContent('[data-testid="balance"]')}`);

# Commit to PR, re-run workflow
# Check GitHub Actions logs for debug output
```

---

## Post-Mortem Template

For recurring smoke test failures, document in incident:

```markdown
## Smoke Test Failure Post-Mortem

**Date**: YYYY-MM-DD  
**Impact**: Blocked deployment for X hours  
**Root Cause**: [TBD]  

### Timeline
- HH:MM - Smoke test started failing
- HH:MM - Issue identified as [category]
- HH:MM - Fix deployed/override approved
- HH:MM - Smoke tests passing again

### Resolution
[Describe fix or workaround]

### Prevention
[How to prevent recurrence]

### Owner
[Name/team responsible for this area]
```

---

## FAQ

**Q: Can I skip smoke tests for emergency hotfixes?**  
A: Yes, but requires:
1. Override in workflow input
2. Documentation in PR explaining urgency
3. Manual testing confirmation
4. Follow-up fix to prevent regression

**Q: How often are tests updated?**  
A: When UI changes (selectors), when backend changes (API, contract), or for performance tuning. Follow TDD: update test first, then implementation.

**Q: What if smoke test passes but prod breaks?**  
A: Smoke test may not cover regression. Add regression to smoke suite and re-test. Post-mortem to improve coverage.

**Q: How do I add a new step to smoke tests?**  
A: See `apps/dapp/frontend/tests/smoke/README.md` → Contributing section. Requires PR review to ensure step is deterministic and <90s.

**Q: Can smoke tests run in parallel?**  
A: No, tests currently sequential (same test account, to avoid conflicts). Can parallelize different accounts/vaults if needed (requires refactoring).

---

## Support & Contact

- **Documentation**: See `apps/dapp/frontend/tests/smoke/README.md`
- **Questions**: Open issue with `smoke-test` label
- **Incidents**: Post to #nester-incidents Slack channel
- **Team**: Reach out to platform engineering team in #nester-dev

---

**Last Updated**: August 26, 2024  
**Version**: 1.0  
**Maintainers**: Platform Engineering Team
