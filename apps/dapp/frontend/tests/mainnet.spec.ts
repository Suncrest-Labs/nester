import { expect, test } from "@playwright/test";
import {
  MAINNET_PASSPHRASE,
  mockFreighter,
  TEST_ADDRESS,
} from "./helpers/mock-freighter";

const VAULT_ID = "mainnet-smoke-vault";
const CONTRACT_ID = `C${"A".repeat(55)}`;

function apiResponse(data: unknown) {
  return {
    status: 200,
    contentType: "application/json",
    body: JSON.stringify({ success: true, data }),
  };
}

async function mockMainnetApis(page: import("@playwright/test").Page) {
  await page.route("**/api/v1/**", async (route) => {
    const { pathname } = new URL(route.request().url());

    if (pathname.endsWith("/status")) {
      return route.fulfill(
        apiResponse({
          deposits_paused: false,
          withdrawals_paused: false,
          indexer_lag_ledgers: 0,
          rpc_healthy: true,
        })
      );
    }

    if (pathname.endsWith(`/vaults/${VAULT_ID}`)) {
      return route.fulfill(
        apiResponse({
          id: VAULT_ID,
          user_id: "mainnet-smoke-user",
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

    if (pathname.endsWith(`/vaults/${VAULT_ID}/performance`)) {
      return route.fulfill(
        apiResponse({
          vault_id: VAULT_ID,
          current_balance: 0,
          total_deposited: 0,
          total_yield: 0,
          roi_pct: 0,
          apy_7d: 0.05,
          apy_30d: 0.05,
          apy_90d: 0.05,
          snapshot_count: 0,
        })
      );
    }

    if (pathname.endsWith("/yields/harvests")) {
      return route.fulfill(apiResponse({ items: [], next_cursor: "" }));
    }

    return route.fulfill(apiResponse([]));
  });

  await page.route("**/api/notifications/preferences", (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        categories: {
          safety: { in_app: true, email: true, push: true },
          transactional: { in_app: true, email: true, push: false },
          nudge: { in_app: true, email: false, push: false },
        },
      }),
    })
  );
  await page.route("**/api/notifications?*", (route) =>
    route.fulfill({ status: 200, contentType: "application/json", body: "[]" })
  );
  await page.route("**/vaults/*/risk", (route) =>
    route.fulfill(
      apiResponse({
        id: "mainnet-smoke-risk",
        vault_id: VAULT_ID,
        overall: 0,
        confidence: 1,
        tier: "low",
        factors: [],
        computed_at: "2026-01-01T00:00:00Z",
      })
    )
  );
  await page.route("https://horizon.stellar.org/accounts/**", (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        sequence: "1",
        subentry_count: 1,
        num_sponsoring: 0,
        num_sponsored: 0,
        balances: [
          { asset_type: "native", balance: "100.0000000" },
          {
            asset_type: "credit_alphanum4",
            asset_code: "USDC",
            balance: "100.0000000",
            asset_issuer: "GAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAWHF",
          },
        ],
      }),
    })
  );
  await page.route("https://soroban-rpc.mainnet.stellar.org/**", (route) =>
    route.fulfill({
      status: 503,
      contentType: "application/json",
      body: JSON.stringify({ error: "RPC is intentionally unavailable in this read-only smoke test" }),
    })
  );
}

async function openMainnetVault(page: import("@playwright/test").Page) {
  await mockFreighter(page, {
    walletPassphrase: MAINNET_PASSPHRASE,
    dappNetwork: "mainnet",
  });
  await mockMainnetApis(page);
  await page.goto("/");
  await page.waitForFunction(() =>
    Boolean(window.localStorage.getItem("@StellarWalletsKit/activeAddress"))
  );
  await page.evaluate((vaultId) => {
    (window as unknown as { next: { router: { push: (href: string) => void } } })
      .next.router.push(`/vaults/${vaultId}`);
  }, VAULT_ID);
}

test.describe("Mainnet read-only smoke @mainnet", () => {
  test("renders the landing page from the Mainnet build", async ({ page }) => {
    await page.addInitScript(() => {
      window.localStorage.setItem("nester_network_id", "mainnet");
    });

    await page.goto("/");

    await expect(page).toHaveTitle(/nester/i);
    await expect(page.getByRole("button", { name: "Connect" })).toBeVisible();
  });

  test("restores a mocked Mainnet wallet session", async ({ page }) => {
    await mockFreighter(page, {
      walletPassphrase: MAINNET_PASSPHRASE,
      dappNetwork: "mainnet",
    });
    await mockMainnetApis(page);

    await page.goto("/");
    await expect(page).toHaveURL(/\/dashboard$/);
    await expect(page.getByText(TEST_ADDRESS.slice(0, 4))).toBeVisible();
    await expect(page.getByText(/you are on testnet/i)).toHaveCount(0);
  });

  test("shows the Mainnet vault and opens its read-only deposit modal", async ({
    page,
  }) => {
    await openMainnetVault(page);

    await expect(
      page.getByRole("heading", { name: "USDC Market" })
    ).toBeVisible();
    await expect(page.getByText("Mainnet deployment")).toBeVisible();
    await expect(page.getByText(/you are on testnet/i)).toHaveCount(0);
    await expect(
      page.getByRole("link", { name: "View Vault on stellar.expert" })
    ).toHaveAttribute(
      "href",
      `https://stellar.expert/explorer/public/contract/${CONTRACT_ID}`
    );

    await page.getByRole("button", { name: `Supply to USDC Market` }).click();
    await expect(
      page.getByRole("heading", { name: "Deposit into USDC Market" })
    ).toBeVisible();
    await expect(page.getByText("A Freighter popup will appear to confirm signing.")).toBeVisible();
    await page.getByPlaceholder("0.00").fill("10");

    // Keep this smoke test read-only: it verifies the built Mainnet UI through
    // transaction preparation without asking a wallet to sign or submitting.
    await expect(page.getByRole("button", { name: "Confirm Deposit" })).toBeVisible();
    expect(
      await page.evaluate(
        () =>
          (window as unknown as { __freighterSignCalls: number })
            .__freighterSignCalls
      )
    ).toBe(0);
  });
});
