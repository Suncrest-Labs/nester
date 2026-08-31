import { test, expect, type Page, type Route } from "@playwright/test";

/**
 * Loading-state coverage for issue #1046: a skeleton must appear while a slow
 * request is in flight, and the empty state must never stand in for it.
 *
 * Rather than delaying responses by a fixed number of milliseconds — which
 * races against dev-server compile time and makes the tests flaky under
 * parallel load — each interceptor holds the response open until the test
 * explicitly releases it. The "slow connection" window is therefore as long as
 * the assertions need it to be.
 */

const YIELDS_API = "**/api/v1/yields?*";
const HARVESTS_API = "**/api/v1/yields/harvests*";

interface HeldRoute {
  /** Resolves once the app has actually issued the request. */
  requested: Promise<void>;
  /** Completes the pending request with the given JSON body. */
  release: () => Promise<void>;
}

/**
 * Intercept `pattern` and hold the first matching request until `release()` is
 * called, at which point it is fulfilled with `body`.
 */
async function holdRequest(
  page: Page,
  pattern: string,
  body: unknown
): Promise<HeldRoute> {
  let held: Route | undefined;
  let markRequested: () => void = () => {};
  const requested = new Promise<void>((resolve) => {
    markRequested = resolve;
  });

  await page.route(pattern, async (route) => {
    if (held) {
      // Any follow-up request (a retry, a next page) resolves immediately.
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(body),
      });
      return;
    }
    held = route;
    markRequested();
  });

  return {
    requested,
    release: async () => {
      await held?.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(body),
      });
    },
  };
}

function delay(ms: number) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

const EMPTY_HARVESTS = {
  success: true,
  data: { items: [], next_cursor: "" },
};

const FIVE_HARVESTS = {
  success: true,
  data: {
    items: Array.from({ length: 5 }).map((_, i) => ({
      id: `harvest-${i}`,
      user_id: "user-1",
      vault_id: "vault-1",
      protocol: "Blend",
      amount: "12.5",
      currency: "USDC",
      harvested_at: `2026-01-0${i + 1}T10:00:00Z`,
      tx_hash: "",
    })),
    next_cursor: "",
  },
};

const ONE_POOL = {
  success: true,
  data: {
    data: [
      {
        pool: "test-pool-1",
        project: "Blend",
        symbol: "USDC",
        chain: "Stellar",
        apy: 8.4,
        apyBase: 6.2,
        apyReward: 2.2,
        tvlUsd: 5_000_000,
        apyPct7d: 0.3,
        riskScore: 20,
      },
    ],
    meta: { stale: false },
  },
};

test.describe("Delayed response", () => {
  test("shows a skeleton while the request is in flight, then the content", async ({
    page,
  }) => {
    const yields = await holdRequest(page, YIELDS_API, ONE_POOL);

    await page.goto("/yields");
    await yields.requested;

    // A structural skeleton, announced once as a busy region.
    const loading = page.getByTestId("loading-region").first();
    await expect(loading).toBeVisible({ timeout: 20_000 });
    await expect(loading).toHaveAttribute("aria-busy", "true");

    // Critically: the empty state must not show while we are still loading.
    await expect(page.getByTestId("yields-empty-state")).toHaveCount(0);

    // Once the response lands the skeleton is replaced by real content.
    await yields.release();
    await expect(loading).toHaveCount(0, { timeout: 20_000 });
    await expect(page.getByText("USDC").first()).toBeVisible({
      timeout: 20_000,
    });
  });

  test("never flashes the empty state before the response arrives", async ({
    page,
  }) => {
    const harvests = await holdRequest(page, HARVESTS_API, EMPTY_HARVESTS);

    await page.goto("/yields/history");
    await harvests.requested;

    const loading = page.getByTestId("loading-region").first();
    const empty = page.getByTestId("harvests-empty-state");

    await expect(loading).toBeVisible({ timeout: 20_000 });

    // Sample repeatedly while the request is still held open. The empty state
    // must never appear — that is the "loading looks like empty" bug this
    // guards against.
    for (let i = 0; i < 10; i += 1) {
      expect(await empty.count()).toBe(0);
      await delay(150);
    }
    await expect(loading).toBeVisible();

    // And once the (empty) response lands, the empty state does take over.
    await harvests.release();
    await expect(empty).toBeVisible({ timeout: 20_000 });
    await expect(loading).toHaveCount(0);
  });
});

test.describe("Loading and empty are distinct", () => {
  test("an empty result renders the empty state, not a skeleton", async ({
    page,
  }) => {
    await page.route(HARVESTS_API, (route) =>
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(EMPTY_HARVESTS),
      })
    );

    await page.goto("/yields/history");

    const empty = page.getByTestId("harvests-empty-state");
    await expect(empty).toBeVisible({ timeout: 20_000 });

    // The empty state says something a skeleton cannot.
    await expect(empty).toContainText(/no harvests yet/i);

    // And the skeleton is gone.
    await expect(page.getByTestId("loading-region")).toHaveCount(0);
  });

  test("the skeleton carries no empty-state copy", async ({ page }) => {
    const harvests = await holdRequest(page, HARVESTS_API, EMPTY_HARVESTS);

    await page.goto("/yields/history");
    await harvests.requested;

    const loading = page.getByTestId("loading-region").first();
    await expect(loading).toBeVisible({ timeout: 20_000 });

    const text = await loading.innerText();
    expect(text).not.toMatch(/no harvests/i);
    expect(text).not.toMatch(/nothing here/i);
    // It does carry an accessible announcement for screen readers.
    await expect(loading).toContainText(/loading/i);

    await harvests.release();
  });
});

test.describe("Skeleton layout stability", () => {
  test("the skeleton occupies roughly the space the content will take", async ({
    page,
  }) => {
    const harvests = await holdRequest(page, HARVESTS_API, FIVE_HARVESTS);

    await page.goto("/yields/history");
    await harvests.requested;

    const loading = page.getByTestId("loading-region").first();
    await expect(loading).toBeVisible({ timeout: 20_000 });
    const skeletonBox = await loading.boundingBox();

    await harvests.release();
    await expect(loading).toHaveCount(0, { timeout: 20_000 });

    const table = page.locator("table").first();
    await expect(table).toBeVisible({ timeout: 20_000 });
    const contentBox = await table.boundingBox();

    expect(skeletonBox).not.toBeNull();
    expect(contentBox).not.toBeNull();

    // Within a generous band — the point is that content does not appear in a
    // wildly different place than the placeholder it replaced.
    const drift = Math.abs(skeletonBox!.height - contentBox!.height);
    expect(drift).toBeLessThan(320);
  });
});
