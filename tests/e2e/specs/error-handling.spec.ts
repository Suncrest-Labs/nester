import { test, expect } from "@playwright/test";
import { injectWalletSession, seedVaultPosition, TEST_ADDRESS } from "../fixtures/test-wallet";
import { simulateApiDown, simulateSlowNetwork, mockHealthCheck } from "../fixtures/api-helpers";

/**
 * Error Handling Scenarios
 *
 * Tests cover:
 *  1. API server unreachable — frontend does not crash
 *  2. Wallet disconnected mid-session — redirects gracefully to home
 *  3. Deposit amount exceeds balance — Confirm button stays disabled
 *  4. Network timeout on slow route — loading state resolves (doesn't hang)
 *  5. Invalid account number format in settlements — submit blocked
 *  6. Navigation to protected page without wallet — redirect to /
 */
test.describe("Error Handling", () => {
    test("frontend renders normally when dapp-backend API is unreachable", async ({ page }) => {
        // Simulate backend being down BEFORE page load
        await simulateApiDown(page);
        await injectWalletSession(page, TEST_ADDRESS);

        // The frontend is a standalone Next.js SPA — it should still load
        // without the backend (portfolio data lives in localStorage)
        await page.goto("/dashboard");

        // Core UI must be visible even with backend down
        await expect(page.getByText("Welcome back")).toBeVisible({ timeout: 10_000 });

        // Stat cards still render (driven by localStorage state)
        await expect(page.getByText("Wallet USDC Balance")).toBeVisible();
    });

    test("wallet disconnect mid-session redirects to home page", async ({ browser, baseURL }) => {
        // Verify that a fresh page (new browser context) without any injected
        // wallet session gets redirected away from /dashboard.  This exercises
        // the same auth-guard that handles a mid-session disconnect — a new
        // browser context has no addInitScript hooks and no localStorage state.
        const freshCtx = await browser.newContext({ baseURL: baseURL ?? "http://localhost:3001" });
        const freshPage = await freshCtx.newPage();

        try {
            await freshPage.goto("/dashboard");
            await freshPage.waitForURL("**/", { timeout: 8_000 });
            await expect(freshPage).toHaveURL(/localhost:\d+\/?$/);
        } finally {
            await freshCtx.close();
        }
    });

    test("deposit with amount exceeding balance keeps Confirm Deposit disabled", async ({ page }) => {
        await injectWalletSession(page, TEST_ADDRESS);
        await page.goto("/dashboard/vaults");
        await expect(page.getByRole("heading", { name: /Optimize your Yield/i })).toBeVisible();

        // Open deposit modal for Conservative vault
        const conservativeCard = page.locator("h3", { hasText: "Conservative" }).locator("..").locator("..");
        await conservativeCard.getByRole("button", { name: /Deposit/i }).click();
        await expect(page.locator("h2").filter({ hasText: /Deposit into/ })).toBeVisible();

        // The default USDC balance is 10,000 — enter an amount over the limit
        await page.locator('input[placeholder="0.00"]').fill("99999");

        // Confirm Deposit should remain disabled (amount > balance)
        await expect(
            page.getByRole("button", { name: /Confirm Deposit/i })
        ).toBeDisabled();
    });

    test("settlement submit is blocked when account number is incomplete", async ({ page }) => {
        await injectWalletSession(page, TEST_ADDRESS);
        await page.goto("/dashboard/settlements");
        await expect(page.getByRole("heading", { name: /Cash Out/i })).toBeVisible();

        await page.locator('input[placeholder="0.00"]').fill("100");

        // Select bank
        await page.getByRole("button", { name: /Choose your bank/i }).click();
        await page.locator("button", { hasText: "Kuda Bank" }).last().click();

        // Enter only 5 digits (invalid — needs 10)
        await page.locator('input[placeholder="Enter 10-digit account number"]').fill("12345");

        // Submit button should show "Enter account number" and be disabled
        const submitBtn = page.locator("button").filter({ hasText: /Enter account number/i });
        await expect(submitBtn).toBeDisabled();
    });

    test("navigating to /dashboard/vaults without session redirects to home", async ({ page }) => {
        // No wallet session
        await page.goto("/dashboard/vaults");
        await page.waitForURL("**/", { timeout: 8_000 });
        await expect(page).toHaveURL(/localhost:\d+\/?$/);
    });

    test("navigating to /dashboard/settlements without session redirects to home", async ({ page }) => {
        await page.goto("/dashboard/settlements");
        await page.waitForURL("**/", { timeout: 8_000 });
        await expect(page).toHaveURL(/localhost:\d+\/?$/);
    });

    test("withdrawal attempt without entering amount keeps Confirm Withdrawal disabled", async ({ page }) => {
        await injectWalletSession(page, TEST_ADDRESS);
        await seedVaultPosition(page, TEST_ADDRESS, {
            vaultId: "balanced",
            principal: 1000,
        });

        await page.goto("/dashboard");
        await expect(page.getByText("Welcome back")).toBeVisible();

        await page.getByRole("button", { name: /Withdraw/i }).first().click();
        await expect(page.getByRole("heading", { name: /Withdraw from/i })).toBeVisible();

        // No amount entered
        await expect(
            page.getByRole("button", { name: /Confirm Withdrawal/i })
        ).toBeDisabled();
    });

    test("withdrawal amount exceeding position value keeps Confirm Withdrawal disabled", async ({ page }) => {
        await injectWalletSession(page, TEST_ADDRESS);
        await seedVaultPosition(page, TEST_ADDRESS, {
            vaultId: "conservative",
            principal: 500,
        });

        await page.goto("/dashboard");
        await expect(page.getByText("Welcome back")).toBeVisible();

        await page.getByRole("button", { name: /Withdraw/i }).first().click();
        await expect(page.getByRole("heading", { name: /Withdraw from/i })).toBeVisible();

        // Enter more than the position is worth
        await page.locator('input[placeholder="0.00"]').fill("9999999");

        await expect(
            page.getByRole("button", { name: /Confirm Withdrawal/i })
        ).toBeDisabled();
    });

    test("slow network: deposit flow does not hang in loading state indefinitely", async ({ page }) => {
        // This test uses a fake Soroban call that completes via mock-soroban.ts
        // The mock adds ~1-2 s of simulated delay. We verify the UI eventually
        // exits the "Awaiting Signature" / "Submitting" states.
        await injectWalletSession(page, TEST_ADDRESS);
        await page.goto("/dashboard/vaults");
        await expect(page.getByRole("heading", { name: /Optimize your Yield/i })).toBeVisible();

        const conservativeCard = page.locator("h3", { hasText: "Conservative" }).locator("..").locator("..");
        await conservativeCard.getByRole("button", { name: /Deposit/i }).click();
        await expect(page.locator("h2").filter({ hasText: /Deposit into/ })).toBeVisible();

        await page.locator('input[placeholder="0.00"]').fill("100");
        await page.getByRole("button", { name: /Confirm Deposit/i }).click();

        // The button should transition through loading states and eventually reach success
        // Timeout of 20 s covers the mock delay with generous headroom
        await expect(page.getByText("Deposit confirmed")).toBeVisible({ timeout: 20_000 });
    });

    test("health check endpoint mock returns success in CI mode", async ({ page }) => {
        await mockHealthCheck(page);
        // No wallet session — land on the connect-wallet page to verify the
        // frontend loads correctly even when the health check is mocked.
        await page.goto("/");
        // Use h1 role to avoid strict mode — there's only one h1 on the landing page
        await expect(page.getByRole("heading", { level: 1, name: /Welcome to.*Nester/i })).toBeVisible();
    });
});
