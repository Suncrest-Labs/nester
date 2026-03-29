
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
    prometheusInsights: number;
}

const VAULTS = ["Conservative Yield", "Balanced Growth", "DeFi500 Index", "Growth Strategy"];
const ASSETS = ["USDC", "XLM"];
const RISK_TIERS: RiskTier[] = ["Safe", "Balanced", "Aggressive", "Balanced"];
const APYS = ["6.4%", "11.2%", "18.9%", "9.5%"];

const generateMockTransactions = (count: number): Transaction[] => {
    const txs: Transaction[] = [];
    const now = new Date();

    for (let i = 0; i < count; i++) {
        const typeIdx = Math.floor(Math.random() * 4);
        const types: TransactionType[] = ["Deposit", "Withdrawal", "Yield Accrual", "Rebalance"];
        const type = types[typeIdx];

        const vault = VAULTS[Math.floor(Math.random() * VAULTS.length)];
        const asset = ASSETS[Math.floor(Math.random() * ASSETS.length)];

        const amount = type === "Rebalance" ? "0.00" :
            type === "Withdrawal" ? `-${(Math.random() * 500 + 50).toFixed(2)}` :
                `+${(Math.random() * 1500 + 10).toFixed(2)}`;

        const status: TransactionStatus = Math.random() > 0.9 ? "Failed" : (Math.random() > 0.8 ? "Pending" : "Confirmed");

        const date = new Date(now.getTime() - i * (Math.random() * 86400000 + 3600000));

        txs.push({
            id: (i + 1).toString(),
            type,
            amount,
            asset,
            vaultName: vault,
            timestamp: date.toISOString(),
            status,
            txHash: Array.from({ length: 64 }, () => Math.floor(Math.random() * 16).toString(16)).join(""),
        });
    }
    return txs;
};

export const mockTransactions: Transaction[] = generateMockTransactions(50);

export const mockVaultPositions: VaultPosition[] = VAULTS.map((name, i) => {
    const baseBalance = [243625.30, 152480.20, 91145.10, 45000.00][i % 4];
    const baseYield = [1245.12, 3842.50, 5410.20, 950.00][i % 4];

    return {
        id: (i + 1).toString(),
        vaultName: name,
        riskTier: RISK_TIERS[i % RISK_TIERS.length],
        balance: baseBalance,
        apy: APYS[i % APYS.length],
        yieldEarned: baseYield,
        nVaultBalance: Math.floor(baseBalance * 0.98).toString(),
        asset: "USDC",
        trendData: Array.from({ length: 7 }, () => Math.floor(Math.random() * 40))
    };
});

const totalBal = mockVaultPositions.reduce((acc, pos) => acc + pos.balance, 0);
const totalYield = mockVaultPositions.reduce((acc, pos) => acc + pos.yieldEarned, 0);

export const mockPortfolioStats: PortfolioStats = {
    totalBalance: totalBal,
    totalYieldEarned: totalYield,
    activeVaults: VAULTS.length,
    prometheusInsights: 3
};

export const mockPerformanceHistory = Array.from({ length: 30 }, (_, i) => {
    const progress = i / 30;
    const balance = totalBal * (0.92 + progress * 0.08 + (Math.random() * 0.01));
    const yieldAmt = totalYield * (0.7 + progress * 0.3 + (Math.random() * 0.05));
    return {
        date: new Date(Date.now() - (30 - i) * 86400000).toISOString().split('T')[0],
        balance,
        yield: yieldAmt,
        benchmark: totalBal * 0.9 + (i * 300)
    };
});


