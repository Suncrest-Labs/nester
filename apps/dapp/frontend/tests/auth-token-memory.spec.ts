import { test, expect } from "@playwright/test";

test.describe("Auth token storage security (#1506)", () => {
  test("never persists nester_auth_token in localStorage after login or reload", async ({
    page,
  }) => {
    await page.route("**/api/v1/auth/refresh", async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          success: true,
          data: {
            access_token: "short-lived-memory-jwt",
            expires_in: 300,
            token_type: "Bearer",
          },
        }),
      });
    });

    await page.addInitScript(() => {
      window.localStorage.setItem("nester_auth_token", "legacy-token-to-purge");
    });

    await page.goto("/dashboard", { waitUntil: "domcontentloaded" });

    const storedToken = await page.evaluate(() =>
      window.localStorage.getItem("nester_auth_token")
    );
    expect(storedToken).toBeNull();
  });
});
