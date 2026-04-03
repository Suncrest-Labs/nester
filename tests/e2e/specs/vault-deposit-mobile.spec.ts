/**
 * Mobile viewport: deposit flow on iPhone 13
 *
 * test.use() must be top-level (not inside a describe block) when setting
 * defaultBrowserType or viewport device, because Playwright forces a new
 * worker for viewport changes and cannot do so inside a describe group.
 *
 * The "mobile" project in playwright.config.ts is scoped to this file via
 * testMatch: ["**\/vault-deposit-mobile.spec.ts"].
 */
import { test, expect, devices } from "@playwright/test";
import { injectWalletSession, TEST_ADDRESS } from "../fixtures/test-wallet";
import { VaultsPage } from "../pages/vaults.page";

test.use({ ...devices["iPhone 13"] });

test("deposit modal is usable on mobile screen (iPhone 13)", async ({ page }) => {
    await injectWalletSession(page, TEST_ADDRESS);

    const vaultsPage = new VaultsPage(page);
    await vaultsPage.goto();
    await vaultsPage.waitForLoad();

    // Vault cards are visible in single-column layout on mobile
    await expect(page.locator("h3", { hasText: "Conservative" })).toBeVisible();

    await vaultsPage.openDepositModal("Conservative");

    // Modal input must be reachable on a small screen
    await vaultsPage.depositAmountInput.fill("250");
    await expect(vaultsPage.confirmDepositButton).toBeEnabled();
    await vaultsPage.confirmDepositButton.click();
    await vaultsPage.waitForDepositSuccess();
});
