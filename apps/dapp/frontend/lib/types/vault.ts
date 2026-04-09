export type MarketType = "single" | "pair" | "index";

export interface VaultAllocation {
  protocol: string;
  percentage: number;
  apy: number;
  color: string;
}

export interface ApyDataPoint {
  date: string;
  apy: number;
}

export interface MarketStrategy {
  id: string;
  name: string;
  description: string;
  apy: number;
  risk: "low" | "medium" | "high";
  lockDays: number | null;
  penaltyPct: number;
}

export interface Vault {
  id: string;
  name: string;
  description: string;
  marketType: MarketType;
  tokens: string[];
  currentApy: number;
  apyRange: string;
  tvl: number;
  utilization: number;
  allocations: VaultAllocation[];
  supportedAssets: string[];
  maturityTerms: string;
  earlyWithdrawalPenalty: string;
  apyHistory: ApyDataPoint[];
  strategies: MarketStrategy[];
}

// Keep RiskTier as alias for backward compat during transition
export type RiskTier = "Conservative" | "Balanced" | "Growth" | "DeFi500";
