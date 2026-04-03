import { type Page, type Route } from "@playwright/test";

const DAPP_BACKEND_BASE = process.env.DAPP_BACKEND_URL ?? "http://localhost:8080";

/**
 * Intercepts all dapp-backend API calls and responds with the provided handler.
 * Useful for simulating server-down scenarios or seeding specific response data.
 */
export async function mockApiRoute(
    page: Page,
    urlPattern: string | RegExp,
    handler: (route: Route) => Promise<void> | void
): Promise<void> {
    await page.route(urlPattern, handler);
}

/**
 * Simulates the dapp-backend being completely unreachable.
 * Routes matching the backend base URL will return a network error.
 */
export async function simulateApiDown(page: Page): Promise<void> {
    await page.route(`${DAPP_BACKEND_BASE}/**`, (route) => route.abort("failed"));
}

/**
 * Simulates a slow network for a specific route — useful for verifying
 * that loading states don't hang indefinitely.
 */
export async function simulateSlowNetwork(
    page: Page,
    urlPattern: string | RegExp,
    delayMs = 5000
): Promise<void> {
    await page.route(urlPattern, async (route) => {
        await new Promise((resolve) => setTimeout(resolve, delayMs));
        await route.continue();
    });
}

/**
 * Intercepts the health-check endpoint and returns a healthy response.
 * Useful for CI mock mode where no real backend is running.
 */
export async function mockHealthCheck(page: Page): Promise<void> {
    await page.route(`${DAPP_BACKEND_BASE}/health-check`, (route) =>
        route.fulfill({
            status: 200,
            contentType: "application/json",
            body: JSON.stringify({
                success: true,
                message: "Service is healthy",
                responseObject: {
                    status: "UP",
                    uptime: 12345,
                    timestamp: new Date().toISOString(),
                },
                statusCode: 200,
            }),
        })
    );
}

/**
 * Returns a mock user object matching the dapp-backend's ServiceResponse schema.
 */
export function makeMockUser(id = 1) {
    return {
        id,
        name: `Test User ${id}`,
        email: `testuser${id}@nester.xyz`,
        age: 30,
        createdAt: new Date().toISOString(),
        updatedAt: new Date().toISOString(),
    };
}

/**
 * Mocks the users endpoint with predictable test data.
 */
export async function mockUsersEndpoint(page: Page): Promise<void> {
    await page.route(`${DAPP_BACKEND_BASE}/users`, (route) =>
        route.fulfill({
            status: 200,
            contentType: "application/json",
            body: JSON.stringify({
                success: true,
                message: "Users found",
                responseObject: [makeMockUser(1), makeMockUser(2)],
                statusCode: 200,
            }),
        })
    );

    await page.route(`${DAPP_BACKEND_BASE}/users/*`, (route) => {
        const url = route.request().url();
        const id = parseInt(url.split("/").pop() ?? "1", 10);
        route.fulfill({
            status: 200,
            contentType: "application/json",
            body: JSON.stringify({
                success: true,
                message: "User found",
                responseObject: makeMockUser(id),
                statusCode: 200,
            }),
        });
    });
}
