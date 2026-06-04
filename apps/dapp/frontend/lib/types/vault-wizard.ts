export type VaultType = "Stable Yield" | "Balanced" | "Aggressive Growth";

export type RiskLevel = "low" | "medium" | "high";

export type RebalanceFrequency = "Daily" | "Weekly" | "Monthly";

export type LockPeriod = "None" | "30 Days" | "90 Days";

export interface ProtocolOption {
  id: string;
  name: string;
  estimatedApy: number;
  riskLevel: RiskLevel;
  description: string;
  iconUrl?: string;
  color?: string;
}

export interface ProtocolAllocation {
  protocolId: string;
  percentage: number;
}

export interface WizardVaultData {
  // Step 1: Basics
  name: string;
  description: string;
  type: VaultType | null;

  // Step 2: Allocation
  allocations: ProtocolAllocation[];

  // Step 3: Risk & Limits
  maxCapacity: number | null;
  lockPeriod: LockPeriod;
  autoRebalance: boolean;
  rebalanceFrequency: RebalanceFrequency | null;
}

export const PROTOCOL_OPTIONS: ProtocolOption[] = [
  {
    id: "blend",
    name: "Blend Protocol",
    estimatedApy: 8.5,
    riskLevel: "low",
    description: "Lending and borrowing protocol on Soroban.",
    color: "hsl(var(--chart-1))"
  },
  {
    id: "lobstr",
    name: "Lobstr Earn",
    estimatedApy: 12.0,
    riskLevel: "medium",
    description: "Automated yield generation through AMM liquidity.",
    color: "hsl(var(--chart-2))"
  },
  {
    id: "aquarius",
    name: "Aquarius Yield",
    estimatedApy: 15.5,
    riskLevel: "high",
    description: "High-yield liquidity provisioning incentives.",
    color: "hsl(var(--chart-3))"
  },
  {
    id: "soroswap",
    name: "Soroswap Strategy",
    estimatedApy: 10.2,
    riskLevel: "medium",
    description: "Decentralized exchange liquidity pools.",
    color: "hsl(var(--chart-4))"
  }
];

export const INITIAL_WIZARD_DATA: WizardVaultData = {
  name: "",
  description: "",
  type: null,
  allocations: PROTOCOL_OPTIONS.map((p, i) => ({
    protocolId: p.id,
    percentage: i === 0 ? 100 : 0, // Default 100% to first protocol
  })),
  maxCapacity: null,
  lockPeriod: "None",
  autoRebalance: false,
  rebalanceFrequency: null,
};
