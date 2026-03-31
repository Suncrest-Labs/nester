export type RiskTier = "Conservative" | "Balanced" | "Growth" | "DeFi500";

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

export interface Vault {
  id: string;
  name: string;
  description: string;
  riskTier: RiskTier;
  currentApy: number;
  apyRange: string;
  tvl: number;
  userCount: number;
  allocations: VaultAllocation[];
  supportedAssets: string[];
  maturityTerms: string;
  earlyWithdrawalPenalty: string;
  apyHistory: ApyDataPoint[];
}
