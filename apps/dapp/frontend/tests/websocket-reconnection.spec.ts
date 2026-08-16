import { test, expect, type Page } from "@playwright/test";

/**
 * WebSocket reconnection E2E tests.
 *
 * Uses Playwright's route interception to block/unblock the WebSocket
 * connection, simulating server restarts and network drops. The connection
 * status badge should reflect the live/reconnecting/disconnected states.
 */

test.describe("WebSocket reconnection", () => {
  /** Helper: block WebSocket connections to the app's WS endpoint. */
  async function blockWebSocket(page: Page) {
    await page.route("**/ws", (route) => route.abort());
  }

  /** Helper: unblock WebSocket connections. */
  async function unblockWebSocket(page: Page) {
    await page.unroute("**/ws");
  }

  test("shows the live indicator when connected", async ({ page }) => {
    // Navigate to the dashboard where the WebSocket is active.
    await page.goto("/dashboard");

    // Wait for the connection status badge to appear in "Live" state.
    const badge = page.locator('[role="status"]');
    await expect(badge).toBeVisible({ timeout: 15_000 });

    // The badge should show "Live" when the WebSocket is connected.
    await expect(badge).toContainText("Live", { timeout: 10_000 });
  });

  test("shows the reconnecting indicator when the socket drops, then clears on reconnect", async ({
    page,
  }) => {
    await page.goto("/dashboard");

    // Wait for the badge to show "Live" initially.
    const badge = page.locator('[role="status"]');
    await expect(badge).toContainText("Live", { timeout: 15_000 });

    // Block WebSocket to simulate a connection drop.
    await blockWebSocket(page);

    // Wait for the reconnecting indicator to appear.
    // The heartbeat timeout is 10s, so the indicator should appear within 15s.
    await expect(badge).toContainText("Reconnecting", { timeout: 15_000 });

    // Unblock and wait for the badge to show "Live" again.
    await unblockWebSocket(page);
    await page.reload();
    await expect(badge).toContainText("Live", { timeout: 15_000 });
  });

  test("shows stale-data timestamp when disconnected", async ({ page }) => {
    await page.goto("/dashboard");

    const badge = page.locator('[role="status"]');
    await expect(badge).toContainText("Live", { timeout: 15_000 });

    // Block WebSocket to force a disconnect.
    await blockWebSocket(page);

    // Wait for the reconnecting state to appear (should include a relative timestamp).
    await expect(badge).toContainText("Reconnecting", { timeout: 15_000 });

    // After enough reconnection attempts, the badge should show "Using delayed updates".
    // The reconnection limit is 5 attempts with backoff ~1+2+4+8+16 = 31s.
    // Wait up to 45s for the offline state.
    await expect(badge).toContainText("Using delayed updates", { timeout: 45_000 });
  });
});