"use client";

import { type ReactNode } from "react";
import { useWebSocketContext } from "@/components/websocket-provider";
import { useRelativeAge } from "@/hooks/useRelativeAge";
import { cn } from "@/lib/utils";

interface LiveValueProps {
    /** The figure to render. */
    children: ReactNode;
    /** Extra classes applied in both states. */
    className?: string;
    /**
     * Accessible name for the figure, used to build the stale announcement
     * (e.g. "Total balance"). Defaults to a generic phrasing.
     */
    label?: string;
}

/**
 * Wraps a pushed value so it can never be mistaken for a live one once the
 * socket is down.
 *
 * When the connection is not live the figure is dimmed and underlined with a
 * dashed rule, and screen readers are told the value is not current. This is
 * the whole point of the feature: for a savings product the balance is the
 * reason the app was opened, so showing a remembered number in live styling
 * is a correctness bug, not a cosmetic one.
 */
export function LiveValue({ children, className, label = "This value" }: LiveValueProps) {
    const { isStale, lastUpdatedAt } = useWebSocketContext();

    // Ticks while stale. Formatting during render would freeze this string at
    // the moment the socket dropped, and the sr-only note below is the only
    // freshness signal a screen reader gets from the figure itself.
    const age = useRelativeAge(lastUpdatedAt, isStale);
    const staleNote = age
        ? `${label} is not live — last updated ${age}.`
        : `${label} is not live.`;

    return (
        <span
            data-testid="live-value"
            data-stale={isStale ? "true" : "false"}
            title={isStale ? staleNote : undefined}
            aria-describedby={undefined}
            className={cn(
                "inline-flex items-baseline transition-opacity",
                isStale &&
                    "opacity-55 decoration-dashed underline underline-offset-[6px] decoration-1 decoration-current/40",
                className
            )}
        >
            {children}
            {isStale && <span className="sr-only"> ({staleNote})</span>}
        </span>
    );
}
