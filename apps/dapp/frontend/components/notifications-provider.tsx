"use client";

import {
    createContext,
    useCallback,
    useContext,
    useEffect,
    useMemo,
    useState,
    type ReactNode,
} from "react";
import {
    INITIAL_NOTIFICATIONS,
    type AppNotification,
    type NotificationDraft,
} from "@/lib/notifications";
import { useToast } from "@/components/toast-provider";

interface NotificationsState {
    notifications: AppNotification[];
    unreadCount: number;
    addNotification: (
        notification: NotificationDraft,
        options?: { showToast?: boolean }
    ) => void;
    markAsRead: (id: string) => void;
    markAllAsRead: () => void;
}

const NotificationsContext = createContext<NotificationsState>({
    notifications: [],
    unreadCount: 0,
    addNotification: () => {},
    markAsRead: () => {},
    markAllAsRead: () => {},
});

const NOTIFICATIONS_STORAGE_KEY = "nester.notifications.v1";

function buildId(prefix: string) {
    if (typeof crypto !== "undefined" && "randomUUID" in crypto) {
        return `${prefix}-${crypto.randomUUID()}`;
    }
    return `${prefix}-${Date.now()}-${Math.floor(Math.random() * 10000)}`;
}

function toastVariantForType(type: NotificationDraft["type"]) {
    switch (type) {
        case "deposit_confirmed":
        case "withdrawal_processed":
            return "success" as const;
        case "ai_alert":
        case "rebalance_event":
            return "warning" as const;
        case "offramp_status":
        default:
            return "info" as const;
    }
}

export function NotificationsProvider({ children }: { children: ReactNode }) {
    const toast = useToast();
    const [notifications, setNotifications] =
        useState<AppNotification[]>(INITIAL_NOTIFICATIONS);

    useEffect(() => {
        if (typeof window === "undefined") {
            return;
        }

        const raw = window.localStorage.getItem(NOTIFICATIONS_STORAGE_KEY);
        if (!raw) {
            return;
        }

        try {
            const parsed = JSON.parse(raw) as AppNotification[];
            if (!Array.isArray(parsed)) {
                return;
            }

            const valid = parsed.filter((item) => {
                if (!item || typeof item !== "object") {
                    return false;
                }

                return (
                    typeof item.id === "string" &&
                    typeof item.type === "string" &&
                    typeof item.title === "string" &&
                    typeof item.message === "string" &&
                    typeof item.timestamp === "string" &&
                    typeof item.read === "boolean"
                );
            });

            if (valid.length > 0) {
                setNotifications(valid);
            }
        } catch {
            window.localStorage.removeItem(NOTIFICATIONS_STORAGE_KEY);
        }
    }, []);

    useEffect(() => {
        if (typeof window === "undefined") {
            return;
        }

        window.localStorage.setItem(
            NOTIFICATIONS_STORAGE_KEY,
            JSON.stringify(notifications)
        );
    }, [notifications]);

    const addNotification = useCallback(
        (notification: NotificationDraft, options?: { showToast?: boolean }) => {
            const newNotification: AppNotification = {
                id: buildId("notif"),
                timestamp: new Date().toISOString(),
                read: false,
                ...notification,
            };

            setNotifications((prev) => [newNotification, ...prev]);

            if (!options?.showToast) {
                return;
            }

            const toastOpts = {
                description: notification.message,
                action: notification.actionUrl
                    ? {
                          label: notification.actionLabel || "View Transaction",
                          href: notification.actionUrl,
                      }
                    : undefined,
            };

            const variant = toastVariantForType(notification.type);
            if (variant === "success") {
                toast.success(notification.title, toastOpts);
            } else if (variant === "warning") {
                toast.warning(notification.title, toastOpts);
            } else {
                toast.info(notification.title, toastOpts);
            }
        },
        [toast]
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

    const unreadCount = useMemo(
        () => notifications.filter((notification) => !notification.read).length,
        [notifications]
    );

    const value = useMemo(
        () => ({
            notifications,
            unreadCount,
            addNotification,
            markAsRead,
            markAllAsRead,
        }),
        [notifications, unreadCount, addNotification, markAsRead, markAllAsRead]
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
