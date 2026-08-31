"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  type ReactNode,
} from "react";
import { useWallet } from "@/components/wallet-provider";
import { usePortfolio } from "@/components/portfolio-provider";
import { useNotifications } from "@/components/notifications-provider";
import { useWebSocket, type UseWebSocketReturn } from "@/hooks/useWebSocket";
import { getAccessToken } from "@/lib/auth/token-store";
import {
  type WSConnectionStatus,
  type WSEvent,
  type BalanceUpdatedPayload,
  type DepositConfirmedPayload,
  type WithdrawalConfirmedPayload,
  type YieldAccruedPayload,
  type SettlementStatusChangedPayload,
  type VaultPausedPayload,
  type VaultUnpausedPayload,
  type EmergencyQueueFillPayload,
  type SecurityEventPayload,
  type BreakerTripPayload,
  type GoalMilestonePayload,
  type NudgeAlertPayload,
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
  /**
   * True when displayed values may no longer reflect the chain — i.e. the
   * socket is not live. Components rendering a balance must use this to
   * visually distinguish the value from a live one.
   */
  isStale: boolean;
  /** Epoch ms of the last confirmed sync (event or HTTP reconcile) */
  lastUpdatedAt: number | null;
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
  isStale: true,
  lastUpdatedAt: null,
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
 */
interface WebSocketProviderProps {
  children: ReactNode;
  /**
   * Channels to subscribe to instead of the wallet-derived set.
   *
   * Only the E2E harness passes this. The production tree subscribes on
   * behalf of a connected wallet, which a browser-driven test cannot
   * produce; without an override the harness would subscribe to nothing and
   * the re-subscribe-on-reconnect behaviour would be untestable.
   */
  channelsOverride?: string[];
  /**
   * Token to authenticate the socket with instead of the stored session one.
   *
   * Only the E2E harness passes this, for the same reason as
   * channelsOverride. The real token is minted by a wallet signature, and
   * AuthProvider clears the stored session whenever no wallet is connected,
   * so a browser-driven test cannot hold one: the socket would be refused
   * for want of a credential and every reconnection assertion would observe
   * "offline" rather than the behaviour under test.
   */
  tokenOverride?: string;
  /** Heartbeat ping interval in ms. Harness-only; see channelsOverride. */
  heartbeatInterval?: number;
  /** Pong grace period in ms. Harness-only; see channelsOverride. */
  heartbeatTimeout?: number;
}

export function WebSocketProvider({
  children,
  channelsOverride,
  tokenOverride,
  heartbeatInterval,
  heartbeatTimeout,
}: WebSocketProviderProps) {
  const { address } = useWallet();
  const { applyBalanceUpdate, applyYieldAccrual, refreshBalances } =
    usePortfolio();
  const { addNotification, setConnectionState } = useNotifications();

  // Get current token from token store. Real session token, not a forged one.
  // Pass a token getter function to useWebSocket so it always uses the current token
  // before each connection attempt (not just at render time).
  const getToken = useCallback(
    () => tokenOverride ?? getAccessToken(),
    [tokenOverride],
  );

  const walletChannels = useMemo<string[]>(() => {
    if (!address) return [];
    return [
      `user:${address}`,
      "vaults:global",
      "settlements:global",
      "notifications:safety",
    ];
  }, [address]);

  const channels = channelsOverride ?? walletChannels;

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
            { showToast: true },
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
            { showToast: true },
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
                p.message ?? `Settlement ${p.settlementId} is now ${p.status}`,
              actionUrl: "/offramp",
              actionLabel: "View Off-ramp",
            },
            { showToast: true },
          );
          break;
        }

        case "vault_paused": {
          const p = event.payload as unknown as VaultPausedPayload;
          addNotification(
            {
              type: "breaker_trip",
              category: "safety",
              priority: "safety",
              title: "Vault Paused / Circuit Breaker",
              message: p.reason
                ? `Vault paused: ${p.reason}`
                : `Vault ${p.vaultId} has been paused by the safety circuit breaker.`,
              actionUrl: "/vaults",
              actionLabel: "View Vault Status",
            },
            { showToast: true },
          );
          break;
        }

        case "vault_unpaused": {
          const p = event.payload as unknown as VaultUnpausedPayload;
          addNotification(
            {
              type: "rebalance_event",
              category: "transactional",
              priority: "transactional",
              title: "Vault Resumed",
              message: `Vault ${p.vaultId || "operations"} has been resumed. Deposits and withdrawals are active.`,
              actionUrl: "/vaults",
              actionLabel: "View Vault",
            },
            { showToast: true },
          );
          break;
        }

        case "emergency_queue_fill": {
          const p = event.payload as unknown as EmergencyQueueFillPayload;
          addNotification(
            {
              type: "emergency_queue_fill",
              category: "safety",
              priority: "safety",
              title: "Emergency Queue Warning",
              message:
                p.message ||
                `Emergency queue ${p.queueId} (${p.asset}) fill level reached ${p.fillPercentage}%.`,
              actionUrl: "/vaults",
              actionLabel: "Manage Emergency Queue",
              coalesceKey: `emergency_queue_fill_${p.asset}`,
            },
            { showToast: true },
          );
          break;
        }

        case "security_event": {
          const p = event.payload as unknown as SecurityEventPayload;
          addNotification(
            {
              type: "security_event",
              category: "safety",
              priority: "safety",
              title: "Security Event Detected",
              message:
                p.details ||
                `Security event ${p.eventType} flagged on account.`,
              actionUrl: "/settings",
              actionLabel: "Security Settings",
            },
            { showToast: true },
          );
          break;
        }

        case "breaker_trip": {
          const p = event.payload as unknown as BreakerTripPayload;
          addNotification(
            {
              type: "breaker_trip",
              category: "safety",
              priority: "safety",
              title: "Circuit Breaker Tripped",
              message:
                p.reason ||
                `Circuit breaker ${p.breakerId} tripped for ${p.asset || "vault"}.`,
              actionUrl: "/vaults",
              actionLabel: "Review Circuit Breaker",
            },
            { showToast: true },
          );
          break;
        }

        case "goal_milestone": {
          const p = event.payload as unknown as GoalMilestonePayload;
          addNotification(
            {
              type: "goal_milestone",
              title: "Goal Milestone Reached!",
              message:
                p.message ||
                `Congratulations! You reached ${p.progress}% of your ${p.goalTitle} goal.`,
              actionUrl: "/savings",
              actionLabel: "View Savings Goal",
            },
            { showToast: true },
          );
          break;
        }

        case "nudge_alert": {
          const p = event.payload as unknown as NudgeAlertPayload;
          addNotification(
            {
              type: "nudge_recommendation",
              title: p.title || "New Nudge",
              message: p.message,
              actionUrl: p.actionUrl || "/savings",
              actionLabel: p.actionLabel || "Learn More",
            },
            { showToast: true },
          );
          break;
        }

        default:
          break;
      }
    },
    [applyBalanceUpdate, applyYieldAccrual, addNotification],
  );

  // Pull the authoritative balances over HTTP after every (re)connect.
  // The hub replays a bounded per-channel history on subscribe, which is
  // not the same as a snapshot — reconciling is what makes a reconnected
  // client's numbers trustworthy rather than merely recent.
  //
  // Notifications reconcile on their own via setConnectionState below;
  // duplicating that call here would double-fetch on every reconnect.
  const reconcile = useCallback(async () => {
    await refreshBalances();
  }, [refreshBalances]);

  const {
    isConnected,
    status,
    lastEvent,
    lastUpdatedAt,
    subscribe,
    unsubscribe,
    disconnect,
    manualReconnect,
  } = useWebSocket({
    url: WS_URL,
    getToken,
    channels,
    onEvent: handleEvent,
    onPoll: refreshBalances,
    onReconcile: reconcile,
    heartbeatInterval,
    heartbeatTimeout,
  });

  const hasMountedRef = useRef(false);
  useEffect(() => {
    // Skip the initial invocation on mount so that merely mounting the
    // provider (or a wallet-address change) does not trigger a redundant
    // reconciliation fetch.  Only actual connection-state transitions
    // (true→false or false→true) should be reported.
    if (!hasMountedRef.current) {
      hasMountedRef.current = true;
      return;
    }
    setConnectionState(WS_URL ? isConnected : false);
  }, [isConnected, setConnectionState]);

  const value = useMemo<WebSocketContextValue>(() => {
    const effectiveStatus: WSConnectionStatus = WS_URL ? status : "offline";
    return {
      status: effectiveStatus,
      isConnected: WS_URL ? isConnected : false,
      isStale: effectiveStatus !== "connected",
      lastUpdatedAt,
      lastEvent,
      subscribe,
      unsubscribe,
      disconnect,
      manualReconnect,
    };
  }, [
    status,
    isConnected,
    lastUpdatedAt,
    lastEvent,
    subscribe,
    unsubscribe,
    disconnect,
    manualReconnect,
  ]);

  return (
    <WebSocketContext.Provider value={value}>
      {children}
    </WebSocketContext.Provider>
  );
}

export function useWebSocketContext() {
  return useContext(WebSocketContext);
}

export function useWebSocketEvents() {
  return useContext(WebSocketContext);
}
