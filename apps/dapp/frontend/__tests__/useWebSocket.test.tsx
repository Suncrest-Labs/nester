import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { act, renderHook } from "@testing-library/react";
import {
    useWebSocket,
    getReconnectDelay,
    getJitteredReconnectDelay,
    BACKOFF_JITTER_RATIO,
    MAX_BACKOFF_MS,
} from "@/hooks/useWebSocket";

// ---------------------------------------------------------------------------
// Minimal controllable WebSocket mock
// ---------------------------------------------------------------------------

type Handler = ((ev: unknown) => void) | null;

class MockWebSocket {
    static CONNECTING = 0;
    static OPEN = 1;
    static CLOSING = 2;
    static CLOSED = 3;
    static instances: MockWebSocket[] = [];

    onopen: Handler = null;
    onclose: Handler = null;
    onerror: Handler = null;
    onmessage: Handler = null;
    readyState = MockWebSocket.OPEN;
    sent: string[] = [];

    constructor(public url: string) {
        MockWebSocket.instances.push(this);
    }

    send(data: string) {
        this.sent.push(data);
    }

    close() {
        this.readyState = MockWebSocket.CLOSED;
        this.onclose?.({});
    }

    // Test helpers
    open() {
        this.readyState = MockWebSocket.OPEN;
        this.onopen?.({});
    }

    receive(obj: unknown) {
        this.onmessage?.({ data: JSON.stringify(obj) });
    }

    /** Frames sent by the client, parsed. */
    frames(): Array<{ action?: string; channels?: string[] }> {
        return this.sent.map((m) => JSON.parse(m));
    }

    static last() {
        return MockWebSocket.instances[MockWebSocket.instances.length - 1];
    }

    static reset() {
        MockWebSocket.instances = [];
    }
}

const WS_URL = "wss://example.test/ws";

/** Drive document.visibilityState, which jsdom exposes read-only. */
function setVisibility(state: "visible" | "hidden") {
    Object.defineProperty(document, "visibilityState", {
        configurable: true,
        get: () => state,
    });
    document.dispatchEvent(new Event("visibilitychange"));
}

beforeEach(() => {
    vi.useFakeTimers();
    MockWebSocket.reset();
    // @ts-expect-error — replace global for the hook under test
    global.WebSocket = MockWebSocket;
    setVisibility("visible");
    // Pin jitter to its ceiling so back-off timings are exact in tests that
    // are not themselves about jitter.
    vi.spyOn(Math, "random").mockReturnValue(1);
});

afterEach(() => {
    vi.restoreAllMocks();
    vi.clearAllTimers();
    vi.useRealTimers();
});

// ---------------------------------------------------------------------------
// Back-off schedule
// ---------------------------------------------------------------------------

describe("getReconnectDelay", () => {
    it("produces the exponential back-off schedule capped at 30s", () => {
        const schedule = [0, 1, 2, 3, 4, 5, 6].map((a) => getReconnectDelay(a));
        expect(schedule).toEqual([1000, 2000, 4000, 8000, 16000, 30000, 30000]);
    });

    it("never exceeds the maximum back-off", () => {
        expect(getReconnectDelay(20)).toBe(MAX_BACKOFF_MS);
    });

    it("respects a custom base interval", () => {
        expect(getReconnectDelay(0, 500)).toBe(500);
        expect(getReconnectDelay(2, 500)).toBe(2000);
    });

    it("treats negative attempts as the first attempt", () => {
        expect(getReconnectDelay(-3)).toBe(1000);
    });
});

// ---------------------------------------------------------------------------
// Jitter
//
// Without jitter, every client that was connected to a server when it
// restarted retries in the same instant and knocks it over again. These
// tests pin that the randomisation is really there and really bounded.
// ---------------------------------------------------------------------------

describe("getJitteredReconnectDelay", () => {
    it("actually randomises: different random draws give different delays", () => {
        const low = getJitteredReconnectDelay(3, 1000, () => 0);
        const mid = getJitteredReconnectDelay(3, 1000, () => 0.5);
        const high = getJitteredReconnectDelay(3, 1000, () => 1);

        expect(new Set([low, mid, high]).size).toBe(3);
        expect(low).toBeLessThan(mid);
        expect(mid).toBeLessThan(high);
    });

    it("spreads the delay across the jitter window below the exponential ceiling", () => {
        for (const attempt of [0, 1, 2, 3, 4, 5]) {
            const ceiling = getReconnectDelay(attempt);
            const floor = ceiling * (1 - BACKOFF_JITTER_RATIO);

            expect(getJitteredReconnectDelay(attempt, 1000, () => 0)).toBe(floor);
            expect(getJitteredReconnectDelay(attempt, 1000, () => 1)).toBe(ceiling);
        }
    });

    it("keeps every draw inside [ceiling/2, ceiling] and never exceeds the cap", () => {
        const draws = Array.from({ length: 200 }, () => Math.random());
        for (const attempt of [0, 2, 4, 9]) {
            const ceiling = getReconnectDelay(attempt);
            for (const r of draws) {
                const delay = getJitteredReconnectDelay(attempt, 1000, () => r);
                expect(delay).toBeGreaterThanOrEqual(ceiling * (1 - BACKOFF_JITTER_RATIO));
                expect(delay).toBeLessThanOrEqual(ceiling);
                expect(delay).toBeLessThanOrEqual(MAX_BACKOFF_MS);
            }
        }
    });

    it("never returns zero, so a flapping link cannot hot-loop", () => {
        expect(getJitteredReconnectDelay(0, 1000, () => 0)).toBeGreaterThan(0);
    });

    it("draws different delays for concurrent clients sharing a schedule", () => {
        // Simulate 50 clients all reconnecting from attempt 0 after a server
        // restart, each with its own random source.
        const seeds = Array.from({ length: 50 }, (_, i) => i / 50);
        const delays = seeds.map((s) => getJitteredReconnectDelay(0, 1000, () => s));
        expect(new Set(delays).size).toBeGreaterThan(1);
    });
});

// ---------------------------------------------------------------------------
// Wire protocol
// ---------------------------------------------------------------------------

describe("useWebSocket wire protocol", () => {
    const baseOpts = {
        url: WS_URL,
        getToken: () => "jwt-123",
        channels: ["user:abc", "vaults:global"],
        onEvent: () => {},
    };

    it("authenticates via the upgrade URL query string using the token getter", () => {
        renderHook(() => useWebSocket(baseOpts));
        expect(MockWebSocket.last().url).toBe(`${WS_URL}?token=jwt-123`);
    });

    it("calls the token getter before each connection attempt", () => {
        const getToken = vi.fn().mockReturnValue("jwt-initial");
        renderHook(() => useWebSocket({ ...baseOpts, getToken }));
        expect(getToken).toHaveBeenCalled();
        const initialCalls = getToken.mock.calls.length;

        act(() => MockWebSocket.last().close());
        act(() => vi.advanceTimersByTime(1000));
        expect(getToken.mock.calls.length).toBeGreaterThan(initialCalls);
    });

    it("reconnects when the token getter returns a different token", () => {
        let currentToken = "jwt-initial";
        const getToken = () => currentToken;
        const { rerender } = renderHook(() => useWebSocket({ ...baseOpts, getToken }));
        act(() => MockWebSocket.last().open());
        const before = MockWebSocket.instances.length;

        // Simulate token refresh by changing what the getter returns
        currentToken = "jwt-refreshed";
        rerender();

        // Should reconnect with the new token
        expect(MockWebSocket.instances.length).toBeGreaterThan(before);
        expect(MockWebSocket.last().url).toBe(`${WS_URL}?token=jwt-refreshed`);
    });

    it("uses stale token on offline without socket", () => {
        renderHook(() => useWebSocket({ ...baseOpts, url: "" }));
        // No socket should be created when url is empty
        expect(MockWebSocket.instances.length).toBe(0);
    });

    it("subscribes with the hub's {action, channels} frame", () => {
        renderHook(() => useWebSocket(baseOpts));
        act(() => MockWebSocket.last().open());

        const subs = MockWebSocket.last().frames().filter((f) => f.action === "subscribe");
        expect(subs).toHaveLength(1);
        expect(subs[0].channels).toEqual(["user:abc", "vaults:global"]);
    });

    it("translates hub events into the app's event shape", () => {
        const onEvent = vi.fn();
        renderHook(() => useWebSocket({ ...baseOpts, onEvent }));
        act(() => MockWebSocket.last().open());

        act(() =>
            MockWebSocket.last().receive({
                channel: "user:abc",
                event: "balance_updated",
                data: { asset: "USDC", newBalance: 42 },
                timestamp: "2026-08-21T00:00:00Z",
            })
        );

        expect(onEvent).toHaveBeenCalledWith({
            type: "balance_updated",
            channel: "user:abc",
            payload: { asset: "USDC", newBalance: 42 },
            timestamp: "2026-08-21T00:00:00Z",
        });
    });

    it("subscribes to newly requested channels without dropping the socket", () => {
        const { rerender } = renderHook(
            ({ channels }) => useWebSocket({ ...baseOpts, channels }),
            { initialProps: { channels: ["user:abc"] } }
        );
        act(() => MockWebSocket.last().open());
        const socketCount = MockWebSocket.instances.length;

        act(() => rerender({ channels: ["user:abc", "vaults:global"] }));

        expect(MockWebSocket.instances.length).toBe(socketCount); // no reconnect
        const subs = MockWebSocket.last().frames().filter((f) => f.action === "subscribe");
        expect(subs[subs.length - 1].channels).toEqual(["vaults:global"]);
    });

    it("unsubscribes from channels the caller no longer wants", () => {
        const { rerender } = renderHook(
            ({ channels }) => useWebSocket({ ...baseOpts, channels }),
            { initialProps: { channels: ["user:abc", "vaults:global"] } }
        );
        act(() => MockWebSocket.last().open());
        act(() => rerender({ channels: ["user:abc"] }));

        const unsubs = MockWebSocket.last().frames().filter((f) => f.action === "unsubscribe");
        expect(unsubs).toHaveLength(1);
        expect(unsubs[0].channels).toEqual(["vaults:global"]);
    });
});

// ---------------------------------------------------------------------------
// State transitions
// ---------------------------------------------------------------------------

describe("useWebSocket state transitions", () => {
    const baseOpts = {
        url: WS_URL,
        getToken: () => "jwt",
        channels: ["user:abc", "vaults:global"],
        onEvent: () => {},
    };

    it("reports 'connected' once the socket opens", () => {
        const { result } = renderHook(() => useWebSocket(baseOpts));
        act(() => MockWebSocket.last().open());
        expect(result.current.status).toBe("connected");
        expect(result.current.isConnected).toBe(true);
    });

    it("goes 'reconnecting' on close and reconnects after the back-off delay", () => {
        const { result } = renderHook(() => useWebSocket(baseOpts));
        act(() => MockWebSocket.last().open());

        const before = MockWebSocket.instances.length;
        act(() => MockWebSocket.last().close());
        expect(result.current.status).toBe("reconnecting");

        // With Math.random pinned to 1 the first back-off is its 1000ms ceiling.
        act(() => vi.advanceTimersByTime(999));
        expect(MockWebSocket.instances.length).toBe(before);
        act(() => vi.advanceTimersByTime(1));
        expect(MockWebSocket.instances.length).toBe(before + 1);
    });

    it("re-subscribes to every channel after reconnecting", () => {
        renderHook(() => useWebSocket(baseOpts));
        act(() => MockWebSocket.last().open());
        act(() => MockWebSocket.last().close());
        act(() => vi.advanceTimersByTime(1000));
        act(() => MockWebSocket.last().open());

        const subs = MockWebSocket.last().frames().filter((f) => f.action === "subscribe");
        expect(subs).toHaveLength(1);
        expect(subs[0].channels).toEqual(["user:abc", "vaults:global"]);
    });

    it("resets the attempt counter after a successful reconnect", () => {
        const { result } = renderHook(() =>
            useWebSocket({ ...baseOpts, reconnectAttempts: 5 })
        );
        act(() => MockWebSocket.last().open());

        act(() => MockWebSocket.last().close());
        act(() => vi.advanceTimersByTime(1000));
        act(() => MockWebSocket.last().open());
        expect(result.current.status).toBe("connected");

        // Next drop should again use the *first* back-off (1000ms ceiling).
        const before = MockWebSocket.instances.length;
        act(() => MockWebSocket.last().close());
        act(() => vi.advanceTimersByTime(999));
        expect(MockWebSocket.instances.length).toBe(before); // not yet
        act(() => vi.advanceTimersByTime(1));
        expect(MockWebSocket.instances.length).toBe(before + 1);
    });

    it("falls back to 'offline' polling after the retries are exhausted", () => {
        const onPoll = vi.fn().mockResolvedValue(undefined);
        const { result } = renderHook(() =>
            useWebSocket({ ...baseOpts, reconnectAttempts: 2, onPoll })
        );
        act(() => MockWebSocket.last().open());

        // Attempt 1
        act(() => MockWebSocket.last().close());
        act(() => vi.advanceTimersByTime(1000));
        // Attempt 2
        act(() => MockWebSocket.last().close());
        act(() => vi.advanceTimersByTime(2000));
        // Exhausted -> offline + polling
        act(() => MockWebSocket.last().close());
        expect(result.current.status).toBe("offline");

        act(() => vi.advanceTimersByTime(30_000));
        expect(onPoll).toHaveBeenCalled();
    });

    it("stops opening sockets once retries are exhausted", () => {
        renderHook(() => useWebSocket({ ...baseOpts, reconnectAttempts: 1 }));
        act(() => MockWebSocket.last().open());
        act(() => MockWebSocket.last().close());
        act(() => vi.advanceTimersByTime(1000));
        act(() => MockWebSocket.last().close());

        const settled = MockWebSocket.instances.length;
        act(() => vi.advanceTimersByTime(120_000));
        expect(MockWebSocket.instances.length).toBe(settled);
    });
});

// ---------------------------------------------------------------------------
// Heartbeat
// ---------------------------------------------------------------------------

describe("useWebSocket heartbeat", () => {
    const baseOpts = {
        url: WS_URL,
        getToken: () => "jwt",
        channels: ["user:abc"],
        onEvent: () => {},
    };

    it("sends a ping every 30s while connected", () => {
        renderHook(() => useWebSocket(baseOpts));
        act(() => MockWebSocket.last().open());

        act(() => vi.advanceTimersByTime(30_000));
        const pings = MockWebSocket.last().frames().filter((f) => f.action === "ping");
        expect(pings).toHaveLength(1);

        // Answer the pong so the link stays alive, then expect a second ping.
        act(() => MockWebSocket.last().receive({ event: "pong" }));
        act(() => vi.advanceTimersByTime(30_000));
        const pings2 = MockWebSocket.last().frames().filter((f) => f.action === "ping");
        expect(pings2).toHaveLength(2);
    });

    it("reconnects if no pong is received within the timeout", () => {
        const { result } = renderHook(() => useWebSocket(baseOpts));
        act(() => MockWebSocket.last().open());

        act(() => vi.advanceTimersByTime(30_000)); // ping sent
        act(() => vi.advanceTimersByTime(10_000)); // no pong -> close
        expect(result.current.status).toBe("reconnecting");
    });

    it("stays connected when a pong arrives in time", () => {
        const { result } = renderHook(() => useWebSocket(baseOpts));
        act(() => MockWebSocket.last().open());

        act(() => vi.advanceTimersByTime(30_000)); // ping
        act(() => MockWebSocket.last().receive({ event: "pong" }));
        act(() => vi.advanceTimersByTime(10_000)); // timeout would have fired
        expect(result.current.status).toBe("connected");
    });

    it("accepts the legacy {type:'pong'} frame shape", () => {
        const { result } = renderHook(() => useWebSocket(baseOpts));
        act(() => MockWebSocket.last().open());

        act(() => vi.advanceTimersByTime(30_000));
        act(() => MockWebSocket.last().receive({ type: "pong" }));
        act(() => vi.advanceTimersByTime(10_000));
        expect(result.current.status).toBe("connected");
    });

    it("does not forward heartbeat frames as domain events", () => {
        const onEvent = vi.fn();
        renderHook(() => useWebSocket({ ...baseOpts, onEvent }));
        act(() => MockWebSocket.last().open());

        act(() => MockWebSocket.last().receive({ event: "pong" }));
        act(() => MockWebSocket.last().receive({ type: "pong" }));
        expect(onEvent).not.toHaveBeenCalled();
    });
});

// ---------------------------------------------------------------------------
// Reconciliation and freshness
// ---------------------------------------------------------------------------

describe("useWebSocket reconciliation", () => {
    const baseOpts = {
        url: WS_URL,
        getToken: () => "jwt",
        channels: ["user:abc"],
        onEvent: () => {},
    };

    it("fetches current state over HTTP on every connect, not just the first", async () => {
        const onReconcile = vi.fn().mockResolvedValue(undefined);
        renderHook(() => useWebSocket({ ...baseOpts, onReconcile }));

        await act(async () => { MockWebSocket.last().open(); });
        expect(onReconcile).toHaveBeenCalledTimes(1);

        act(() => MockWebSocket.last().close());
        act(() => vi.advanceTimersByTime(1000));
        await act(async () => { MockWebSocket.last().open(); });
        expect(onReconcile).toHaveBeenCalledTimes(2);
    });

    it("falls back to onPoll when no dedicated reconcile fetcher is given", async () => {
        const onPoll = vi.fn().mockResolvedValue(undefined);
        renderHook(() => useWebSocket({ ...baseOpts, onPoll }));
        await act(async () => { MockWebSocket.last().open(); });
        expect(onPoll).toHaveBeenCalledTimes(1);
    });

    it("stamps lastUpdatedAt when a reconcile succeeds and when events arrive", async () => {
        const onReconcile = vi.fn().mockResolvedValue(undefined);
        const { result } = renderHook(() => useWebSocket({ ...baseOpts, onReconcile }));

        expect(result.current.lastUpdatedAt).toBeNull();

        await act(async () => { MockWebSocket.last().open(); });
        const afterReconcile = result.current.lastUpdatedAt;
        expect(afterReconcile).not.toBeNull();

        act(() => vi.advanceTimersByTime(5_000));
        act(() =>
            MockWebSocket.last().receive({
                channel: "user:abc",
                event: "balance_updated",
                data: { asset: "USDC", newBalance: 1 },
            })
        );
        expect(result.current.lastUpdatedAt!).toBeGreaterThan(afterReconcile!);
    });

    it("leaves lastUpdatedAt alone when the reconcile fetch fails", async () => {
        const onReconcile = vi.fn().mockRejectedValue(new Error("network"));
        const { result } = renderHook(() => useWebSocket({ ...baseOpts, onReconcile }));
        await act(async () => { MockWebSocket.last().open(); });
        expect(result.current.lastUpdatedAt).toBeNull();
    });
});

// ---------------------------------------------------------------------------
// Hidden tab
//
// A background tab that keeps retrying forever burns the user's battery and
// data, and hammers the server after a restart. It must park instead, and
// come back promptly when the user does.
// ---------------------------------------------------------------------------

describe("useWebSocket in a hidden tab", () => {
    const baseOpts = {
        url: WS_URL,
        getToken: () => "jwt",
        channels: ["user:abc"],
        onEvent: () => {},
    };

    it("does not schedule reconnects while the tab is hidden", () => {
        const { result } = renderHook(() => useWebSocket(baseOpts));
        act(() => MockWebSocket.last().open());

        act(() => setVisibility("hidden"));
        const before = MockWebSocket.instances.length;
        act(() => MockWebSocket.last().close());

        expect(result.current.status).toBe("reconnecting");

        // Ten minutes of a hidden tab: not one reconnect attempt.
        act(() => vi.advanceTimersByTime(600_000));
        expect(MockWebSocket.instances.length).toBe(before);
    });

    it("reconnects immediately when the tab becomes visible again", () => {
        renderHook(() => useWebSocket(baseOpts));
        act(() => MockWebSocket.last().open());

        act(() => setVisibility("hidden"));
        const before = MockWebSocket.instances.length;
        act(() => MockWebSocket.last().close());
        act(() => vi.advanceTimersByTime(600_000));
        expect(MockWebSocket.instances.length).toBe(before);

        act(() => setVisibility("visible"));
        expect(MockWebSocket.instances.length).toBe(before + 1);
    });

    it("skips the polling fallback while hidden", async () => {
        const onPoll = vi.fn().mockResolvedValue(undefined);
        renderHook(() => useWebSocket({ ...baseOpts, reconnectAttempts: 1, onPoll }));
        act(() => MockWebSocket.last().open());

        // Exhaust retries so the poll timer is running.
        act(() => MockWebSocket.last().close());
        act(() => vi.advanceTimersByTime(1000));
        act(() => MockWebSocket.last().close());

        // onPoll doubles as the reconcile fetcher, so it has already run once
        // for the initial connect. What must not happen is it ticking on.
        const callsBeforeHiding = onPoll.mock.calls.length;
        act(() => setVisibility("hidden"));
        act(() => vi.advanceTimersByTime(300_000));
        expect(onPoll.mock.calls.length).toBe(callsBeforeHiding);

        // …and it resumes once the tab is visible again.
        act(() => setVisibility("visible"));
        await act(async () => { await vi.advanceTimersByTimeAsync(30_000); });
        expect(onPoll.mock.calls.length).toBeGreaterThan(callsBeforeHiding);
    });

    it("reconciles over HTTP when returning to a tab whose socket stayed open", async () => {
        const onReconcile = vi.fn().mockResolvedValue(undefined);
        renderHook(() => useWebSocket({ ...baseOpts, onReconcile }));
        await act(async () => { MockWebSocket.last().open(); });
        expect(onReconcile).toHaveBeenCalledTimes(1);

        act(() => setVisibility("hidden"));
        await act(async () => { setVisibility("visible"); });

        expect(onReconcile).toHaveBeenCalledTimes(2);
    });

    it("reconnects when the browser reports the network is back", () => {
        renderHook(() => useWebSocket({ ...baseOpts, reconnectAttempts: 1 }));
        act(() => MockWebSocket.last().open());
        act(() => MockWebSocket.last().close());
        act(() => vi.advanceTimersByTime(1000));
        act(() => MockWebSocket.last().close());

        const exhausted = MockWebSocket.instances.length;
        act(() => window.dispatchEvent(new Event("online")));
        expect(MockWebSocket.instances.length).toBe(exhausted + 1);
    });
});
