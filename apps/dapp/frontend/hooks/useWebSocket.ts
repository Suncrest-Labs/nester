"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import {
  buildSocketUrl,
  isPongFrame,
  normalizeServerEvent,
  type ClientWireMessage,
  type WSConnectionStatus,
  type WSEvent,
} from "@/lib/ws-events";

interface UseWebSocketOptions {
  /** WebSocket server URL, e.g. wss://api.nester.fi/ws */
  url: string;
  /** Function to get JWT for authenticating the session. Called before each connection attempt. */
  getToken: () => string | null;
  /** Channels to subscribe to on connect, then again after reconnection */
  channels: string[];
  /** Called for every event message received */
  onEvent: (event: WSEvent) => void;
  /** How many reconnect attempts before giving up (default: 5) */
  reconnectAttempts?: number;
  /** Base interval for exponential back-off in ms (default: 1000) */
  reconnectInterval?: number;
  /** Interval in ms for REST polling fallback (default: 30 000) */
  pollInterval?: number;
  /** Optional: called to fetch latest snapshot via REST when polling */
  onPoll?: () => Promise<void>;
  /**
   * Called after every successful (re)connect to reconcile state over HTTP.
   * The hub replays buffered history on subscribe, but that buffer is
   * bounded and channel-scoped — it is not a snapshot. Treating the first
   * pushed message as the current state is how a reconnected client ends up
   * confidently displaying a wrong balance. Defaults to `onPoll`.
   */
  onReconcile?: () => Promise<void>;
  /** How often to send a heartbeat ping in ms (default: 30 000) */
  heartbeatInterval?: number;
  /** How long to wait for a pong before assuming the link is dead (default: 10 000) */
  heartbeatTimeout?: number;
}

export interface UseWebSocketReturn {
  isConnected: boolean;
  status: WSConnectionStatus;
  lastEvent: WSEvent | null;
  /**
   * Epoch ms of the last time displayed data was known-current — either an
   * event arrived or an HTTP reconcile completed. null until first sync.
   */
  lastUpdatedAt: number | null;
  subscribe: (channel: string) => void;
  unsubscribe: (channel: string) => void;
  disconnect: () => void;
  manualReconnect: () => void;
}

const MAX_RECONNECT_ATTEMPTS = 5;
const BASE_RECONNECT_INTERVAL_MS = 1000;
const POLL_INTERVAL_MS = 30_000;
const HEARTBEAT_INTERVAL_MS = 30_000;
const HEARTBEAT_TIMEOUT_MS = 10_000;

/** Maximum back-off between reconnect attempts (matches the issue spec). */
export const MAX_BACKOFF_MS = 30_000;

/**
 * Fraction of each back-off window that is randomised.
 *
 * 0.5 gives "equal jitter": half the delay is the deterministic exponential
 * floor, half is random. The floor keeps a flapping link from hot-looping;
 * the random half is what stops every client that was connected to a
 * restarted server from retrying in the same instant and knocking it over
 * again.
 */
export const BACKOFF_JITTER_RATIO = 0.5;

/**
 * Deterministic ceiling of the exponential back-off schedule.
 *
 * delay = base * 2^attempt, capped at MAX_BACKOFF_MS. With the default base of
 * 1000ms this yields the schedule 1s, 2s, 4s, 8s, 16s, 30s, 30s…
 *
 * This is the upper bound; the delay actually used is this value with jitter
 * subtracted — see getJitteredReconnectDelay.
 */
export function getReconnectDelay(
  attempt: number,
  base: number = BASE_RECONNECT_INTERVAL_MS,
): number {
  const safeAttempt = Math.max(0, attempt);
  return Math.min(base * 2 ** safeAttempt, MAX_BACKOFF_MS);
}

/**
 * The back-off delay actually used between reconnect attempts: the
 * exponential ceiling with `BACKOFF_JITTER_RATIO` of it randomised, so the
 * result lands somewhere in [ceiling/2, ceiling].
 *
 * `random` is injectable so the jitter can be asserted deterministically.
 */
export function getJitteredReconnectDelay(
  attempt: number,
  base: number = BASE_RECONNECT_INTERVAL_MS,
  random: () => number = Math.random,
): number {
  const ceiling = getReconnectDelay(attempt, base);
  const floor = ceiling * (1 - BACKOFF_JITTER_RATIO);
  return Math.round(floor + random() * (ceiling - floor));
}

function isDocumentHidden(): boolean {
  return (
    typeof document !== "undefined" && document.visibilityState === "hidden"
  );
}

export function useWebSocket({
  url,
  getToken,
  channels,
  onEvent,
  reconnectAttempts = MAX_RECONNECT_ATTEMPTS,
  reconnectInterval = BASE_RECONNECT_INTERVAL_MS,
  pollInterval = POLL_INTERVAL_MS,
  onPoll,
  onReconcile,
  heartbeatInterval = HEARTBEAT_INTERVAL_MS,
  heartbeatTimeout = HEARTBEAT_TIMEOUT_MS,
}: UseWebSocketOptions): UseWebSocketReturn {
  const [status, setStatus] = useState<WSConnectionStatus>("offline");
  const [lastEvent, setLastEvent] = useState<WSEvent | null>(null);
  const [lastUpdatedAt, setLastUpdatedAt] = useState<number | null>(null);

  // Keep stable references so interval/event callbacks don't go stale.
  const wsRef = useRef<WebSocket | null>(null);
  const attemptsRef = useRef(0);
  const isMountedRef = useRef(true);
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const pollTimerRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const heartbeatTimerRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const pongTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const channelsRef = useRef<string[]>(channels);
  const subscribedRef = useRef<Set<string>>(new Set());
  const onEventRef = useRef(onEvent);
  const onPollRef = useRef(onPoll);
  const onReconcileRef = useRef(onReconcile);
  const getTokenRef = useRef(getToken);
  /** Token the currently-open socket was authenticated with. */
  const connectedTokenRef = useRef<string | null>(null);
  const connectRef = useRef<() => void>(() => {});
  /**
   * Set when a reconnect was withheld because the tab is hidden. A hidden
   * tab must not sit in a retry loop for hours; it waits for the user to
   * come back and reconnects then.
   */
  const deferredWhileHiddenRef = useRef(false);
  /** Set by disconnect() so nothing auto-revives the socket. */
  const stoppedRef = useRef(false);

  // Keep refs in sync without triggering reconnects.
  useEffect(() => {
    onEventRef.current = onEvent;
  }, [onEvent]);
  useEffect(() => {
    onPollRef.current = onPoll;
  }, [onPoll]);
  useEffect(() => {
    onReconcileRef.current = onReconcile;
  }, [onReconcile]);
  useEffect(() => {
    getTokenRef.current = getToken;
  }, [getToken]);

  const markFresh = useCallback(() => {
    if (isMountedRef.current) setLastUpdatedAt(Date.now());
  }, []);

  const stopPoll = useCallback(() => {
    if (pollTimerRef.current !== null) {
      clearInterval(pollTimerRef.current);
      pollTimerRef.current = null;
    }
  }, []);

  const startPoll = useCallback(() => {
    if (!onPollRef.current || pollTimerRef.current !== null) return;
    pollTimerRef.current = setInterval(async () => {
      // A hidden tab has nothing to show; don't spend the user's data
      // on it. The visibility handler reconnects and reconciles the
      // moment the tab comes back.
      if (isDocumentHidden()) return;
      try {
        await onPollRef.current?.();
        markFresh();
      } catch {
        // Polling errors are non-fatal; keep trying.
      }
    }, pollInterval);
  }, [pollInterval, markFresh]);

  const stopHeartbeat = useCallback(() => {
    if (heartbeatTimerRef.current !== null) {
      clearInterval(heartbeatTimerRef.current);
      heartbeatTimerRef.current = null;
    }
    if (pongTimerRef.current !== null) {
      clearTimeout(pongTimerRef.current);
      pongTimerRef.current = null;
    }
  }, []);

  // Send a ping every `heartbeatInterval`; if no pong arrives within
  // `heartbeatTimeout`, assume the link is dead and force a reconnect by
  // closing the socket (which triggers onclose → back-off).
  //
  // This is the only thing that detects a blackholed socket promptly. TCP
  // will eventually notice, but "eventually" is minutes — minutes during
  // which the UI is showing a balance it has no basis for.
  const startHeartbeat = useCallback(
    (ws: WebSocket) => {
      stopHeartbeat();
      heartbeatTimerRef.current = setInterval(() => {
        if (ws.readyState !== WebSocket.OPEN) return;

        // A deadline is already pending from an earlier ping that has not
        // been answered. Rearming it here would push the deadline back
        // once per interval and it could never fire, so a peer that stops
        // answering would be detected only if the pong timeout were
        // shorter than the ping interval. Let the pending one run out;
        // onmessage clears it when a pong actually arrives.
        if (pongTimerRef.current !== null) return;

        const ping: ClientWireMessage = { action: "ping" };
        ws.send(JSON.stringify(ping));
        pongTimerRef.current = setTimeout(() => {
          pongTimerRef.current = null;
          // No pong in time — drop the connection so onclose reconnects.
          ws.close();
        }, heartbeatTimeout);
      }, heartbeatInterval);
    },
    [heartbeatInterval, heartbeatTimeout, stopHeartbeat],
  );

  const sendFrame = useCallback(
    (ws: WebSocket | null, frame: ClientWireMessage) => {
      if (!ws || ws.readyState !== WebSocket.OPEN) return false;
      ws.send(JSON.stringify(frame));
      return true;
    },
    [],
  );

  /**
   * (Re)subscribe to every channel the caller currently wants. Called on
   * each open, because the hub's subscription table lives on the
   * connection — a reconnected socket starts with none.
   */
  const sendSubscriptions = useCallback(
    (ws: WebSocket) => {
      const wanted = channelsRef.current;
      subscribedRef.current = new Set(wanted);
      if (wanted.length === 0) return;
      sendFrame(ws, { action: "subscribe", channels: [...wanted] });
    },
    [sendFrame],
  );

  /**
   * Pull current state over HTTP after a reconnect rather than trusting
   * whatever the socket pushes first.
   */
  const reconcile = useCallback(async () => {
    const fn = onReconcileRef.current ?? onPollRef.current;
    if (!fn) return;
    try {
      await fn();
      markFresh();
    } catch {
      // A failed reconcile leaves lastUpdatedAt where it was, so the UI
      // keeps reporting the older (honest) freshness timestamp.
    }
  }, [markFresh]);

  const scheduleReconnect = useCallback(() => {
    if (!isMountedRef.current || stoppedRef.current) return;

    if (isDocumentHidden()) {
      // Hidden tab: stop here. No timer, no attempt counter churn.
      deferredWhileHiddenRef.current = true;
      setStatus("reconnecting");
      return;
    }

    if (attemptsRef.current >= reconnectAttempts) {
      setStatus("offline");
      startPoll();
      return;
    }

    // Back-off uses the current attempt index (0-based) before
    // incrementing: ~1s, ~2s, ~4s, ~8s, ~16s, ~30s… each randomised
    // downward by up to half so clients don't retry in lockstep.
    const delay = getJitteredReconnectDelay(
      attemptsRef.current,
      reconnectInterval,
    );
    attemptsRef.current += 1;
    setStatus("reconnecting");
    if (reconnectTimerRef.current !== null)
      clearTimeout(reconnectTimerRef.current);
    reconnectTimerRef.current = setTimeout(() => connectRef.current(), delay);
  }, [reconnectAttempts, reconnectInterval, startPoll]);

  const connect = useCallback(() => {
    if (!isMountedRef.current || stoppedRef.current) return;

    deferredWhileHiddenRef.current = false;
    if (reconnectTimerRef.current !== null) {
      clearTimeout(reconnectTimerRef.current);
      reconnectTimerRef.current = null;
    }

    // Close any existing socket cleanly.
    stopHeartbeat();
    if (wsRef.current) {
      wsRef.current.onopen = null;
      wsRef.current.onclose = null;
      wsRef.current.onerror = null;
      wsRef.current.onmessage = null;
      wsRef.current.close();
      wsRef.current = null;
    }

    if (!url) {
      // No socket configured (local dev without NEXT_PUBLIC_WS_URL).
      setStatus("offline");
      startPoll();
      return;
    }

    // Get the current token before connecting (not a cached/stale one)
    const socketToken = getTokenRef.current();
    if (!socketToken) {
      setStatus("offline");
      startPoll();
      return;
    }

    let ws: WebSocket;
    try {
      // The hub authenticates from the query string during the upgrade
      // handshake — the browser WebSocket API cannot set headers.
      ws = new WebSocket(buildSocketUrl(url, socketToken));
    } catch {
      // URL may be invalid in dev; fall back to polling.
      setStatus("offline");
      startPoll();
      return;
    }

    wsRef.current = ws;
    connectedTokenRef.current = socketToken;

    ws.onopen = () => {
      if (!isMountedRef.current) return;
      attemptsRef.current = 0;
      setStatus("connected");
      stopPoll();

      sendSubscriptions(ws);

      // Begin liveness pings now that the link is open.
      startHeartbeat(ws);

      // Reconcile over HTTP instead of assuming the first pushed
      // message carries the whole picture.
      void reconcile();
    };

    ws.onmessage = (evt: MessageEvent) => {
      if (!isMountedRef.current) return;
      let frame: unknown;
      try {
        frame = JSON.parse(evt.data as string);
      } catch {
        return; // Ignore malformed frames.
      }

      // Heartbeat frames are transport bookkeeping, not domain events.
      if (isPongFrame(frame)) {
        if (pongTimerRef.current !== null) {
          clearTimeout(pongTimerRef.current);
          pongTimerRef.current = null;
        }
        return;
      }

      const event = normalizeServerEvent(frame);
      if (!event) return;

      setLastEvent(event);
      setLastUpdatedAt(Date.now());
      onEventRef.current(event);
    };

    ws.onclose = () => {
      stopHeartbeat();
      if (!isMountedRef.current) return;
      scheduleReconnect();
    };

    ws.onerror = () => {
      // onerror is always followed by onclose; handle backoff there.
      ws.close();
    };
  }, [
    url,
    sendSubscriptions,
    startPoll,
    stopPoll,
    startHeartbeat,
    stopHeartbeat,
    scheduleReconnect,
    reconcile,
  ]);

  useEffect(() => {
    connectRef.current = connect;
  }, [connect]);

  // Cleanup helper — tears down socket and timers without triggering reconnect.
  const teardown = useCallback(() => {
    if (reconnectTimerRef.current !== null) {
      clearTimeout(reconnectTimerRef.current);
      reconnectTimerRef.current = null;
    }
    deferredWhileHiddenRef.current = false;
    stopPoll();
    stopHeartbeat();
    subscribedRef.current = new Set();
    if (wsRef.current) {
      wsRef.current.onopen = null;
      wsRef.current.onclose = null;
      wsRef.current.onerror = null;
      wsRef.current.onmessage = null;
      wsRef.current.close();
      wsRef.current = null;
    }
  }, [stopPoll, stopHeartbeat]);

  const disconnect = useCallback(() => {
    stoppedRef.current = true;
    attemptsRef.current = reconnectAttempts; // prevent auto-reconnect
    teardown();
    if (isMountedRef.current) setStatus("offline");
  }, [reconnectAttempts, teardown]);

  const manualReconnect = useCallback(() => {
    stoppedRef.current = false;
    attemptsRef.current = 0;
    teardown();
    connect();
  }, [connect, teardown]);

  const subscribe = useCallback(
    (channel: string) => {
      subscribedRef.current.add(channel);
      sendFrame(wsRef.current, { action: "subscribe", channels: [channel] });
    },
    [sendFrame],
  );

  const unsubscribe = useCallback(
    (channel: string) => {
      subscribedRef.current.delete(channel);
      sendFrame(wsRef.current, { action: "unsubscribe", channels: [channel] });
    },
    [sendFrame],
  );

  // Reconcile the live subscription set when the caller's channel list
  // changes (e.g. the wallet resolves after mount and vault channels
  // appear) without dropping the connection.
  useEffect(() => {
    channelsRef.current = channels;
    const ws = wsRef.current;
    if (!ws || ws.readyState !== WebSocket.OPEN) return;

    const wanted = new Set(channels);
    const added = channels.filter((c) => !subscribedRef.current.has(c));
    const removed = [...subscribedRef.current].filter((c) => !wanted.has(c));

    if (added.length > 0)
      sendFrame(ws, { action: "subscribe", channels: added });
    if (removed.length > 0)
      sendFrame(ws, { action: "unsubscribe", channels: removed });
    subscribedRef.current = wanted;
  }, [channels, sendFrame]);

  // The token is part of the upgrade URL, so a new token means the open
  // socket is authenticated as somebody else (or as nobody). Reconnect.
  useEffect(() => {
    if (stoppedRef.current) return;
    if (connectedTokenRef.current === null) return; // never connected yet
    // Identity check on which credential the open socket used — not an
    // authentication decision, so a constant-time compare is not called for.
    // eslint-disable-next-line security/detect-possible-timing-attacks
    const currentToken = getTokenRef.current();
    if (connectedTokenRef.current === currentToken) return;
    attemptsRef.current = 0;
    connectRef.current();
    // Deliberately no dependency array: the token is read from a ref, so
    // there is no value React could watch. With `[]` this ran once on mount
    // and never again, which meant a refreshed token kept the old socket
    // open — authenticated as the previous credential — until something else
    // happened to drop it. The guard above makes the re-run cheap: it
    // returns immediately unless the token actually changed.
  });

  // Suspend/resume. Two things bring a dropped connection back:
  //
  //  - the tab becoming visible again, which also covers the laptop-sleep
  //    case where every timer fired late or not at all, and
  //  - the browser reporting the network is back.
  //
  // Both reset the attempt counter: the previous failures described a
  // network that no longer exists.
  useEffect(() => {
    const resume = () => {
      if (!isMountedRef.current || stoppedRef.current) return;
      if (isDocumentHidden()) return;
      const ws = wsRef.current;
      if (ws && ws.readyState === WebSocket.OPEN) return;
      if (ws && ws.readyState === WebSocket.CONNECTING) return;
      attemptsRef.current = 0;
      connectRef.current();
    };

    const onVisibility = () => {
      if (isDocumentHidden()) return;
      // Always reconcile on return: even a socket that stayed open may
      // have missed events while the tab was throttled.
      if (
        deferredWhileHiddenRef.current ||
        wsRef.current?.readyState !== WebSocket.OPEN
      ) {
        resume();
      } else {
        void reconcile();
      }
    };

    document.addEventListener("visibilitychange", onVisibility);
    window.addEventListener("online", resume);
    return () => {
      document.removeEventListener("visibilitychange", onVisibility);
      window.removeEventListener("online", resume);
    };
  }, [reconcile]);

  // Establish the connection on mount.
  useEffect(() => {
    isMountedRef.current = true;
    stoppedRef.current = false;
    connect();
    return () => {
      isMountedRef.current = false;
      teardown();
    };
    // connect / teardown are stable — only run on mount/unmount.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return {
    isConnected: status === "connected",
    status,
    lastEvent,
    lastUpdatedAt,
    subscribe,
    unsubscribe,
    disconnect,
    manualReconnect,
  };
}
