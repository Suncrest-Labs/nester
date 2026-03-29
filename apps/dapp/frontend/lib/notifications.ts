export type NotificationType =
    | "deposit_confirmed"
    | "withdrawal_processed"
    | "ai_alert"
    | "rebalance_event"
    | "offramp_status"
    | "info";

export interface AppNotification {
    id: string;
    type: NotificationType;
    title: string;
    message: string;
    timestamp: string;
    read: boolean;
    actionUrl?: string;
    actionLabel?: string;
}

export interface NotificationDraft {
    type: NotificationType;
    title: string;
    message: string;
    actionUrl?: string;
    actionLabel?: string;
}

const now = Date.now();

function isoMinutesAgo(minutes: number) {
    return new Date(now - minutes * 60_000).toISOString();
}

export const INITIAL_NOTIFICATIONS: AppNotification[] = [
    {
        id: "seed-1",
        type: "deposit_confirmed",
        title: "Deposit Confirmed",
        message: "Deposited 500 USDC into Balanced Vault",
        timestamp: isoMinutesAgo(8),
        read: false,
    },
    {
        id: "seed-2",
        type: "withdrawal_processed",
        title: "Withdrawal Processed",
        message: "Withdrew 200 USDC from Growth Vault",
        timestamp: isoMinutesAgo(24),
        read: false,
    },
    {
        id: "seed-3",
        type: "ai_alert",
        title: "Prometheus Alert",
        message:
            "Prometheus: Your Balanced Vault APY dropped to 7.2%. Consider reviewing.",
        timestamp: isoMinutesAgo(56),
        read: false,
    },
    {
        id: "seed-4",
        type: "rebalance_event",
        title: "Vault Rebalanced",
        message:
            "Your Balanced Vault was rebalanced - new allocation: 45% Blend, 30% Aave, 25% Kamino",
        timestamp: isoMinutesAgo(145),
        read: true,
    },
    {
        id: "seed-5",
        type: "offramp_status",
        title: "Off-ramp Status",
        message: "Off-ramp settlement is now in queued state and awaiting LP confirmation.",
        timestamp: isoMinutesAgo(220),
        read: true,
    },
];
