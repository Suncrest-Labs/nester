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
    dismissToast: (id: string) => void;
}

const NotificationsContext = createContext<NotificationsState>({
    notifications: [],
    unreadCount: 0,
    toasts: [],
    addNotification: () => {},
    markAsRead: () => {},
    markAllAsRead: () => {},
    dismissToast: () => {},
});

const NOTIFICATIONS_STORAGE_KEY = "nester.notifications.v1";

function buildId(prefix: string) {
    if (typeof crypto !== "undefined" && "randomUUID" in crypto) {
        return `${prefix}-${crypto.randomUUID()}`;
    }
    return `${prefix}-${Date.now()}-${Math.floor(Math.random() * 10000)}`;
}

export function NotificationsProvider({ children }: { children: ReactNode }) {
      // v1: Notifications are client-side only and persisted across page reloads via localStorage.
    const [notifications, setNotifications] =
        useState<AppNotification[]>(INITIAL_NOTIFICATIONS);
    const [toasts, setToasts] = useState<ToastItem[]>([]);
    const timerRef = useRef<Record<string, ReturnType<typeof setTimeout>>>({});

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

            setNotifications((prev) => [newNotification, ...prev]);

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
            dismissToast,
        }),
        [
            notifications,
            unreadCount,
            toasts,
            addNotification,
            markAsRead,
            markAllAsRead,
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
