"use client";

import Link from "next/link";
import { useEffect, useMemo, useRef, useState } from "react";
import { Bell, CheckCheck, ShieldAlert } from "lucide-react";
import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
import { cn } from "@/lib/utils";
import { useNotifications } from "@/components/notifications-provider";
import { NotificationActionLink } from "@/components/notification-action-link";

function formatRelativeTime(timestamp: string) {
    const diffMs = Date.now() - new Date(timestamp).getTime();
    const diffMin = Math.floor(diffMs / 60000);

    if (diffMin < 1) return "Just now";
    if (diffMin < 60) return `${diffMin}m ago`;

    const diffHours = Math.floor(diffMin / 60);
    if (diffHours < 24) return `${diffHours}h ago`;

    const diffDays = Math.floor(diffHours / 24);
    return `${diffDays}d ago`;
}

export function NotificationBell() {
    const [open, setOpen] = useState(false);
    const shouldReduceMotion = useReducedMotion();
    const triggerRef = useRef<HTMLButtonElement>(null);

    const {
        notifications,
        unreadCount,
        safetyCount,
        markAllAsRead,
        markAsRead,
        isDisconnected,
    } = useNotifications();

    const recent = useMemo(() => notifications.slice(0, 6), [notifications]);

    useEffect(() => {
        if (!open) return;

        const handleClick = () => setOpen(false);
        const handleKeyDown = (e: KeyboardEvent) => {
            if (e.key === "Escape") {
                setOpen(false);
                triggerRef.current?.focus();
            }
        };

        document.addEventListener("click", handleClick);
        document.addEventListener("keydown", handleKeyDown);

        return () => {
            document.removeEventListener("click", handleClick);
            document.removeEventListener("keydown", handleKeyDown);
        };
    }, [open]);

    return (
        <div className="relative" onClick={(e) => e.stopPropagation()}>
            <button
                ref={triggerRef}
                onClick={() => setOpen((prev) => !prev)}
                className={cn(
                    "relative inline-flex h-10 w-10 items-center justify-center rounded-full border transition-all hover:shadow-sm focus:outline-none focus:ring-2 focus:ring-foreground/20",
                    safetyCount > 0
                        ? "border-red-500/60 bg-red-50 text-red-600 dark:bg-red-950/50 dark:text-red-400"
                        : "border-border bg-white dark:bg-[#100F0F] text-foreground/70 hover:border-black/20 dark:hover:border-white/20"
                )}
                aria-label={`Notifications, ${unreadCount} unread${
                    safetyCount > 0 ? `, ${safetyCount} critical safety alerts` : ""
                }`}
                aria-expanded={open}
                aria-haspopup="true"
            >
                {safetyCount > 0 ? (
                    <ShieldAlert
                        className={cn(
                            "h-4 w-4 text-red-600 dark:text-red-400",
                            !shouldReduceMotion && "animate-pulse"
                        )}
                    />
                ) : (
                    <Bell className="h-4 w-4 text-foreground/70" />
                )}

                {unreadCount > 0 && (
                    <span
                        className={cn(
                            "absolute -right-1 -top-1 inline-flex min-w-5 items-center justify-center rounded-full px-1.5 py-0.5 text-[10px] font-bold text-white shadow-sm",
                            safetyCount > 0
                                ? cn("bg-red-600", !shouldReduceMotion && "animate-bounce")
                                : "bg-foreground text-background"
                        )}
                    >
                        {unreadCount > 9 ? "9+" : unreadCount}
                    </span>
                )}
            </button>

            <AnimatePresence>
                {open && (
                    <motion.div
                        initial={
                            shouldReduceMotion
                                ? { opacity: 0 }
                                : { opacity: 0, y: 8, scale: 0.96 }
                        }
                        animate={{ opacity: 1, y: 0, scale: 1 }}
                        exit={
                            shouldReduceMotion
                                ? { opacity: 0 }
                                : { opacity: 0, y: 8, scale: 0.96 }
                        }
                        transition={{ duration: 0.15 }}
                        role="region"
                        aria-label="Recent notifications"
                        className="overflow-hidden rounded-2xl border border-border bg-white dark:bg-[#100F0F] shadow-xl shadow-black/8 max-md:fixed max-md:left-3 max-md:right-3 max-md:top-18 max-md:w-auto md:absolute md:right-0 md:top-full md:mt-2 md:w-[min(92vw,26rem)] z-50"
                    >
                        <div className="flex items-center justify-between border-b border-border px-4 py-3">
                            <div className="flex items-center gap-2">
                                <p className="text-sm font-medium text-foreground">
                                    Notifications
                                </p>
                                {isDisconnected && (
                                    <span className="rounded bg-amber-100 px-1.5 py-0.5 text-[10px] font-medium text-amber-800 dark:bg-amber-950/80 dark:text-amber-300">
                                        REST Fallback
                                    </span>
                                )}
                            </div>
                            {unreadCount > 0 && (
                                <button
                                    onClick={markAllAsRead}
                                    className="inline-flex items-center gap-1.5 rounded-lg px-2.5 py-1.5 text-xs font-medium text-foreground/70 transition-colors hover:bg-secondary hover:text-foreground"
                                >
                                    <CheckCheck className="h-3.5 w-3.5" />
                                    Mark all read
                                </button>
                            )}
                        </div>

                        <div className="max-h-[min(24rem,70vh)] overflow-y-auto">
                            {recent.length === 0 ? (
                                <p className="px-4 py-8 text-center text-sm text-muted-foreground">
                                    No notifications yet
                                </p>
                            ) : (
                                recent.map((notification) => {
                                    const isSafety = notification.priority === "safety";

                                    return (
                                        <div
                                            key={notification.id}
                                            className={cn(
                                                "border-b border-border/70 px-4 py-3 transition-colors last:border-b-0",
                                                isSafety
                                                    ? "bg-red-50/60 dark:bg-red-950/30"
                                                    : !notification.read && "bg-secondary/30"
                                            )}
                                        >
                                            <div className="mb-1 flex items-start justify-between gap-3">
                                                <div className="flex items-center gap-1.5">
                                                    {isSafety && (
                                                        <ShieldAlert className="h-3.5 w-3.5 shrink-0 text-red-600 dark:text-red-400" />
                                                    )}
                                                    <p
                                                        className={cn(
                                                            "text-sm font-medium",
                                                            isSafety
                                                                ? "text-red-900 dark:text-red-200"
                                                                : "text-foreground"
                                                        )}
                                                    >
                                                        {notification.title}
                                                    </p>
                                                    {Boolean(
                                                        notification.count && notification.count > 1
                                                    ) && (
                                                        <span className="rounded-full bg-secondary px-1.5 py-0.5 text-[10px] font-semibold text-muted-foreground">
                                                            x{notification.count}
                                                        </span>
                                                    )}
                                                </div>
                                                {!notification.read && (
                                                    <button
                                                        onClick={() => markAsRead(notification.id)}
                                                        className="shrink-0 text-[11px] font-medium text-foreground/55 transition-colors hover:text-foreground"
                                                    >
                                                        Mark read
                                                    </button>
                                                )}
                                            </div>
                                            <p className="text-xs leading-relaxed text-muted-foreground">
                                                {notification.message}
                                            </p>
                                            <div className="mt-2 flex items-center justify-between">
                                                <span className="text-[11px] text-muted-foreground/80">
                                                    {formatRelativeTime(notification.timestamp)}
                                                </span>
                                                {notification.actionUrl && (
                                                    <NotificationActionLink
                                                        href={notification.actionUrl}
                                                        onClick={() => setOpen(false)}
                                                        className="text-[11px] font-medium text-foreground/80 underline-offset-2 transition-colors hover:underline hover:text-foreground"
                                                    >
                                                        {notification.actionLabel || "View"}
                                                    </NotificationActionLink>
                                                )}
                                            </div>
                                        </div>
                                    );
                                })
                            )}
                        </div>

                        <div className="border-t border-border bg-secondary/20 px-4 py-2.5">
                            <Link
                                href="/notifications"
                                onClick={() => setOpen(false)}
                                className="block text-center text-xs font-medium text-foreground/70 transition-colors hover:text-foreground"
                            >
                                Open Notification Center
                            </Link>
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}
