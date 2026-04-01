import type {
  Vault,
  VaultAllocation,
  ApyDataPoint,
  RiskTier,
} from "@/lib/types/vault";

export type { Vault, VaultAllocation, ApyDataPoint, RiskTier };

function generateApyHistory(
  baseApy: number,
  volatility: number,
): ApyDataPoint[] {
  const today = new Date();
  const points: ApyDataPoint[] = [];
  let current = baseApy;
  for (let i = 89; i >= 0; i--) {
    const date = new Date(today);
    date.setDate(date.getDate() - i);
    const direction = ((i * 7) % 3) - 1;
    current = Math.max(
      1,
      current +
        direction * volatility * 0.4 +
        (i % 5 === 0 ? volatility * 0.3 : 0),
    );
    points.push({
      date: date.toISOString().split("T")[0],
      apy: parseFloat(current.toFixed(2)),
    });
  }
  return points;
}

export function formatTvl(value: number): string {
  if (value >= 1_000_000_000) return `$${(value / 1_000_000_000).toFixed(1)}B`;
  if (value >= 1_000_000) return `$${(value / 1_000_000).toFixed(1)}M`;
  if (value >= 1_000) return `$${(value / 1_000).toFixed(0)}K`;
  return `$${value}`;
}

export const VAULTS: Vault[] = [
  {
    id: "conservative",
    name: "Conservative Vault",
    description:
      "Focus on safety and stability using battle-tested lending protocols. Optimized for capital preservation with consistent, low-volatility yields across audited DeFi protocols.",
    riskTier: "Conservative",
    currentApy: 7.4,
    apyRange: "6–8%",
    tvl: 2_400_000,
    userCount: 1_243,
    allocations: [
      { protocol: "Blend", percentage: 40, apy: 6.2, color: "#2EBAC6" },
      { protocol: "Aave", percentage: 35, apy: 8.1, color: "#B6509E" },
      { protocol: "Compound", percentage: 25, apy: 7.8, color: "#6366f1" },
    ],
    supportedAssets: ["USDC", "USDT"],
    maturityTerms: "Flexible — withdraw anytime",
    earlyWithdrawalPenalty: "None",
    apyHistory: generateApyHistory(7.4, 0.4),
  },
  {
    id: "stocks",
    name: "Stocks Vault",
    description:
      "Gain exposure to tokenized stocks with automated portfolio management. Diversified across leading equities and ETFs, rebalanced for optimal risk-adjusted returns.",
    riskTier: "Balanced",
    currentApy: 8.5,
    apyRange: "7–10%",
    tvl: 1_800_000,
    userCount: 1_050,
    allocations: [
      { protocol: "Tokenized S&P 500", percentage: 50, apy: 8.2, color: "#1E88E5" },
      { protocol: "Tech ETF", percentage: 30, apy: 9.1, color: "#43A047" },
      { protocol: "Dividend Stocks", percentage: 20, apy: 7.4, color: "#FDD835" },
    ],
    supportedAssets: ["USDC", "USDT"],
    maturityTerms: "Flexible — withdraw anytime",
    earlyWithdrawalPenalty: "0.5% within 14 days",
    apyHistory: generateApyHistory(8.5, 0.8),
  },
  {
    id: "balanced",
    name: "Balanced Vault",
    description:
      "Optimized mix of stable lending and high-liquidity AMM pools. Balances yield maximization with risk management through diversification across proven protocols.",
    riskTier: "Balanced",
    currentApy: 9.8,
    apyRange: "8–11%",
    tvl: 5_100_000,
    userCount: 2_876,
    allocations: [
      { protocol: "Kamino", percentage: 45, apy: 10.4, color: "#2EBAC6" },
      { protocol: "Blend", percentage: 30, apy: 8.9, color: "#B6509E" },
      { protocol: "Aave", percentage: 25, apy: 9.5, color: "#f59e0b" },
    ],
    supportedAssets: ["USDC", "USDT"],
    maturityTerms: "Flexible — withdraw anytime",
    earlyWithdrawalPenalty: "None",
    apyHistory: generateApyHistory(9.8, 0.7),
  },
  {
    id: "growth",
    name: "Growth Vault",
    description:
      "Dynamic strategies focusing on higher-yielding opportunities with automated risk management. Leverages concentrated liquidity positions and leveraged yield farming.",
    riskTier: "Growth",
    currentApy: 13.2,
    apyRange: "11–15%",
    tvl: 3_750_000,
    userCount: 987,
    allocations: [
      { protocol: "Kamino LP", percentage: 50, apy: 14.8, color: "#f97316" },
      {
        protocol: "Blend Leveraged",
        percentage: 30,
        apy: 12.1,
        color: "#2EBAC6",
      },
      { protocol: "Volatile LP", percentage: 20, apy: 11.5, color: "#B6509E" },
    ],
    supportedAssets: ["USDC", "USDT"],
    maturityTerms: "30-day lock period",
    earlyWithdrawalPenalty: "1.5% within 30 days",
    apyHistory: generateApyHistory(13.2, 1.2),
  },
  {
    id: "defi500",
    name: "DeFi500 Index",
    description:
      "A diversified index of top DeFi protocols, rebalanced monthly. Provides broad exposure to the DeFi ecosystem with automated rebalancing for maximum risk-adjusted returns.",
    riskTier: "DeFi500",
    currentApy: 11.6,
    apyRange: "Variable",
    tvl: 8_900_000,
    userCount: 4_512,
    allocations: [
      {
        protocol: "Multi-Protocol Index",
        percentage: 60,
        apy: 12.2,
        color: "#6366f1",
      },
      { protocol: "Rebalancer", percentage: 25, apy: 10.8, color: "#2EBAC6" },
      { protocol: "Stable Buffer", percentage: 15, apy: 7.4, color: "#B6509E" },
    ],
    supportedAssets: ["USDC", "USDT"],
    maturityTerms: "Flexible — withdraw anytime",
    earlyWithdrawalPenalty: "0.5% within 7 days",
    apyHistory: generateApyHistory(11.6, 0.9),
  },
];

export function getVaultById(id: string): Vault | undefined {
  return VAULTS.find((v) => v.id.toLowerCase() === id.toLowerCase());
}
