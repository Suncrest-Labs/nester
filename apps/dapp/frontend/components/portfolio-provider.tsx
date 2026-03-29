"use client";

import {
    createContext,
    useContext,
    useEffect,
    useMemo,
    useState,
    type ReactNode,
} from "react";

import { useWallet } from "@/components/wallet-provider";
import { getVaultById, type VaultDefinition } from "@/lib/vault-data";

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
}

interface StoredPosition {
    id: string;
    vaultId: string;
    vaultName: string;
    asset: "USDC";
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
    vault: VaultDefinition;
    amount: number;
    txHash: string;
}

interface WithdrawalInput {
    positionId: string;
    grossAmount: number;
    txHash: string;
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
    recordWithdrawal: (input: WithdrawalInput) => WithdrawalQuote | null;
    /** Push a live balance update from WebSocket events */
    applyBalanceUpdate: (asset: string, newBalance: number) => void;
    /** Push a live yield accrual delta from WebSocket events */
    applyYieldAccrual: (positionId: string, deltaYield: number) => void;
}

const defaultBalances = {
    USDC: 0,
    USDT: 0,
    XLM: 0,
};

const PortfolioContext = createContext<PortfolioState | null>(null);

function storageKey(address: string) {
    return `nester_portfolio_v1:${address}`;
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

function createTransactionHash() {
    const alphabet = "abcdef0123456789";
    return Array.from({ length: 64 }, () => alphabet[Math.floor(Math.random() * alphabet.length)]).join("");
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
                transactions: parsed.transactions ?? [],
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

    const recordDeposit = ({ vault, amount, txHash }: DepositInput) => {
        const now = new Date();
        const maturityAt = new Date(now);
        maturityAt.setDate(maturityAt.getDate() + vault.lockDays);

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
                txHash: txHash || createTransactionHash(),
            },
            ...current,
        ]);
    };

    const recordWithdrawal = ({ positionId, grossAmount, txHash }: WithdrawalInput) => {
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
                txHash: txHash || createTransactionHash(),
            },
            ...current,
        ]);

        return quote;
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
                recordWithdrawal,
                applyBalanceUpdate,
                applyYieldAccrual,
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
