// WebSocket event types shared between the hook, provider, and consumers.
// Mirrors the shape the backend service (#146) will produce.

export type WSConnectionStatus = "connected" | "reconnecting" | "offline";

export type WSEventType =
    | "auth_success"
    | "auth_error"
    | "balance_updated"
    | "deposit_confirmed"
    | "withdrawal_confirmed"
    | "yield_accrued"
    | "settlement_status_changed"
    | "vault_paused"
    | "vault_unpaused";

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
