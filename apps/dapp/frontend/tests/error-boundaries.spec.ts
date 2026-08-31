import { test, expect, type Page } from "@playwright/test";

/**
 * Error-boundary coverage for issue #1046.
 *
 * The failure modes are forced with route interception rather than by pointing
 * the app at a broken backend, so the tests are deterministic and do not need a
 * running API.
 */

const YIELDS_API = "**/api/v1/yields?*";
const HARVESTS_API = "**/api/v1/yields/harvests*";

/** Collects the structured entries the client error reporter writes. */
function captureErrorReports(page: Page): string[] {
  const reports: string[] = [];
  page.on("console", (msg) => {
    const text = msg.text();
    if (text.includes("[nester:client-error]")) reports.push(text);
  });
  return reports;
}

/**
 * Marks the current document so a later assertion can prove the browser never
 * performed a full reload — the marker only survives a client-side re-render.
 */
async function markDocument(page: Page) {
  await page.evaluate(() => {
    (window as unknown as { __noReloadMarker?: boolean }).__noReloadMarker = true;
  });
}

async function documentStillAlive(page: Page) {
  return page.evaluate(
    () =>
      (window as unknown as { __noReloadMarker?: boolean }).__noReloadMarker ===
      true
  );
}

test.describe("Failed fetch", () => {
  test("renders the error fallback instead of a blank page and keeps the nav shell", async ({
    page,
  }) => {
    await page.route(HARVESTS_API, (route) =>
      route.fulfill({
        status: 500,
        contentType: "application/json",
        body: JSON.stringify({ success: false, error: { message: "boom" } }),
      })
    );

    await page.goto("/yields/history");

    // The section reports its own failure rather than blanking.
    const errorRegion = page.getByTestId("harvests-error");
    await expect(errorRegion).toBeVisible({ timeout: 20_000 });

    // The page is not blank and the navigation shell survived.
    await expect(page.locator("body")).not.toBeEmpty();
    await expect(
      page.getByRole("link", { name: "Dashboard", exact: true }).first()
    ).toBeVisible();

    // Nothing resembling a stack trace reached the user.
    const body = (await page.locator("body").innerText()).toLowerCase();
    expect(body).not.toContain("at object.");
    expect(body).not.toContain(".tsx:");
  });

  test("a failing yields request does not blank the navigation", async ({
    page,
  }) => {
    await page.route(YIELDS_API, (route) => route.abort("failed"));

    await page.goto("/yields");

    await expect(
      page.getByRole("link", { name: "Dashboard", exact: true }).first()
    ).toBeVisible({ timeout: 20_000 });
    await expect(page.locator("body")).not.toBeEmpty();
  });
});

test.describe("Render-time exception", () => {
  test("the route boundary catches it, shows a fallback and keeps navigation usable", async ({
    page,
  }) => {
    const reports = captureErrorReports(page);

    await page.goto("/diagnostics/render-error");

    const fallback = page.getByTestId("route-error-fallback");
    await expect(fallback).toBeVisible({ timeout: 20_000 });

    // Not a blank page, and the boundary is scoped so the shell is intact.
    await expect(page.locator("body")).not.toBeEmpty();
    await expect(
      page.getByRole("link", { name: "Dashboard", exact: true }).first()
    ).toBeVisible();

    // Recovery affordances are present and reachable.
    await expect(page.getByTestId("route-error-retry")).toBeVisible();
    await expect(page.getByTestId("route-error-home")).toBeVisible();

    // The raw exception is never shown to the user.
    const body = await fallback.innerText();
    expect(body).not.toContain("Diagnostic render failure");
    expect(body).not.toContain(".tsx");

    // The failure reached the logging pipeline with route context.
    await expect
      .poll(() => reports.length, { timeout: 10_000 })
      .toBeGreaterThan(0);
    const payload = JSON.parse(
      reports[0].slice(reports[0].indexOf("{"))
    ) as Record<string, string>;
    expect(payload.route).toBe("/diagnostics/render-error");
    expect(payload.boundary).toBe("diagnostics");
    expect(payload.category).toBeTruthy();
  });

  test("the retry control is keyboard reachable and the heading takes focus", async ({
    page,
  }) => {
    await page.goto("/diagnostics/render-error");
    await expect(page.getByTestId("route-error-fallback")).toBeVisible({
      timeout: 20_000,
    });

    // Focus lands on the fallback heading so keyboard users are not stranded.
    const headingFocused = await page.evaluate(
      () => document.activeElement?.tagName.toLowerCase() === "h2"
    );
    expect(headingFocused).toBe(true);

    // Tabbing forward reaches the retry button.
    await page.keyboard.press("Tab");
    const retryFocused = await page.evaluate(
      () =>
        document.activeElement?.getAttribute("data-testid") ===
        "route-error-retry"
    );
    expect(retryFocused).toBe(true);
  });
});

test.describe("Error logging privacy", () => {
  test("reported payloads carry route context but no wallet, balance or token", async ({
    page,
  }) => {
    const reports = captureErrorReports(page);

    await page.addInitScript(() => {
      window.localStorage.setItem("nester_token", "secret-jwt-value");
      window.localStorage.setItem(
        "nester_wallet_addr",
        "GABCDEFGHIJKLMNOPQRSTUVWXYZ234567ABCDEFGHIJKLMNOPQRSTUVW"
      );
    });

    await page.goto("/diagnostics/render-error");
    await expect(page.getByTestId("route-error-fallback")).toBeVisible({
      timeout: 20_000,
    });
    await expect
      .poll(() => reports.length, { timeout: 10_000 })
      .toBeGreaterThan(0);

    const raw = reports.join("\n");
    expect(raw).toContain("/diagnostics/render-error");
    expect(raw).not.toContain("GABCDEFGHIJKLMNOPQRSTUVWXYZ234567");
    expect(raw).not.toContain("secret-jwt-value");
    expect(raw.toLowerCase()).not.toContain("authorization");
    expect(raw).not.toContain('"stack"');
  });
});

test.describe("Recovery", () => {
  test("retry recovers the failed section without a full browser reload", async ({
    page,
  }) => {
    let shouldFail = true;

    await page.route(HARVESTS_API, (route) => {
      if (shouldFail) {
        return route.fulfill({
          status: 500,
          contentType: "application/json",
          body: JSON.stringify({ success: false, error: { message: "boom" } }),
        });
      }
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          success: true,
          data: { items: [], next_cursor: "" },
        }),
      });
    });

    await page.goto("/yields/history");
    await expect(page.getByTestId("harvests-error")).toBeVisible({
      timeout: 20_000,
    });

    // Anything that survives to the assertion below proves the document was
    // never torn down by a browser reload.
    await markDocument(page);

    let fullNavigations = 0;
    page.on("framenavigated", (frame) => {
      if (frame === page.mainFrame()) fullNavigations += 1;
    });

    // The next request succeeds; recovery happens through a client-side refetch.
    shouldFail = false;
    await page.getByTestId("harvests-retry").click();

    await expect(page.getByTestId("harvests-empty-state")).toBeVisible({
      timeout: 20_000,
    });
    expect(fullNavigations).toBe(0);
    expect(await documentStillAlive(page)).toBe(true);
  });

  test("the boundary reset re-renders in place, keeping the same document", async ({
    page,
  }) => {
    await page.goto("/diagnostics/render-error");
    await expect(page.getByTestId("route-error-fallback")).toBeVisible({
      timeout: 20_000,
    });

    await markDocument(page);

    let fullNavigations = 0;
    page.on("framenavigated", (frame) => {
      if (frame === page.mainFrame()) fullNavigations += 1;
    });

    // Reset re-renders the failing segment. The diagnostic route throws again,
    // so the fallback comes back — but through React, not a page load.
    await page.getByTestId("route-error-retry").click();
    await expect(page.getByTestId("route-error-fallback")).toBeVisible();

    expect(fullNavigations).toBe(0);
    expect(await documentStillAlive(page)).toBe(true);
  });

  test("the home link offers a way out of the failed route", async ({
    page,
  }) => {
    await page.goto("/diagnostics/render-error");
    await expect(page.getByTestId("route-error-fallback")).toBeVisible({
      timeout: 20_000,
    });

    // It is a real link, so it works with middle-click, keyboard and
    // right-click "open in new tab" — not just a scripted onClick handler.
    const home = page.getByTestId("route-error-home");
    await expect(home).toHaveAttribute("href", "/dashboard");

    await home.click();

    // The user leaves the broken route. Where they land depends on whether a
    // wallet is connected, but they must not be stuck on the failed segment.
    await expect
      .poll(() => new URL(page.url()).pathname, { timeout: 30_000 })
      .not.toBe("/diagnostics/render-error");
  });
});
