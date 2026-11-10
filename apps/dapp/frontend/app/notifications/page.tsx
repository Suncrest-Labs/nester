"use client";

import { useEffect, useRef, useState, type KeyboardEvent } from "react";
import { useRouter } from "next/navigation";
import { ShieldAlert, ExternalLink, Lock } from "lucide-react";
import { useWallet } from "@/components/wallet-provider";
import { AppShell } from "@/components/app-shell";
import { useNotifications } from "@/components/notifications-provider";
import { NotificationActionLink } from "@/components/notification-action-link";
import {
    BUTTON_PRIMARY,
    BUTTON_SECONDARY,
    BigNumber,
    PANEL,
    PageHeader,
    SegmentedControl,
} from "@/components/ui/page-kit";
import {
    CATEGORY_METADATA,
    type NotificationCategory,
    type NotificationChannel,
} from "@/lib/notifications";
import { cn } from "@/lib/utils";
import { NotificationsListSkeleton } from "@/components/skeletons/notifications-skeleton";

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

type CategoryFilter = "all" | NotificationCategory;

const CATEGORY_FILTERS: { id: CategoryFilter; label: string }[] = [
    { id: "all", label: "All" },
    { id: "safety", label: "Safety and security" },
    { id: "transactional", label: "Transactions" },
    { id: "nudge", label: "Nudges and milestones" },
];

const CHANNEL_LABELS: Record<NotificationChannel, string> = {
    in_app: "In app",
    email: "Email",
    push: "Push",
};

const TAB_CLASS =
    "border-b-2 px-1 pb-3 text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-foreground";

export default function NotificationsPage() {
    const { isConnected: walletConnected } = useWallet();
    const router = useRouter();

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
    const [categoryFilter, setCategoryFilter] = useState<CategoryFilter>("all");
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
            <PageHeader
                title="Notifications"
                lede={
                    <>
                        <BigNumber className="text-5xl">{unreadCount}</BigNumber>
                        <p className="mt-1 text-sm text-muted-foreground">
                            {unreadCount === 1 ? "unread notification" : "unread notifications"}
                        </p>
                        {isDisconnected && (
                            <p className="mt-3 text-sm text-amber-700 dark:text-amber-400">
                                Live updates are paused. New notifications will appear when the
                                connection comes back.
                            </p>
                        )}
                    </>
                }
                actions={
                    <>
                        {unreadCount > 0 && (
                            <button
                                onClick={markAllAsRead}
                                className={BUTTON_SECONDARY}
                                aria-label="Mark all notifications as read"
                            >
                                Mark all read
                            </button>
                        )}
                        <button
                            onClick={clearAll}
                            className={BUTTON_SECONDARY}
                            aria-label="Clear non-safety read notifications"
                        >
                            Clear read
                        </button>
                    </>
                }
            />

            {safetyNotifications.length > 0 && (
                <div
                    className="mt-8 rounded-xl border border-red-500/40 p-5"
                    role="region"
                    aria-label="Safety critical alerts"
                >
                    <div className="flex items-start gap-3">
                        <ShieldAlert className="mt-0.5 h-4 w-4 shrink-0 text-red-600 dark:text-red-400" />
                        <div>
                            <h2 className="text-sm font-medium text-red-600 dark:text-red-400">
                                {safetyNotifications.length === 1
                                    ? "1 safety alert"
                                    : `${safetyNotifications.length} safety alerts`}
                            </h2>
                            <p className="mt-1 text-sm text-muted-foreground">
                                Safety alerts cover emergency queue fills, circuit breakers and
                                security events. Read them first; they can&apos;t be turned off.
                            </p>
                        </div>
                    </div>
                </div>
            )}

            <div
                className="mt-14 flex gap-6 border-b"
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
                        TAB_CLASS,
                        activeTab === "stream"
                            ? "border-foreground text-foreground"
                            : "border-transparent text-muted-foreground hover:text-foreground"
                    )}
                >
                    All notifications
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
                        TAB_CLASS,
                        activeTab === "preferences"
                            ? "border-foreground text-foreground"
                            : "border-transparent text-muted-foreground hover:text-foreground"
                    )}
                >
                    Delivery preferences
                </button>
            </div>

            {activeTab === "stream" && (
                <div id="panel-stream" role="tabpanel" aria-labelledby="tab-stream" className="mt-6">
                    <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
                        <SegmentedControl
                            label="Category filters"
                            options={CATEGORY_FILTERS}
                            value={categoryFilter}
                            onChange={setCategoryFilter}
                        />

                        <label className="flex cursor-pointer select-none items-center gap-2 text-sm text-muted-foreground">
                            <input
                                type="checkbox"
                                checked={unreadOnly}
                                onChange={(e) => setUnreadOnly(e.target.checked)}
                                className="h-4 w-4 rounded border accent-foreground"
                            />
                            Unread only
                        </label>
                    </div>

                    <div className="overflow-hidden rounded-xl border bg-card">
                        {isLoading ? (
                            <div className="p-6">
                                <NotificationsListSkeleton />
                            </div>
                        ) : error ? (
                            <div className="p-6" aria-live="assertive">
                                <p className="text-sm text-red-600 dark:text-red-400">{error}</p>
                                <p className="mt-2 text-sm text-muted-foreground">
                                    Check your connection and try loading them again.
                                </p>
                                <button onClick={() => refetch()} className={cn(BUTTON_PRIMARY, "mt-4")}>
                                    Try again
                                </button>
                            </div>
                        ) : filteredNotifications.length === 0 ? (
                            <div data-testid="notifications-empty-state" className="p-6">
                                <p className="text-sm text-foreground">You are all caught up</p>
                                <p className="mt-2 max-w-[60ch] text-sm text-muted-foreground">
                                    Nothing matches this filter. Deposits, withdrawals and safety
                                    events will show up here as they happen.
                                </p>
                            </div>
                        ) : (
                            <div className="divide-y" role="feed" aria-label="Notifications list">
                                {filteredNotifications.map((notification) => {
                                    const isSafety = notification.priority === "safety";

                                    return (
                                        <article
                                            key={notification.id}
                                            data-testid={`notif-item-${notification.id}`}
                                            className={cn(
                                                "px-6 py-5",
                                                !notification.read && "bg-foreground/[0.03]"
                                            )}
                                        >
                                            <div className="flex flex-wrap items-start justify-between gap-3">
                                                <div className="flex items-center gap-2">
                                                    {!notification.read && (
                                                        <span
                                                            aria-label="Unread"
                                                            className="h-1.5 w-1.5 shrink-0 rounded-full bg-foreground"
                                                        />
                                                    )}
                                                    {isSafety && (
                                                        <ShieldAlert className="h-4 w-4 shrink-0 text-red-600 dark:text-red-400" />
                                                    )}
                                                    <h2
                                                        className={cn(
                                                            "text-sm font-medium",
                                                            isSafety
                                                                ? "text-red-600 dark:text-red-400"
                                                                : "text-foreground"
                                                        )}
                                                    >
                                                        {notification.title}
                                                    </h2>
                                                    {Boolean(notification.count && notification.count > 1) && (
                                                        <span className="rounded-full border px-2 py-0.5 text-[11px] tabular-nums text-muted-foreground">
                                                            {notification.count} times
                                                        </span>
                                                    )}
                                                </div>

                                                <span className="text-[13px] tabular-nums text-muted-foreground">
                                                    {formatDateLocale(notification.timestamp)}
                                                </span>
                                            </div>

                                            <p className="mt-1.5 text-sm leading-relaxed text-muted-foreground">
                                                {notification.message}
                                            </p>

                                            <div className="mt-3 flex flex-wrap items-center justify-between gap-3 text-[13px]">
                                                <div className="flex items-center gap-4">
                                                    {!notification.read && (
                                                        <button
                                                            onClick={() => markAsRead(notification.id)}
                                                            className="font-medium text-foreground transition-opacity hover:opacity-70"
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
                                                        className="inline-flex items-center gap-1 font-medium text-foreground underline-offset-2 hover:underline"
                                                    >
                                                        {notification.actionLabel || "View details"}
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

            {activeTab === "preferences" && (
                <div id="panel-preferences" role="tabpanel" aria-labelledby="tab-preferences" className="mt-6">
                    <div className={PANEL}>
                        <h2 className="text-[16px] font-semibold text-foreground">Channel preferences</h2>
                        <p className="mt-1 max-w-[60ch] text-sm text-muted-foreground">
                            Choose where each kind of notification reaches you. Safety alerts always
                            go to every channel.
                        </p>

                        <div className="mt-6 divide-y border-t">
                            {(["safety", "transactional", "nudge"] as NotificationCategory[]).map((catKey) => {
                                const meta = CATEGORY_METADATA[catKey];
                                const catPrefs = preferences.categories[catKey];

                                return (
                                    <div key={catKey} className="py-6 last:pb-0">
                                        <div className="flex items-center gap-2">
                                            <h3 className="text-sm font-medium text-foreground">{meta.label}</h3>
                                            {meta.alwaysOn && (
                                                <span className="inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-[11px] text-muted-foreground">
                                                    <Lock className="h-3 w-3" /> Always on
                                                </span>
                                            )}
                                        </div>
                                        <p className="mt-1 text-sm text-muted-foreground">{meta.description}</p>

                                        {meta.alwaysOn && (
                                            <p className="mt-2 text-sm text-amber-700 dark:text-amber-400">
                                                Safety alerts cannot be suppressed or disabled via preferences.
                                            </p>
                                        )}

                                        <div className="mt-4 grid grid-cols-1 gap-3 sm:grid-cols-3">
                                            {(["in_app", "email", "push"] as NotificationChannel[]).map((channel) => {
                                                const isChecked = catPrefs?.[channel] ?? true;
                                                const isDisabled = meta.alwaysOn;

                                                return (
                                                    <label
                                                        key={channel}
                                                        className={cn(
                                                            "flex items-center justify-between rounded-lg border px-4 py-3 transition-colors",
                                                            isDisabled
                                                                ? "cursor-not-allowed opacity-60"
                                                                : "cursor-pointer hover:bg-foreground/5"
                                                        )}
                                                    >
                                                        <span className="text-sm text-foreground">
                                                            {CHANNEL_LABELS[channel]}
                                                        </span>
                                                        <input
                                                            type="checkbox"
                                                            disabled={isDisabled}
                                                            checked={isChecked}
                                                            onChange={(e) =>
                                                                updatePreference(catKey, channel, e.target.checked)
                                                            }
                                                            className="h-4 w-4 rounded border accent-foreground"
                                                        />
                                                    </label>
                                                );
                                            })}
                                        </div>
                                    </div>
                                );
                            })}
                        </div>
                    </div>
                </div>
            )}
        </AppShell>
    );
}
