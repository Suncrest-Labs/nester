/**
 * Configuration for the E2E-only connection harness (app/e2e/*).
 *
 * These routes 404 unless NEXT_PUBLIC_E2E_HARNESS is set, which the Playwright
 * webServer does and no deployed environment does.
 */

export const E2E_HARNESS_ENABLED = process.env.NEXT_PUBLIC_E2E_HARNESS === "1";

/**
 * Channels the harness subscribes to.
 *
 * The production tree derives its channel list from a connected wallet, which
 * a browser-driven test cannot produce without a real wallet extension. With
 * an empty list the client has nothing to re-subscribe to after a reconnect,
 * so the behaviour that matters here would be invisible to the test.
 */
export const E2E_HARNESS_CHANNELS = ["vaults:global"];

/**
 * Token the harness authenticates its socket with.
 *
 * useWebSocket refuses to open a socket without a credential, and the real
 * one is minted by a wallet signature — which a browser-driven test cannot
 * produce. Seeding the token store instead does not work either: AuthProvider
 * clears the stored session whenever no wallet is connected, so the token is
 * wiped moments after the page loads and the socket closes with it.
 *
 * This value never reaches a deployed environment: it is only read when
 * NEXT_PUBLIC_E2E_HARNESS is set, and the Playwright suite routes the socket
 * to an in-process fake hub that never checks it. A real hub would reject it.
 */
export const E2E_HARNESS_TOKEN = "e2e-harness-token";

/**
 * Heartbeat timings for the harness.
 *
 * Production pings every 30s and allows 10s for the pong, so a blackholed
 * socket takes up to 40s to detect — longer than a Playwright test should run.
 * Compressing the same two-phase sequence keeps the assertion about detection
 * behaviour rather than about wall-clock patience.
 */
export const E2E_HARNESS_HEARTBEAT_INTERVAL_MS = 1_000;
export const E2E_HARNESS_HEARTBEAT_TIMEOUT_MS = 2_000;
