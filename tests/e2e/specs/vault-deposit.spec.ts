import { test, expect, devices } from "@playwright/test";
import { injectWalletSession, TEST_ADDRESS } from "../fixtures/test-wallet";
import { VaultsPage } from "../pages/vaults.page";
import { DashboardPage } from "../pages/dashboard.page";

/**
 * Vault Deposit Flow
 *
 * Happy path:
 *   vault list renders → click Deposit → modal opens → enter amount →
 *   confirm → success banner → dashboard balance updated → history entry
 *
 * Also covers:
 *   - APY and strategy info visible on vault cards
 *   - Fee breakdown in the deposit modal
 *   - Mock-signature flow (no real wallet extension needed)
 *   - Mobile viewport (iPhone 13)
 */
test.describe("Vault Deposit Flow", () => {
    test.beforeEach(async ({ page }) => {
        await injectWalletSession(page, TEST_ADDRESS);
    });

    test("vault list page renders all four vaults with APY labels", async ({ page }) => {
        const vaultsPage = new VaultsPage(page);
        await vaultsPage.goto();
        await vaultsPage.waitForLoad();

        const expectedVaults = [
            { name: "Conservative", apy: /6-8%/ },
            { name: "Balanced",     apy: /8-11%/ },
            { name: "Growth",       apy: /11-15%/ },
            { name: "DeFi500 Index",apy: /Variable/ },
        ];

        for (const vault of expectedVaults) {
            const card = page.locator("h3", { hasText: vault.name });
            await expect(card).toBeVisible();

            // APY is in the card — check it's present
            const cardContainer = card.locator("../../..");
            await expect(cardContainer.locator("p.font-heading").first()).toContainText(vault.apy);
        }
    });

    test("vault cards show lock period and early-exit penalty", async ({ page }) => {
        const vaultsPage = new VaultsPage(page);
        await vaultsPage.goto();
        await vaultsPage.waitForLoad();

        // Conservative vault: 30 days lock, 0.1% penalty
        const conservativeCard = page.locator("h3", { hasText: "Conservative" }).locator("..").locator("..");
        await expect(conservativeCard.getByText("30 days")).toBeVisible();
        await expect(conservativeCard.getByText("0.1%")).toBeVisible();
    });

    test("clicking Deposit opens the deposit modal for the correct vault", async ({ page }) => {
        const vaultsPage = new VaultsPage(page);
        await vaultsPage.goto();
        await vaultsPage.waitForLoad();

        await vaultsPage.openDepositModal("Conservative");

        // Modal title includes the vault name
        await expect(vaultsPage.depositModalTitle).toContainText("Conservative");

        // Amount input and USDC label visible
        await expect(vaultsPage.depositAmountInput).toBeVisible();
        await expect(page.getByText("USDC").first()).toBeVisible();
    });

    test("fee breakdown is visible in the deposit modal", async ({ page }) => {
        const vaultsPage = new VaultsPage(page);
        await vaultsPage.goto();
        await vaultsPage.waitForLoad();

        await vaultsPage.openDepositModal("Balanced");

        // Fee breakdown rows
        await expect(page.getByText("Estimated annual yield")).toBeVisible();
        await expect(page.getByText("nVault shares to receive")).toBeVisible();
        await expect(page.getByText("Lock period")).toBeVisible();
        await expect(page.getByText("Management fee (annual)")).toBeVisible();
        await expect(page.getByText("Performance fee (on yield)")).toBeVisible();
    });

    test("full deposit flow: enter amount → confirm → success", async ({ page }) => {
        const vaultsPage = new VaultsPage(page);
        await vaultsPage.goto();
        await vaultsPage.waitForLoad();

        await vaultsPage.openDepositModal("Conservative");

        // Enter a deposit amount
        await vaultsPage.depositAmountInput.fill("1000");

        // Confirm Deposit button should become enabled
        await expect(vaultsPage.confirmDepositButton).toBeEnabled();

        // Trigger the mock sign + submit flow
        await vaultsPage.confirmDepositButton.click();

        // Success banner should appear (mock submission takes ~1–2 s)
        await vaultsPage.waitForDepositSuccess();

        // "View on Explorer" link should be present in the success state
        await expect(vaultsPage.viewOnExplorerLink).toBeVisible();

        // Signature method badge
        await expect(page.getByText(/Mock signature used|Wallet signature captured/i)).toBeVisible();
    });

    test("after deposit, dashboard balance decreases and position appears", async ({ page }) => {
        // Step 1: Deposit 1000 USDC into Conservative vault
        const vaultsPage = new VaultsPage(page);
        await vaultsPage.goto();
        await vaultsPage.waitForLoad();
        await vaultsPage.openDepositModal("Conservative");
        await vaultsPage.depositAmountInput.fill("1000");
        await vaultsPage.confirmDepositButton.click();
        await vaultsPage.waitForDepositSuccess();
        // Close modal
        await page.getByRole("button", { name: /Close/i }).click();

        // Step 2: Navigate to dashboard
        const dashboardPage = new DashboardPage(page);
        await dashboardPage.goto();
        await dashboardPage.waitForLoad();

        // USDC balance should now be 9,000
        const usdcCard = page.locator("p", { hasText: "Wallet USDC Balance" }).locator("..");
        await expect(usdcCard.locator("p.font-heading")).toContainText("9,000.00");

        // Active Vaults should be 1
        const activeVaultsCard = page.locator("p", { hasText: "Active Vaults" }).locator("..");
        await expect(activeVaultsCard.locator("p.font-heading")).toContainText("1");
    });

    test("after deposit, transaction appears in Recent Activity", async ({ page }) => {
        const vaultsPage = new VaultsPage(page);
        await vaultsPage.goto();
        await vaultsPage.waitForLoad();
        await vaultsPage.openDepositModal("Growth");
        await vaultsPage.depositAmountInput.fill("500");
        await vaultsPage.confirmDepositButton.click();
        await vaultsPage.waitForDepositSuccess();
        await page.getByRole("button", { name: /Close/i }).click();

        // Navigate to dashboard
        await page.goto("/dashboard");
        await expect(page.getByText("Welcome back")).toBeVisible();

        // Recent Activity should show a Deposit entry for Growth vault
        await expect(page.getByText(/Deposit.*Growth/i)).toBeVisible();
    });

    test("Max button fills the deposit input with full USDC balance", async ({ page }) => {
        const vaultsPage = new VaultsPage(page);
        await vaultsPage.goto();
        await vaultsPage.waitForLoad();
        await vaultsPage.openDepositModal("Conservative");

        await vaultsPage.maxButton.click();

        // The available balance is 10,000 USDC
        const inputValue = await vaultsPage.depositAmountInput.inputValue();
        expect(parseFloat(inputValue)).toBeCloseTo(10000, 0);
    });

    test("Confirm Deposit is disabled when amount is empty", async ({ page }) => {
        const vaultsPage = new VaultsPage(page);
        await vaultsPage.goto();
        await vaultsPage.waitForLoad();
        await vaultsPage.openDepositModal("Balanced");

        // No amount entered — button must be disabled
        await expect(vaultsPage.confirmDepositButton).toBeDisabled();
    });

    test("Cancel button closes the deposit modal without depositing", async ({ page }) => {
        const vaultsPage = new VaultsPage(page);
        await vaultsPage.goto();
        await vaultsPage.waitForLoad();
        await vaultsPage.openDepositModal("Conservative");
        await vaultsPage.depositAmountInput.fill("500");

        await page.getByRole("button", { name: /Cancel/i }).first().click();

        // Modal should be gone
        await expect(vaultsPage.depositModalTitle).not.toBeVisible();

        // USDC balance unchanged
        await page.goto("/dashboard");
        await expect(page.getByText("Welcome back")).toBeVisible();
        const usdcCard = page.locator("p", { hasText: "Wallet USDC Balance" }).locator("..");
        await expect(usdcCard.locator("p.font-heading")).toContainText("10,000.00");
    });

    test("transaction flow steps highlight in sequence during deposit", async ({ page }) => {
        const vaultsPage = new VaultsPage(page);
        await vaultsPage.goto();
        await vaultsPage.waitForLoad();
        await vaultsPage.openDepositModal("Conservative");
        await vaultsPage.depositAmountInput.fill("200");

        // Before clicking Confirm, all steps show the clock icon (pending)
        await expect(page.getByText("Prepare contract call")).toBeVisible();
        await expect(page.getByText("Request wallet signature")).toBeVisible();
        await expect(page.getByText("Submit and confirm")).toBeVisible();

        await vaultsPage.confirmDepositButton.click();

        // After success, the steps show the check icon (done state — green background)
        await vaultsPage.waitForDepositSuccess();

        // All three steps should be in done state (emerald background class)
        const doneSteps = page.locator("div.bg-emerald-50");
        await expect(doneSteps).toHaveCount(3, { timeout: 10_000 });
    });
});

/**
 * Mobile viewport: deposit flow on iPhone 13
 * Playwright config restricts the "mobile" project to this file.
 */
test.describe("Vault Deposit Flow (mobile viewport)", () => {
    test.use({ ...devices["iPhone 13"] });

    test("deposit modal is usable on mobile screen", async ({ page }) => {
        await injectWalletSession(page, TEST_ADDRESS);

        const vaultsPage = new VaultsPage(page);
        await vaultsPage.goto();
        await vaultsPage.waitForLoad();

        // Vault cards should be in single-column layout on mobile
        const vaultCards = page.locator("h3", { hasText: "Conservative" });
        await expect(vaultCards).toBeVisible();

        await vaultsPage.openDepositModal("Conservative");

        // Modal input must be scrollable / reachable
        await vaultsPage.depositAmountInput.fill("250");
        await expect(vaultsPage.confirmDepositButton).toBeEnabled();
        await vaultsPage.confirmDepositButton.click();
        await vaultsPage.waitForDepositSuccess();
    });
});
