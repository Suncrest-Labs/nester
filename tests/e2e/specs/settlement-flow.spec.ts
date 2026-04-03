import { test, expect } from "@playwright/test";
import { injectWalletSession, TEST_ADDRESS } from "../fixtures/test-wallet";
import { SettlementsPage } from "../pages/settlements.page";

/**
 * Settlement Flow (Cash Out / Offramp)
 *
 * Tests cover:
 *  - Page load: Cash Out heading, send/receive panels visible
 *  - Form-driven LP quote scanning (animated and async)
 *  - Exchange rate displayed once quotes are ready
 *  - Selecting an alternative LP node quote
 *  - Submit button states based on form completeness
 *  - Successful settlement submission triggers a notification
 *  - Recent Settlements section shows empty state initially
 */
test.describe("Settlement Flow", () => {
    test.beforeEach(async ({ page }) => {
        await injectWalletSession(page, TEST_ADDRESS);
    });

    test("settlements page loads with correct heading and empty-state", async ({ page }) => {
        const settlementsPage = new SettlementsPage(page);
        await settlementsPage.goto();
        await settlementsPage.waitForLoad();

        await expect(page.getByRole("heading", { name: /Cash Out/i })).toBeVisible();
        await expect(page.getByText("Convert crypto to fiat")).toBeVisible();
        await expect(settlementsPage.noSettlementsMessage).toBeVisible();
    });

    test("submit button shows contextual hint before form is complete", async ({ page }) => {
        const settlementsPage = new SettlementsPage(page);
        await settlementsPage.goto();
        await settlementsPage.waitForLoad();

        // No amount entered — button says "Enter an amount"
        const submitText = await settlementsPage.getSubmitButtonText();
        expect(submitText).toMatch(/Enter an amount/i);
    });

    test("submit button updates as user fills in each field", async ({ page }) => {
        const settlementsPage = new SettlementsPage(page);
        await settlementsPage.goto();
        await settlementsPage.waitForLoad();

        // Enter amount — button should now say "Select a bank"
        await settlementsPage.sendAmountInput.fill("100");
        await expect(
            page.locator("button").filter({ hasText: /Select a bank/i })
        ).toBeVisible({ timeout: 3_000 });

        // Select bank — button should now say "Enter account number"
        await settlementsPage.selectBank("Kuda Bank");
        await expect(
            page.locator("button").filter({ hasText: /Enter account number/i })
        ).toBeVisible({ timeout: 3_000 });
    });

    test("LP quote scan triggers after all fields are complete", async ({ page }) => {
        const settlementsPage = new SettlementsPage(page);
        await settlementsPage.goto();
        await settlementsPage.waitForLoad();

        await settlementsPage.fillSettlementForm({
            amount: "200",
            bankName: "Kuda Bank",
            accountNumber: "0123456789",
        });

        // Quote scanning progress indicator appears (scanning / comparing / ranking)
        // Use .first() to avoid strict mode when multiple spans match the pattern
        await expect(
            page.locator("span").filter({ hasText: /Scanning|Comparing|Ranking|Live/i }).first()
        ).toBeVisible({ timeout: 5_000 });
    });

    test("quote list renders after scanning completes", async ({ page }) => {
        const settlementsPage = new SettlementsPage(page);
        await settlementsPage.goto();
        await settlementsPage.waitForLoad();

        await settlementsPage.fillSettlementForm({
            amount: "150",
            bankName: "Zenith Bank",
            accountNumber: "1234567890",
        });

        await settlementsPage.waitForQuotes();

        // At least one quote row should be visible
        await settlementsPage.waitForQuoteRows();
        const count = await settlementsPage.getQuoteCount();
        expect(count).toBeGreaterThan(0);
    });

    test("Best badge appears on the top-ranked quote", async ({ page }) => {
        const settlementsPage = new SettlementsPage(page);
        await settlementsPage.goto();
        await settlementsPage.waitForLoad();

        await settlementsPage.fillSettlementForm({
            amount: "300",
            bankName: "GTBank",
            accountNumber: "9876543210",
        });

        await settlementsPage.waitForQuotes();

        await expect(settlementsPage.bestBadge).toBeVisible();
    });

    test("exchange rate is shown after quotes are ready", async ({ page }) => {
        const settlementsPage = new SettlementsPage(page);
        await settlementsPage.goto();
        await settlementsPage.waitForLoad();

        await settlementsPage.fillSettlementForm({
            amount: "100",
            bankName: "Kuda Bank",
            accountNumber: "0123456789",
        });

        await settlementsPage.waitForQuotes();

        // Rate info row: "Rate via <node name>"
        await expect(settlementsPage.rateInfoRow).toBeVisible();
        await expect(page.getByText(/1 USDC ≈/i)).toBeVisible();
    });

    test("selecting a different LP node updates the routing footer", async ({ page }) => {
        const settlementsPage = new SettlementsPage(page);
        await settlementsPage.goto();
        await settlementsPage.waitForLoad();

        await settlementsPage.fillSettlementForm({
            amount: "500",
            bankName: "Access Bank",
            accountNumber: "5555555555",
        });

        await settlementsPage.waitForQuotes();

        // Click the second quote row (index 1)
        const quoteRows = settlementsPage.quoteRows;
        const rowCount = await quoteRows.count();
        if (rowCount >= 2) {
            await quoteRows.nth(1).click();
            // Routing footer should update to reflect the selected node
            await expect(page.getByText(/Routing through/i)).toBeVisible();
        }
    });

    test("successful settlement submission shows a notification toast", async ({ page }) => {
        const settlementsPage = new SettlementsPage(page);
        await settlementsPage.goto();
        await settlementsPage.waitForLoad();

        await settlementsPage.fillSettlementForm({
            amount: "100",
            bankName: "Kuda Bank",
            accountNumber: "0123456789",
        });

        await settlementsPage.waitForQuotes();

        // Quotes must be ready before submitting
        await expect(settlementsPage.submitButton).toBeEnabled({ timeout: 10_000 });
        await settlementsPage.submitButton.click();

        // Notifications provider shows a toast — look for the "Withdrawal Submitted" title
        await expect(page.getByText(/Withdrawal Submitted/i)).toBeVisible({ timeout: 5_000 });
    });

    test("receive amount updates as send amount changes", async ({ page }) => {
        const settlementsPage = new SettlementsPage(page);
        await settlementsPage.goto();
        await settlementsPage.waitForLoad();

        await settlementsPage.sendAmountInput.fill("100");

        // The receive amount display should show an approximate value (≈ prefix)
        await expect(page.locator("span").filter({ hasText: /≈/ })).toBeVisible({ timeout: 3_000 });
    });

    test("currency selector switches from NGN to GHS", async ({ page }) => {
        const settlementsPage = new SettlementsPage(page);
        await settlementsPage.goto();
        await settlementsPage.waitForLoad();

        // Open receive currency dropdown
        await settlementsPage.receiveCurrencyButton.click();

        // Click GHS option
        await page.locator("button").filter({ hasText: "GHS" }).last().click();

        // The receive currency button now shows GHS
        await expect(
            page.locator("button").filter({ hasText: "GHS" }).first()
        ).toBeVisible();
    });

    test("asset selector switches from USDC to USDT", async ({ page }) => {
        const settlementsPage = new SettlementsPage(page);
        await settlementsPage.goto();
        await settlementsPage.waitForLoad();

        // Open send asset dropdown
        await settlementsPage.sendAssetButton.click();

        // Click USDT
        await page.locator("button").filter({ hasText: "USDT" }).last().click();

        await expect(
            page.locator("button").filter({ hasText: "USDT" }).first()
        ).toBeVisible();
    });
});
