import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './tests',
  fullyParallel: !process.env.SMOKE_TEST, // Smoke tests must run sequentially
  forbidOnly: !!process.env.CI,
  retries: process.env.SMOKE_TEST ? 0 : (process.env.CI ? 2 : 0), // No retries for smoke tests
  workers: process.env.SMOKE_TEST ? 1 : (process.env.CI ? 1 : undefined), // Smoke tests always single worker
  reporter: process.env.SMOKE_TEST 
    ? [['json'], ['html'], ['list']]
    : 'html',
  use: {
    baseURL: process.env.STAGING_URL || 'http://localhost:3001',
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
  // smoke.spec.ts is a full-stack test against a deployed environment: it needs
  // a real API, a funded testnet account and on-chain settlement. Everything
  // else forces its failure modes with route interception and needs nothing but
  // the dev server, which is what makes those safe to gate a PR on.
  testIgnore: process.env.SMOKE_TEST ? undefined : /smoke\.spec\.ts/,
  // A local server is started unless we are pointed at a deployed one. Keying
  // this off CI would leave the PR suite with nothing to talk to.
  webServer: process.env.SMOKE_TEST || process.env.STAGING_URL ? undefined : {
    command: 'npm run dev',
    url: 'http://localhost:3001',
    reuseExistingServer: !process.env.CI,
    timeout: 120000,
    env: {
      // Enables app/e2e/* harness routes, which 404 everywhere else.
      NEXT_PUBLIC_E2E_HARNESS: '1',
      // Points the client at a socket URL the tests intercept with
      // page.routeWebSocket — no real hub needs to be running.
      NEXT_PUBLIC_WS_URL: 'ws://localhost:3001/ws',
    },
  },
  timeout: process.env.SMOKE_TEST ? 600000 : undefined, // 10-minute timeout for smoke tests
});
