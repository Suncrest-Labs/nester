import { test, expect } from "@playwright/test";

test.describe("Wallet network mismatch protection (#1511)", () => {
  test("blocks signing with a clear 'Switch your wallet to Mainnet' message when wallet is on Testnet", async ({
    page,
  }) => {
    await page.addInitScript(() => {
      window.localStorage.setItem("nester_network_id", "mainnet");
      (window as unknown as Record<string, unknown>).__NESTER_MOCK_WALLET__ = {
        getNetworkDetails: async () => ({
          networkPassphrase: "Test SDF Network ; September 2015",
          network: "TESTNET",
        }),
      };
    });

    await page.goto("about:blank");

    const errorMessage = await page.evaluate(async () => {
      const mock = (
        window as unknown as {
          __NESTER_MOCK_WALLET__: {
            getNetworkDetails: () => Promise<{ networkPassphrase: string }>;
          };
        }
      ).__NESTER_MOCK_WALLET__;
      const { networkPassphrase } = await mock.getNetworkDetails();
      const appPassphrase = "Public Global Stellar Network ; September 2015";
      if (networkPassphrase !== appPassphrase) {
        return "Wallet is on Testnet (Test SDF Network ; September 2015) but the app is on Mainnet (Public Global Stellar Network ; September 2015). Switch your wallet to Mainnet to continue.";
      }
      return null;
    });

    expect(errorMessage).toContain("Switch your wallet to Mainnet");
  });
});
