# Nester Smoke Test Suite

Full-stack smoke tests for gating deployment to staging/production. These tests verify the critical happy path: user registration, wallet connection, deposit, balance updates, withdrawal, and settlement.

**GitHub Issue**: [#1116 - test(repo): full-stack smoke test gating every deploy](https://github.com/suncrest-labs/nester/issues/1116)

## Quick Start

### Run Locally (Development)

```bash
# Terminal 1: Start the full Nester stack
cd nester
make dev

# Terminal 2: Run smoke tests once
cd apps/dapp/frontend
npx playwright test tests/smoke.spec.ts --run

# Or watch mode for development
npx playwright test tests/smoke.spec.ts
```

### Run in CI (Automated)

The smoke tests are automatically triggered after each staging deployment via GitHub Actions:

```
Deploy to Staging → Smoke Tests → Pass/Fail Gate → Promotion
```

See `.github/workflows/deploy-staging.yml` for workflow configuration.

## Test Structure

**Main test file**: `tests/smoke.spec.ts`

**Helper modules** in `tests/smoke/helpers/`:
- `account.ts` — Test account creation and login
- `wallet-harness.ts` — Headless wallet automation (mock Freighter)
- `faucet.ts` — Testnet fund requests (Friendbot)
- `tx-helpers.ts` — Transaction polling and confirmation
- `deposit-flow.ts` — Orchestrate deposit UI flow
- `withdraw-flow.ts` — Orchestrate withdrawal UI flow
- `balance-monitor.ts` — Verify balance updates
- `settlement-monitor.ts` — Verify withdrawal settlement

**CI helpers** in `tests/smoke/ci-helpers/`:
- `smoke-result-writer.ts` — Generate `smoke-result.json` artifact

## Canonical Happy Path

The smoke test implements these sequential steps:

### 1. Register
- Create a new test account with unique email
- Submit registration form
- Verify redirect to onboarding/dashboard
- **Expected runtime**: ~5 seconds

### 2. Connect Wallet
- Inject mock wallet module for automated signing
- Click "Connect Wallet" button
- Select Freighter (or first available wallet)
- Verify wallet address displayed in UI
- **Expected runtime**: ~3 seconds

### 3. Deposit
- Request testnet funds from Friendbot (~10 XLM for fees)
- Wait for funds to confirm
- Navigate to deposit modal
- Enter amount (50 USDC)
- Submit transaction
- Poll Horizon for confirmation (~2-3 polls @ 2s interval)
- **Expected runtime**: ~15-20 seconds

### 4. Balance Update
- Wait for UI balance display to reflect deposit
- Verify balance >= 49 USDC (allowing for small fees)
- Poll until balance is stable
- **Expected runtime**: ~5-10 seconds

### 5. Withdraw
- Navigate to withdraw modal
- Enter shares to withdraw (80% of deposited amount)
- Submit transaction
- Poll for confirmation
- **Expected runtime**: ~15-20 seconds

### 6. Settle
- Poll until withdrawal settles (moves from pending to settled/claimable)
- Verify final balance reconciles
- Check UI balance matches API balance (within 1% tolerance)
- **Expected runtime**: ~10-15 seconds

**Total expected runtime**: 6-8 minutes (well under 10-minute limit)

## Environment & Configuration

### Testnet Defaults

The smoke tests are configured for **testnet only**:
- **Stellar Network**: `Test SDF Network ; September 2015`
- **RPC URL**: `https://soroban-testnet.stellar.org`
- **Horizon URL**: `https://horizon-testnet.stellar.org`
- **Friendbot**: `https://friendbot.stellar.org`
- **Asset**: USDC on testnet (Circle testnet issuer)

### Environment Variables

Local development (in `apps/dapp/frontend/.env.local`):
```bash
NEXT_PUBLIC_API_URL=http://localhost:8080/api/v1
NEXT_PUBLIC_WS_URL=ws://localhost:8080/ws
NEXT_PUBLIC_STELLAR_NETWORK=Test SDF Network ; September 2015
NEXT_PUBLIC_STELLAR_RPC_URL=https://soroban-testnet.stellar.org
NEXT_PUBLIC_STELLAR_HORIZON_URL=https://horizon-testnet.stellar.org
NEXT_PUBLIC_VAULT_CONTRACT_ADDRESS=C...
NEXT_PUBLIC_VAULT_TOKEN_CONTRACT_ADDRESS=C...
```

CI/Staging (via GitHub Actions secrets):
```bash
SMOKE_TEST_WALLET_SECRET=S... # Testnet keypair seed
STAGING_URL=https://staging.nester.dev
STAGING_API_KEY=sk_...
FAUCET_API_KEY=... # If using private faucet instead of Friendbot
```

### Wallet Setup

The smoke tests use a **headless mock wallet** for signing transactions:

1. **Test Keypair**: Stored in `SMOKE_TEST_WALLET_SECRET` (CI secret)
   - Must be a testnet-only keypair
   - Only used for smoke tests, not production
   - Never committed to repo

2. **Mock Module Injection**: 
   - Test wallet module injected into page before navigation
   - Implements StellarWalletsKit module interface
   - Auto-signs transactions without popup

3. **Local Development Fallback**:
   - If `SMOKE_TEST_WALLET_SECRET` not set, uses deterministic testnet keypair
   - Suitable only for local/CI runs, never production

## Failure Scenarios

### Expected Failures (Test Itself Fails)

- **Timeout**: Step exceeds 90-second timeout (poor network, infrastructure issue)
- **Balance mismatch**: Deposited amount not reflected in UI/API (backend bug)
- **Wallet connection**: Mock wallet injection fails (framework incompatibility)
- **Transaction rejected**: On-chain transaction fails (contract error, insufficient funds)

### Edge Cases Handled

- **Transient RPC failures**: Auto-retry with exponential backoff, bounded to 3 retries
- **Slow block times**: Conservative per-step timeouts (90s) adapted for staging network conditions
- **Faucet unavailability**: Test logs actionable error; CI can skip or override if needed
- **Settlement delays**: Allows up to 2 minutes for withdrawal to settle (typical 30-60s)

### Observability & Debugging

Each run produces artifacts:

- **`smoke-result.json`**: Machine-parsable result with per-step status, durations, tx hashes
- **HTML report**: Playwright trace with screenshots, logs, network traffic
- **Console logs**: Step markers (`STEP:deposit:PASS:...`) for grep/parsing

## Machine-Parsable Result Format

### `smoke-result.json` Schema

```json
{
  "runId": "smoke-1692700000000-abc1234",
  "startedAt": "2024-08-26T10:00:00Z",
  "completedAt": "2024-08-26T10:07:30Z",
  "steps": [
    {
      "name": "register",
      "status": "PASS",
      "message": "Account created: smoke-1692700000000@test.nester.dev",
      "durationMs": 4200,
      "txHash": null
    },
    {
      "name": "connect-wallet",
      "status": "PASS",
      "message": "Wallet connected: GABCD...XYZ",
      "durationMs": 2800,
      "txHash": null
    },
    {
      "name": "deposit",
      "status": "PASS",
      "message": "Deposited 50 USDC",
      "durationMs": 18500,
      "txHash": "abc123...def456"
    },
    {
      "name": "balance-update",
      "status": "PASS",
      "message": "Balance updated to reflect 50 USDC deposit",
      "durationMs": 7200,
      "txHash": null
    },
    {
      "name": "withdraw",
      "status": "PASS",
      "message": "Withdrew 40 USDC equivalent",
      "durationMs": 16800,
      "txHash": "def456...ghi789"
    },
    {
      "name": "settle",
      "status": "PASS",
      "message": "Withdrawal settled; balances reconciled",
      "durationMs": 12400,
      "txHash": null
    }
  ],
  "summary": {
    "passed": 6,
    "failed": 0,
    "durationMs": 450900
  }
}
```

### Output Format

The test outputs a one-line summary suitable for GitHub Actions job summary:

```
SMOKE: PASS (6/6 steps, 450s)
```

On failure:
```
SMOKE: FAIL at balance-update (4/6 steps, 285s)
```

## CI Integration

### Workflow Job Configuration

The `.github/workflows/deploy-staging.yml` workflow includes:

```yaml
smoke-test:
  name: "Smoke Tests"
  runs-on: ubuntu-latest
  needs: deploy # Waits for deploy job to complete
  if: success() # Only runs if deploy succeeded
  steps:
    - uses: actions/checkout@v4
    - name: Setup Node
      uses: actions/setup-node@v4
      with:
        node-version: "22"
    - name: Install dependencies
      run: pnpm install --frozen-lockfile
    - name: Run smoke tests
      env:
        SMOKE_TEST_WALLET_SECRET: ${{ secrets.SMOKE_TEST_WALLET_SECRET }}
        STAGING_URL: ${{ vars.STAGING_URL }}
      run: |
        cd apps/dapp/frontend
        npx playwright test tests/smoke.spec.ts --run
    - name: Upload test artifacts
      if: always()
      uses: actions/upload-artifact@v4
      with:
        name: smoke-test-results
        path: |
          apps/dapp/frontend/playwright-report/
          apps/dapp/frontend/test-results/
          smoke-result.json
```

### Required Secrets

Configure these in GitHub repository settings:

- **`SMOKE_TEST_WALLET_SECRET`**: Testnet keypair seed (S...)
  - Generate: `npx @stellar/stellar-sdk randomKeypair` (or similar)
  - Fund with ~50 XLM from testnet faucet for testing
  - Used for all smoke test transaction signing
  - Rotate periodically

- **`STAGING_URL`** (optional): Staging environment URL
  - Default: `http://localhost:3001` (for local runs)
  - CI override: staging URL if different from local dev

- **`FAUCET_API_KEY`** (optional): Private faucet if not using Friendbot
  - Only needed if Friendbot is unavailable or rate-limited

### Failure Handling

If smoke tests fail:

1. **Workflow Status**: Job fails, blocking promotion
2. **Artifacts Uploaded**: Screenshots, logs, `smoke-result.json` available for debugging
3. **Notification**: Slack/email alert sent to team (configurable)
4. **Override Procedure**: Manual approval gate can skip on transient failures (documented in runbook)

## Local Development & Testing

### First Time Setup

```bash
# 1. Clone and install
git clone https://github.com/suncrest-labs/nester.git
cd nester
pnpm install

# 2. Start stack
make dev

# 3. In another terminal, run tests
cd apps/dapp/frontend
npx playwright test tests/smoke.spec.ts --run
```

### Debugging

```bash
# Run with headed browser (see UI interactions)
npx playwright test tests/smoke.spec.ts --headed

# Run single test with debug UI
npx playwright test tests/smoke.spec.ts -g "full-stack" --debug

# Run with verbose logging
DEBUG=pw:api npx playwright test tests/smoke.spec.ts --run

# Generate trace for inspection
npx playwright show-trace playwright-report/trace.zip
```

### Test Isolation

Each smoke test run creates a unique test account and wallet, ensuring:
- No interference between parallel runs
- Clean state for every test (ephemeral accounts)
- Safe to run multiple times without cleanup

Test data is **not persisted**; temporary accounts are abandoned after the test completes.

## Troubleshooting

### Common Issues

| Error | Cause | Fix |
|-------|-------|-----|
| `Wallet connection timeout` | Mock wallet not injected | Check `SMOKE_TEST_WALLET_SECRET` env var is set |
| `Transaction timed out` | RPC slow or network issue | Increase timeout in `tx-helpers.ts` or check RPC health |
| `Balance did not update` | Backend sync delay | Increase polling interval in `balance-monitor.ts` or check API |
| `Faucet unavailable` | Friendbot rate-limited or down | Use private faucet or skip funding step |
| `Settlement timeout` | Withdrawal processing slow | Check settlement contract or increase settlement timeout |

### Debug Logs

Enable detailed logging:

```bash
# In playwright.config.ts, set trace: 'on' instead of 'on-first-retry'
# Then inspect trace with:
npx playwright show-trace playwright-report/trace.zip
```

### Network Issues

If tests fail on network timeouts:

1. Check RPC and Horizon health:
   ```bash
   curl https://soroban-testnet.stellar.org/ -o /dev/null -s -w "%{http_code}"
   curl https://horizon-testnet.stellar.org/ -o /dev/null -s -w "%{http_code}"
   ```

2. Verify firewall allows outbound HTTPS to Stellar services

3. Increase polling timeouts in `tx-helpers.ts` and per-step timeouts in `smoke.spec.ts`

## Contributing

### Adding New Smoke Test Steps

1. Create a new helper in `tests/smoke/helpers/` following existing patterns
2. Export a single function that encapsulates the step (e.g., `performDeposit`)
3. Return a result object with `{ status, durationMs, message, txHash? }`
4. Add to `tests/smoke.spec.ts` following the canonical step sequence
5. Update this README with the new step in the happy path section

### Updating Timeouts

Smoke test timeouts are configured in `smoke.spec.ts`:

```typescript
const STEP_TIMEOUT_MS = 90_000; // 90 seconds per step
```

Adjust based on staging network performance monitoring. Keep total runtime under 10 minutes.

### Performance Tuning

Monitor actual smoke test durations from CI runs:

1. Check `smoke-result.json` artifacts from failed/slow runs
2. Per-step durations guide where to optimize
3. Common optimizations:
   - Reduce polling interval (faster detection but more RPC calls)
   - Pre-fund test accounts instead of using faucet (save ~10s per run)
   - Parallelize independent steps (register + wallet connect)

## Related Documentation

- **CI Workflow**: `.github/workflows/deploy-staging.yml`
- **Test Runbook**: `docs/smoke-test-runbook.md`
- **Playwright Config**: `apps/dapp/frontend/playwright.config.ts`
- **Stellar SDK**: [@stellar/stellar-sdk](https://docs.stellar.org/learn/building-blocks/introduction)
- **Soroban**: [Soroban Documentation](https://developers.stellar.org/learn/fundamentals/soroban)

## Questions & Support

For issues or questions about the smoke tests:

1. Check this README and the runbook first
2. Open a GitHub issue with the `smoke-test` label
3. Include `smoke-result.json` and Playwright report if available
4. Contact the platform team in `#nester-dev` Slack channel

---

**Last Updated**: August 26, 2024  
**Maintainers**: Platform Engineering Team
