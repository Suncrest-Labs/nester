import { test, expect, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

const AXE_TAGS = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'];

/**
 * Scan a page once it has stopped moving.
 *
 * Entry animations fade content in from opacity 0. axe resolves colours
 * through whatever opacity is in effect at the instant it runs, so scanning
 * mid-animation measures text against a blended backdrop no user ever sees and
 * reports contrast failures that are pure artifacts — 1.05:1 between two greys
 * that are never actually composited together. It also reports them
 * inconsistently, which is what made these tests flaky rather than merely red.
 *
 * Emulating `prefers-reduced-motion` settles the page without a sleep, and is
 * the honest thing to scan besides: it is the state a motion-sensitive user
 * actually gets.
 */
async function scan(page: Page, path: string) {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.goto(path);
  await page.waitForLoadState('networkidle').catch(() => {});

  return new AxeBuilder({ page }).withTags(AXE_TAGS).analyze();
}

test.describe('Accessibility', () => {
  test('Dashboard should not have automatically detectable accessibility violations', async ({ page }) => {
    const results = await scan(page, '/dashboard');
    expect(results.violations).toEqual([]);
  });

  test('Markets page should not have automatically detectable accessibility violations', async ({ page }) => {
    const results = await scan(page, '/vaults');
    expect(results.violations).toEqual([]);
  });

  test('Savings page should not have automatically detectable accessibility violations', async ({ page }) => {
    const results = await scan(page, '/savings');
    expect(results.violations).toEqual([]);
  });

  test('Portfolio page should not have automatically detectable accessibility violations', async ({ page }) => {
    const results = await scan(page, '/portfolio');
    expect(results.violations).toEqual([]);
  });
});
