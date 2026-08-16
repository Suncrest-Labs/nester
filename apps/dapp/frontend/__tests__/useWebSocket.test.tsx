import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { act, renderHook } from "@testing-library/react";
import {
    useWebSocket,
    getReconnectDelay,
    MAX_BACKOFF_MS,
} from "@/hooks/useWebSocket";

// ---------------------------------------------------------------------------
// Minimal controllable WebSocket mock
// ---------------------------------------------------------------------------

type Handler = ((ev: unknown) => void) | null;

class MockWebSocket {
    static OPEN = 1;
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

    static last() {
        return MockWebSocket.instances[MockWebSocket.instances.length - 1];
    }

    static reset() {
        MockWebSocket.instances = [];
    }
}

const WS_URL = "wss://example.test/ws";

beforeEach(() => {
    vi.useFakeTimers();
    MockWebSocket.reset();
    // @ts-expect-error — replace global for the hook under test
    global.WebSocket = MockWebSocket;
});

afterEach(() => {
    vi.clearAllTimers();
    vi.useRealTimers();
});

// ---------------------------------------------------------------------------
// Back-off schedule
// ---------------------------------------------------------------------------

describe("getReconnectDelay", () => {
    it("produces the exponential back-off schedule capped at 30s (with jitter)", () => {
        // With full jitter, each delay lands in [raw * 0.5, raw]. The raw
        // schedule is 1s, 2s, 4s, 8s, 16s, 30s (capped), 30s (capped).
        const schedule = [0, 1, 2, 3, 4, 5, 6].map((a) => getReconnectDelay(a));

        // Attempt 0: raw 1000 → [500, 1000]
        expect(schedule[0]).toBeGreaterThanOrEqual(500);
        expect(schedule[0]).toBeLessThanOrEqual(1000);

        // Attempt 1: raw 2000 → [1000, 2000]
        expect(schedule[1]).toBeGreaterThanOrEqual(1000);
        expect(schedule[1]).toBeLessThanOrEqual(2000);

        // Attempt 2: raw 4000 → [2000, 4000]
        expect(schedule[2]).toBeGreaterThanOrEqual(2000);
        expect(schedule[2]).toBeLessThanOrEqual(4000);

        // Attempt 3: raw 8000 → [4000, 8000]
        expect(schedule[3]).toBeGreaterThanOrEqual(4000);
        expect(schedule[3]).toBeLessThanOrEqual(8000);

        // Attempt 4: raw 16000 → [8000, 16000]
        expect(schedule[4]).toBeGreaterThanOrEqual(8000);
        expect(schedule[4]).toBeLessThanOrEqual(16000);

        // Attempt 5+: capped at 30000 → [15000, 30000]
        expect(schedule[5]).toBeGreaterThanOrEqual(15000);
        expect(schedule[5]).toBeLessThanOrEqual(MAX_BACKOFF_MS);
        expect(schedule[6]).toBeGreaterThanOrEqual(15000);
        expect(schedule[6]).toBeLessThanOrEqual(MAX_BACKOFF_MS);
    });

    it("never exceeds the maximum back-off (including jitter)", () => {
        for (let attempt = 0; attempt < 30; attempt++) {
            expect(getReconnectDelay(attempt)).toBeLessThanOrEqual(MAX_BACKOFF_MS);
        }
    });

    it("respects a custom base interval (within jitter bounds)", () => {
        // raw delay at attempt 0 with base 500 is 500 → [250, 500]
        const custom = getReconnectDelay(0, 500);
        expect(custom).toBeGreaterThanOrEqual(250);
        expect(custom).toBeLessThanOrEqual(500);
    });

    it("treats negative attempts as the first attempt", () => {
        // Both attempt 0 and negative attempts use the same raw delay (1000ms),
        // so the result with jitter lands in [500, 1000].
        const negative = getReconnectDelay(-3);
        expect(negative).toBeGreaterThanOrEqual(500);
        expect(negative).toBeLessThanOrEqual(1000);
    });

    it("includes jitter so successive calls vary", () => {
        const runs = Array.from({ length: 30 }, () => getReconnectDelay(0));
        const unique = new Set(runs);
        // With a continuous jitter range, 30 calls must not all agree.
        expect(unique.size).toBeGreaterThanOrEqual(2);
    });
});

// ---------------------------------------------------------------------------
// State transitions
// ---------------------------------------------------------------------------

describe("useWebSocket state transitions", () => {
    const baseOpts = {
        url: WS_URL,
        token: "jwt",
        channels: ["user:abc", "vaults:global"],
        onEvent: () => {},
    };

    it("reports 'connected' once the socket opens", () => {
        const { result } = renderHook(() => useWebSocket(baseOpts));
        act(() => MockWebSocket.last().open());
        expect(result.current.status).toBe("connected");
        expect(result.current.isConnected).toBe(true);
    });

    it("subscribes to every channel exactly once on connect (no duplicates)", () => {
        renderHook(() => useWebSocket(baseOpts));
        act(() => MockWebSocket.last().open());

        const subs = MockWebSocket.last().sent.filter((m) =>
            m.includes('"subscribe"')
        );
        expect(subs).toHaveLength(2);
    });

    it("goes 'reconnecting' on close and reconnects after the back-off delay", () => {
        const { result } = renderHook(() => useWebSocket(baseOpts));
        act(() => MockWebSocket.last().open());

        const before = MockWebSocket.instances.length;
        act(() => MockWebSocket.last().close());
        expect(result.current.status).toBe("reconnecting");

        // First back-off is 1000ms.
        act(() => vi.advanceTimersByTime(1000));
        expect(MockWebSocket.instances.length).toBe(before + 1);
    });

    it("resets the attempt counter after a successful reconnect", () => {
        const { result } = renderHook(() =>
            useWebSocket({ ...baseOpts, reconnectAttempts: 5 })
        );
        act(() => MockWebSocket.last().open());

        // Drop once; the first back-off is 1000ms raw, ∈ [500, 1000] with
        // jitter, so advancing the full 1000ms always triggers the reconnect.
        act(() => MockWebSocket.last().close());
        act(() => vi.advanceTimersByTime(1000));
        act(() => MockWebSocket.last().open());
        expect(result.current.status).toBe("connected");

        // Next drop should again use the *first* back-off (raw 1000ms, min 500ms
        // with jitter), proving the attempt counter reset.
        const before = MockWebSocket.instances.length;
        act(() => MockWebSocket.last().close());

        // 499ms is below the jitter minimum (500ms) — the timer must not fire.
        act(() => vi.advanceTimersByTime(499));
        expect(MockWebSocket.instances.length).toBe(before); // not yet

        // Advancing to the full raw interval (1000ms) always covers the jitter
        // range (max is 1000ms), so the reconnect is guaranteed by then.
        act(() => vi.advanceTimersByTime(501));
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
});

// ---------------------------------------------------------------------------
// lastEventTime
// ---------------------------------------------------------------------------

describe("useWebSocket lastEventTime", () => {
    const baseOpts = {
        url: WS_URL,
        token: "jwt",
        channels: ["user:abc"],
        onEvent: () => {},
    };

    it("starts as null before any event is received", () => {
        const { result } = renderHook(() => useWebSocket(baseOpts));
        expect(result.current.lastEventTime).toBeNull();
    });

    it("records a timestamp when a domain event arrives", () => {
        const { result } = renderHook(() => useWebSocket(baseOpts));
        act(() => MockWebSocket.last().open());

        act(() => {
            MockWebSocket.last().receive({
                type: "balance_updated",
                channel: "user:test",
                payload: { asset: "XLM", newBalance: 100, previousBalance: 50 },
                timestamp: new Date().toISOString(),
            });
        });

        expect(result.current.lastEventTime).not.toBeNull();
        expect(typeof result.current.lastEventTime).toBe("number");
        expect(result.current.lastEventTime).toBeGreaterThan(0);
    });

    it("does not update lastEventTime for heartbeat frames", () => {
        const { result } = renderHook(() => useWebSocket(baseOpts));
        act(() => MockWebSocket.last().open());

        const spy = vi.spyOn(Date, "now").mockReturnValue(12345);
        act(() => {
            MockWebSocket.last().receive({ type: "pong" });
            MockWebSocket.last().receive({ type: "ping" });
        });
        expect(result.current.lastEventTime).toBeNull();
        spy.mockRestore();
    });
});

// ---------------------------------------------------------------------------
// Heartbeat
// ---------------------------------------------------------------------------

describe("useWebSocket heartbeat", () => {
    const baseOpts = {
        url: WS_URL,
        token: "jwt",
        channels: ["user:abc"],
        onEvent: () => {},
    };

    it("sends a ping every 30s while connected", () => {
        renderHook(() => useWebSocket(baseOpts));
        act(() => MockWebSocket.last().open());

        act(() => vi.advanceTimersByTime(30_000));
        const pings = MockWebSocket.last().sent.filter((m) => m.includes('"ping"'));
        expect(pings).toHaveLength(1);

        // Answer the pong so the link stays alive, then expect a second ping.
        act(() => MockWebSocket.last().receive({ type: "pong" }));
        act(() => vi.advanceTimersByTime(30_000));
        const pings2 = MockWebSocket.last().sent.filter((m) => m.includes('"ping"'));
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
        act(() => MockWebSocket.last().receive({ type: "pong" }));
        act(() => vi.advanceTimersByTime(10_000)); // timeout would have fired
        expect(result.current.status).toBe("connected");
    });

    it("does not forward heartbeat frames as domain events", () => {
        const onEvent = vi.fn();
        renderHook(() => useWebSocket({ ...baseOpts, onEvent }));
        act(() => MockWebSocket.last().open());

        act(() => MockWebSocket.last().receive({ type: "pong" }));
        act(() => MockWebSocket.last().receive({ type: "ping" }));
        expect(onEvent).not.toHaveBeenCalled();
    });
});
