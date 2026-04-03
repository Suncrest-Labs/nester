import { test, expect } from "@playwright/test";
import { injectWalletSession, seedVaultPosition, TEST_ADDRESS } from "../fixtures/test-wallet";

/**
 * Vault Withdrawal Flow
 *
 * Tests cover:
 *  - Opening the withdrawal modal from the dashboard
 *  - Max button populates the full position value
 *  - Early withdrawal penalty is shown for immature positions
 *  - "Penalty free" label for matured positions
 *  - Full withdrawal removes the position from the dashboard
 *  - Partial withdrawal reduces position value but keeps the card
 *  - Wallet USDC balance increases after withdrawal
 *  - Transaction history records a Withdrawal entry
 */
test.describe("Vault Withdrawal Flow", () => {
    test.beforeEach(async ({ page }) => {
        // Inject wallet session first, then seed a position
        await injectWalletSession(page, TEST_ADDRESS);
        await seedVaultPosition(page, TEST_ADDRESS, {
            vaultId: "conservative",
            principal: 2000,
            depositedDaysAgo: 10, // 10 days in — not yet matured (30-day lock)
        });
    });

    test("dashboard shows existing vault position after seeding", async ({ page }) => {
        await page.goto("/dashboard");
        await expect(page.getByText("Welcome back")).toBeVisible();

        // Position card should be visible
        await expect(page.getByText("Conservative")).toBeVisible();
        await expect(page.getByText(/nVault shares/i)).toBeVisible();
    });

    test("withdrawal modal opens from the dashboard Withdraw button", async ({ page }) => {
        await page.goto("/dashboard");
        await expect(page.getByText("Welcome back")).toBeVisible();

        await page.getByRole("button", { name: /Withdraw/i }).first().click();

        // Modal title
        await expect(page.getByRole("heading", { name: /Withdraw from/i })).toBeVisible();
    });

    test("early withdrawal: penalty warning is shown for immature position", async ({ page }) => {
        await page.goto("/dashboard");
        await expect(page.getByText("Welcome back")).toBeVisible();

        await page.getByRole("button", { name: /Withdraw/i }).first().click();

        // Penalty badge visible (amber background)
        await expect(page.getByText(/% early exit/i)).toBeVisible();

        // Days remaining text
        await expect(page.getByText(/days remaining/i)).toBeVisible();
    });

    test("withdrawal modal shows current value and yield earned", async ({ page }) => {
        await page.goto("/dashboard");
        await page.getByRole("button", { name: /Withdraw/i }).first().click();

        await expect(page.getByText(/Current value/i)).toBeVisible();
        await expect(page.getByText(/Yield earned/i)).toBeVisible();
        await expect(page.getByText(/Shares burned/i)).toBeVisible();
    });

    test("Max button fills the withdrawal input with full position value", async ({ page }) => {
        await page.goto("/dashboard");
        await page.getByRole("button", { name: /Withdraw/i }).first().click();

        const maxBtn = page.getByRole("button", { name: "Max" }).first();
        await maxBtn.click();

        const inputValue = await page.locator('input[placeholder="0.00"]').inputValue();
        expect(parseFloat(inputValue)).toBeGreaterThan(0);
    });

    test("Confirm Withdrawal is disabled when amount is empty", async ({ page }) => {
        await page.goto("/dashboard");
        await page.getByRole("button", { name: /Withdraw/i }).first().click();

        await expect(
            page.getByRole("button", { name: /Confirm Withdrawal/i })
        ).toBeDisabled();
    });

    test("full withdrawal flow succeeds and shows receipt", async ({ page }) => {
        await page.goto("/dashboard");
        await page.getByRole("button", { name: /Withdraw/i }).first().click();

        // Fill in a withdrawal amount
        const input = page.locator('input[placeholder="0.00"]');
        await input.fill("500");

        const confirmBtn = page.getByRole("button", { name: /Confirm Withdrawal/i });
        await expect(confirmBtn).toBeEnabled();
        await confirmBtn.click();

        // Success banner
        await expect(page.getByText("Withdrawal confirmed")).toBeVisible({ timeout: 15_000 });

        // Receipt shows net amount and penalty info
        await expect(page.getByText(/USDC is on its way back/i)).toBeVisible();
        await expect(page.getByText(/Penalty applied:/i)).toBeVisible();

        // Explorer link present
        await expect(page.getByRole("link", { name: /View on Explorer/i })).toBeVisible();
    });

    test("after successful withdrawal, USDC balance increases", async ({ page }) => {
        await page.goto("/dashboard");

        // Record the initial USDC balance
        const usdcCard = page.locator("p", { hasText: "Wallet USDC Balance" }).locator("..");
        const beforeText = await usdcCard.locator("p.font-heading").textContent() ?? "";
        const before = parseFloat(beforeText.replace(/[^0-9.]/g, ""));

        await page.getByRole("button", { name: /Withdraw/i }).first().click();
        await page.locator('input[placeholder="0.00"]').fill("500");
        await page.getByRole("button", { name: /Confirm Withdrawal/i }).click();
        await expect(page.getByText("Withdrawal confirmed")).toBeVisible({ timeout: 15_000 });
        await page.getByRole("button", { name: /Close/i }).click();

        // Balance should be higher now
        await expect(page.getByText("Welcome back")).toBeVisible();
        const afterText = await usdcCard.locator("p.font-heading").textContent() ?? "";
        const after = parseFloat(afterText.replace(/[^0-9.]/g, ""));
        expect(after).toBeGreaterThan(before);
    });

    test("withdrawal transaction appears in Recent Activity", async ({ page }) => {
        await page.goto("/dashboard");
        await page.getByRole("button", { name: /Withdraw/i }).first().click();
        await page.locator('input[placeholder="0.00"]').fill("300");
        await page.getByRole("button", { name: /Confirm Withdrawal/i }).click();
        await expect(page.getByText("Withdrawal confirmed")).toBeVisible({ timeout: 15_000 });
        await page.getByRole("button", { name: /Close/i }).click();

        await expect(page.getByText("Welcome back")).toBeVisible();

        // Check recent activity section — transaction type and vault name in the same <p>
        await expect(page.locator("p").filter({ hasText: /Withdrawal.*Conservative/i })).toBeVisible();
    });

    test("Cancel closes the withdrawal modal without withdrawing", async ({ page }) => {
        await page.goto("/dashboard");
        await page.getByRole("button", { name: /Withdraw/i }).first().click();
        await page.locator('input[placeholder="0.00"]').fill("100");

        await page.getByRole("button", { name: /Cancel/i }).first().click();

        await expect(page.getByRole("heading", { name: /Withdraw from/i })).not.toBeVisible();
    });
});

/**
 * Withdrawal flow for a MATURED position (no penalty)
 */
test.describe("Vault Withdrawal Flow (matured position)", () => {
    test.beforeEach(async ({ page }) => {
        await injectWalletSession(page, TEST_ADDRESS);
        await seedVaultPosition(page, TEST_ADDRESS, {
            vaultId: "conservative",
            principal: 1500,
            matured: true, // Deposited 35 days ago, lock is 30 days
        });
    });

    test("matured position shows Penalty free badge", async ({ page }) => {
        await page.goto("/dashboard");
        await expect(page.getByText("Welcome back")).toBeVisible();

        await page.getByRole("button", { name: /Withdraw/i }).first().click();

        // "Matured - no penalty applies" text
        await expect(page.getByText(/Matured.*no penalty applies/i)).toBeVisible();

        // Green "Penalty free" badge — exact:true to avoid matching the
        // dashboard card text "Matured and penalty free" (substring) which is
        // also visible in the DOM behind the open modal.
        await expect(page.getByText("Penalty free", { exact: true })).toBeVisible();
    });

    test("full withdrawal from matured position removes position card", async ({ page }) => {
        await page.goto("/dashboard");
        await page.getByRole("button", { name: /Withdraw/i }).first().click();

        // Withdraw the full position — wait for Max to populate the input
        // before clicking Confirm, otherwise the button stays disabled.
        await page.getByRole("button", { name: "Max" }).first().click();
        await expect(page.getByRole("button", { name: /Confirm Withdrawal/i })).toBeEnabled();
        await page.getByRole("button", { name: /Confirm Withdrawal/i }).click();
        await expect(page.getByText("Withdrawal confirmed")).toBeVisible({ timeout: 15_000 });
        await page.getByRole("button", { name: /Close/i }).click();

        // Position card should be gone — empty state should show
        await expect(page.getByText("No vaults yet")).toBeVisible();
    });
});
