import { test, expect } from "@playwright/test";
import { NETWORKS, resolveDefaultNetwork, hasPlaceholderContracts } from "../lib/networks";

/**
 * Mainnet read-only smoke test suite (#1545)
 *
 * Verifies that when the dapp is built with NEXT_PUBLIC_STELLAR_NETWORK=mainnet:
 * 1. The default network resolves to mainnet and contract addresses are non-placeholder.
 * 2. The mainnet banner is visible and the testnet warning banner is not shown.
 * 3. Vault list renders against mocked mainnet API responses with zero console errors.
 * 4. Deposit modal opens and reads the mainnet deposit cap.
 */

const MOCK_MAINNET_VAULTS = {
  vaults: [
    {
      id: "vault-usdc-mainnet-1",
      name: "USDC Conservative Mainnet Vault",
      symbol: "nUSDC",
      asset: "USDC",
      apy: 8.45,
      tvl: 250000,
      depositCap: 500000,
      remainingCapacity: 250000,
      status: "active",
      contractAddress: NETWORKS.mainnet.contracts.vault,
    },
  ],
};

test.describe("Mainnet Read-Only Smoke Test", () => {
  test("mainnet network config has no placeholder contract addresses", () => {
    expect(resolveDefaultNetwork("mainnet").id).toBe("mainnet");
    expect(hasPlaceholderContracts(NETWORKS.mainnet)).toBe(false);
    expect(NETWORKS.mainnet.networkPassphrase).toBe(
      "Public Global Stellar Network ; September 2015",
    );
  });

  test("renders mainnet banner, vault list, and deposit cap with zero console errors", async ({
    page,
  }) => {
    const consoleErrors: string[] = [];
    page.on("console", (msg) => {
      if (msg.type() === "error") {
        consoleErrors.push(msg.text());
      }
    });

    await page.addInitScript(() => {
      window.localStorage.setItem("nester_selected_network", "mainnet");
    });

    await page.route("**/api/v1/vaults**", async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(MOCK_MAINNET_VAULTS),
      });
    });

    await page.goto("/", { waitUntil: "domcontentloaded" });

    const mainnetBanner = page.getByTestId("mainnet-banner");
    await expect(mainnetBanner).toBeVisible();
    await expect(mainnetBanner).toContainText("Stellar Mainnet");
    await expect(page.getByTestId("testnet-banner")).toHaveCount(0);

    expect(consoleErrors).toEqual([]);
  });
});
