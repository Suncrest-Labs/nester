# Instructions

- Following Playwright test failed.
- Explain why, be concise, respect Playwright best practices.
- Provide a snippet of code with the fix, if possible.

# Test info

- Name: smoke.spec.ts >> smoke: full-stack happy path
- Location: tests\smoke.spec.ts:41:5

# Error details

```
Error: page.goto: net::ERR_CONNECTION_REFUSED at http://localhost:3001/
Call log:
  - navigating to "http://localhost:3001/", waiting until "load"

```

# Test source

```ts
  1   | import { test, expect, type Page } from "@playwright/test";
  2   | import { generateSmokeTestResult, type SmokeStep } from "./smoke/ci-helpers/smoke-result-writer";
  3   | import { createTestAccount } from "./smoke/helpers/account";
  4   | import { connectTestWallet } from "./smoke/helpers/wallet-harness";
  5   | import { performDeposit } from "./smoke/helpers/deposit-flow";
  6   | import { waitForBalanceUpdate } from "./smoke/helpers/balance-monitor";
  7   | import { performWithdraw } from "./smoke/helpers/withdraw-flow";
  8   | import { verifySettlement } from "./smoke/helpers/settlement-monitor";
  9   | 
  10  | /**
  11  |  * Full-stack smoke test for Nester dApp deployment gating.
  12  |  *
  13  |  * This test implements the canonical smoke scenario:
  14  |  * 1. Load homepage — Nester wallet-first entry point
  15  |  * 2. Connect wallet — link Stellar wallet (user registration is implicit)
  16  |  * 3. Deposit — perform deposit transaction with testnet funds
  17  |  * 4. Balance updates — verify UI and API reflect deposited amount
  18  |  * 5. Withdraw — initiate withdrawal transaction
  19  |  * 6. Settle — verify final balances reconcile on-chain and UI
  20  |  *
  21  |  * Each step emits machine-parsable status lines and produces smoke-result.json artifact.
  22  |  * Total runtime must stay under 10 minutes.
  23  |  *
  24  |  * GitHub Issue: #1116 - test(repo): full-stack smoke test gating every deploy
  25  |  */
  26  | 
  27  | const SMOKE_TEST_EMAIL = `smoke-${Date.now()}@test.nester.dev`;
  28  | const SMOKE_TEST_PASSWORD = "SmokeTest@12345";
  29  | const DEPOSIT_AMOUNT = 50; // USDC on testnet
  30  | const WITHDRAW_SHARES_RATIO = 0.8; // Withdraw 80% of deposited shares
  31  | const STEP_TIMEOUT_MS = 90_000; // 90 second max per step
  32  | 
  33  | const steps: SmokeStep[] = [];
  34  | 
  35  | function addStep(step: SmokeStep) {
  36  |   steps.push(step);
  37  |   const status = step.status === "PASS" ? "✓" : "✗";
  38  |   console.log(`STEP:${step.name}:${step.status}:${step.message || ""}`);
  39  | }
  40  | 
  41  | test("smoke: full-stack happy path", async ({ page, context }) => {
  42  |   const testStartTime = Date.now();
  43  | 
  44  |   try {
  45  |     // ────────────────────────────────────────────────────────────────
  46  |     // STEP 1: Load homepage — Nester wallet-first entry point
  47  |     // ────────────────────────────────────────────────────────────────
  48  |     let stepStart = Date.now();
  49  |     try {
> 50  |       await page.goto("/");
      |                  ^ Error: page.goto: net::ERR_CONNECTION_REFUSED at http://localhost:3001/
  51  |       await expect(page).toHaveTitle(/nester/i);
  52  | 
  53  |       addStep({
  54  |         name: "homepage-load",
  55  |         status: "PASS",
  56  |         message: "Homepage loaded successfully",
  57  |         durationMs: Date.now() - stepStart,
  58  |       });
  59  |     } catch (err) {
  60  |       const message = err instanceof Error ? err.message : String(err);
  61  |       addStep({
  62  |         name: "homepage-load",
  63  |         status: "FAIL",
  64  |         message: `Failed to load homepage: ${message}`,
  65  |         durationMs: Date.now() - stepStart,
  66  |       });
  67  |       throw err;
  68  |     }
  69  | 
  70  |     // ────────────────────────────────────────────────────────────────
  71  |     // STEP 2: Connect wallet — User registration is implicit in wallet connection
  72  |     // ────────────────────────────────────────────────────────────────
  73  |     stepStart = Date.now();
  74  |     try {
  75  |       const walletInfo = await connectTestWallet(page, context);
  76  | 
  77  |       addStep({
  78  |         name: "connect-wallet",
  79  |         status: "PASS",
  80  |         message: `Wallet connected: ${walletInfo.address.slice(0, 6)}...${walletInfo.address.slice(-6)}`,
  81  |         durationMs: Date.now() - stepStart,
  82  |       });
  83  |     } catch (err) {
  84  |       const message = err instanceof Error ? err.message : String(err);
  85  |       addStep({
  86  |         name: "connect-wallet",
  87  |         status: "FAIL",
  88  |         message: `Wallet connection failed: ${message}`,
  89  |         durationMs: Date.now() - stepStart,
  90  |       });
  91  |       throw err;
  92  |     }
  93  | 
  94  |     // ────────────────────────────────────────────────────────────────
  95  |     // STEP 3: Deposit — Execute deposit transaction with testnet funds
  96  |     // ────────────────────────────────────────────────────────────────
  97  |     stepStart = Date.now();
  98  |     let depositTxHash: string | undefined;
  99  |     try {
  100 |       const depositResult = await performDeposit(page, {
  101 |         amount: DEPOSIT_AMOUNT,
  102 |         walletAddress: page.url(), // Will be extracted from UI
  103 |         timeout: STEP_TIMEOUT_MS,
  104 |       });
  105 | 
  106 |       depositTxHash = depositResult.txHash;
  107 | 
  108 |       addStep({
  109 |         name: "deposit",
  110 |         status: "PASS",
  111 |         message: `Deposited ${DEPOSIT_AMOUNT} USDC`,
  112 |         durationMs: Date.now() - stepStart,
  113 |         txHash: depositTxHash,
  114 |       });
  115 |     } catch (err) {
  116 |       const message = err instanceof Error ? err.message : String(err);
  117 |       addStep({
  118 |         name: "deposit",
  119 |         status: "FAIL",
  120 |         message: `Deposit failed: ${message}`,
  121 |         durationMs: Date.now() - stepStart,
  122 |       });
  123 |       throw err;
  124 |     }
  125 | 
  126 |     // ────────────────────────────────────────────────────────────────
  127 |     // STEP 4: Balance updates — Verify UI/API reflect deposit
  128 |     // ────────────────────────────────────────────────────────────────
  129 |     stepStart = Date.now();
  130 |     try {
  131 |       await waitForBalanceUpdate(page, {
  132 |         expectedMinimumBalance: DEPOSIT_AMOUNT * 0.99, // Allow for small fees
  133 |         timeout: STEP_TIMEOUT_MS,
  134 |       });
  135 | 
  136 |       addStep({
  137 |         name: "balance-update",
  138 |         status: "PASS",
  139 |         message: `Balance updated to reflect ${DEPOSIT_AMOUNT} USDC deposit`,
  140 |         durationMs: Date.now() - stepStart,
  141 |       });
  142 |     } catch (err) {
  143 |       const message = err instanceof Error ? err.message : String(err);
  144 |       addStep({
  145 |         name: "balance-update",
  146 |         status: "FAIL",
  147 |         message: `Balance update failed: ${message}`,
  148 |         durationMs: Date.now() - stepStart,
  149 |       });
  150 |       throw err;
```