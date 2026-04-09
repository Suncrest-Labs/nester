"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { motion } from "framer-motion";
import { Bell, CheckCheck, Circle } from "lucide-react";
import { useWallet } from "@/components/wallet-provider";
import { AppShell } from "@/components/app-shell";
import { useNotifications } from "@/components/notifications-provider";

function formatDate(timestamp: string) {
    return new Date(timestamp).toLocaleString("en-US", {
        month: "short",
        day: "numeric",
        hour: "numeric",
        minute: "2-digit",
    });
}

export default function NotificationsPage() {
    const { isConnected } = useWallet();
    const router = useRouter();
    const { notifications, unreadCount, markAsRead, markAllAsRead } =
        useNotifications();

    useEffect(() => {
        if (!isConnected) {
            router.push("/");
        }
    }, [isConnected, router]);

    if (!isConnected) return null;

    return (
        <AppShell>

            <div>
                <motion.div
                    initial={{ opacity: 0, y: 20 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ duration: 0.45 }}
                    className="mb-8 flex flex-wrap items-start justify-between gap-4"
                >
                    <div>
                        <h1 className="font-heading text-3xl font-light text-foreground sm:text-4xl">
                            Notifications
                        </h1>
                        <p className="mt-1 text-sm text-muted-foreground">
                            {unreadCount} unread updates across vault activity,
                            Prometheus alerts, and settlement status.
                        </p>
                    </div>
                    <button
                        onClick={markAllAsRead}
                        className="inline-flex items-center gap-2 rounded-full border border-border bg-white px-4 py-2 text-sm font-medium text-foreground/75 transition-colors hover:bg-secondary hover:text-foreground"
                    >
                        <CheckCheck className="h-4 w-4" />
                        Mark all as read
                    </button>
                </motion.div>

                <motion.div
                    initial={{ opacity: 0, y: 20 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ duration: 0.45, delay: 0.1 }}
                    className="overflow-hidden rounded-3xl border border-border bg-white"
                >
                    {notifications.length === 0 ? (
                        <div className="flex flex-col items-center justify-center px-6 py-20 text-center">
                            <div className="mb-4 rounded-2xl bg-secondary p-3">
                                <Bell className="h-6 w-6 text-muted-foreground" />
                            </div>
                            <p className="text-sm font-medium text-foreground/80">
                                You are all caught up
                            </p>
                            <p className="mt-1 text-xs text-muted-foreground">
                                New vault and AI events will appear here.
                            </p>
                        </div>
                    ) : (
                        notifications.map((notification) => (
                            <div
                                key={notification.id}
                                className="border-b border-border p-5 last:border-b-0"
                            >
                                <div className="mb-2 flex flex-wrap items-start justify-between gap-3">
                                    <div className="flex items-center gap-2">
                                        {!notification.read && (
                                            <Circle className="h-2.5 w-2.5 fill-foreground text-foreground" />
                                        )}
                                        <h2 className="text-sm font-medium text-foreground">
                                            {notification.title}
                                        </h2>
                                    </div>
                                    <span className="text-xs text-muted-foreground">
                                        {formatDate(notification.timestamp)}
                                    </span>
                                </div>

                                <p className="text-sm leading-relaxed text-muted-foreground">
                                    {notification.message}
                                </p>

                                <div className="mt-3 flex items-center gap-3 text-xs">
                                    {!notification.read && (
                                        <button
                                            onClick={() =>
                                                markAsRead(notification.id)
                                            }
                                            className="font-medium text-foreground/65 transition-colors hover:text-foreground"
                                        >
                                            Mark as read
                                        </button>
                                    )}
                                    {notification.actionUrl && (
                                        <a
                                            href={notification.actionUrl}
                                            target="_blank"
                                            rel="noreferrer"
                                            className="font-medium text-foreground/65 transition-colors hover:text-foreground"
                                        >
                                            {notification.actionLabel ||
                                                "View details"}
                                        </a>
                                    )}
                                </div>
                            </div>
                        ))
                    )}
                </motion.div>
            </div>
        </AppShell>
    );
}
