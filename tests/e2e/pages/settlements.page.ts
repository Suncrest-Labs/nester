import { type Page, type Locator, expect } from "@playwright/test";

/**
 * Page object for /dashboard/settlements ("Cash Out")
 *
 * The page has:
 *  - A send-amount input and asset selector
 *  - A receive-amount display and currency selector
 *  - Bank selector and account-number input
 *  - A real-time LP node quote list (scanned asynchronously)
 *  - A submit "Withdraw" button
 *  - A "Recent Settlements" history section
 */
export class SettlementsPage {
    readonly page: Page;

    // Header
    readonly heading: Locator;

    // Send panel
    readonly sendAmountInput: Locator;
    readonly sendAssetButton: Locator;

    // Receive panel
    readonly receiveCurrencyButton: Locator;

    // Bank details
    readonly bankSelectorButton: Locator;
    readonly accountNumberInput: Locator;

    // Quote list
    readonly quotesPanel: Locator;
    readonly bestBadge: Locator;
    readonly quoteRows: Locator;
    readonly liveIndicator: Locator;

    // Submit
    readonly submitButton: Locator;

    // Rate info row
    readonly rateInfoRow: Locator;

    // Recent settlements section
    readonly recentSettlementsPanel: Locator;
    readonly noSettlementsMessage: Locator;

    constructor(page: Page) {
        this.page = page;

        this.heading = page.getByRole("heading", { name: /Cash Out/i });

        this.sendAmountInput = page.locator('input[placeholder="0.00"]').first();
        this.sendAssetButton = page
            .locator("button")
            .filter({ hasText: /USDC|USDT|XLM/ })
            .first();

        this.receiveCurrencyButton = page
            .locator("button")
            .filter({ hasText: /NGN|GHS|KES/ })
            .first();

        this.bankSelectorButton = page.getByRole("button", { name: /Choose your bank/i }).or(
            page.locator("button").filter({ hasText: /Kuda Bank|Moniepoint|Access Bank|GTBank|First Bank|UBA|Zenith Bank|Opay/ })
        );

        this.accountNumberInput = page.locator('input[placeholder="Enter 10-digit account number"]');

        this.quotesPanel = page.locator("div").filter({ hasText: /Quotes/ }).first();
        this.bestBadge = page.locator("span").filter({ hasText: /^Best$/ });
        this.quoteRows = page.locator("button").filter({ hasText: /% fee/ });
        this.liveIndicator = page.getByText("Live", { exact: true }).first();

        this.submitButton = page.locator("button").filter({ hasText: /Withdraw|Enter an amount|Select a bank|Enter account number|Finding best rate/ });

        this.rateInfoRow = page.locator("span").filter({ hasText: /Rate via/ });

        this.recentSettlementsPanel = page.locator("h3", { hasText: "Recent Settlements" }).locator("..");
        this.noSettlementsMessage = page.getByText("No settlements yet");
    }

    async goto(): Promise<void> {
        await this.page.goto("/dashboard/settlements");
    }

    async waitForLoad(): Promise<void> {
        await expect(this.heading).toBeVisible();
        await expect(this.sendAmountInput).toBeVisible();
    }

    /** Selects a bank from the dropdown */
    async selectBank(bankName: string): Promise<void> {
        // Click the bank selector to open the dropdown
        const trigger = this.page
            .locator("button")
            .filter({ hasText: /Choose your bank/i })
            .or(this.page.locator("button").filter({ hasText: new RegExp(bankName, "i") }))
            .first();
        await trigger.click();
        // Click the bank option in the dropdown
        await this.page.locator("button", { hasText: bankName }).last().click();
    }

    /** Fills all required settlement fields */
    async fillSettlementForm(opts: {
        amount: string;
        bankName: string;
        accountNumber: string;
    }): Promise<void> {
        await this.sendAmountInput.fill(opts.amount);
        await this.selectBank(opts.bankName);
        await this.accountNumberInput.fill(opts.accountNumber);
    }

    /** Waits for the LP quote scan to complete and quotes to appear */
    async waitForQuotes(): Promise<void> {
        await expect(this.liveIndicator).toBeVisible({ timeout: 10_000 });
    }

    /** Waits for at least one quote row to be rendered */
    async waitForQuoteRows(): Promise<void> {
        await expect(this.quoteRows.first()).toBeVisible({ timeout: 10_000 });
    }

    /** Clicks the submit button — only enabled when quotes are ready */
    async submit(): Promise<void> {
        await expect(this.submitButton).toBeEnabled({ timeout: 10_000 });
        await this.submitButton.click();
    }

    /** Returns the text of the submit button (reflects current form state) */
    async getSubmitButtonText(): Promise<string> {
        return (await this.submitButton.textContent()) ?? "";
    }

    /** Returns the number of quote rows rendered */
    async getQuoteCount(): Promise<number> {
        return await this.quoteRows.count();
    }
}
