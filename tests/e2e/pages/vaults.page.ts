import { type Page, type Locator, expect } from "@playwright/test";

/**
 * Page object for /dashboard/vaults
 *
 * The vaults page lists all vault definitions (Conservative, Balanced, Growth,
 * DeFi500 Index). Each card shows APY, risk level, lock period, early-exit
 * penalty, and a "Deposit" button. Clicking Deposit opens the DepositModal.
 */
export class VaultsPage {
    readonly page: Page;

    // Header
    readonly heading: Locator;

    // Vault cards grid
    readonly vaultCards: Locator;
    readonly conservativeCard: Locator;
    readonly balancedCard: Locator;
    readonly growthCard: Locator;
    readonly defi500Card: Locator;

    // Deposit modal
    readonly depositModal: Locator;
    readonly depositModalTitle: Locator;
    readonly depositAmountInput: Locator;
    readonly maxButton: Locator;
    readonly confirmDepositButton: Locator;
    readonly cancelButton: Locator;
    readonly depositSuccessMessage: Locator;
    readonly depositErrorMessage: Locator;
    readonly viewOnExplorerLink: Locator;

    // Transaction flow steps
    readonly step_prepareContractCall: Locator;
    readonly step_requestSignature: Locator;
    readonly step_submitAndConfirm: Locator;

    constructor(page: Page) {
        this.page = page;

        this.heading = page.getByRole("heading", { name: /Optimize your Yield/i });

        // Each vault card has the vault name as a heading
        this.vaultCards = page.locator(".grid > div").filter({ has: page.locator("button", { hasText: /Deposit/i }) });
        this.conservativeCard = page.locator("h3", { hasText: "Conservative" }).locator("..").locator("..");
        this.balancedCard = page.locator("h3", { hasText: "Balanced" }).locator("..").locator("..");
        this.growthCard = page.locator("h3", { hasText: "Growth" }).locator("..").locator("..");
        this.defi500Card = page.locator("h3", { hasText: "DeFi500 Index" }).locator("..").locator("..");

        // Deposit modal — identified by the "Vault Action" mono label
        this.depositModal = page.locator("div").filter({ hasText: /^Vault Action$/ }).locator("..").locator("..");
        this.depositModalTitle = page.locator("h2").filter({ hasText: /Deposit into/ });
        this.depositAmountInput = page.locator('input[placeholder="0.00"]').first();
        this.maxButton = page.getByRole("button", { name: "Max" }).first();
        this.confirmDepositButton = page.getByRole("button", { name: /Confirm Deposit/i });
        this.cancelButton = page.getByRole("button", { name: /Cancel/i }).first();
        this.depositSuccessMessage = page.getByText("Deposit confirmed");
        this.depositErrorMessage = page.locator("div").filter({ hasText: /Deposit failed/ });
        this.viewOnExplorerLink = page.getByRole("link", { name: /View on Explorer/i });

        this.step_prepareContractCall = page.locator("span", { hasText: "Prepare contract call" });
        this.step_requestSignature   = page.locator("span", { hasText: "Request wallet signature" });
        this.step_submitAndConfirm   = page.locator("span", { hasText: "Submit and confirm" });
    }

    async goto(): Promise<void> {
        await this.page.goto("/dashboard/vaults");
    }

    async waitForLoad(): Promise<void> {
        await expect(this.heading).toBeVisible();
        // Wait for at least one vault card to render
        await expect(this.vaultCards.first()).toBeVisible();
    }

    /** Opens the deposit modal for the vault matching the given name */
    async openDepositModal(vaultName: "Conservative" | "Balanced" | "Growth" | "DeFi500 Index"): Promise<void> {
        const card = this.page.locator("h3", { hasText: vaultName }).locator("..").locator("..");
        await card.getByRole("button", { name: /Deposit/i }).click();
        await expect(this.depositModalTitle).toBeVisible();
    }

    /** Fills the deposit amount and submits */
    async deposit(amount: string): Promise<void> {
        await this.depositAmountInput.fill(amount);
        await expect(this.confirmDepositButton).toBeEnabled();
        await this.confirmDepositButton.click();
    }

    /** Waits for the deposit success banner to appear */
    async waitForDepositSuccess(): Promise<void> {
        await expect(this.depositSuccessMessage).toBeVisible({ timeout: 15_000 });
    }

    /** Returns the APY label shown on a vault card */
    async getVaultAPY(vaultName: string): Promise<string> {
        const card = this.page.locator("h3", { hasText: vaultName }).locator("../../../..");
        return (await card.locator("p.font-heading").first().textContent()) ?? "";
    }

    /** Returns the lock period shown in a vault card's details table */
    async getVaultLockDays(vaultName: string): Promise<string> {
        const card = this.page.locator("h3", { hasText: vaultName }).locator("..").locator("..");
        const lockRow = card.locator("div").filter({ hasText: /^Lock period$/ }).locator("..");
        return (await lockRow.locator("span.font-medium").textContent()) ?? "";
    }

    /** Closes the deposit modal via the cancel / close button */
    async closeModal(): Promise<void> {
        const closeBtn = this.page.getByRole("button", { name: /Close|Cancel/i }).first();
        await closeBtn.click();
        await expect(this.depositModalTitle).not.toBeVisible();
    }
}
