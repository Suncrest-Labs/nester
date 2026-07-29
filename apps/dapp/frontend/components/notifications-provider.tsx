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
import { useWallet } from "@/components/wallet-provider";
import {
    DEFAULT_NOTIFICATION_PREFERENCES,
    INITIAL_NOTIFICATIONS,
    NOTIF_STORAGE_KEY_PREFIX,
    coalesceNotifications,
    dismissNotificationApi,
    fetchNotificationPreferencesApi,
    fetchNotificationsApi,
    getStorageKey,
    mapTypeToCategoryAndPriority,
    markAllNotificationsReadApi,
    markNotificationReadApi,
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

function buildId(prefix: string) {
    if (typeof crypto !== "undefined" && "randomUUID" in crypto) {
        return `${prefix}-${crypto.randomUUID()}`;
    }
    return `${prefix}-${Date.now()}-${Math.floor(Math.random() * 10000)}`;
}

export function NotificationsProvider({ children }: { children: ReactNode }) {
    const { address: addressRaw } = useWallet();
    const address = addressRaw ?? undefined;

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
    const isDisconnectedRef = useRef(false);
    // Track whether the last load was successful and non-empty to gate persistence
    const lastLoadSuccessful = useRef(false);

    // Initial load from storage/REST and preferences scoped to address
    useEffect(() => {
        let isMounted = true;

        async function loadData() {
            try {
                setIsLoading(true);
                setError(null);
                lastLoadSuccessful.current = false;

                const [fetchedPrefs, fetchedNotifs] = await Promise.all([
                    fetchNotificationPreferencesApi(address),
                    fetchNotificationsApi(address),
                ]);

                if (!isMounted) return;

                setPreferences(fetchedPrefs);

                if (Array.isArray(fetchedNotifs)) {
                    const coalesced = coalesceNotifications(fetchedNotifs);
                    setNotifications(coalesced);
                    lastLoadSuccessful.current = true;
                } else {
                    setNotifications(coalesceNotifications(INITIAL_NOTIFICATIONS));
                    lastLoadSuccessful.current = true;
                }
            } catch {
                if (!isMounted) return;
                setError("Failed to load notifications stream. Please try again.");
                setNotifications([]);
                lastLoadSuccessful.current = false;
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
    }, [address]);

    // Sync notifications to localStorage scoped to wallet address.
    // Only persist when the last load was successful and the list is non-empty
    // to avoid overwriting a valid cache with an error-state empty array.
    useEffect(() => {
        if (typeof window === "undefined" || !initialLoadedRef.current) return;
        if (!lastLoadSuccessful.current || notifications.length === 0) return;
        try {
            const key = getStorageKey(NOTIF_STORAGE_KEY_PREFIX, address);
            window.localStorage.setItem(key, JSON.stringify(notifications));
        } catch {
            // Ignore quota errors
        }
    }, [notifications, address]);

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
                    return;
                }
            }

            const id = buildId("notif");
            const newNotif: AppNotification = {
                id,
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
                count: 1,
                mergedIds: [id],
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

    // Move API calls outside the setNotifications updater to keep updaters
    // purely transformational and avoid side-effects inside React state batches.
    const markAsRead = useCallback(
        (id: string) => {
            setNotifications((prev) => {
                const target = prev.find(
                    (n) => n.id === id || (n.mergedIds && n.mergedIds.includes(id))
                );
                const idsToMark = target?.mergedIds?.length ? target.mergedIds : [id];
                idsToMark.forEach((mId) => markNotificationReadApi(mId));

                return prev.map((notif) => {
                    if (notif.id === id || (notif.mergedIds && notif.mergedIds.includes(id))) {
                        return { ...notif, read: true };
                    }
                    return notif;
                });
            });
        },
        []
    );

    const markAllAsRead = useCallback(() => {
        markAllNotificationsReadApi();
        setNotifications((prev) => prev.map((notif) => ({ ...notif, read: true })));
    }, []);

    const dismissNotification = useCallback(
        (id: string) => {
            setNotifications((prev) => {
                const target = prev.find(
                    (n) => n.id === id || (n.mergedIds && n.mergedIds.includes(id))
                );
                const idsToDismiss = target?.mergedIds?.length ? target.mergedIds : [id];
                idsToDismiss.forEach((mId) => dismissNotificationApi(mId));

                return prev.filter(
                    (notif) =>
                        notif.id !== id &&
                        (!notif.mergedIds || !notif.mergedIds.includes(id))
                );
            });
        },
        []
    );

    const clearAll = useCallback(() => {
        setNotifications((prev) => {
            const toRemove = prev.filter((notif) => notif.category !== "safety");
            toRemove.forEach((notif) => {
                const ids = notif.mergedIds?.length ? notif.mergedIds : [notif.id];
                ids.forEach((mId) => dismissNotificationApi(mId));
            });
            // Preserve ALL safety notifications (read and unread)
            return prev.filter((notif) => notif.category === "safety");
        });
    }, []);

    const reconcileWithREST = useCallback(async () => {
        try {
            const latest = await fetchNotificationsApi(address);
            setNotifications((prev) => {
                // Build an expanded index covering the notification's own id AND
                // every id in its mergedIds collection so that server items already
                // represented by a coalesced group are correctly skipped.
                const existingMap = new Map<string, AppNotification>();
                for (const n of prev) {
                    existingMap.set(n.id, n);
                    if (n.mergedIds) {
                        for (const mid of n.mergedIds) {
                            existingMap.set(mid, n);
                        }
                    }
                }

                const merged = [...prev];
                for (const item of latest) {
                    if (!existingMap.has(item.id)) {
                        merged.push(item);
                    } else {
                        const existing = existingMap.get(item.id)!;
                        if (existing.read && !item.read) {
                            existing.read = false;
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
    }, [address]);

    const setConnectionState = useCallback(
        (connected: boolean) => {
            const wasDisconnected = isDisconnectedRef.current;
            const newDisconnected = !connected;

            isDisconnectedRef.current = newDisconnected;
            setIsDisconnected(newDisconnected);

            if (!connected || wasDisconnected) {
                reconcileWithREST();
            }
        },
        [reconcileWithREST]
    );

    const updatePreference = useCallback(
        async (
            category: NotificationCategory,
            channel: NotificationChannel,
            enabled: boolean
        ) => {
            if (category === "safety") {
                return;
            }

            const updatedPreferences: UserNotificationPreferences = {
                ...preferences,
                categories: {
                    ...preferences.categories,
                    [category]: {
                        ...preferences.categories[category],
                        [channel]: enabled,
                    },
                },
            };

            setPreferences(updatedPreferences);

            try {
                await updateNotificationPreferencesApi(updatedPreferences, address);
            } catch {
                // Ignore API sync errors
            }
        },
        [preferences, address]
    );

    const refetch = useCallback(async () => {
        setIsLoading(true);
        setError(null);
        try {
            const data = await fetchNotificationsApi(address);
            setNotifications(coalesceNotifications(data));
        } catch {
            setError("Failed to reload notifications. Please try again.");
            setNotifications([]);
        } finally {
            setIsLoading(false);
        }
    }, [address]);

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
