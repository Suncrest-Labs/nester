import { test, expect } from "@playwright/test";

test.describe("Content-Security-Policy and security headers (#1504)", () => {
  for (const route of ["/", "/dashboard"]) {
    test(`sends strict CSP and security headers on ${route} with no CSP violations`, async ({
      page,
    }) => {
      const cspViolations: string[] = [];
      page.on("console", (msg) => {
        if (
          msg.type() === "error" &&
          msg.text().toLowerCase().includes("content security policy")
        ) {
          cspViolations.push(msg.text());
        }
      });

      const response = await page.goto(route, { waitUntil: "domcontentloaded" });
      expect(response).not.toBeNull();

      const headers = response!.headers();
      const csp = headers["content-security-policy"] ?? "";

      expect(csp).toContain("default-src 'self'");
      expect(csp).toContain("frame-ancestors 'none'");
      expect(csp).toContain("object-src 'none'");
      expect(csp).toContain("base-uri 'self'");
      expect(csp).toMatch(/'nonce-[A-Za-z0-9+/=]+'/);

      expect(headers["strict-transport-security"]).toContain("max-age=63072000");
      expect(headers["x-content-type-options"]).toBe("nosniff");
      expect(headers["referrer-policy"]).toBe("strict-origin-when-cross-origin");
      expect(headers["permissions-policy"]).toContain("camera=()");

      expect(cspViolations).toEqual([]);
    });
  }
});
