"use client";

import Link from "next/link";
import { useEffect, useMemo, useState } from "react";
import { Bell, CheckCheck } from "lucide-react";
import { AnimatePresence, motion } from "framer-motion";
import { cn } from "@/lib/utils";
import { useNotifications } from "@/components/notifications-provider";

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
    const {
        notifications,
        unreadCount,
        markAllAsRead,
        markAsRead,
    } = useNotifications();

    const recent = useMemo(() => notifications.slice(0, 6), [notifications]);

    useEffect(() => {
        if (!open) return;

        const handleClick = () => setOpen(false);
        document.addEventListener("click", handleClick);

        return () => document.removeEventListener("click", handleClick);
    }, [open]);

    return (
        <div className="relative" onClick={(e) => e.stopPropagation()}>
            <button
                onClick={() => setOpen((prev) => !prev)}
                className="relative inline-flex h-10 w-10 items-center justify-center rounded-full border border-border bg-white transition-all hover:border-black/20 hover:shadow-sm"
                aria-label="Notifications"
            >
                <Bell className="h-4 w-4 text-foreground/70" />
                {unreadCount > 0 && (
                    <span className="absolute -right-1 -top-1 inline-flex min-w-5 items-center justify-center rounded-full bg-foreground px-1.5 py-0.5 text-[10px] font-semibold text-background">
                        {unreadCount > 9 ? "9+" : unreadCount}
                    </span>
                )}
            </button>

            <AnimatePresence>
                {open && (
                    <motion.div
                        initial={{ opacity: 0, y: 8, scale: 0.96 }}
                        animate={{ opacity: 1, y: 0, scale: 1 }}
                        exit={{ opacity: 0, y: 8, scale: 0.96 }}
                        transition={{ duration: 0.15 }}
                        className="overflow-hidden rounded-2xl border border-border bg-white shadow-xl shadow-black/8 max-md:fixed max-md:left-3 max-md:right-3 max-md:top-18 max-md:w-auto md:absolute md:right-0 md:top-full md:mt-2 md:w-[min(92vw,26rem)]"
                    >
                        <div className="flex items-center justify-between border-b border-border px-4 py-3">
                            <div>
                                <p className="text-sm font-medium text-foreground">
                                    Notifications
                                </p>
                                <p className="text-xs text-muted-foreground">
                                    {unreadCount} unread
                                </p>
                            </div>
                            <button
                                onClick={markAllAsRead}
                                className="inline-flex items-center gap-1.5 rounded-lg px-2.5 py-1.5 text-xs font-medium text-foreground/70 transition-colors hover:bg-secondary hover:text-foreground"
                            >
                                <CheckCheck className="h-3.5 w-3.5" />
                                Mark all read
                            </button>
                        </div>

                        <div className="max-h-[min(24rem,70vh)] overflow-y-auto">
                            {recent.length === 0 ? (
                                <p className="px-4 py-8 text-center text-sm text-muted-foreground">
                                    No notifications yet
                                </p>
                            ) : (
                                recent.map((notification) => (
                                    <div
                                        key={notification.id}
                                        className={cn(
                                            "border-b border-border/70 px-4 py-3 last:border-b-0",
                                            !notification.read &&
                                                "bg-secondary/30"
                                        )}
                                    >
                                        <div className="mb-1 flex items-start justify-between gap-4">
                                            <p className="text-sm font-medium text-foreground">
                                                {notification.title}
                                            </p>
                                            {!notification.read && (
                                                <button
                                                    onClick={() =>
                                                        markAsRead(
                                                            notification.id
                                                        )
                                                    }
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
                                                {formatRelativeTime(
                                                    notification.timestamp
                                                )}
                                            </span>
                                            {notification.actionUrl && (
                                                <a
                                                    href={notification.actionUrl}
                                                    target="_blank"
                                                    rel="noreferrer"
                                                    className="text-[11px] font-medium text-foreground/70 transition-colors hover:text-foreground"
                                                >
                                                    {notification.actionLabel ||
                                                        "Open"}
                                                </a>
                                            )}
                                        </div>
                                    </div>
                                ))
                            )}
                        </div>

                        <div className="border-t border-border bg-secondary/20 px-4 py-2.5">
                            <Link
                                href="/notifications"
                                className="block text-center text-xs font-medium text-foreground/70 transition-colors hover:text-foreground"
                            >
                                View all notifications
                            </Link>
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}
