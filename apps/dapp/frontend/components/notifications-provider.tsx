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
    DEFAULT_NOTIFICATION_PREFERENCES,
    INITIAL_NOTIFICATIONS,
    coalesceNotifications,
    dismissNotificationApi,
    fetchNotificationPreferencesApi,
    fetchNotificationsApi,
    mapTypeToCategoryAndPriority,
    markAllNotificationsReadApi,
    markNotificationReadApi,
    normalizeNotification,
    updateNotificationPreferencesApi,
    type AppNotification,
    type NotificationCategory,
    type NotificationChannel,
    type NotificationDraft,
    type UserNotificationPreferences,
} from "@/lib/notifications";

export interface ToastItem {
    id: string;
    title: string;
    message: string;
    priority?: "safety" | "transactional" | "nudge";
    actionUrl?: string;
    actionLabel?: string;
}

interface NotificationsState {
    notifications: AppNotification[];
    unreadCount: number;
    safetyCount: number;
    toasts: ToastItem[];
    preferences: UserNotificationPreferences;
    isLoading: boolean;
    error: string | null;
    isDisconnected: boolean;
    addNotification: (
        notification: NotificationDraft,
        options?: { showToast?: boolean }
    ) => void;
    markAsRead: (id: string) => void;
    markAllAsRead: () => void;
    dismissNotification: (id: string) => void;
    clearAll: () => void;
    dismissToast: (id: string) => void;
    updatePreference: (
        category: NotificationCategory,
        channel: NotificationChannel,
        enabled: boolean
    ) => Promise<void>;
    reconcileWithREST: () => Promise<void>;
    setConnectionState: (connected: boolean) => void;
    refetch: () => Promise<void>;
}

const NotificationsContext = createContext<NotificationsState>({
    notifications: [],
    unreadCount: 0,
    safetyCount: 0,
    toasts: [],
    preferences: DEFAULT_NOTIFICATION_PREFERENCES,
    isLoading: false,
    error: null,
    isDisconnected: false,
    addNotification: () => {},
    markAsRead: () => {},
    markAllAsRead: () => {},
    dismissNotification: () => {},
    clearAll: () => {},
    dismissToast: () => {},
    updatePreference: async () => {},
    reconcileWithREST: async () => {},
    setConnectionState: () => {},
    refetch: async () => {},
});

const NOTIFICATIONS_STORAGE_KEY = "nester.notifications.v1";

function buildId(prefix: string) {
    if (typeof crypto !== "undefined" && "randomUUID" in crypto) {
        return `${prefix}-${crypto.randomUUID()}`;
    }
    return `${prefix}-${Date.now()}-${Math.floor(Math.random() * 10000)}`;
}

export function NotificationsProvider({ children }: { children: ReactNode }) {
    const [notifications, setNotifications] = useState<AppNotification[]>([]);
    const [preferences, setPreferences] = useState<UserNotificationPreferences>(
        DEFAULT_NOTIFICATION_PREFERENCES
    );
    const [toasts, setToasts] = useState<ToastItem[]>([]);
    const [isLoading, setIsLoading] = useState<boolean>(true);
    const [error, setError] = useState<string | null>(null);
    const [isDisconnected, setIsDisconnected] = useState<boolean>(false);

    const timerRef = useRef<Record<string, ReturnType<typeof setTimeout>>>({});
    const initialLoadedRef = useRef(false);

    // Initial load from storage/REST and preferences
    useEffect(() => {
        let isMounted = true;

        async function loadData() {
            try {
                setIsLoading(true);
                setError(null);

                const [fetchedPrefs, fetchedNotifs] = await Promise.all([
                    fetchNotificationPreferencesApi(),
                    fetchNotificationsApi(),
                ]);

                if (!isMounted) return;

                setPreferences(fetchedPrefs);

                if (fetchedNotifs && fetchedNotifs.length > 0) {
                    setNotifications(coalesceNotifications(fetchedNotifs));
                } else {
                    setNotifications(coalesceNotifications(INITIAL_NOTIFICATIONS));
                }
            } catch (err) {
                if (!isMounted) return;
                setError("Failed to load notifications stream. Please try again.");
                setNotifications(coalesceNotifications(INITIAL_NOTIFICATIONS));
            } finally {
                if (isMounted) {
                    setIsLoading(false);
                    initialLoadedRef.current = true;
                }
            }
        }

        loadData();

        return () => {
            isMounted = false;
        };
    }, []);

    // Sync notifications to localStorage
    useEffect(() => {
        if (typeof window === "undefined" || !initialLoadedRef.current) return;
        try {
            window.localStorage.setItem(
                NOTIFICATIONS_STORAGE_KEY,
                JSON.stringify(notifications)
            );
        } catch {
            // Ignore quota errors
        }
    }, [notifications]);

    const dismissToast = useCallback((id: string) => {
        setToasts((prev) => prev.filter((toast) => toast.id !== id));
        if (timerRef.current[id]) {
            clearTimeout(timerRef.current[id]);
            delete timerRef.current[id];
        }
    }, []);

    const addNotification = useCallback(
        (draft: NotificationDraft, options?: { showToast?: boolean }) => {
            const mapped = mapTypeToCategoryAndPriority(draft.type);
            const category = draft.category || mapped.category;
            const priority = draft.priority || mapped.priority;

            // Check preferences (Safety is always allowed, others check category settings)
            if (category !== "safety") {
                const categoryPref = preferences.categories[category];
                if (categoryPref && !categoryPref.in_app) {
                    // Category suppressed by user preference
                    return;
                }
            }

            const newNotif: AppNotification = {
                id: buildId("notif"),
                type: draft.type,
                category,
                priority,
                title: draft.title,
                message: draft.message,
                timestamp: new Date().toISOString(),
                read: false,
                actionUrl: draft.actionUrl,
                actionLabel: draft.actionLabel,
                coalesceKey: draft.coalesceKey,
                metadata: draft.metadata,
            };

            setNotifications((prev) => coalesceNotifications([newNotif, ...prev]));

            if (options?.showToast) {
                const toastId = buildId("toast");
                const duration = priority === "safety" ? 8000 : 5000;

                setToasts((prev) => [
                    {
                        id: toastId,
                        title: draft.title,
                        message: draft.message,
                        priority,
                        actionUrl: draft.actionUrl,
                        actionLabel: draft.actionLabel,
                    },
                    ...prev,
                ]);

                timerRef.current[toastId] = setTimeout(() => {
                    dismissToast(toastId);
                }, duration);
            }
        },
        [preferences, dismissToast]
    );

    const markAsRead = useCallback((id: string) => {
        setNotifications((prev) =>
            prev.map((notif) => (notif.id === id ? { ...notif, read: true } : notif))
        );
        markNotificationReadApi(id);
    }, []);

    const markAllAsRead = useCallback(() => {
        setNotifications((prev) => prev.map((notif) => ({ ...notif, read: true })));
        markAllNotificationsReadApi();
    }, []);

    const dismissNotification = useCallback((id: string) => {
        setNotifications((prev) => prev.filter((notif) => notif.id !== id));
        dismissNotificationApi(id);
    }, []);

    const clearAll = useCallback(() => {
        // Keep unread safety notifications when clearing all
        setNotifications((prev) =>
            prev.filter((notif) => notif.priority === "safety" && !notif.read)
        );
    }, []);

    const reconcileWithREST = useCallback(async () => {
        try {
            const latest = await fetchNotificationsApi();
            setNotifications((prev) => {
                const existingMap = new Map(prev.map((n) => [n.id, n]));
                const merged = [...prev];

                for (const item of latest) {
                    if (!existingMap.has(item.id)) {
                        merged.push(item);
                    } else {
                        // preserve read state if read locally
                        const existing = existingMap.get(item.id)!;
                        if (existing.read && !item.read) {
                            item.read = true;
                        }
                    }
                }

                return coalesceNotifications(
                    merged.sort(
                        (a, b) =>
                            new Date(b.timestamp).getTime() -
                            new Date(a.timestamp).getTime()
                    )
                );
            });
        } catch {
            // Reconcile failed silently
        }
    }, []);

    const setConnectionState = useCallback(
        (connected: boolean) => {
            const wasDisconnected = isDisconnected;
            setIsDisconnected(!connected);

            // Reconcile when transitioning back to connected or when running in disconnected mode
            if (!connected || wasDisconnected) {
                reconcileWithREST();
            }
        },
        [isDisconnected, reconcileWithREST]
    );

    const updatePreference = useCallback(
        async (
            category: NotificationCategory,
            channel: NotificationChannel,
            enabled: boolean
        ) => {
            // Safety notifications are ALWAYS ON
            if (category === "safety") {
                return;
            }

            setPreferences((prev) => {
                const updated: UserNotificationPreferences = {
                    ...prev,
                    categories: {
                        ...prev.categories,
                        [category]: {
                            ...prev.categories[category],
                            [channel]: enabled,
                        },
                    },
                };
                updateNotificationPreferencesApi(updated);
                return updated;
            });
        },
        []
    );

    const refetch = useCallback(async () => {
        setIsLoading(true);
        setError(null);
        try {
            const data = await fetchNotificationsApi();
            setNotifications(coalesceNotifications(data));
        } catch (err) {
            setError("Failed to reload notifications. Please try again.");
        } finally {
            setIsLoading(false);
        }
    }, []);

    useEffect(() => {
        return () => {
            Object.values(timerRef.current).forEach((t) => clearTimeout(t));
            timerRef.current = {};
        };
    }, []);

    const unreadCount = useMemo(
        () => notifications.filter((n) => !n.read).length,
        [notifications]
    );

    const safetyCount = useMemo(
        () => notifications.filter((n) => n.priority === "safety" && !n.read).length,
        [notifications]
    );

    // Sorted notifications: Safety at the top, then by timestamp descending
    const sortedNotifications = useMemo(() => {
        return [...notifications].sort((a, b) => {
            if (a.priority === "safety" && b.priority !== "safety") return -1;
            if (a.priority !== "safety" && b.priority === "safety") return 1;
            return new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime();
        });
    }, [notifications]);

    const value = useMemo(
        () => ({
            notifications: sortedNotifications,
            unreadCount,
            safetyCount,
            toasts,
            preferences,
            isLoading,
            error,
            isDisconnected,
            addNotification,
            markAsRead,
            markAllAsRead,
            dismissNotification,
            clearAll,
            dismissToast,
            updatePreference,
            reconcileWithREST,
            setConnectionState,
            refetch,
        }),
        [
            sortedNotifications,
            unreadCount,
            safetyCount,
            toasts,
            preferences,
            isLoading,
            error,
            isDisconnected,
            addNotification,
            markAsRead,
            markAllAsRead,
            dismissNotification,
            clearAll,
            dismissToast,
            updatePreference,
            reconcileWithREST,
            setConnectionState,
            refetch,
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
