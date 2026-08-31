"use client";

import { useEffect, useState } from "react";

/**
 * Short relative age, e.g. "just now", "3m ago", "2h ago".
 *
 * Deliberately coarse: the point is to tell the user roughly how much they
 * should trust the number, not to give a precise clock reading.
 */
export function formatRelativeAge(from: number, now: number = Date.now()): string {
    const seconds = Math.max(0, Math.floor((now - from) / 1000));
    if (seconds < 10) return "just now";
    if (seconds < 60) return `${seconds}s ago`;
    const minutes = Math.floor(seconds / 60);
    if (minutes < 60) return `${minutes}m ago`;
    const hours = Math.floor(minutes / 60);
    if (hours < 24) return `${hours}h ago`;
    return `${Math.floor(hours / 24)}d ago`;
}

export const DEFAULT_TICK_MS = 15_000;

/**
 * Current time, re-read on a timer so relative ages keep counting up.
 *
 * Ticks only while `active`, so a live connection costs nothing.
 */
export function useNow(active: boolean, intervalMs: number = DEFAULT_TICK_MS): number {
    const [now, setNow] = useState(() => Date.now());
    useEffect(() => {
        if (!active) return;
        setNow(Date.now());
        const id = setInterval(() => setNow(Date.now()), intervalMs);
        return () => clearInterval(id);
    }, [active, intervalMs]);
    return now;
}

/**
 * Relative age of `from` that keeps counting while `active`.
 *
 * The ticking is the whole point. A component that only subscribes to the
 * socket stops re-rendering the moment the socket goes quiet — which is
 * exactly when the age matters — so a timestamp formatted during render would
 * freeze at the instant staleness began and sit there reading "just now" while
 * the connection badge next to it climbs to "10m ago".
 *
 * Returns null when there is no timestamp to describe.
 */
export function useRelativeAge(
    from: number | null,
    active: boolean,
    intervalMs: number = DEFAULT_TICK_MS,
): string | null {
    const now = useNow(active, intervalMs);
    if (from === null) return null;
    return formatRelativeAge(from, now);
}
