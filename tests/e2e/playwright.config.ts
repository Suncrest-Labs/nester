import { defineConfig, devices } from "@playwright/test";

const baseURL = process.env.E2E_BASE_URL ?? "http://localhost:3001";

export default defineConfig({
    testDir: "./specs",
    fullyParallel: true,
    forbidOnly: !!process.env.CI,
    retries: process.env.CI ? 2 : 0,
    workers: process.env.CI ? 1 : undefined,
    reporter: [
        ["list"],
        ["html", { outputFolder: "playwright-report", open: "never" }],
    ],
    use: {
        baseURL,
        trace: "on-first-retry",
        screenshot: "only-on-failure",
        video: "retain-on-failure",
    },
    projects: [
        {
            name: "chromium",
            use: { ...devices["Desktop Chrome"] },
        },
        {
            name: "mobile",
            use: { ...devices["iPhone 13"] },
            // Only run the deposit flow on mobile to keep CI fast
            testMatch: ["**/vault-deposit.spec.ts"],
        },
    ],
    // Only spin up the dev server locally — CI uses Docker Compose
    webServer: process.env.CI
        ? undefined
        : {
              command: "npm run dev",
              cwd: "../../apps/dapp/frontend",
              port: 3001,
              reuseExistingServer: true,
              timeout: 120_000,
          },
});
