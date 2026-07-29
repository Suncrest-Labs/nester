// WebSocket event types shared between the hook, provider, and consumers.
// Mirrors the shape the backend service (#146 / #870) will produce.

export type WSConnectionStatus = "connected" | "reconnecting" | "offline";

export type WSEventType =
    | "balance_updated"
    | "deposit_confirmed"
    | "withdrawal_confirmed"
    | "yield_accrued"
    | "settlement_status_changed"
    | "vault_paused"
    | "vault_unpaused"
    | "emergency_queue_fill"
    | "security_event"
    | "breaker_trip"
    | "goal_milestone"
    | "nudge_alert";

export interface WSEvent {
    type: WSEventType;
    channel: string;
    payload: Record<string, unknown>;
    timestamp: string;
}

// Payloads for each event type. Components can narrow via `event.type`.

export interface BalanceUpdatedPayload {
    asset: string;
    newBalance: number;
    previousBalance: number;
}

export interface DepositConfirmedPayload {
    vaultId: string;
    vaultName: string;
    amount: number;
    asset: string;
    txHash: string;
}

export interface WithdrawalConfirmedPayload {
    vaultId: string;
    vaultName: string;
    netAmount: number;
    asset: string;
    txHash: string;
}

export interface YieldAccruedPayload {
    positionId: string;
    deltaYield: number;
    asset: string;
}

export interface SettlementStatusChangedPayload {
    settlementId: string;
    status: string;
    message?: string;
}

export interface VaultPausedPayload {
    vaultId: string;
    reason?: string;
}

export interface VaultUnpausedPayload {
    vaultId: string;
}

export interface EmergencyQueueFillPayload {
    queueId: string;
    asset: string;
    fillPercentage: number;
    message?: string;
}

export interface SecurityEventPayload {
    eventId: string;
    eventType: string;
    details: string;
    severity?: "info" | "warning" | "high" | "critical";
}

export interface BreakerTripPayload {
    breakerId: string;
    asset?: string;
    reason?: string;
}

export interface GoalMilestonePayload {
    goalId: string;
    goalTitle: string;
    progress: number;
    message?: string;
}

export interface NudgeAlertPayload {
    nudgeId?: string;
    title: string;
    message: string;
    actionUrl?: string;
    actionLabel?: string;
}
