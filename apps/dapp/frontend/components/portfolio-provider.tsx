"use client";

import {
    createContext,
    useContext,
    useEffect,
    useMemo,
    useRef,
    useState,
    type ReactNode,
} from "react";

import { useWallet } from "@/components/wallet-provider";
import { getVaultContractById as getVaultById, type SupportedAsset } from "@/lib/vault-contracts";
import { useNetwork } from "@/hooks/useNetwork";

export type PortfolioTransactionType =
    | "Deposit"
    | "Withdrawal"
    | "Yield Accrual"
    | "Rebalance";
export type PortfolioTransactionStatus = "Confirmed" | "Pending" | "Failed";

export interface PortfolioTransaction {
    id: string;
    type: PortfolioTransactionType;
    amount: string;
    asset: string;
    vaultName: string;
    timestamp: string;
    status: PortfolioTransactionStatus;
    txHash: string;
    isOnChain?: boolean;
}

export interface PendingTransactionRecord {
    /** The portfolio transaction id to update after on-chain confirmation. */
    transactionId: string;
    /** The vault id this deposit is for. */
    vaultId: string;
    /** Human-readable vault name. */
    vaultName: string;
    /** Asset deposited. */
    asset: SupportedAsset;
    /** Amount deposited. */
    amount: number;
    /** APY of the vault. */
    apy: number;
    /** Lock period in days. */
    lockDays: number;
    /** Early withdrawal penalty percentage. */
    earlyWithdrawalPenaltyPct: number;
}

interface StoredPosition {
    id: string;
    vaultId: string;
    vaultName: string;
    asset: SupportedAsset;
    principal: number;
    shares: number;
    apy: number;
    depositedAt: string;
    maturityAt: string;
    earlyWithdrawalPenaltyPct: number;
}

export interface PortfolioPosition extends StoredPosition {
    currentValue: number;
    yieldEarned: number;
    isMatured: boolean;
    daysRemaining: number;
}

interface DepositInput {
    vault: {
        id: string;
        name: string;
        asset: SupportedAsset;
        apy: number;
        lockDays: number | null;
        earlyWithdrawalPenaltyPct: number;
    };
    amount: number;
    txHash: string;
    isOnChain?: boolean;
}

/** Input for a deposit that has not been submitted yet. Deliberately omits
 *  txHash: a pending deposit has no transaction hash until the chain accepts
 *  it, and requiring one forces callers to invent a placeholder. */
type PendingDepositInput = Omit<DepositInput, "txHash">;

interface TransferInput {
    fromPositionId: string;
    toVault: {
        id: string;
        name: string;
        asset: SupportedAsset;
        apy: number;
        lockDays: number | null;
        earlyWithdrawalPenaltyPct: number;
    };
    amount: number;
    txHash: string;
}

interface WithdrawalInput {
    positionId: string;
    grossAmount: number;
    txHash: string;
    isOnChain?: boolean;
}

interface WithdrawalQuote {
    grossAmount: number;
    penaltyPct: number;
    penaltyAmount: number;
    netAmount: number;
    sharesBurned: number;
    isMatured: boolean;
    daysRemaining: number;
}

interface PortfolioState {
    balances: Record<string, number>;
    positions: PortfolioPosition[];
    transactions: PortfolioTransaction[];
    getAvailableBalance: (asset?: string) => number;
    getWithdrawalQuote: (positionId: string, grossAmount: number) => WithdrawalQuote | null;
    recordDeposit: (input: DepositInput) => void;
    recordTransfer: (input: TransferInput) => void;
    recordWithdrawal: (input: WithdrawalInput) => WithdrawalQuote | null;
    /** Record a pending deposit before on-chain confirmation. Returns the transaction id. */
    recordPendingDeposit: (input: PendingDepositInput) => string;
    /** Mark a pending deposit as confirmed after on-chain success. */
    confirmPendingDeposit: (transactionId: string, txHash: string) => void;
    /** Mark a pending deposit as failed after on-chain failure. */
    failPendingDeposit: (transactionId: string) => void;
    /** Push a live balance update from WebSocket events */
    applyBalanceUpdate: (asset: string, newBalance: number) => void;
    /** Push a live yield accrual delta from WebSocket events */
    applyYieldAccrual: (positionId: string, deltaYield: number) => void;
    /** Re-fetch wallet balances from Horizon (call after on-chain tx confirms) */
    refreshBalances: () => Promise<void>;
}

const defaultBalances = {
    USDC: 0,
    USDT: 0,
    XLM: 0,
};

const PortfolioContext = createContext<PortfolioState | null>(null);

function storageKey(address: string) {
    return `nester_portfolio_v2:${address}`;
}

function calculatePositionMetrics(position: StoredPosition): PortfolioPosition {
    const now = new Date();
    const depositedAt = new Date(position.depositedAt);
    const maturityAt = new Date(position.maturityAt);
    const elapsedMs = Math.max(0, now.getTime() - depositedAt.getTime());
    const elapsedDays = elapsedMs / (1000 * 60 * 60 * 24);
    const accruedYield = position.principal * position.apy * (elapsedDays / 365);
    const currentValue = position.principal + accruedYield;
    const msRemaining = maturityAt.getTime() - now.getTime();
    const daysRemaining = Math.max(0, Math.ceil(msRemaining / (1000 * 60 * 60 * 24)));

    return {
        ...position,
        currentValue,
        yieldEarned: currentValue - position.principal,
        isMatured: daysRemaining === 0,
        daysRemaining,
    };
}


export function PortfolioProvider({ children }: { children: ReactNode }) {
    const { address } = useWallet();
    return (
        <PortfolioStore key={address ?? "guest"} address={address}>
            {children}
        </PortfolioStore>
    );
}

function PortfolioStore({
    address,
    children,
}: {
    address: string | null;
    children: ReactNode;
}) {
    const { currentNetwork } = useNetwork();

    const initialState = useMemo(() => {
        if (!address || typeof window === "undefined") {
            return {
                balances: defaultBalances,
                positions: [] as StoredPosition[],
                transactions: [] as PortfolioTransaction[],
            };
        }

        const raw = window.localStorage.getItem(storageKey(address));
        if (!raw) {
            return {
                balances: defaultBalances,
                positions: [] as StoredPosition[],
                transactions: [] as PortfolioTransaction[],
            };
        }

        try {
            const parsed = JSON.parse(raw) as {
                balances?: Record<string, number>;
                positions?: StoredPosition[];
                transactions?: PortfolioTransaction[];
            };
            return {
                balances: parsed.balances ?? defaultBalances,
                positions: parsed.positions ?? [],
                transactions: (parsed.transactions ?? []).filter((t) => t.isOnChain === true),
            };
        } catch {
            return {
                balances: defaultBalances,
                positions: [] as StoredPosition[],
                transactions: [] as PortfolioTransaction[],
            };
        }
    }, [address]);

    const [balances, setBalances] = useState<Record<string, number>>(
        initialState.balances
    );
    const [storedPositions, setStoredPositions] = useState<StoredPosition[]>(
        initialState.positions
    );
    const [transactions, setTransactions] = useState<PortfolioTransaction[]>(
        initialState.transactions
    );

    // Holds the details of deposits awaiting on-chain confirmation. A ref
    // rather than state: confirmPendingDeposit runs in the same tick the
    // pending row is inserted, so any state read would still be stale.
    const pendingDepositsRef = useRef<Map<string, PendingTransactionRecord>>(new Map());

    useEffect(() => {
        if (!address || typeof window === "undefined") return;
        window.localStorage.setItem(
            storageKey(address),
            JSON.stringify({
                balances,
                positions: storedPositions,
                transactions,
            })
        );
    }, [address, balances, storedPositions, transactions]);

    // Sync real on-chain balances from Horizon whenever address or network changes
    useEffect(() => {
        if (!address) return;

        const fetchOnChainBalances = async () => {
            try {
                const res = await fetch(
                    `${currentNetwork.horizonUrl}/accounts/${address}`
                );
                if (!res.ok) return;
                const data = await res.json() as {
                    balances?: Array<{
                        asset_type: string;
                        asset_code?: string;
                        balance: string;
                    }>;
                };
                const raw = data.balances ?? [];

                const xlm = raw.find((b) => b.asset_type === "native");
                const usdc = raw.find(
                    (b) => b.asset_type !== "native" && b.asset_code === "USDC"
                );

                setBalances((prev) => ({
                    ...prev,
                    XLM: xlm ? parseFloat(xlm.balance) : (prev.XLM ?? 0),
                    USDC: usdc ? parseFloat(usdc.balance) : (prev.USDC ?? 0),
                }));
            } catch {
                // silently ignore — local balances remain as fallback
            }
        };

        fetchOnChainBalances();
    }, [address, currentNetwork.horizonUrl]);

    const refreshBalances = async () => {
        if (!address) return;
        try {
            const res = await fetch(
                `${currentNetwork.horizonUrl}/accounts/${address}`
            );
            if (!res.ok) return;
            const data = await res.json() as {
                balances?: Array<{
                    asset_type: string;
                    asset_code?: string;
                    balance: string;
                }>;
            };
            const raw = data.balances ?? [];
            const xlm = raw.find((b) => b.asset_type === "native");
            const usdc = raw.find(
                (b) => b.asset_type !== "native" && b.asset_code === "USDC"
            );
            setBalances((prev) => ({
                ...prev,
                XLM: xlm ? parseFloat(xlm.balance) : (prev.XLM ?? 0),
                USDC: usdc ? parseFloat(usdc.balance) : (prev.USDC ?? 0),
            }));
        } catch {
            // silently ignore
        }
    };

    const positions = useMemo(
        () =>
            storedPositions
                .map(calculatePositionMetrics)
                .sort(
                    (a, b) =>
                        new Date(b.depositedAt).getTime() - new Date(a.depositedAt).getTime()
                ),
        [storedPositions]
    );

    const getAvailableBalance = (asset = "USDC") => balances[asset] ?? 0;

    // WebSocket live-update helpers — additive only, existing flow unchanged.
    const applyBalanceUpdate = (asset: string, newBalance: number) => {
        setBalances((current) => ({ ...current, [asset]: newBalance }));
    };

    const applyYieldAccrual = (positionId: string, deltaYield: number) => {
        setStoredPositions((current) =>
            current.map((position) =>
                position.id === positionId
                    ? { ...position, principal: position.principal + deltaYield }
                    : position
            )
        );
    };

    const getWithdrawalQuote = (positionId: string, grossAmount: number) => {
        const position = positions.find((item) => item.id === positionId);
        if (!position || grossAmount <= 0 || grossAmount > position.currentValue) {
            return null;
        }

        const ratio = grossAmount / position.currentValue;
        const sharesBurned = position.shares * ratio;
        const penaltyPct = position.isMatured ? 0 : position.earlyWithdrawalPenaltyPct;
        const penaltyAmount = grossAmount * (penaltyPct / 100);

        return {
            grossAmount,
            penaltyPct,
            penaltyAmount,
            netAmount: grossAmount - penaltyAmount,
            sharesBurned,
            isMatured: position.isMatured,
            daysRemaining: position.daysRemaining,
        };
    };

    const recordDeposit = ({ vault, amount, txHash, isOnChain }: DepositInput) => {
        const now = new Date();
        const maturityAt = new Date(now);
        maturityAt.setDate(maturityAt.getDate() + (vault.lockDays ?? 0));

        const shares = amount;
        const position: StoredPosition = {
            id: crypto.randomUUID(),
            vaultId: vault.id,
            vaultName: vault.name,
            asset: vault.asset,
            principal: amount,
            shares,
            apy: vault.apy,
            depositedAt: now.toISOString(),
            maturityAt: maturityAt.toISOString(),
            earlyWithdrawalPenaltyPct: vault.earlyWithdrawalPenaltyPct,
        };

        setBalances((current) => ({
            ...current,
            [vault.asset]: Math.max(0, (current[vault.asset] ?? 0) - amount),
        }));
        setStoredPositions((current) => [position, ...current]);
        setTransactions((current) => [
            {
                id: crypto.randomUUID(),
                type: "Deposit",
                amount: `+${amount.toFixed(2)}`,
                asset: vault.asset,
                vaultName: vault.name,
                timestamp: now.toISOString(),
                status: "Confirmed",
                txHash: txHash ?? "",
                isOnChain: isOnChain ?? false,
            },
            ...current,
        ]);
    };

    /** Record a deposit as Pending before on-chain confirmation. Returns the
     *  transaction id so the modal can update it later without touching the
     *  wallet balance until the tx is confirmed. */
    const recordPendingDeposit = ({ vault, amount }: PendingDepositInput): string => {
        const txId = crypto.randomUUID();
        // Stash everything the confirm step needs. Recovering it later by
        // reading `transactions` fails: the confirm call happens in the same
        // tick as this insert, so the render closure has not seen it yet, and
        // parsing the amount back out of a display string is lossy.
        pendingDepositsRef.current.set(txId, {
            transactionId: txId,
            vaultId: vault.id,
            vaultName: vault.name,
            asset: vault.asset,
            amount,
            apy: vault.apy,
            lockDays: vault.lockDays ?? 0,
            earlyWithdrawalPenaltyPct: vault.earlyWithdrawalPenaltyPct,
        });
        setTransactions((current) => [
            {
                id: txId,
                type: "Deposit",
                amount: `+${amount.toFixed(2)}`,
                asset: vault.asset,
                vaultName: vault.name,
                timestamp: new Date().toISOString(),
                status: "Pending",
                txHash: "",
                isOnChain: true,
            },
            ...current,
        ]);
        return txId;
    };

    /** Confirm a pending deposit: create the position, deduct balance, update tx status. */
    const confirmPendingDeposit = (transactionId: string, txHash: string) => {
        const pending = pendingDepositsRef.current.get(transactionId);
        if (!pending) return;
        pendingDepositsRef.current.delete(transactionId);

        const now = new Date();
        const maturityAt = new Date(now);
        maturityAt.setDate(maturityAt.getDate() + pending.lockDays);

        // Create the position on confirmation, not on submission. Until the
        // chain confirms, the user holds no shares; creating it earlier would
        // show a position for a deposit that may still fail.
        const position: StoredPosition = {
            id: crypto.randomUUID(),
            vaultId: pending.vaultId,
            vaultName: pending.vaultName,
            asset: pending.asset,
            principal: pending.amount,
            shares: pending.amount,
            apy: pending.apy,
            depositedAt: now.toISOString(),
            maturityAt: maturityAt.toISOString(),
            earlyWithdrawalPenaltyPct: pending.earlyWithdrawalPenaltyPct,
        };

        setStoredPositions((current) => [position, ...current]);
        setBalances((current) => ({
            ...current,
            [pending.asset]: Math.max(0, (current[pending.asset] ?? 0) - pending.amount),
        }));
        setTransactions((current) =>
            current.map((t) =>
                t.id === transactionId ? { ...t, status: "Confirmed" as const, txHash } : t
            )
        );
    };

    /** Mark a pending deposit as failed and remove the pending transaction record. */
    const failPendingDeposit = (transactionId: string) => {
        pendingDepositsRef.current.delete(transactionId);
        setTransactions((current) =>
            current.map((t) =>
                t.id === transactionId ? { ...t, status: "Failed" as const } : t
            )
        );
    };

    const recordWithdrawal = ({ positionId, grossAmount, txHash, isOnChain }: WithdrawalInput) => {
        const quote = getWithdrawalQuote(positionId, grossAmount);
        if (!quote) return null;

        const target = positions.find((item) => item.id === positionId);
        if (!target) return null;

        setBalances((current) => ({
            ...current,
            [target.asset]: (current[target.asset] ?? 0) + quote.netAmount,
        }));

        setStoredPositions((current) =>
            current.flatMap((position) => {
                if (position.id !== positionId) return [position];

                const live = calculatePositionMetrics(position);
                const ratio = quote.grossAmount / live.currentValue;
                const nextPrincipal = Math.max(0, position.principal - position.principal * ratio);
                const nextShares = Math.max(0, position.shares - quote.sharesBurned);

                if (nextPrincipal <= 0.01 || nextShares <= 0.01) {
                    return [];
                }

                return [
                    {
                        ...position,
                        principal: nextPrincipal,
                        shares: nextShares,
                    },
                ];
            })
        );

        setTransactions((current) => [
            {
                id: crypto.randomUUID(),
                type: "Withdrawal",
                amount: `-${quote.netAmount.toFixed(2)}`,
                asset: target.asset,
                vaultName: target.vaultName,
                timestamp: new Date().toISOString(),
                status: "Confirmed",
                txHash: txHash ?? "",
                isOnChain: isOnChain ?? false,
            },
            ...current,
        ]);

        return quote;
    };

    const recordTransfer = ({ fromPositionId, toVault, amount, txHash }: TransferInput) => {
        const source = positions.find((p) => p.id === fromPositionId);
        if (!source || amount <= 0 || amount > source.currentValue) return;

        const ratio = amount / source.currentValue;
        const sharesBurned = source.shares * ratio;

        // Reduce / remove source position
        setStoredPositions((current) =>
            current.flatMap((pos) => {
                if (pos.id !== fromPositionId) return [pos];
                const live = calculatePositionMetrics(pos);
                const nextPrincipal = Math.max(0, pos.principal - pos.principal * ratio);
                const nextShares = Math.max(0, pos.shares - sharesBurned);
                if (nextPrincipal <= 0.01 || nextShares <= 0.01) return [];
                return [{ ...pos, principal: nextPrincipal, shares: nextShares }];
            })
        );

        // Create new position in destination vault
        const now = new Date();
        const maturityAt = new Date(now);
        maturityAt.setDate(maturityAt.getDate() + (toVault.lockDays ?? 0));
        const newPosition: StoredPosition = {
            id: crypto.randomUUID(),
            vaultId: toVault.id,
            vaultName: toVault.name,
            asset: toVault.asset,
            principal: amount,
            shares: amount,
            apy: toVault.apy,
            depositedAt: now.toISOString(),
            maturityAt: maturityAt.toISOString(),
            earlyWithdrawalPenaltyPct: toVault.earlyWithdrawalPenaltyPct,
        };
        setStoredPositions((current) => [newPosition, ...current]);

        setTransactions((current) => [
            {
                id: crypto.randomUUID(),
                type: "Rebalance",
                amount: `${amount.toFixed(2)}`,
                asset: toVault.asset,
                vaultName: `${source.vaultName} → ${toVault.name}`,
                timestamp: now.toISOString(),
                status: "Confirmed",
                txHash: txHash ?? "",
            },
            ...current,
        ]);
    };

    return (
        <PortfolioContext.Provider
            value={{
                balances,
                positions,
                transactions,
                getAvailableBalance,
                getWithdrawalQuote,
                recordDeposit,
                recordTransfer,
                recordWithdrawal,
                recordPendingDeposit,
                confirmPendingDeposit,
                failPendingDeposit,
                applyBalanceUpdate,
                applyYieldAccrual,
                refreshBalances,
            }}
        >
            {children}
        </PortfolioContext.Provider>
    );
}

export function usePortfolio() {
    const context = useContext(PortfolioContext);
    if (!context) {
        throw new Error("usePortfolio must be used within PortfolioProvider");
    }
    return context;
}

export function getVaultForPosition(position: PortfolioPosition) {
    return getVaultById(position.vaultId);
}
