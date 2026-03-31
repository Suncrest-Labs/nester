"use client";

import { useEffect, useState } from "react";
import { AlertTriangle, RefreshCw, Wallet } from "lucide-react";
import {
    classifyHttpStatus,
    getHttpErrorCopy,
    type HttpErrorKind,
} from "@/lib/http-errors";

export function PageError({
    title = "This page couldn't load",
    description = "Something went wrong while rendering this view.",
    onRetry,
}: {
    title?: string;
    description?: string;
    onRetry?: () => void;
}) {
    return (
        <div className="flex min-h-[320px] flex-col items-center justify-center rounded-2xl border border-dashed border-border bg-secondary/20 px-6 py-16 text-center">
            <div className="mb-4 flex h-12 w-12 items-center justify-center rounded-2xl bg-white shadow-sm">
                <AlertTriangle className="h-6 w-6 text-amber-500" />
            </div>
            <h2 className="font-heading text-lg font-light text-foreground">
                {title}
            </h2>
            <p className="mt-2 max-w-md text-sm text-muted-foreground">
                {description}
            </p>
            {onRetry && (
                <button
                    type="button"
                    onClick={onRetry}
                    className="mt-6 inline-flex min-h-11 items-center gap-2 rounded-full border border-border bg-white px-5 py-2.5 text-sm font-medium text-foreground transition-colors hover:bg-secondary"
                >
                    <RefreshCw className="h-4 w-4" />
                    Try again
                </button>
            )}
        </div>
    );
}

export function WidgetError({
    title,
    description = "This widget failed to render.",
    onRetry,
}: {
    title: string;
    description?: string;
    onRetry?: () => void;
}) {
    return (
        <div className="flex flex-col items-center justify-center rounded-2xl border border-border bg-secondary/15 px-4 py-10 text-center">
            <AlertTriangle className="mb-2 h-5 w-5 text-amber-500/90" />
            <p className="text-sm font-medium text-foreground">{title}</p>
            <p className="mt-1 text-xs text-muted-foreground">{description}</p>
            {onRetry && (
                <button
                    type="button"
                    onClick={onRetry}
                    className="mt-4 text-xs font-medium text-foreground/70 underline-offset-2 hover:text-foreground hover:underline"
                >
                    Retry
                </button>
            )}
        </div>
    );
}

export function ApiErrorState({
    status,
    onRetry,
    onReconnect,
    className,
}: {
    status: number | undefined;
    onRetry?: () => void;
    onReconnect?: () => void;
    className?: string;
}) {
    const kind: HttpErrorKind = classifyHttpStatus(status);
    const copy = getHttpErrorCopy(kind);
    const totalWait = copy.rateLimitSeconds ?? 30;
    const [secondsLeft, setSecondsLeft] = useState(
        kind === "rate_limited" ? totalWait : 0
    );

    useEffect(() => {
        if (kind === "rate_limited") {
            setSecondsLeft(totalWait);
        } else {
            setSecondsLeft(0);
        }
    }, [kind, totalWait, status]);

    useEffect(() => {
        if (kind !== "rate_limited" || !onRetry) {
            return;
        }
        let remaining = totalWait;
        const id = setInterval(() => {
            remaining -= 1;
            setSecondsLeft(remaining);
            if (remaining <= 0) {
                clearInterval(id);
                onRetry();
            }
        }, 1000);
        return () => clearInterval(id);
    }, [kind, onRetry, totalWait, status]);

    return (
        <div
            className={`flex min-h-[280px] flex-col items-center justify-center rounded-2xl border border-border bg-white px-6 py-14 text-center shadow-sm ${className ?? ""}`}
        >
            <div className="mb-4 flex h-11 w-11 items-center justify-center rounded-xl bg-secondary">
                <AlertTriangle className="h-5 w-5 text-foreground/50" />
            </div>
            <h2 className="font-heading text-lg font-light text-foreground">
                {copy.title}
            </h2>
            <p className="mt-2 max-w-md text-sm text-muted-foreground">
                {copy.description}
            </p>
            {kind === "rate_limited" && copy.rateLimitSeconds ? (
                <p className="mt-3 font-mono text-xs text-muted-foreground">
                    Retrying in {secondsLeft}s…
                </p>
            ) : null}
            <div className="mt-6 flex flex-wrap items-center justify-center gap-3">
                {copy.showRetry && onRetry && kind !== "rate_limited" && (
                    <button
                        type="button"
                        onClick={onRetry}
                        className="inline-flex min-h-11 items-center gap-2 rounded-full border border-border bg-foreground px-5 py-2.5 text-sm font-medium text-background transition-opacity hover:opacity-90"
                    >
                        <RefreshCw className="h-4 w-4" />
                        Retry
                    </button>
                )}
                {copy.showReconnect && onReconnect && (
                    <button
                        type="button"
                        onClick={onReconnect}
                        className="inline-flex min-h-11 items-center gap-2 rounded-full border border-border bg-white px-5 py-2.5 text-sm font-medium text-foreground transition-colors hover:bg-secondary"
                    >
                        <Wallet className="h-4 w-4" />
                        Reconnect wallet
                    </button>
                )}
            </div>
        </div>
    );
}
