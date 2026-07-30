export type NotificationCategory = "safety" | "transactional" | "nudge";
export type NotificationPriority = "safety" | "transactional" | "nudge";
export type NotificationChannel = "in_app" | "email" | "push";

export type NotificationType =
    | "emergency_queue_fill"
    | "security_event"
    | "breaker_trip"
    | "deposit_confirmed"
    | "withdrawal_processed"
    | "ai_alert"
    | "rebalance_event"
    | "offramp_status"
    | "goal_milestone"
    | "nudge_recommendation"
    | "promotional"
    | "info";

export interface AppNotification {
    id: string;
    type: NotificationType;
    category: NotificationCategory;
    priority: NotificationPriority;
    title: string;
    message: string;
    timestamp: string;
    read: boolean;
    actionUrl?: string;
    actionLabel?: string;
    coalesceKey?: string;
    count?: number;
    mergedIds?: string[];
    metadata?: Record<string, unknown>;
}

export interface NotificationDraft {
    type: NotificationType;
    category?: NotificationCategory;
    priority?: NotificationPriority;
    title: string;
    message: string;
    actionUrl?: string;
    actionLabel?: string;
    coalesceKey?: string;
    metadata?: Record<string, unknown>;
}

export type CategoryPreferences = Record<NotificationChannel, boolean>;

export interface UserNotificationPreferences {
    categories: Record<NotificationCategory, CategoryPreferences>;
}

export const CATEGORY_METADATA: Record<
    NotificationCategory,
    { label: string; description: string; alwaysOn: boolean }
> = {
    safety: {
        label: "Safety & Security",
        description: "Critical emergency-queue fills, security alerts, and circuit breaker trips.",
        alwaysOn: true,
    },
    transactional: {
        label: "Transactions & Vault Activity",
        description: "Deposit confirmations, withdrawal processing, yield accrual, and settlement status.",
        alwaysOn: false,
    },
    nudge: {
        label: "Nudges & Milestones",
        description: "Goal milestone achievements, Prometheus AI recommendations, and promotional updates.",
        alwaysOn: false,
    },
};

export const DEFAULT_NOTIFICATION_PREFERENCES: UserNotificationPreferences = {
    categories: {
        safety: {
            in_app: true,
            email: true,
            push: true,
        },
        transactional: {
            in_app: true,
            email: true,
            push: false,
        },
        nudge: {
            in_app: true,
            email: false,
            push: false,
        },
    },
};

const now = Date.now();

function isoMinutesAgo(minutes: number) {
    return new Date(now - minutes * 60_000).toISOString();
}

export function mapTypeToCategoryAndPriority(type: NotificationType): {
    category: NotificationCategory;
    priority: NotificationPriority;
} {
    switch (type) {
        case "emergency_queue_fill":
        case "security_event":
        case "breaker_trip":
            return { category: "safety", priority: "safety" };
        case "deposit_confirmed":
        case "withdrawal_processed":
        case "rebalance_event":
        case "offramp_status":
            return { category: "transactional", priority: "transactional" };
        case "ai_alert":
        case "goal_milestone":
        case "nudge_recommendation":
        case "promotional":
        case "info":
        default:
            return { category: "nudge", priority: "nudge" };
    }
}

export const INITIAL_NOTIFICATIONS: AppNotification[] = [
    {
        id: "seed-safety-1",
        type: "emergency_queue_fill",
        category: "safety",
        priority: "safety",
        title: "Emergency Queue High Fill",
        message: "USDC emergency queue reached 88% capacity. Action required to review buffer.",
        timestamp: isoMinutesAgo(2),
        read: false,
        actionUrl: "/vaults",
        actionLabel: "Manage Vault Buffer",
        coalesceKey: "emergency_queue_fill_USDC",
        mergedIds: ["seed-safety-1"],
    },
    {
        id: "seed-1",
        type: "deposit_confirmed",
        category: "transactional",
        priority: "transactional",
        title: "Deposit Confirmed",
        message: "Deposited 500 USDC into Balanced Vault",
        timestamp: isoMinutesAgo(8),
        read: false,
        actionUrl: "/vaults",
        actionLabel: "View Vault",
        mergedIds: ["seed-1"],
    },
    {
        id: "seed-2",
        type: "withdrawal_processed",
        category: "transactional",
        priority: "transactional",
        title: "Withdrawal Processed",
        message: "Withdrew 200 USDC from Growth Vault",
        timestamp: isoMinutesAgo(24),
        read: false,
        actionUrl: "/vaults",
        actionLabel: "View Vault",
        mergedIds: ["seed-2"],
    },
    {
        id: "seed-3",
        type: "ai_alert",
        category: "nudge",
        priority: "nudge",
        title: "Prometheus Alert",
        message: "Prometheus: Your Balanced Vault APY dropped to 7.2%. Consider reviewing.",
        timestamp: isoMinutesAgo(56),
        read: false,
        actionUrl: "/savings",
        actionLabel: "Review Strategy",
        mergedIds: ["seed-3"],
    },
    {
        id: "seed-4",
        type: "rebalance_event",
        category: "transactional",
        priority: "transactional",
        title: "Vault Rebalanced",
        message: "Your Balanced Vault was rebalanced - new allocation: 45% Blend, 30% Aave, 25% Kamino",
        timestamp: isoMinutesAgo(145),
        read: true,
        mergedIds: ["seed-4"],
    },
    {
        id: "seed-5",
        type: "offramp_status",
        category: "transactional",
        priority: "transactional",
        title: "Off-ramp Status",
        message: "Off-ramp settlement is now in queued state and awaiting LP confirmation.",
        timestamp: isoMinutesAgo(220),
        read: true,
        actionUrl: "/offramp",
        actionLabel: "View Off-ramp",
        mergedIds: ["seed-5"],
    },
];

/**
 * Deduplicates / coalesces notifications with matching `coalesceKey` or
 * identical type + title within a 15-minute window.
 * When a newer item is merged in, its title, message, actionUrl, and
 * actionLabel are copied to keep the displayed content up-to-date.
 */
export function coalesceNotifications(notifications: AppNotification[]): AppNotification[] {
    const result: AppNotification[] = [];
    const seenMap = new Map<string, AppNotification>();

    for (const item of notifications) {
        const key = item.coalesceKey || `${item.type}:${item.title.toLowerCase()}`;
        const existing = seenMap.get(key);

        if (existing) {
            const timeDiff = Math.abs(
                new Date(item.timestamp).getTime() - new Date(existing.timestamp).getTime()
            );
            // Coalesce within 15 minutes (900,000 ms)
            if (timeDiff < 15 * 60 * 1000) {
                existing.count = (existing.count || 1) + (item.count || 1);

                const existingIds = existing.mergedIds || [existing.id];
                const itemIds = item.mergedIds || [item.id];
                existing.mergedIds = Array.from(new Set([...existingIds, ...itemIds]));

                // Keep the newest timestamp and copy content from the newer item
                if (new Date(item.timestamp).getTime() > new Date(existing.timestamp).getTime()) {
                    existing.timestamp = item.timestamp;
                    existing.title = item.title;
                    existing.message = item.message;
                    existing.actionUrl = item.actionUrl;
                    existing.actionLabel = item.actionLabel;
                }
                if (!item.read) {
                    existing.read = false;
                }
                continue;
            }
        }

        const clone = {
            ...item,
            mergedIds: item.mergedIds ? [...item.mergedIds] : [item.id],
        };
        seenMap.set(key, clone);
        result.push(clone);
    }

    return result;
}

// ---------------------------------------------------------------------------
// REST API Handlers with graceful fallback
// ---------------------------------------------------------------------------

const API_BASE = "/api/notifications";
export const NOTIF_STORAGE_KEY_PREFIX = "nester.notifications.v1";
const PREFS_STORAGE_KEY_PREFIX = "nester.notification_preferences.v1";

export function getStorageKey(prefix: string, address?: string) {
    return `${prefix}.${address || "guest"}`;
}

export async function fetchNotificationsApi(
    address?: string,
    since?: string
): Promise<AppNotification[]> {
    let fetchError: Error | null = null;
    try {
        const params = new URLSearchParams();
        if (since) params.set("since", since);
        if (address) params.set("address", address);
        const queryString = params.toString() ? `?${params.toString()}` : "";

        const signal = AbortSignal.timeout ? AbortSignal.timeout(5000) : undefined;
        const res = await fetch(`${API_BASE}${queryString}`, {
            headers: { Accept: "application/json" },
            signal,
        });

        if (res.ok) {
            const data = await res.json();
            if (Array.isArray(data)) {
                return data.map(normalizeNotification);
            }
            // Successful response but non-array body — treat as an error so
            // malformed payloads cannot silently reach the INITIAL_NOTIFICATIONS
            // return path or a stale localStorage cache.
            fetchError = new Error("Server returned unexpected non-array response");
        } else {
            fetchError = new Error(`Server returned status ${res.status}`);
        }
    } catch (err: unknown) {
        fetchError = err instanceof Error ? err : new Error("Network request failed");
    }

    // Attempt fallback to local storage
    if (typeof window !== "undefined") {
        const storageKey = getStorageKey(NOTIF_STORAGE_KEY_PREFIX, address);
        const raw = window.localStorage.getItem(storageKey);
        if (raw) {
            try {
                const parsed = JSON.parse(raw);
                if (Array.isArray(parsed) && parsed.length > 0) {
                    return parsed.map(normalizeNotification);
                }
            } catch {
                // Ignore parse errors
            }
        }
    }

    // If fetch failed and no local storage data exists, rethrow error to trigger provider Error UI
    if (fetchError) {
        throw fetchError;
    }

    return INITIAL_NOTIFICATIONS;
}

export async function markNotificationReadApi(id: string): Promise<boolean> {
    try {
        const res = await fetch(`${API_BASE}/${id}/read`, { method: "POST" });
        return res.ok;
    } catch {
        return false;
    }
}

export async function markAllNotificationsReadApi(): Promise<boolean> {
    try {
        const res = await fetch(`${API_BASE}/read-all`, { method: "POST" });
        return res.ok;
    } catch {
        return false;
    }
}

export async function dismissNotificationApi(id: string): Promise<boolean> {
    try {
        const res = await fetch(`${API_BASE}/${id}`, { method: "DELETE" });
        return res.ok;
    } catch {
        return false;
    }
}

export async function fetchNotificationPreferencesApi(
    address?: string
): Promise<UserNotificationPreferences> {
    try {
        const signal = AbortSignal.timeout ? AbortSignal.timeout(5000) : undefined;
        const res = await fetch(`${API_BASE}/preferences`, {
            headers: { Accept: "application/json" },
            signal,
        });
        if (res.ok) {
            const data = await res.json();
            if (data && data.categories) {
                return sanitizePreferences(data);
            }
        }
    } catch {
        // Fallback to local storage
    }

    if (typeof window !== "undefined") {
        const key = getStorageKey(PREFS_STORAGE_KEY_PREFIX, address);
        const raw = window.localStorage.getItem(key);
        if (raw) {
            try {
                return sanitizePreferences(JSON.parse(raw));
            } catch {
                // Ignore
            }
        }
    }

    return DEFAULT_NOTIFICATION_PREFERENCES;
}

export async function updateNotificationPreferencesApi(
    prefs: UserNotificationPreferences,
    address?: string
): Promise<UserNotificationPreferences> {
    const sanitized = sanitizePreferences(prefs);
    try {
        const signal = AbortSignal.timeout ? AbortSignal.timeout(5000) : undefined;
        const res = await fetch(`${API_BASE}/preferences`, {
            method: "PUT",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify(sanitized),
            signal,
        });
        if (res.ok) {
            const updated = await res.json();
            const result = sanitizePreferences(updated);
            // Also refresh the address-scoped localStorage cache on success
            if (typeof window !== "undefined") {
                const key = getStorageKey(PREFS_STORAGE_KEY_PREFIX, address);
                window.localStorage.setItem(key, JSON.stringify(result));
            }
            return result;
        }
    } catch {
        // Fallback to local persistence
    }

    if (typeof window !== "undefined") {
        const key = getStorageKey(PREFS_STORAGE_KEY_PREFIX, address);
        window.localStorage.setItem(key, JSON.stringify(sanitized));
    }

    return sanitized;
}

export function sanitizePreferences(
    input?: Partial<UserNotificationPreferences>
): UserNotificationPreferences {
    const base = DEFAULT_NOTIFICATION_PREFERENCES;
    if (!input || !input.categories) return base;

    const transactional = input.categories.transactional;
    const nudge = input.categories.nudge;

    return {
        categories: {
            // Safety notifications are ALWAYS ON per requirement
            safety: {
                in_app: true,
                email: true,
                push: true,
            },
            transactional: {
                in_app: transactional?.in_app ?? base.categories.transactional.in_app,
                email: transactional?.email ?? base.categories.transactional.email,
                push: transactional?.push ?? base.categories.transactional.push,
            },
            nudge: {
                in_app: nudge?.in_app ?? base.categories.nudge.in_app,
                email: nudge?.email ?? base.categories.nudge.email,
                push: nudge?.push ?? base.categories.nudge.push,
            },
        },
    };
}

export function normalizeNotification(item: unknown): AppNotification {
    const obj =
        item && typeof item === "object" ? (item as Record<string, unknown>) : {};

    const type: NotificationType =
        typeof obj.type === "string" ? (obj.type as NotificationType) : "info";

    const mapped = mapTypeToCategoryAndPriority(type);

    const validCategories: NotificationCategory[] = ["safety", "transactional", "nudge"];
    const category: NotificationCategory = validCategories.includes(
        obj.category as NotificationCategory
    )
        ? (obj.category as NotificationCategory)
        : mapped.category;

    const validPriorities: NotificationPriority[] = ["safety", "transactional", "nudge"];
    const priority: NotificationPriority = validPriorities.includes(
        obj.priority as NotificationPriority
    )
        ? (obj.priority as NotificationPriority)
        : mapped.priority;

    let timestampStr = new Date().toISOString();
    if (obj.timestamp) {
        const parsedMs = new Date(obj.timestamp as string).getTime();
        if (!isNaN(parsedMs)) {
            timestampStr = new Date(parsedMs).toISOString();
        }
    }

    const id = String(obj.id || `notif-${Date.now()}-${Math.random()}`);

    // Ensure mergedIds always contains at least the notification's own id.
    // An empty array is treated the same as a missing value.
    const rawMergedIds = Array.isArray(obj.mergedIds)
        ? (obj.mergedIds as unknown[]).map(String).filter(Boolean)
        : [];
    const mergedIds =
        rawMergedIds.length > 0
            ? Array.from(new Set(rawMergedIds))
            : [id];

    return {
        id,
        type,
        category,
        priority,
        title: String(obj.title || "Notification"),
        message: String(obj.message || ""),
        timestamp: timestampStr,
        read: Boolean(obj.read),
        actionUrl: typeof obj.actionUrl === "string" ? obj.actionUrl : undefined,
        actionLabel: typeof obj.actionLabel === "string" ? obj.actionLabel : undefined,
        coalesceKey: typeof obj.coalesceKey === "string" ? obj.coalesceKey : undefined,
        count: typeof obj.count === "number" ? obj.count : 1,
        mergedIds,
        metadata:
            obj.metadata && typeof obj.metadata === "object"
                ? (obj.metadata as Record<string, unknown>)
                : undefined,
    };
}
