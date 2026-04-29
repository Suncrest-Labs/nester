"use client";

import {
    createContext,
    useCallback,
    useContext,
    useMemo,
    type ReactNode,
} from "react";
import { useWallet } from "@/components/wallet-provider";
import { usePortfolio } from "@/components/portfolio-provider";
import { useNotifications } from "@/components/notifications-provider";
import { useAuth } from "@/components/auth-provider";
import { useWebSocket, type UseWebSocketReturn } from "@/hooks/useWebSocket";
import {
    type WSConnectionStatus,
    type WSEvent,
    type BalanceUpdatedPayload,
    type DepositConfirmedPayload,
    type WithdrawalConfirmedPayload,
    type YieldAccruedPayload,
    type SettlementStatusChangedPayload,
    type VaultPausedPayload,
} from "@/lib/ws-events";
import { getExplorerTxUrl } from "@/utils/explorer";

// ---------------------------------------------------------------------------
// Context shape
// ---------------------------------------------------------------------------

interface WebSocketContextValue {
    /** Friendly connection state for UI indicators */
    status: WSConnectionStatus;
    /** True only when the socket is fully open */
    isConnected: boolean;
    /** True only when the socket is fully authenticated */
    isAuthenticated: boolean;
    /** The most recent raw event received */
    lastEvent: WSEvent | null;
    /** Imperatively subscribe to an additional channel */
    subscribe: UseWebSocketReturn["subscribe"];
    /** Imperatively unsubscribe from a channel */
    unsubscribe: UseWebSocketReturn["unsubscribe"];
    /** Force-close the socket and stop reconnects */
    disconnect: UseWebSocketReturn["disconnect"];
    /** Reset attempt counter and reconnect immediately */
    manualReconnect: UseWebSocketReturn["manualReconnect"];
}

const WebSocketContext = createContext<WebSocketContextValue>({
    status: "offline",
    isConnected: false,
    isAuthenticated: false,
    lastEvent: null,
    subscribe: () => {},
    unsubscribe: () => {},
    disconnect: () => {},
    manualReconnect: () => {},
});

// ---------------------------------------------------------------------------
// Provider
// ---------------------------------------------------------------------------

const WS_URL = process.env.NEXT_PUBLIC_WS_URL ?? "";

/**
 * WebSocketProvider
 *
 * Must be rendered **inside** <PortfolioProvider> and <NotificationsProvider>
 * so it can call usePortfolio() / useNotifications() to dispatch live updates.
 *
 * When NEXT_PUBLIC_WS_URL is not set the hook starts in "offline" mode; the
 * existing mock/localStorage flow is completely unaffected.
 */
export function WebSocketProvider({ children }: { children: ReactNode }) {
    const { address, disconnect: disconnectWallet } = useWallet();
    const { applyBalanceUpdate, applyYieldAccrual } = usePortfolio();
    const { addNotification } = useNotifications();
    const { token, setToken } = useAuth();

    // 🚨 SECURITY NOTE:
    // WebSocket authentication MUST use backend-issued JWT.
    // NEVER construct tokens on the frontend.
    // NEVER use wallet address as authentication.
    // Reintroducing mock auth will create a critical vulnerability.

    // Build the list of channels the connected user should subscribe to.
    const channels = useMemo<string[]>(() => {
        if (!address) return [];
        return [
            `user:${address}`,
            "vaults:global",
            "settlements:global",
        ];
    }, [address]);

    const handleEvent = useCallback(
        (event: WSEvent) => {
            switch (event.type) {
                case "balance_updated": {
                    const p = event.payload as unknown as BalanceUpdatedPayload;
                    applyBalanceUpdate(p.asset, p.newBalance);
                    break;
                }

                case "deposit_confirmed": {
                    const p = event.payload as unknown as DepositConfirmedPayload;
                    addNotification(
                        {
                            type: "deposit_confirmed",
                            title: "Deposit Confirmed",
                            message: `Deposited ${p.amount.toFixed(2)} ${p.asset} into ${p.vaultName}`,
                            actionUrl: getExplorerTxUrl(p.txHash),
                            actionLabel: "View on Explorer",
                        },
                        { showToast: true }
                    );
                    break;
                }

                case "withdrawal_confirmed": {
                    const p = event.payload as unknown as WithdrawalConfirmedPayload;
                    addNotification(
                        {
                            type: "withdrawal_processed",
                            title: "Withdrawal Confirmed",
                            message: `Received ${p.netAmount.toFixed(2)} ${p.asset} from ${p.vaultName}`,
                            actionUrl: getExplorerTxUrl(p.txHash),
                            actionLabel: "View on Explorer",
                        },
                        { showToast: true }
                    );
                    break;
                }

                case "yield_accrued": {
                    const p = event.payload as unknown as YieldAccruedPayload;
                    applyYieldAccrual(p.positionId, p.deltaYield);
                    break;
                }

                case "settlement_status_changed": {
                    const p = event.payload as unknown as SettlementStatusChangedPayload;
                    addNotification(
                        {
                            type: "offramp_status",
                            title: "Settlement Updated",
                            message:
                                p.message ??
                                `Settlement ${p.settlementId} is now ${p.status}`,
                        },
                        { showToast: true }
                    );
                    break;
                }

                case "vault_paused": {
                    const p = event.payload as unknown as VaultPausedPayload;
                    addNotification(
                        {
                            type: "rebalance_event",
                            title: "Vault Paused",
                            message: p.reason
                                ? `Vault paused: ${p.reason}`
                                : `Vault ${p.vaultId} has been paused by the operator.`,
                        },
                        { showToast: true }
                    );
                    break;
                }

                case "vault_unpaused": {
                    addNotification(
                        {
                            type: "rebalance_event",
                            title: "Vault Resumed",
                            message: "Deposits and withdrawals are now available again.",
                        },
                        { showToast: true }
                    );
                    break;
                }

                default:
                    break;
            }
        },
        [applyBalanceUpdate, applyYieldAccrual, addNotification]
    );

    const handleAuthError = useCallback((message: string) => {
        setToken(null);
        if (disconnectWallet) {
            disconnectWallet();
        }
        addNotification(
            {
                type: "offramp_status", // Re-using a generic type or create an auth specific one if it existed.
                title: "Session Expired",
                message: message || "Your session has expired. Please reconnect your wallet.",
            },
            { showToast: true }
        );
    }, [setToken, disconnectWallet, addNotification]);

    const {
        isConnected,
        isAuthenticated,
        status,
        lastEvent,
        subscribe,
        unsubscribe,
        disconnect,
        manualReconnect,
    } = useWebSocket({
        // If WS_URL is empty the hook immediately goes "offline" — safe to call.
        url: WS_URL,
        token,
        channels,
        onEvent: handleEvent,
        onAuthError: handleAuthError,
    });

    const value = useMemo<WebSocketContextValue>(
        () => ({
            status: WS_URL ? status : "offline",
            isConnected: WS_URL ? isConnected : false,
            isAuthenticated: WS_URL ? isAuthenticated : false,
            lastEvent,
            subscribe,
            unsubscribe,
            disconnect,
            manualReconnect,
        }),
        // WS_URL is a module-level constant — intentionally excluded from deps.
         
        [status, isConnected, isAuthenticated, lastEvent, subscribe, unsubscribe, disconnect, manualReconnect]
    );

    return (
        <WebSocketContext.Provider value={value}>
            {children}
        </WebSocketContext.Provider>
    );
}

// ---------------------------------------------------------------------------
// Hooks
// ---------------------------------------------------------------------------

/** Access raw connection state (status, isConnected, lastEvent, controls). */
export function useWebSocketContext() {
    return useContext(WebSocketContext);
}

/**
 * Convenience alias — mirrors the naming used elsewhere in the codebase
 * (useWallet, usePortfolio, useNotifications, useSettings).
 */
export function useWebSocketEvents() {
    return useContext(WebSocketContext);
}
