import { type Page, type Locator, expect } from "@playwright/test";

/**
 * Page object for /dashboard
 *
 * The dashboard shows four stat cards (Total Balance, Total Yield Earned,
 * Active Vaults, Wallet USDC Balance), a vault positions panel, a Prometheus
 * insights panel, and a Recent Activity panel.
 */
export class DashboardPage {
    readonly page: Page;

    // Navigation
    readonly navBar: Locator;
    readonly addDepositLink: Locator;

    // Stat cards
    readonly statCards: Locator;
    readonly totalBalanceCard: Locator;
    readonly totalYieldCard: Locator;
    readonly activeVaultsCard: Locator;
    readonly walletUSDCCard: Locator;

    // Vault positions panel
    readonly vaultPositionsPanel: Locator;
    readonly emptyVaultsMessage: Locator;
    readonly getStartedButton: Locator;
    readonly positionCards: Locator;

    // Prometheus insights panel
    readonly prometheusPanel: Locator;
    readonly noInsightsMessage: Locator;

    // Recent activity panel
    readonly recentActivityPanel: Locator;
    readonly noTransactionsMessage: Locator;
    readonly transactionRows: Locator;

    // Withdraw modal trigger (from position cards)
    readonly withdrawButtons: Locator;

    constructor(page: Page) {
        this.page = page;

        this.navBar = page.locator("nav").first();
        this.addDepositLink = page.getByRole("link", { name: /Add Deposit/i });

        // Stat cards — identified by their label text
        this.statCards = page.locator(".grid > div").filter({ hasText: /Total Balance|Total Yield|Active Vaults|Wallet USDC/i });
        this.totalBalanceCard = page.locator("div").filter({ hasText: /^Total Balance$/ }).locator("..");
        this.totalYieldCard = page.locator("p", { hasText: "Total Yield Earned" }).locator("..");
        this.activeVaultsCard = page.locator("p", { hasText: "Active Vaults" }).locator("..");
        this.walletUSDCCard = page.locator("p", { hasText: "Wallet USDC Balance" }).locator("..");

        // Vault positions
        this.vaultPositionsPanel = page.locator("h2", { hasText: "Your Vaults" }).locator("..").locator("..");
        this.emptyVaultsMessage = page.getByText("No vaults yet");
        this.getStartedButton = page.getByRole("button", { name: /Get Started/i });
        this.positionCards = page.locator("div").filter({ hasText: /nVault shares/ }).filter({ hasText: /Withdraw/ });
        this.withdrawButtons = page.getByRole("button", { name: /Withdraw/i });

        // Prometheus insights
        this.prometheusPanel = page.locator("h2").filter({ hasText: /Prometheus/ }).locator("..").locator("..");
        this.noInsightsMessage = page.getByText("No insights available");

        // Recent activity
        this.recentActivityPanel = page.locator("h2", { hasText: "Recent Activity" }).locator("..");
        this.noTransactionsMessage = page.getByText("No recent transactions");
        this.transactionRows = page.locator("div").filter({ hasText: /Deposit|Withdrawal|Yield Accrual/ }).filter({ hasText: /Confirmed|Pending|Failed/ });
    }

    async goto(): Promise<void> {
        await this.page.goto("/dashboard");
    }

    async waitForLoad(): Promise<void> {
        // The welcome heading is always present when connected
        await expect(this.page.getByText("Welcome back")).toBeVisible();
    }

    /** Returns the text content of the total-balance stat card value */
    async getTotalBalance(): Promise<string> {
        const card = this.page.locator("p", { hasText: "Total Balance" }).locator("..");
        return (await card.locator("p.font-heading").textContent()) ?? "";
    }

    /** Returns the text content of the wallet USDC balance stat card value */
    async getUSDCBalance(): Promise<string> {
        const card = this.page.locator("p", { hasText: "Wallet USDC Balance" }).locator("..");
        return (await card.locator("p.font-heading").textContent()) ?? "";
    }

    /** Returns the number of vault position cards currently rendered */
    async getPositionCount(): Promise<number> {
        return await this.positionCards.count();
    }

    /** Returns the number of recent transaction rows */
    async getTransactionCount(): Promise<number> {
        return await this.transactionRows.count();
    }

    /** Clicks the "Add Deposit" link to navigate to /dashboard/vaults */
    async clickAddDeposit(): Promise<void> {
        await this.addDepositLink.click();
    }

    /** Clicks the Withdraw button on the first vault position card */
    async clickWithdrawOnFirstPosition(): Promise<void> {
        await this.withdrawButtons.first().click();
    }

    /** Returns the truncated address shown in the welcome sub-heading */
    async getDisplayedAddress(): Promise<string> {
        return (await this.page.locator("p.font-mono").first().textContent()) ?? "";
    }
}
