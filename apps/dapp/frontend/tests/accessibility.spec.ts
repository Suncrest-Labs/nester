import { test, expect } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

test.describe('Accessibility', () => {
  test('Dashboard should not have automatically detectable accessibility violations', async ({ page }) => {
    await page.goto('/dashboard');
    // Wait for the page to be ready (e.g., wallet connected state or loading finished)
    // For these tests we assume the user is connected or we test the landing/auth state
    
    const accessibilityScanResults = await new AxeBuilder({ page })
        .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
        .analyze();

    expect(accessibilityScanResults.violations).toEqual([]);
  });

  test('Markets page should not have automatically detectable accessibility violations', async ({ page }) => {
    await page.goto('/vaults');
    const accessibilityScanResults = await new AxeBuilder({ page })
        .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
        .analyze();

    expect(accessibilityScanResults.violations).toEqual([]);
  });

  test('Savings page should not have automatically detectable accessibility violations', async ({ page }) => {
    await page.goto('/savings');
    const accessibilityScanResults = await new AxeBuilder({ page })
        .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
        .analyze();

    expect(accessibilityScanResults.violations).toEqual([]);
  });

  test('Portfolio page should not have automatically detectable accessibility violations', async ({ page }) => {
    await page.goto('/portfolio');
    const accessibilityScanResults = await new AxeBuilder({ page })
        .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
        .analyze();

    expect(accessibilityScanResults.violations).toEqual([]);
  });
});
