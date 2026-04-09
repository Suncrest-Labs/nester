import type {
  Vault,
  VaultAllocation,
  ApyDataPoint,
  MarketType,
  MarketStrategy,
  RiskTier,
} from "@/lib/types/vault";

export type { Vault, VaultAllocation, ApyDataPoint, MarketType, MarketStrategy, RiskTier };

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
    id: "usdc",
    name: "USDC Market",
    description:
      "Supply USDC to earn yield from lending demand. Funds are deployed across audited lending protocols for stable, consistent returns.",
    marketType: "single",
    tokens: ["USDC"],
    currentApy: 7.4,
    apyRange: "6-8%",
    tvl: 2_400_000,
    utilization: 72,
    allocations: [
      { protocol: "Blend", percentage: 40, apy: 6.2, color: "#2EBAC6" },
      { protocol: "Aave", percentage: 35, apy: 8.1, color: "#B6509E" },
      { protocol: "Compound", percentage: 25, apy: 7.8, color: "#6366f1" },
    ],
    supportedAssets: ["USDC"],
    maturityTerms: "Flexible - withdraw anytime",
    earlyWithdrawalPenalty: "None",
    apyHistory: generateApyHistory(7.4, 0.4),
    strategies: [
      {
        id: "lending",
        name: "Lending",
        description: "Supply to lending pools and earn from borrower interest. Lowest risk, stable returns.",
        apy: 6.2,
        risk: "low",
        lockDays: null,
        penaltyPct: 0,
      },
      {
        id: "optimized",
        name: "Optimized Yield",
        description: "Auto-routed across multiple lending protocols for the best rate. Rebalanced daily.",
        apy: 7.8,
        risk: "medium",
        lockDays: null,
        penaltyPct: 0,
      },
      {
        id: "leveraged",
        name: "Leveraged Lending",
        description: "Recursive lending with up to 3x leverage for amplified yield. Higher risk, higher reward.",
        apy: 12.5,
        risk: "high",
        lockDays: 7,
        penaltyPct: 0.5,
      },
    ],
  },
  {
    id: "xlm",
    name: "XLM Market",
    description:
      "Supply XLM and earn from borrowing demand and protocol incentives. Optimized across Stellar-native lending markets.",
    marketType: "single",
    tokens: ["XLM"],
    currentApy: 5.8,
    apyRange: "4-7%",
    tvl: 1_600_000,
    utilization: 58,
    allocations: [
      { protocol: "Blend", percentage: 55, apy: 5.4, color: "#2EBAC6" },
      { protocol: "Stellar AMM", percentage: 45, apy: 6.3, color: "#B6509E" },
    ],
    supportedAssets: ["XLM"],
    maturityTerms: "Flexible - withdraw anytime",
    earlyWithdrawalPenalty: "None",
    apyHistory: generateApyHistory(5.8, 0.5),
    strategies: [
      {
        id: "lending",
        name: "Lending",
        description: "Supply XLM to lending pools. Earn from borrower demand with low risk.",
        apy: 4.8,
        risk: "low",
        lockDays: null,
        penaltyPct: 0,
      },
      {
        id: "staking-plus",
        name: "Staking + Lending",
        description: "Combines staking rewards with lending yield for higher returns on your XLM.",
        apy: 6.5,
        risk: "medium",
        lockDays: null,
        penaltyPct: 0,
      },
    ],
  },
  {
    id: "xlm-usdc",
    name: "XLM / USDC",
    description:
      "Provide liquidity to the XLM/USDC pair across DEX pools. Earn trading fees and protocol incentives from the highest-volume Stellar pair.",
    marketType: "pair",
    tokens: ["XLM", "USDC"],
    currentApy: 12.4,
    apyRange: "10-15%",
    tvl: 5_100_000,
    utilization: 84,
    allocations: [
      { protocol: "Stellar DEX LP", percentage: 50, apy: 13.2, color: "#2EBAC6" },
      { protocol: "Blend LP", percentage: 30, apy: 11.8, color: "#B6509E" },
      { protocol: "Concentrated LP", percentage: 20, apy: 12.0, color: "#f59e0b" },
    ],
    supportedAssets: ["USDC", "XLM"],
    maturityTerms: "Flexible - withdraw anytime",
    earlyWithdrawalPenalty: "None",
    apyHistory: generateApyHistory(12.4, 1.0),
    strategies: [
      {
        id: "standard-lp",
        name: "Standard LP",
        description: "Full-range liquidity provision. Earn trading fees with moderate impermanent loss risk.",
        apy: 10.8,
        risk: "medium",
        lockDays: null,
        penaltyPct: 0,
      },
      {
        id: "concentrated-lp",
        name: "Concentrated LP",
        description: "Tight-range liquidity for higher fee capture. Requires more active management but higher yields.",
        apy: 15.2,
        risk: "high",
        lockDays: null,
        penaltyPct: 0,
      },
    ],
  },
  {
    id: "stocks",
    name: "Tokenized Equities",
    description:
      "Gain exposure to tokenized stocks with automated portfolio management. Diversified across leading equities and ETFs on-chain.",
    marketType: "index",
    tokens: ["USDC"],
    currentApy: 8.5,
    apyRange: "7-10%",
    tvl: 1_800_000,
    utilization: 65,
    allocations: [
      { protocol: "Tokenized S&P 500", percentage: 50, apy: 8.2, color: "#1E88E5" },
      { protocol: "Tech ETF", percentage: 30, apy: 9.1, color: "#43A047" },
      { protocol: "Dividend Stocks", percentage: 20, apy: 7.4, color: "#FDD835" },
    ],
    supportedAssets: ["USDC"],
    maturityTerms: "Flexible - withdraw anytime",
    earlyWithdrawalPenalty: "0.5% within 14 days",
    apyHistory: generateApyHistory(8.5, 0.8),
    strategies: [
      {
        id: "passive-index",
        name: "Passive Index",
        description: "Track a diversified equity index. Low-touch, auto-rebalanced quarterly.",
        apy: 7.4,
        risk: "medium",
        lockDays: null,
        penaltyPct: 0,
      },
      {
        id: "dividend-yield",
        name: "Dividend Yield",
        description: "Focus on high-dividend stocks. Dividends auto-converted to USDC and compounded.",
        apy: 9.2,
        risk: "medium",
        lockDays: 14,
        penaltyPct: 0.5,
      },
    ],
  },
  {
    id: "defi500",
    name: "DeFi500 Index",
    description:
      "A diversified index tracking the top DeFi protocols, rebalanced monthly. Broad exposure to the DeFi ecosystem with automated rebalancing.",
    marketType: "index",
    tokens: ["USDC", "XLM"],
    currentApy: 11.6,
    apyRange: "Variable",
    tvl: 8_900_000,
    utilization: 91,
    allocations: [
      { protocol: "Multi-Protocol Index", percentage: 60, apy: 12.2, color: "#6366f1" },
      { protocol: "Rebalancer", percentage: 25, apy: 10.8, color: "#2EBAC6" },
      { protocol: "Stable Buffer", percentage: 15, apy: 7.4, color: "#B6509E" },
    ],
    supportedAssets: ["USDC", "XLM"],
    maturityTerms: "Flexible - withdraw anytime",
    earlyWithdrawalPenalty: "0.5% within 7 days",
    apyHistory: generateApyHistory(11.6, 0.9),
    strategies: [
      {
        id: "core-index",
        name: "Core Index",
        description: "Broad DeFi exposure across top protocols. Monthly rebalancing, stable risk profile.",
        apy: 10.2,
        risk: "medium",
        lockDays: null,
        penaltyPct: 0,
      },
      {
        id: "yield-boosted",
        name: "Yield Boosted",
        description: "Core index plus yield farming positions for amplified returns. Higher volatility.",
        apy: 14.8,
        risk: "high",
        lockDays: 7,
        penaltyPct: 0.5,
      },
    ],
  },
];

export function getVaultById(id: string): Vault | undefined {
  return VAULTS.find((v) => v.id.toLowerCase() === id.toLowerCase());
}
