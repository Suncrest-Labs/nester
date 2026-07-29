"use client";

import { useEffect, useRef, useState, type KeyboardEvent } from "react";
import { useRouter } from "next/navigation";
import { motion, useReducedMotion } from "framer-motion";
import {
    Bell,
    CheckCheck,
    Circle,
    ShieldAlert,
    Wifi,
    WifiOff,
    Trash2,
    Lock,
    Sparkles,
    ArrowRightLeft,
    RefreshCw,
    ExternalLink,
    Sliders,
} from "lucide-react";
import { useWallet } from "@/components/wallet-provider";
import { AppShell } from "@/components/app-shell";
import { useNotifications } from "@/components/notifications-provider";
import { NotificationActionLink } from "@/components/notification-action-link";
import {
    CATEGORY_METADATA,
    type NotificationCategory,
    type NotificationChannel,
} from "@/lib/notifications";
import { cn } from "@/lib/utils";

function formatDateLocale(timestamp: string) {
    try {
        const date = new Date(timestamp);
        return new Intl.DateTimeFormat("en-US", {
            month: "short",
            day: "numeric",
            hour: "numeric",
            minute: "2-digit",
            timeZone: "UTC",
        }).format(date);
    } catch {
        return timestamp;
    }
}

const CATEGORY_FILTERS: { id: "all" | NotificationCategory; label: string }[] = [
    { id: "all", label: "All" },
    { id: "safety", label: "Safety & Security" },
    { id: "transactional", label: "Transactions" },
    { id: "nudge", label: "Nudges & Milestones" },
];

export default function NotificationsPage() {
    const { isConnected: walletConnected } = useWallet();
    const router = useRouter();
    const shouldReduceMotion = useReducedMotion();

    const {
        notifications,
        unreadCount,
        preferences,
        isLoading,
        error,
        isDisconnected,
        markAsRead,
        markAllAsRead,
        dismissNotification,
        clearAll,
        updatePreference,
        refetch,
    } = useNotifications();

    const [activeTab, setActiveTab] = useState<"stream" | "preferences">("stream");
    const [categoryFilter, setCategoryFilter] = useState<"all" | NotificationCategory>("all");
    const [unreadOnly, setUnreadOnly] = useState<boolean>(false);

    const streamTabRef = useRef<HTMLButtonElement>(null);
    const preferencesTabRef = useRef<HTMLButtonElement>(null);

    useEffect(() => {
        if (!walletConnected) {
            router.push("/");
        }
    }, [walletConnected, router]);

    if (!walletConnected) return null;

    const filteredNotifications = notifications.filter((item) => {
        if (unreadOnly && item.read) return false;
        if (categoryFilter !== "all" && item.category !== categoryFilter) return false;
        return true;
    });

    const safetyNotifications = notifications.filter((n) => n.priority === "safety");

    const handleTabKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
        if (e.key === "ArrowRight") {
            e.preventDefault();
            setActiveTab("preferences");
            preferencesTabRef.current?.focus();
        } else if (e.key === "ArrowLeft") {
            e.preventDefault();
            setActiveTab("stream");
            streamTabRef.current?.focus();
        }
    };

    return (
        <AppShell>
            <div className="mx-auto max-w-5xl px-4 py-6 sm:px-6">
                {/* Header */}
                <motion.div
                    initial={shouldReduceMotion ? false : { opacity: 0, y: 20 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ duration: 0.3 }}
                    className="mb-6 flex flex-wrap items-center justify-between gap-4"
                >
                    <div>
                        <div className="flex items-center gap-3">
                            <h1 className="font-heading text-3xl font-light text-foreground sm:text-4xl">
                                Notification Center
                            </h1>
                            {isDisconnected ? (
                                <span className="inline-flex items-center gap-1.5 rounded-full bg-amber-100 px-3 py-1 text-xs font-semibold text-amber-800 dark:bg-amber-950/80 dark:text-amber-300">
                                    <WifiOff className="h-3.5 w-3.5" /> REST Fallback
                                </span>
                            ) : (
                                <span className="inline-flex items-center gap-1.5 rounded-full bg-emerald-100 px-3 py-1 text-xs font-semibold text-emerald-800 dark:bg-emerald-950/80 dark:text-emerald-300">
                                    <Wifi className="h-3.5 w-3.5" /> Live WebSocket
                                </span>
                            )}
                        </div>
                        <p className="mt-1 text-sm text-muted-foreground">
                            Real-time alerts, safety breakers, vault activities, and delivery preferences.
                        </p>
                    </div>

                    <div className="flex items-center gap-2">
                        {unreadCount > 0 && (
                            <button
                                onClick={markAllAsRead}
                                className="inline-flex items-center gap-2 rounded-full border border-border bg-white dark:bg-[#100F0F] px-4 py-2 text-xs font-medium text-foreground/80 transition-colors hover:bg-secondary hover:text-foreground focus-visible:ring-2 focus-visible:ring-foreground focus-visible:outline-none"
                                aria-label="Mark all notifications as read"
                            >
                                <CheckCheck className="h-4 w-4" />
                                Mark all read
                            </button>
                        )}
                        <button
                            onClick={clearAll}
                            className="inline-flex items-center gap-1.5 rounded-full border border-border bg-white dark:bg-[#100F0F] px-3.5 py-2 text-xs font-medium text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground focus-visible:ring-2 focus-visible:ring-foreground focus-visible:outline-none"
                            aria-label="Clear non-safety read notifications"
                        >
                            <Trash2 className="h-3.5 w-3.5" />
                            Clear
                        </button>
                    </div>
                </motion.div>

                {/* Safety Critical Banner Callout */}
                {safetyNotifications.length > 0 && (
                    <motion.div
                        initial={shouldReduceMotion ? false : { opacity: 0, scale: 0.98 }}
                        animate={{ opacity: 1, scale: 1 }}
                        className="mb-6 rounded-3xl border border-red-500/40 bg-red-50/90 dark:bg-red-950/40 p-5 shadow-lg shadow-red-500/5"
                        role="region"
                        aria-label="Safety critical alerts"
                    >
                        <div className="flex items-start justify-between gap-4">
                            <div className="flex items-start gap-3">
                                <div className="rounded-2xl bg-red-600 p-2 text-white shadow-sm">
                                    <ShieldAlert className="h-6 w-6" />
                                </div>
                                <div>
                                    <div className="flex items-center gap-2">
                                        <h2 className="text-base font-semibold text-red-950 dark:text-red-100">
                                            Safety Critical Alerts ({safetyNotifications.length})
                                        </h2>
                                        <span className="rounded-full bg-red-200 px-2 py-0.5 text-[10px] font-bold text-red-900 dark:bg-red-900/80 dark:text-red-200 uppercase">
                                            Always On
                                        </span>
                                    </div>
                                    <p className="mt-1 text-xs text-red-900/90 dark:text-red-200/90 leading-relaxed">
                                        Safety notifications (emergency queue fills, circuit breakers, security events) require immediate attention and cannot be suppressed.
                                    </p>
                                </div>
                            </div>
                        </div>
                    </motion.div>
                )}

                {/* Main Tabs Navigation */}
                <div
                    className="mb-6 flex border-b border-border"
                    role="tablist"
                    aria-label="Notification center tabs"
                    onKeyDown={handleTabKeyDown}
                >
                    <button
                        id="tab-stream"
                        ref={streamTabRef}
                        role="tab"
                        aria-selected={activeTab === "stream"}
                        aria-controls="panel-stream"
                        tabIndex={activeTab === "stream" ? 0 : -1}
                        onClick={() => setActiveTab("stream")}
                        className={cn(
                            "flex items-center gap-2 border-b-2 px-4 py-3 text-sm font-medium transition-colors focus-visible:ring-2 focus-visible:ring-foreground focus-visible:outline-none",
                            activeTab === "stream"
                                ? "border-foreground text-foreground"
                                : "border-transparent text-muted-foreground hover:text-foreground"
                        )}
                    >
                        <Bell className="h-4 w-4" />
                        Notifications Stream
                        {unreadCount > 0 && (
                            <span className="rounded-full bg-foreground px-2 py-0.5 text-[11px] font-bold text-background">
                                {unreadCount}
                            </span>
                        )}
                    </button>

                    <button
                        id="tab-preferences"
                        ref={preferencesTabRef}
                        role="tab"
                        aria-selected={activeTab === "preferences"}
                        aria-controls="panel-preferences"
                        tabIndex={activeTab === "preferences" ? 0 : -1}
                        onClick={() => setActiveTab("preferences")}
                        className={cn(
                            "flex items-center gap-2 border-b-2 px-4 py-3 text-sm font-medium transition-colors focus-visible:ring-2 focus-visible:ring-foreground focus-visible:outline-none",
                            activeTab === "preferences"
                                ? "border-foreground text-foreground"
                                : "border-transparent text-muted-foreground hover:text-foreground"
                        )}
                    >
                        <Sliders className="h-4 w-4" />
                        Preferences & Delivery
                    </button>
                </div>

                {/* TAB 1: NOTIFICATIONS STREAM */}
                {activeTab === "stream" && (
                    <div
                        id="panel-stream"
                        role="tabpanel"
                        aria-labelledby="tab-stream"
                    >
                        {/* Filters bar */}
                        <div className="mb-4 flex flex-wrap items-center justify-between gap-3 rounded-2xl border border-border bg-white dark:bg-[#100F0F] p-3">
                            <div className="flex flex-wrap items-center gap-1.5" role="group" aria-label="Category filters">
                                {CATEGORY_FILTERS.map((cat) => (
                                    <button
                                        key={cat.id}
                                        aria-pressed={categoryFilter === cat.id}
                                        onClick={() => setCategoryFilter(cat.id)}
                                        className={cn(
                                            "rounded-xl px-3 py-1.5 text-xs font-medium transition-colors focus-visible:ring-2 focus-visible:ring-foreground focus-visible:outline-none",
                                            categoryFilter === cat.id
                                                ? "bg-foreground text-background"
                                                : "text-muted-foreground hover:bg-secondary hover:text-foreground"
                                        )}
                                    >
                                        {cat.label}
                                    </button>
                                ))}
                            </div>

                            <label className="flex items-center gap-2 text-xs text-muted-foreground cursor-pointer select-none">
                                <input
                                    type="checkbox"
                                    checked={unreadOnly}
                                    onChange={(e) => setUnreadOnly(e.target.checked)}
                                    className="rounded border-border text-foreground focus:ring-foreground"
                                />
                                Show unread only
                            </label>
                        </div>

                        {/* Stream Content */}
                        <div className="overflow-hidden rounded-3xl border border-border bg-white dark:bg-[#100F0F]">
                            {/* Loading State */}
                            {isLoading ? (
                                <div className="flex flex-col items-center justify-center p-12 text-center" aria-live="polite">
                                    <RefreshCw className="h-6 w-6 animate-spin text-muted-foreground mb-3" />
                                    <p className="text-sm font-medium text-foreground/80">
                                        Loading notification stream...
                                    </p>
                                </div>
                            ) : error ? (
                                /* Error State */
                                <div className="flex flex-col items-center justify-center p-12 text-center" aria-live="assertive">
                                    <ShieldAlert className="h-8 w-8 text-red-500 mb-3" />
                                    <p className="text-sm font-medium text-foreground">{error}</p>
                                    <button
                                        onClick={() => refetch()}
                                        className="mt-4 inline-flex items-center gap-2 rounded-full bg-foreground px-4 py-2 text-xs font-medium text-background hover:opacity-90"
                                    >
                                        <RefreshCw className="h-3.5 w-3.5" /> Retry
                                    </button>
                                </div>
                            ) : filteredNotifications.length === 0 ? (
                                /* Empty State */
                                <div className="flex flex-col items-center justify-center px-6 py-20 text-center">
                                    <div className="mb-4 rounded-2xl bg-secondary p-4">
                                        <Bell className="h-8 w-8 text-muted-foreground" />
                                    </div>
                                    <p className="text-base font-medium text-foreground/90">
                                        You are all caught up
                                    </p>
                                    <p className="mt-1 text-xs text-muted-foreground max-w-sm">
                                        No notifications matching your filter criteria. Real-time safety events and vault updates will automatically populate here.
                                    </p>
                                </div>
                            ) : (
                                /* Notification List */
                                <div className="divide-y divide-border" role="feed" aria-label="Notifications list">
                                    {filteredNotifications.map((notification) => {
                                        const isSafety = notification.priority === "safety";

                                        return (
                                            <article
                                                key={notification.id}
                                                data-testid={`notif-item-${notification.id}`}
                                                className={cn(
                                                    "p-5 transition-colors",
                                                    isSafety
                                                        ? "bg-red-50/50 dark:bg-red-950/20"
                                                        : !notification.read && "bg-secondary/30"
                                                )}
                                            >
                                                <div className="mb-2 flex flex-wrap items-start justify-between gap-3">
                                                    <div className="flex items-center gap-2.5">
                                                        {!notification.read && (
                                                            <Circle className="h-2.5 w-2.5 fill-foreground text-foreground shrink-0" />
                                                        )}
                                                        {isSafety ? (
                                                            <ShieldAlert className="h-4 w-4 shrink-0 text-red-600 dark:text-red-400" />
                                                        ) : notification.type === "goal_milestone" ? (
                                                            <Sparkles className="h-4 w-4 shrink-0 text-amber-500" />
                                                        ) : (
                                                            <ArrowRightLeft className="h-4 w-4 shrink-0 text-muted-foreground" />
                                                        )}
                                                        <h2 className={cn("text-sm font-semibold", isSafety ? "text-red-950 dark:text-red-100" : "text-foreground")}>
                                                            {notification.title}
                                                        </h2>
                                                        {Boolean(notification.count && notification.count > 1) && (
                                                            <span className="rounded-full bg-secondary px-2 py-0.5 text-[10px] font-bold text-muted-foreground">
                                                                {notification.count} coalesced
                                                            </span>
                                                        )}
                                                    </div>

                                                    <span className="text-xs text-muted-foreground">
                                                        {formatDateLocale(notification.timestamp)}
                                                    </span>
                                                </div>

                                                <p className="ml-5 text-sm leading-relaxed text-muted-foreground">
                                                    {notification.message}
                                                </p>

                                                <div className="ml-5 mt-3 flex flex-wrap items-center justify-between gap-3 text-xs">
                                                    <div className="flex items-center gap-3">
                                                        {!notification.read && (
                                                            <button
                                                                onClick={() => markAsRead(notification.id)}
                                                                className="font-medium text-foreground/75 transition-colors hover:text-foreground"
                                                            >
                                                                Mark read
                                                            </button>
                                                        )}
                                                        <button
                                                            onClick={() => dismissNotification(notification.id)}
                                                            className="font-medium text-muted-foreground transition-colors hover:text-foreground"
                                                        >
                                                            Dismiss
                                                        </button>
                                                    </div>

                                                    {notification.actionUrl && (
                                                        <NotificationActionLink
                                                            href={notification.actionUrl}
                                                            className={cn(
                                                                "inline-flex items-center gap-1 font-medium transition-colors hover:underline",
                                                                isSafety
                                                                    ? "text-red-700 dark:text-red-300"
                                                                    : "text-foreground"
                                                            )}
                                                        >
                                                            {notification.actionLabel || "View Action"}
                                                            <ExternalLink className="h-3 w-3" />
                                                        </NotificationActionLink>
                                                    )}
                                                </div>
                                            </article>
                                        );
                                    })}
                                </div>
                            )}
                        </div>
                    </div>
                )}

                {/* TAB 2: PREFERENCES & DELIVERY */}
                {activeTab === "preferences" && (
                    <div
                        id="panel-preferences"
                        role="tabpanel"
                        aria-labelledby="tab-preferences"
                    >
                        <motion.div
                            initial={shouldReduceMotion ? false : { opacity: 0, y: 10 }}
                            animate={{ opacity: 1, y: 0 }}
                            className="space-y-6"
                        >
                            <div className="rounded-3xl border border-border bg-white dark:bg-[#100F0F] p-6 shadow-sm">
                                <h2 className="text-lg font-medium text-foreground mb-1">
                                    Notification Channel Preferences
                                </h2>
                                <p className="text-xs text-muted-foreground mb-6">
                                    Manage how and where you receive notifications across categories. Safety notifications are mandatory to protect system integrity.
                                </p>

                                <div className="divide-y divide-border">
                                    {(["safety", "transactional", "nudge"] as NotificationCategory[]).map(
                                        (catKey) => {
                                            const meta = CATEGORY_METADATA[catKey];
                                            const catPrefs = preferences.categories[catKey];

                                            return (
                                                <div key={catKey} className="py-6 first:pt-0 last:pb-0">
                                                    <div className="flex items-start justify-between gap-4 mb-4">
                                                        <div>
                                                            <div className="flex items-center gap-2">
                                                                <h3 className="text-base font-semibold text-foreground">
                                                                    {meta.label}
                                                                </h3>
                                                                {meta.alwaysOn && (
                                                                    <span className="inline-flex items-center gap-1 rounded-full bg-red-100 dark:bg-red-950/80 px-2.5 py-0.5 text-[10px] font-bold text-red-800 dark:text-red-300">
                                                                        <Lock className="h-3 w-3" /> ALWAYS ON
                                                                    </span>
                                                                )}
                                                            </div>
                                                            <p className="mt-1 text-xs text-muted-foreground">
                                                                {meta.description}
                                                            </p>
                                                        </div>
                                                    </div>

                                                    {meta.alwaysOn && (
                                                        <div className="mb-4 rounded-xl border border-amber-500/30 bg-amber-50/50 dark:bg-amber-950/20 p-3 text-xs text-amber-900 dark:text-amber-200">
                                                            Safety alerts cannot be suppressed or disabled via preferences.
                                                        </div>
                                                    )}

                                                    <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
                                                        {(["in_app", "email", "push"] as NotificationChannel[]).map(
                                                            (channel) => {
                                                                const isChecked = catPrefs?.[channel] ?? true;
                                                                const isDisabled = meta.alwaysOn;

                                                                return (
                                                                    <label
                                                                        key={channel}
                                                                        className={cn(
                                                                            "flex items-center justify-between rounded-2xl border border-border p-3.5 transition-colors",
                                                                            isDisabled
                                                                                ? "bg-secondary/40 cursor-not-allowed opacity-80"
                                                                                : "cursor-pointer hover:bg-secondary/20"
                                                                        )}
                                                                    >
                                                                        <span className="text-xs font-medium text-foreground capitalize">
                                                                            {channel.replace("_", " ")}
                                                                        </span>
                                                                        <input
                                                                            type="checkbox"
                                                                            disabled={isDisabled}
                                                                            checked={isChecked}
                                                                            onChange={(e) =>
                                                                                updatePreference(
                                                                                    catKey,
                                                                                    channel,
                                                                                    e.target.checked
                                                                                )
                                                                            }
                                                                            className="h-4 w-4 rounded border-border text-foreground focus:ring-foreground disabled:opacity-60"
                                                                        />
                                                                    </label>
                                                                );
                                                            }
                                                        )}
                                                    </div>
                                                </div>
                                            );
                                        }
                                    )}
                                </div>
                            </div>
                        </motion.div>
                    </div>
                )}
            </div>
        </AppShell>
    );
}
