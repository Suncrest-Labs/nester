"use client";

import React, { useCallback, useEffect, useState } from "react";
import { AlertTriangle, Clock, PauseCircle, ServerCrash } from "lucide-react";

export interface PublicSystemStatus {
    status: "ok" | "degraded" | "paused";
    deposits_paused: boolean;
    withdrawals_paused: boolean;
    pause_reason?: string;
    indexer_lag_seconds: number;
    indexer_lag_ledgers?: number;
    indexer_stale: boolean;
    rpc_healthy: boolean;
    updated_at?: string;
}

export type DegradedBannerKind =
    | "api_unreachable"
    | "withdrawals_paused"
    | "deposits_paused"
    | "indexer_lag";

export interface DegradedBannerState {
    kind: DegradedBannerKind;
    message: string;
}

export const INDEXER_LAG_BANNER_THRESHOLD_SECONDS = 120;
const DEFAULT_POLL_INTERVAL_MS = 30_000;

export function deriveDegradedBannerState(
    status: PublicSystemStatus | null,
    apiUnreachable: boolean,
): DegradedBannerState | null {
    if (apiUnreachable) {
        return {
            kind: "api_unreachable",
            message:
                "We're having trouble reaching Nester servers. On-chain funds are unaffected.",
        };
    }

    if (!status) {
        return null;
    }

    if (status.withdrawals_paused) {
        return {
            kind: "withdrawals_paused",
            message:
                "Withdrawals are temporarily paused. Your funds remain on-chain.",
        };
    }

    if (status.deposits_paused) {
        return {
            kind: "deposits_paused",
            message:
                "Deposits are temporarily paused for maintenance. Existing funds and withdrawals are unaffected.",
        };
    }

    if (
        status.indexer_stale ||
        status.indexer_lag_seconds > INDEXER_LAG_BANNER_THRESHOLD_SECONDS ||
        !status.rpc_healthy
    ) {
        const minutes = Math.max(1, Math.ceil((status.indexer_lag_seconds || 120) / 60));
        return {
            kind: "indexer_lag",
            message: `Displayed balances may be up to ${minutes} ${minutes === 1 ? "minute" : "minutes"} behind the chain.`,
        };
    }

    return null;
}

export interface DegradedModeBannerProps {
    pollIntervalMs?: number;
    statusEndpoint?: string;
}

export function DegradedModeBanner({
    pollIntervalMs = DEFAULT_POLL_INTERVAL_MS,
    statusEndpoint,
}: DegradedModeBannerProps = {}) {
    const [status, setStatus] = useState<PublicSystemStatus | null>(null);
    const [apiUnreachable, setApiUnreachable] = useState(false);

    const endpoint =
        statusEndpoint ??
        `${(process.env.NEXT_PUBLIC_API_URL ?? "").replace(/\/+$/, "")}/api/v1/status`;

    const fetchStatus = useCallback(async () => {
        if (typeof fetch !== "function") return;
        try {
            const res = await fetch(endpoint, {
                method: "GET",
                headers: { Accept: "application/json" },
            });
            if (!res.ok) {
                setApiUnreachable(true);
                return;
            }
            const body = (await res.json()) as {
                success?: boolean;
                data?: PublicSystemStatus;
            } & Partial<PublicSystemStatus>;
            const payload = body?.data ?? (body as PublicSystemStatus);
            setStatus(payload);
            setApiUnreachable(false);
        } catch {
            setApiUnreachable(true);
        }
    }, [endpoint]);

    useEffect(() => {
        void fetchStatus();
        if (pollIntervalMs <= 0) return;
        const timer = setInterval(() => {
            void fetchStatus();
        }, pollIntervalMs);
        return () => clearInterval(timer);
    }, [fetchStatus, pollIntervalMs]);

    const banner = deriveDegradedBannerState(status, apiUnreachable);
    if (!banner) return null;

    const stylesByKind: Record<DegradedBannerKind, string> = {
        api_unreachable:
            "bg-red-500/10 border-red-500/30 text-red-700 dark:text-red-300",
        withdrawals_paused:
            "bg-red-500/10 border-red-500/30 text-red-700 dark:text-red-300",
        deposits_paused:
            "bg-amber-500/10 border-amber-500/30 text-amber-800 dark:text-amber-300",
        indexer_lag:
            "bg-amber-500/10 border-amber-500/30 text-amber-800 dark:text-amber-300",
    };

    const Icon =
        banner.kind === "api_unreachable"
            ? ServerCrash
            : banner.kind === "indexer_lag"
              ? Clock
              : banner.kind === "withdrawals_paused"
                ? AlertTriangle
                : PauseCircle;

    return (
        <div
            role="status"
            aria-live="polite"
            data-testid="degraded-mode-banner"
            data-banner-kind={banner.kind}
            className={`w-full border-b px-4 py-2 text-xs font-medium flex items-center justify-center gap-2 ${stylesByKind[banner.kind]}`}
        >
            <Icon className="h-3.5 w-3.5 shrink-0" />
            <span>{banner.message}</span>
        </div>
    );
}
