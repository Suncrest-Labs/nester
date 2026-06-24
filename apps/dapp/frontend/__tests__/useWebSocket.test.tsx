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

        // Drop once, reconnect fires after 1s, then succeeds.
        act(() => MockWebSocket.last().close());
        act(() => vi.advanceTimersByTime(1000));
        act(() => MockWebSocket.last().open());
        expect(result.current.status).toBe("connected");

        // Next drop should again use the *first* back-off (1000ms), proving reset.
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
