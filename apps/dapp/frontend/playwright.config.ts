import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './tests',
  fullyParallel: !process.env.SMOKE_TEST, // Smoke tests must run sequentially
  forbidOnly: !!process.env.CI,
  retries: process.env.SMOKE_TEST ? 0 : (process.env.CI ? 2 : 0), // No retries for smoke tests
  workers: process.env.SMOKE_TEST ? 1 : (process.env.CI ? 1 : undefined), // Smoke tests always single worker
  reporter: process.env.SMOKE_TEST 
    ? ([['json'], ['html'], ['list']] as any)
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
  webServer: process.env.CI || process.env.SMOKE_TEST ? undefined : {
    command: 'npm run dev',
    url: 'http://localhost:3001',
    reuseExistingServer: true,
    timeout: 120000,
  },
  timeout: process.env.SMOKE_TEST ? 600000 : undefined, // 10-minute timeout for smoke tests
});
