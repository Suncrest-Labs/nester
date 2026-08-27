/**
 * account.ts
 *
 * Test account creation and management for smoke tests.
 *
 * Provides helpers for creating ephemeral test accounts for each smoke test run.
 * Accounts are created via the dApp registration flow and cleaned up after tests.
 */

import { type Page } from "@playwright/test";

export interface TestAccount {
  email: string;
  password: string;
  createdAt: number;
}

/**
 * Generate a unique test account email for this smoke test run.
 *
 * Uses timestamp and random string to ensure uniqueness across parallel runs.
 * Format: smoke-<timestamp>-<random>@test.nester.dev
 */
export function generateTestAccountEmail(): string {
  const timestamp = Date.now();
  const random = Math.random().toString(36).slice(2, 8);
  return `smoke-${timestamp}-${random}@test.nester.dev`;
}

/**
 * Generate a strong, deterministic test password.
 *
 * Password must meet typical security requirements:
 * - At least 8 characters
 * - Mix of uppercase, lowercase, numbers, symbols
 */
export function generateTestPassword(): string {
  return `SmokeTest@${Date.now().toString().slice(-4)}`;
}

/**
 * Create a test account via the dApp registration flow.
 *
 * Navigates to /register (or similar), fills the form, and submits.
 * Waits for successful registration (redirect to onboarding/dashboard).
 *
 * @param page - Playwright page object
 * @param email - Email to register (uses generated if not provided)
 * @param password - Password (uses generated if not provided)
 * @returns TestAccount with created credentials
 * @throws if registration fails or times out
 */
export async function createTestAccount(
  page: Page,
  email?: string,
  password?: string
): Promise<TestAccount> {
  const testEmail = email || generateTestAccountEmail();
  const testPassword = password || generateTestPassword();

  // Navigate to signup page
  await page.goto("/signup", { waitUntil: "networkidle" });

  // Wait for form to load
  const emailInput = page.locator('input[type="email"], input[name*="email"]').first();
  await emailInput.waitFor({ state: "visible", timeout: 10_000 });

  // Fill form
  await emailInput.fill(testEmail, { timeout: 5_000 });

  const passwordInput = page.locator('input[type="password"], input[name*="password"]').first();
  await passwordInput.fill(testPassword, { timeout: 5_000 });

  // Optional: Accept ToS if checkbox exists
  const tosCheckbox = page.locator('input[type="checkbox"]').first();
  if (await tosCheckbox.count()) {
    await tosCheckbox.check({ force: true, timeout: 5_000 });
  }

  // Submit form
  const submitButton = page
    .locator('button[type="submit"]:has-text("Sign Up"), button[type="submit"]:has-text("Create Account")')
    .first();
  await submitButton.click({ timeout: 5_000 });

  // Wait for successful registration
  // Should redirect to onboarding or dashboard
  try {
    await page.waitForURL(/\/(onboarding|dashboard)/, { timeout: 15_000 });
  } catch {
    // Fallback: check for success message
    const successMessage = page.locator('text=/welcome|success/i').first();
    await successMessage.waitFor({ state: "visible", timeout: 10_000 });
  }

  console.log(`Test account created: ${testEmail}`);

  return {
    email: testEmail,
    password: testPassword,
    createdAt: Date.now(),
  };
}

/**
 * Login with an existing test account.
 *
 * Navigates to /login (or similar) and submits credentials.
 *
 * @param page - Playwright page object
 * @param email - Account email
 * @param password - Account password
 * @throws if login fails or times out
 */
export async function loginTestAccount(
  page: Page,
  email: string,
  password: string
): Promise<void> {
  // Navigate to login page
  await page.goto("/login", { waitUntil: "networkidle" });

  // Wait for form
  const emailInput = page.locator('input[type="email"], input[name*="email"]').first();
  await emailInput.waitFor({ state: "visible", timeout: 10_000 });

  // Fill form
  await emailInput.fill(email, { timeout: 5_000 });

  const passwordInput = page.locator('input[type="password"], input[name*="password"]').first();
  await passwordInput.fill(password, { timeout: 5_000 });

  // Submit
  const submitButton = page
    .locator('button[type="submit"]:has-text("Sign In"), button[type="submit"]:has-text("Log In")')
    .first();
  await submitButton.click({ timeout: 5_000 });

  // Wait for dashboard or onboarding
  try {
    await page.waitForURL(/\/(onboarding|dashboard)/, { timeout: 15_000 });
  } catch {
    const successIndicator = page.locator('[data-testid="wallet-address"], .user-menu').first();
    await successIndicator.waitFor({ state: "visible", timeout: 10_000 });
  }

  console.log(`Logged in as: ${email}`);
}

/**
 * Logout the current test account.
 *
 * Clicks the logout/sign-out button and waits for redirect to login/home.
 */
export async function logoutTestAccount(page: Page): Promise<void> {
  const logoutButton = page
    .locator('button:has-text("Log Out"), button:has-text("Sign Out"), a:has-text("Logout")')
    .first();

  if (await logoutButton.count()) {
    await logoutButton.click({ timeout: 5_000 });
  }

  // Wait for redirect to login or home
  try {
    await page.waitForURL(/\/(login|home|$)/, { timeout: 10_000 });
  } catch {
    // Verify logged out by checking for login button
    const loginButton = page.locator('button:has-text("Log In"), button:has-text("Sign In")').first();
    await loginButton.waitFor({ state: "visible", timeout: 5_000 });
  }

  console.log("Logged out");
}

/**
 * Check if user is currently logged in.
 *
 * Looks for indicators like wallet address display or user menu.
 */
export async function isUserLoggedIn(page: Page): Promise<boolean> {
  try {
    const walletAddress = page
      .locator('[data-testid="wallet-address"], .wallet-address, .user-menu')
      .first();

    return (await walletAddress.count()) > 0;
  } catch {
    return false;
  }
}

/**
 * Get the currently logged-in user's email (if visible in UI).
 *
 * Returns null if not visible or not logged in.
 */
export async function getCurrentUserEmail(page: Page): Promise<string | null> {
  try {
    const userEmail = page
      .locator('[data-testid="user-email"], .user-email, [class*="email"]')
      .first();

    if (await userEmail.count()) {
      return await userEmail.textContent();
    }

    return null;
  } catch {
    return null;
  }
}
