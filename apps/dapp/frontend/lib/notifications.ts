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
    },
];

/**
 * Deduplicates / coalesces notifications with matching `coalesceKey` or
 * identical type + title within a 15-minute window.
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
            // Coalesce within 15 minutes (900,000 ms) or if explicitly set coalesceKey
            if (item.coalesceKey || timeDiff < 15 * 60 * 1000) {
                existing.count = (existing.count || 1) + 1;
                // keep the newest timestamp and read state if unread
                if (new Date(item.timestamp).getTime() > new Date(existing.timestamp).getTime()) {
                    existing.timestamp = item.timestamp;
                }
                if (!item.read) {
                    existing.read = false;
                }
                continue;
            }
        }

        const clone = { ...item };
        seenMap.set(key, clone);
        result.push(clone);
    }

    return result;
}

// ---------------------------------------------------------------------------
// REST API Handlers with graceful fallback
// ---------------------------------------------------------------------------

const API_BASE = "/api/notifications";
const NOTIF_STORAGE_KEY = "nester.notifications.v1";
const PREFS_STORAGE_KEY = "nester.notification_preferences.v1";

export async function fetchNotificationsApi(since?: string): Promise<AppNotification[]> {
    try {
        const url = since ? `${API_BASE}?since=${encodeURIComponent(since)}` : API_BASE;
        const res = await fetch(url, { headers: { Accept: "application/json" } });
        if (res.ok) {
            const data = await res.json();
            if (Array.isArray(data)) {
                return data.map(normalizeNotification);
            }
        }
    } catch {
        // Fallback to local storage / seed data on network or backend unavailability
    }

    if (typeof window !== "undefined") {
        const raw = window.localStorage.getItem(NOTIF_STORAGE_KEY);
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

    return INITIAL_NOTIFICATIONS;
}

export async function markNotificationReadApi(id: string): Promise<boolean> {
    try {
        const res = await fetch(`${API_BASE}/${id}/read`, { method: "POST" });
        if (res.ok) return true;
    } catch {
        // Fallback sync handled locally
    }
    return true;
}

export async function markAllNotificationsReadApi(): Promise<boolean> {
    try {
        const res = await fetch(`${API_BASE}/read-all`, { method: "POST" });
        if (res.ok) return true;
    } catch {
        // Fallback sync handled locally
    }
    return true;
}

export async function dismissNotificationApi(id: string): Promise<boolean> {
    try {
        const res = await fetch(`${API_BASE}/${id}`, { method: "DELETE" });
        if (res.ok) return true;
    } catch {
        // Fallback sync handled locally
    }
    return true;
}

export async function fetchNotificationPreferencesApi(): Promise<UserNotificationPreferences> {
    try {
        const res = await fetch(`${API_BASE}/preferences`, { headers: { Accept: "application/json" } });
        if (res.ok) {
            const data = await res.json();
            if (data && data.categories) {
                return sanitizePreferences(data);
            }
        }
    } catch {
        // Fallback
    }

    if (typeof window !== "undefined") {
        const raw = window.localStorage.getItem(PREFS_STORAGE_KEY);
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
    prefs: UserNotificationPreferences
): Promise<UserNotificationPreferences> {
    const sanitized = sanitizePreferences(prefs);
    try {
        const res = await fetch(`${API_BASE}/preferences`, {
            method: "PUT",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify(sanitized),
        });
        if (res.ok) {
            const updated = await res.json();
            return sanitizePreferences(updated);
        }
    } catch {
        // Fallback to local persistence
    }

    if (typeof window !== "undefined") {
        window.localStorage.setItem(PREFS_STORAGE_KEY, JSON.stringify(sanitized));
    }

    return sanitized;
}

export function sanitizePreferences(
    input?: Partial<UserNotificationPreferences>
): UserNotificationPreferences {
    const base = DEFAULT_NOTIFICATION_PREFERENCES;
    if (!input || !input.categories) return base;

    const safety = input.categories.safety;
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

export function normalizeNotification(item: any): AppNotification {
    const type: NotificationType = item.type || "info";
    const mapped = mapTypeToCategoryAndPriority(type);

    return {
        id: String(item.id || `notif-${Date.now()}-${Math.random()}`),
        type,
        category: item.category || mapped.category,
        priority: item.priority || mapped.priority,
        title: String(item.title || "Notification"),
        message: String(item.message || ""),
        timestamp: item.timestamp ? new Date(item.timestamp).toISOString() : new Date().toISOString(),
        read: Boolean(item.read),
        actionUrl: item.actionUrl,
        actionLabel: item.actionLabel,
        coalesceKey: item.coalesceKey,
        count: item.count,
        metadata: item.metadata,
    };
}
