"use client";

import {
    createContext,
    useCallback,
    useContext,
    useEffect,
    useMemo,
    useRef,
    useState,
    type ReactNode,
} from "react";
import {
    INITIAL_NOTIFICATIONS,
    type AppNotification,
    type NotificationDraft,
} from "@/lib/notifications";
import { safeStorage } from "@/lib/storage";

interface ToastItem {
    id: string;
    title: string;
    message: string;
    actionUrl?: string;
    actionLabel?: string;
}

interface NotificationsState {
    notifications: AppNotification[];
    unreadCount: number;
    toasts: ToastItem[];
    addNotification: (
        notification: NotificationDraft,
        options?: { showToast?: boolean }
    ) => void;
    markAsRead: (id: string) => void;
    markAllAsRead: () => void;
    clearAll: () => void;
    dismissToast: (id: string) => void;
}

const NotificationsContext = createContext<NotificationsState>({
    notifications: [],
    unreadCount: 0,
    toasts: [],
    addNotification: () => {},
    markAsRead: () => {},
    markAllAsRead: () => {},
    clearAll: () => {},
    dismissToast: () => {},
});

const NOTIFICATIONS_STORAGE_KEY = "nester.notifications.v1";
const MAX_NOTIFICATIONS = 100;
const NOTIFICATION_TTL_DAYS = 30;

function buildId(prefix: string) {
    if (typeof crypto !== "undefined" && "randomUUID" in crypto) {
        return `${prefix}-${crypto.randomUUID()}`;
    }
    return `${prefix}-${Date.now()}-${Math.floor(Math.random() * 10000)}`;
}

function isValidNotification(item: unknown): item is AppNotification {
    if (!item || typeof item !== "object") return false;
    const n = item as Record<string, unknown>;
    return (
        typeof n.id === "string" &&
        typeof n.type === "string" &&
        typeof n.title === "string" &&
        typeof n.message === "string" &&
        typeof n.timestamp === "string" &&
        typeof n.read === "boolean"
    );
}

/**
 * Apply retention policy: drop entries older than NOTIFICATION_TTL_DAYS, then
 * clip to MAX_NOTIFICATIONS keeping the newest. Pure function, safe to call
 * from anywhere — also used at app startup, after every add, and after
 * cross-tab sync to keep storage bounded.
 */
function applyRetention(items: AppNotification[]): AppNotification[] {
    const cutoff = Date.now() - NOTIFICATION_TTL_DAYS * 24 * 60 * 60 * 1000;
    const fresh = items.filter((n) => {
        const t = Date.parse(n.timestamp);
        return Number.isFinite(t) ? t >= cutoff : true;
    });
    if (fresh.length <= MAX_NOTIFICATIONS) return fresh;
    // Newest first — sort by timestamp desc, take MAX_NOTIFICATIONS.
    return [...fresh]
        .sort((a, b) => Date.parse(b.timestamp) - Date.parse(a.timestamp))
        .slice(0, MAX_NOTIFICATIONS);
}

export function NotificationsProvider({ children }: { children: ReactNode }) {
    const [notifications, setNotifications] =
        useState<AppNotification[]>(INITIAL_NOTIFICATIONS);
    const [toasts, setToasts] = useState<ToastItem[]>([]);
    const timerRef = useRef<Record<string, ReturnType<typeof setTimeout>>>({});

    // Load from storage and apply retention on mount.
    useEffect(() => {
        const parsed = safeStorage.get<unknown>(NOTIFICATIONS_STORAGE_KEY, null);
        if (!Array.isArray(parsed)) return;
        const valid = parsed.filter(isValidNotification);
        if (valid.length === 0) return;
        const pruned = applyRetention(valid);
        setNotifications(pruned);
        // If retention dropped anything, persist the trimmed list now so other
        // tabs see the cleaned state immediately.
        if (pruned.length !== valid.length) {
            safeStorage.set(NOTIFICATIONS_STORAGE_KEY, pruned);
        }
    }, []);

    // Persist on every change.
    useEffect(() => {
        safeStorage.set(NOTIFICATIONS_STORAGE_KEY, notifications);
    }, [notifications]);

    // Cross-tab sync — adopt the other tab's view (after revalidating).
    useEffect(() => {
        return safeStorage.subscribe<unknown>(
            NOTIFICATIONS_STORAGE_KEY,
            (next) => {
                if (!Array.isArray(next)) return;
                const valid = next.filter(isValidNotification);
                setNotifications(applyRetention(valid));
            }
        );
    }, []);

    const dismissToast = useCallback((id: string) => {
        setToasts((prev) => prev.filter((toast) => toast.id !== id));

        const timer = timerRef.current[id];
        if (timer) {
            clearTimeout(timer);
            delete timerRef.current[id];
        }
    }, []);

    const addNotification = useCallback(
        (notification: NotificationDraft, options?: { showToast?: boolean }) => {
            const newNotification: AppNotification = {
                id: buildId("notif"),
                timestamp: new Date().toISOString(),
                read: false,
                ...notification,
            };

            // Apply retention here so a long-running session doesn't grow past
            // the cap even if storage write succeeds.
            setNotifications((prev) =>
                applyRetention([newNotification, ...prev])
            );

            if (!options?.showToast) {
                return;
            }

            const toastId = buildId("toast");
            setToasts((prev) => [
                {
                    id: toastId,
                    title: notification.title,
                    message: notification.message,
                    actionUrl: notification.actionUrl,
                    actionLabel: notification.actionLabel,
                },
                ...prev,
            ]);

            timerRef.current[toastId] = setTimeout(() => {
                setToasts((prev) => prev.filter((toast) => toast.id !== toastId));
                delete timerRef.current[toastId];
            }, 5000);
        },
        []
    );

    const markAsRead = useCallback((id: string) => {
        setNotifications((prev) =>
            prev.map((notification) =>
                notification.id === id
                    ? { ...notification, read: true }
                    : notification
            )
        );
    }, []);

    const markAllAsRead = useCallback(() => {
        setNotifications((prev) =>
            prev.map((notification) => ({ ...notification, read: true }))
        );
    }, []);

    const clearAll = useCallback(() => {
        setNotifications([]);
        safeStorage.set(NOTIFICATIONS_STORAGE_KEY, []);
    }, []);

    useEffect(() => {
        return () => {
            Object.values(timerRef.current).forEach((timer) => clearTimeout(timer));
            timerRef.current = {};
        };
    }, []);

    const unreadCount = useMemo(
        () => notifications.filter((notification) => !notification.read).length,
        [notifications]
    );

    const value = useMemo(
        () => ({
            notifications,
            unreadCount,
            toasts,
            addNotification,
            markAsRead,
            markAllAsRead,
            clearAll,
            dismissToast,
        }),
        [
            notifications,
            unreadCount,
            toasts,
            addNotification,
            markAsRead,
            markAllAsRead,
            clearAll,
            dismissToast,
        ]
    );

    return (
        <NotificationsContext.Provider value={value}>
            {children}
        </NotificationsContext.Provider>
    );
}

export function useNotifications() {
    return useContext(NotificationsContext);
}
