/**
 * Domain types for dashboard components.
 *
 * These live here (lib/types.ts) rather than in lib/mock-data.ts so that
 * production components never import from the mock module — making it
 * structurally impossible for mock values to drift back into shipped code.
 */

export type TransactionType = "Deposit" | "Withdrawal" | "Yield Accrual" | "Rebalance";
export type TransactionStatus = "Confirmed" | "Pending" | "Failed";

export interface Transaction {
    id: string;
    type: TransactionType;
    amount: string;
    asset: string;
    vaultName: string;
    timestamp: string;
    status: TransactionStatus;
    txHash: string;
    isOnChain?: boolean;
}

export type RiskTier = "Safe" | "Balanced" | "Aggressive";

export interface VaultPosition {
    id: string;
    vaultName: string;
    riskTier: RiskTier;
    balance: number;
    apy: string;
    yieldEarned: number;
    nVaultBalance: string;
    asset: string;
    trendData: number[];
}

export interface PortfolioStats {
    totalBalance: number;
    totalYieldEarned: number;
    activeVaults: number;
}

export type LoadingState = "loading" | "error" | "success" | "empty";
