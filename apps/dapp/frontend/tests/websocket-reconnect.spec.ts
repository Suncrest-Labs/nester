import { test, expect, type Page, type WebSocketRoute } from '@playwright/test';

/**
 * WebSocket keepalive + reconnection (P-08 / E-06).
 *
 * The failure this guards against is silent: when the socket drops, the UI
 * keeps showing the last balance it received with live styling, and the user
 * has no way to tell that the number stopped being true. These tests drive a
 * real browser against an intercepted socket and assert the three states the
 * user can actually see.
 */

/**
 * Match the harness socket regardless of its query string.
 *
 * The client authenticates during the upgrade by appending `?token=…` (the
 * browser WebSocket API cannot set headers), so the URL is not a fixed
 * string. An exact-match pattern silently stops intercepting the moment a
 * token is present: the route never fires, the page talks to a `/ws` that
 * nothing is serving, and every assertion here reports "offline" instead of
 * the reconnection behaviour under test.
 */
const WS_URL = /^ws:\/\/localhost:3001\/ws(\?.*)?$/;
const HARNESS = '/e2e/connection';

interface FakeHub {
    /** Sockets the page has opened, in order. */
    sockets: WebSocketRoute[];
    /** Every frame the client has sent, parsed, across all sockets. */
    frames: Array<{ action?: string; channels?: string[] }>;
    /** Drop the currently-open socket as a server restart would. */
    dropCurrent: () => Promise<void>;
    /** Stop answering pings, so the client's heartbeat has to notice. */
    setAnswerPings: (answer: boolean) => void;
}

/**
 * Stand in for the Go hub: accept the upgrade, answer subscribes, and reply
 * to the application-level ping with the pong the client's heartbeat waits
 * for (browsers cannot send protocol ping frames, so the keepalive rides on
 * ordinary messages — see apps/api/internal/ws/client.go).
 */
async function installFakeHub(page: Page): Promise<FakeHub> {
    // Seed a session token before the app boots. useWebSocket refuses to open
    // a socket without one — it sets status "offline" and falls back to
    // polling — so with no token the badge never reaches "connected" and every
    // assertion in this file times out.
    //
    // This became necessary when the provider stopped forging
    // `mock_jwt_${address}` (nester#1230) and started reading the real token
    // store. The forged value was always truthy, which is why the harness got
    // away with never authenticating.
    await page.addInitScript(() => {
        try {
            window.localStorage.setItem('nester_auth_token', 'e2e-harness-token');
        } catch {
            // Storage unavailable — the assertions below will surface it.
        }
    });

    const hub: FakeHub = {
        sockets: [],
        frames: [],
        dropCurrent: async () => {
            const socket = hub.sockets[hub.sockets.length - 1];
            await socket?.close({ code: 1006, reason: 'server restart' });
        },
        setAnswerPings: (answer: boolean) => {
            answerPings = answer;
        },
    };
    let answerPings = true;

    await page.routeWebSocket(WS_URL, (ws) => {
        hub.sockets.push(ws);

        ws.onMessage((message) => {
            let frame: { action?: string; channels?: string[] };
            try {
                frame = JSON.parse(String(message));
            } catch {
                return;
            }
            hub.frames.push(frame);

            if (frame.action === 'ping' && answerPings) {
                ws.send(JSON.stringify({ channel: '', event: 'pong', data: null }));
            }
        });
    });

    return hub;
}

const badge = (page: Page) => page.locator('main').getByTestId('connection-status');

test.describe('WebSocket reconnection', () => {
    // Reconnection is a waiting game: the bounded-retry schedule alone runs
    // ~1/2/4/8/16s before the client gives up. CI runners can take up to 90s
    // to cycle through all attempts. A 180s timeout ensures the full
    // reconnection cycle completes deterministically.
    test.setTimeout(180_000);

    test.beforeEach(async ({ page }) => {
        await page.addInitScript(() => {
            try {
                window.localStorage.setItem('nester_auth_token', 'e2e-harness-token');
            } catch {
                // Storage unavailable
            }
        });
    });

    test('shows the reconnecting indicator when the socket closes, and clears it when it reopens', async ({
        page,
    }) => {
        const hub = await installFakeHub(page);
        await page.goto(HARNESS);

        // 1. Live.
        await expect(badge(page)).toHaveAttribute('data-status', 'connected');
        await expect(badge(page)).toContainText('Live');

        // 2. The server goes away.
        await hub.dropCurrent();
        await expect(badge(page)).toHaveAttribute('data-status', 'reconnecting');
        await expect(badge(page)).toContainText('Reconnecting');

        // 3. It comes back on its own — the client reconnects without help.
        await expect(badge(page)).toHaveAttribute('data-status', 'connected', {
            timeout: 15_000,
        });
        await expect(badge(page)).toContainText('Live');
        expect(hub.sockets.length).toBeGreaterThan(1);
    });

    test('re-subscribes to its channels on the new connection', async ({ page }) => {
        const hub = await installFakeHub(page);
        await page.goto(HARNESS);
        await expect(badge(page)).toHaveAttribute('data-status', 'connected');

        const subscribesBefore = hub.frames.filter((f) => f.action === 'subscribe').length;

        await hub.dropCurrent();
        await expect(badge(page)).toHaveAttribute('data-status', 'connected', {
            timeout: 15_000,
        });

        // The hub's subscription table lives on the connection, so a
        // reconnected client that does not re-subscribe receives nothing —
        // and looks connected while doing so.
        await expect
            .poll(() => hub.frames.filter((f) => f.action === 'subscribe').length, {
                timeout: 10_000,
            })
            .toBeGreaterThan(subscribesBefore);
    });

    test('never renders a stale value with live styling', async ({ page }) => {
        const hub = await installFakeHub(page);
        await page.goto(HARNESS);

        const value = page.getByTestId('live-value');
        await expect(badge(page)).toHaveAttribute('data-status', 'connected');
        await expect(value).toHaveAttribute('data-stale', 'false');

        await hub.dropCurrent();

        // The figure is still on screen — blanking it would be worse — but it
        // is visibly marked as not current, and says so to a screen reader.
        await expect(value).toHaveAttribute('data-stale', 'true');
        await expect(value).toBeVisible();
        await expect(value).toContainText('not live');
    });

    test('reports when the data was last updated while not live', async ({ page }) => {
        const hub = await installFakeHub(page);
        await page.goto(HARNESS);
        await expect(badge(page)).toHaveAttribute('data-status', 'connected');

        // No freshness caveat while the feed is live.
        await expect(page.getByTestId('connection-last-updated')).toHaveCount(0);

        await hub.dropCurrent();

        await expect(page.getByTestId('connection-last-updated')).toBeVisible();
        await expect(page.getByTestId('connection-last-updated')).toContainText('updated');
    });

    test('gives up after a bounded number of attempts instead of retrying forever', async ({
        page,
    }) => {
        await page.routeWebSocket(WS_URL, (ws) => {
            // Refuse every connection: close as soon as it is opened.
            void ws.close({ code: 1006 });
        });
        await page.goto(HARNESS);

        // First confirm that reconnection attempts have begun
        await expect(badge(page)).toHaveAttribute('data-status', 'reconnecting', {
            timeout: 10_000,
        });

        // Default schedule is 5 attempts at ~1/2/4/8/16s (jitter shortens
        // each), after which the client stops and says so rather than
        // looping in the background.
        await expect(badge(page)).toHaveAttribute('data-status', 'offline', {
            timeout: 60_000,
        });
        await expect(badge(page)).toContainText('Disconnected', {
            timeout: 10_000,
        });
    });

    test('offers a way back once it has given up', async ({ page }) => {
        let refuse = true;
        await page.routeWebSocket(WS_URL, (ws) => {
            if (refuse) {
                void ws.close({ code: 1006 });
                return;
            }
            ws.onMessage((message) => {
                const frame = JSON.parse(String(message)) as { action?: string };
                if (frame.action === 'ping') {
                    ws.send(JSON.stringify({ channel: '', event: 'pong', data: null }));
                }
            });
        });
        await page.goto(HARNESS);

        // First confirm that reconnection attempts have begun
        await expect(badge(page)).toHaveAttribute('data-status', 'reconnecting', {
            timeout: 10_000,
        });

        await expect(badge(page)).toHaveAttribute('data-status', 'offline', {
            timeout: 60_000,
        });

        // The server comes back; bounded retries mean nothing notices on its
        // own, so the user needs an affordance that is not "reload the page".
        refuse = false;
        const retryBtn = page.getByTestId('connection-retry');
        await expect(retryBtn).toBeVisible({ timeout: 15_000 });
        await retryBtn.click();

        await expect(badge(page)).toHaveAttribute('data-status', 'connected', {
            timeout: 15_000,
        });
    });

    test('tears down a blackholed socket when the heartbeat goes unanswered', async ({
        page,
    }) => {
        const hub = await installFakeHub(page);
        await page.goto(HARNESS);
        await expect(badge(page)).toHaveAttribute('data-status', 'connected');

        // The socket stays open at the transport level but the peer stops
        // responding — the laptop-sleep / idle-proxy case. Only the
        // heartbeat can catch this; TCP would take minutes.
        hub.setAnswerPings(false);

        // The harness pings every 1s and allows 2s for the pong (see
        // lib/e2e-harness.ts), so detection lands within ~3s of the last
        // answered ping. Production uses 30s/10s; asserting against those
        // would be a test about waiting, not about the teardown.
        await expect(badge(page)).not.toHaveAttribute('data-status', 'connected', {
            timeout: 15_000,
        });
    });
});
