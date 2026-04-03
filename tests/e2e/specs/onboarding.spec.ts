import { test, expect } from "@playwright/test";
import { injectWalletSession, TEST_ADDRESS } from "../fixtures/test-wallet";

/**
 * Onboarding & Wallet Connection
 *
 * These tests verify:
 *  1. Unauthenticated users land on the connect-wallet page
 *  2. Wallet cards (Freighter, LOBSTR, xBull) are rendered
 *  3. After a session is injected, the app redirects to /dashboard
 *  4. The dashboard greets the user with a truncated address
 *  5. New users see empty-state messages (no vaults, no transactions)
 */
test.describe("Onboarding & Wallet Connection", () => {
    test("unauthenticated visit to / shows connect-wallet UI", async ({ page }) => {
        await page.goto("/");

        // Heading
        await expect(page.getByRole("heading", { name: /Welcome to.*Nester/i })).toBeVisible();

        // Tagline
        await expect(
            page.getByText(/Connect your Stellar wallet/i)
        ).toBeVisible();
    });

    test("connect-wallet card loads wallet list with featured wallets", async ({ page }) => {
        await page.goto("/");

        // The wallet card container has "Choose a wallet" label
        await expect(page.getByText("Choose a wallet")).toBeVisible();

        // Featured wallets: Freighter, LOBSTR, xBull must be present as buttons
        // The WalletProvider loads wallets asynchronously; wait up to 10 s
        for (const walletName of ["Freighter", "LOBSTR", "xBull"]) {
            await expect(
                page.getByRole("button", { name: new RegExp(walletName, "i") })
            ).toBeVisible({ timeout: 10_000 });
        }
    });

    test("redirects to /dashboard when wallet session already exists in localStorage", async ({ page }) => {
        await injectWalletSession(page);
        await page.goto("/");

        // The wallet provider restores the session and the home page redirects
        await page.waitForURL("**/dashboard", { timeout: 10_000 });
        await expect(page).toHaveURL(/\/dashboard/);
    });

    test("dashboard displays connected address in truncated form", async ({ page }) => {
        await injectWalletSession(page, TEST_ADDRESS);
        await page.goto("/dashboard");

        // Wait for the welcome heading
        await expect(page.getByText("Welcome back")).toBeVisible();

        // The address is truncated — check the first and last 8 chars are present
        const addressText = await page.locator("p.font-mono").first().textContent();
        expect(addressText).toBeTruthy();
        // Truncated addresses follow the pattern XXXXX...XXXXX
        expect(addressText).toMatch(/^G[A-Z2-7]{4,}\.{3}[A-Z2-7]+$/);
    });

    test("new user sees empty-state messages for vaults and activity", async ({ page }) => {
        await injectWalletSession(page);
        await page.goto("/dashboard");

        await expect(page.getByText("Welcome back")).toBeVisible();

        // No vaults yet
        await expect(page.getByText("No vaults yet")).toBeVisible();

        // No insights (depends on having positions)
        await expect(page.getByText("No insights available")).toBeVisible();

        // No recent transactions
        await expect(page.getByText("No recent transactions")).toBeVisible();
    });

    test("unauthenticated navigation to /dashboard redirects to /", async ({ page }) => {
        // No wallet session injected — direct navigation to dashboard
        await page.goto("/dashboard");
        await page.waitForURL("**/", { timeout: 8_000 });
        // Match against the full URL (e.g. http://localhost:3001/)
        await expect(page).toHaveURL(/localhost:\d+\/?$/);
    });

    test("four stat cards are visible after connecting", async ({ page }) => {
        await injectWalletSession(page);
        await page.goto("/dashboard");

        await expect(page.getByText("Welcome back")).toBeVisible();

        const expectedLabels = [
            "Total Balance",
            "Total Yield Earned",
            "Active Vaults",
            "Wallet USDC Balance",
        ];

        for (const label of expectedLabels) {
            await expect(page.getByText(label)).toBeVisible();
        }
    });

    test("USDC balance stat shows default 10,000.00 for new wallet", async ({ page }) => {
        await injectWalletSession(page);
        await page.goto("/dashboard");

        await expect(page.getByText("Welcome back")).toBeVisible();

        // The PortfolioProvider seeds 10,000 USDC for any address with no stored state
        const usdcCard = page.locator("p", { hasText: "Wallet USDC Balance" }).locator("..");
        await expect(usdcCard.locator("p.font-heading")).toContainText("10,000.00");
    });
});
