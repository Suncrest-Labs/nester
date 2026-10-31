import { test, expect } from "@playwright/test";
import { mockFreighter, TESTNET_PASSPHRASE, MAINNET_PASSPHRASE } from "./helpers/mock-freighter";

const CONTRACT_ID = `C${"A".repeat(55)}`;

function envelope(data: unknown) {
  return { status: 200, contentType: "application/json", body: JSON.stringify({ success: true, data }) };
}

async function openDepositModal(page: import("@playwright/test").Page, walletPassphrase: string) {
  await mockFreighter(page, { walletPassphrase, dappNetwork: "mainnet" });
  await page.route(/\/accounts\/G[A-Z0-9]{55}$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        balances: [
          { asset_type: "native", balance: "100.0000000" },
          { asset_type: "credit_alphanum4", asset_code: "USDC", balance: "100.0000000" },
        ],
      }),
    })
  );
  await page.route("**/api/v1/**", async (route) => {
    const { pathname } = new URL(route.request().url());
    if (pathname.endsWith("/vaults/vault-1")) {
      return route.fulfill(
        envelope({
          id: "vault-1",
          user_id: "user-1",
          contract_address: CONTRACT_ID,
          total_deposited: "0",
          current_balance: "0",
          currency: "USDC",
          status: "active",
          yield_earned: "0",
          fees_paid: "0",
          created_at: "2026-01-01T00:00:00Z",
          updated_at: "2026-01-01T00:00:00Z",
        })
      );
    }
    return route.fulfill({
      status: 404,
      contentType: "application/json",
      body: JSON.stringify({ success: false, error: { code: "NOT_FOUND", message: "not mocked" } }),
    });
  });

  // Pages redirect home until the saved wallet session is restored, so land
  // on "/" first and navigate client-side once the wallet is connected.
  await page.goto("/");
  await page.waitForFunction(() => window.localStorage.getItem("@StellarWalletsKit/activeAddress"));
  await page.evaluate(() =>
    (window as unknown as { next: { router: { push: (href: string) => void } } }).next.router.push("/vaults/vault-1")
  );
  await page.getByRole("button", { name: /^Supply to/ }).click();
  await page.getByPlaceholder("0.00").fill("10");
}

test.describe("wallet network guard", () => {
  test.skip(
    true,
    "Drives the retired /vaults/:id deposit modal. Porting to the dashboard SavePanel needs Soroban RPC stubs (getAccount, simulateTransaction quote) and NEXT_PUBLIC_SAVINGS_CORE_CONTRACT_ID in the webServer env; the guard on the savings path is unit-tested in lib/__tests__/savings.test.ts"
  );
  test.setTimeout(120_000);

  test("blocks the deposit with a switch-network prompt when the wallet is on testnet and the dapp on mainnet", async ({
    page,
  }) => {
    await openDepositModal(page, TESTNET_PASSPHRASE);

    await page.getByRole("button", { name: "Confirm Deposit" }).dispatchEvent("click");

    await expect(page.getByText("Switch your wallet to Mainnet")).toBeVisible();
    expect(await page.evaluate(() => (window as unknown as { __freighterSignCalls: number }).__freighterSignCalls)).toBe(0);
  });

  test("does not show the prompt when the wallet is on the dapp network", async ({ page }) => {
    await openDepositModal(page, MAINNET_PASSPHRASE);

    await page.getByRole("button", { name: "Confirm Deposit" }).dispatchEvent("click");

    await expect(page.getByText("Switch your wallet to Mainnet")).toHaveCount(0);
  });
});
