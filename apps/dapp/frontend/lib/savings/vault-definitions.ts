export type SavingsVaultType = "flexible" | "auto-compound" | "stablecoin-yield" | "custom";

export interface SavingsVaultDefinition {
  id: string;
  type: SavingsVaultType;
  name: string;
  description: string;
  summary: string;
  lockDays: number | null;
  penaltyPct: number;
  badge: string;
  features: string[];
  supportedAssets: ("USDC" | "XLM")[];
}

/** Static catalogue metadata — APY is injected at runtime from the yield API. */
export const SAVINGS_VAULT_DEFINITIONS: SavingsVaultDefinition[] = [
  {
    id: "flexible-savings",
    type: "flexible",
    name: "Flexible Savings",
    description:
      "Earn yield on your USDC with no lockup period. Deposit and withdraw anytime — ideal for an emergency fund or short-term savings.",
    summary:
      "No lock period. Funds sit in audited lending pools and accrue yield daily. Withdraw the full balance at any time with no fees.",
    lockDays: null,
    penaltyPct: 0,
    badge: "No lockup",
    features: ["Withdraw anytime", "No exit fee", "Daily yield accrual"],
    supportedAssets: ["USDC", "XLM"],
  },
  {
    id: "auto-compound",
    type: "auto-compound",
    name: "Auto-Compound",
    description:
      "Yield is automatically reinvested every 24 hours, compounding your returns without any manual action. Set it and grow.",
    summary:
      "Yield harvested daily and re-deposited automatically. Effective APY is higher than the base rate due to continuous compounding. No manual claiming needed.",
    lockDays: null,
    penaltyPct: 0,
    badge: "Auto-reinvest",
    features: ["Daily auto-compounding", "No manual claiming", "No exit fee"],
    supportedAssets: ["USDC", "XLM"],
  },
  {
    id: "stablecoin-yield",
    type: "stablecoin-yield",
    name: "Stablecoin Yield",
    description:
      "Spread across USDC and XLM liquidity pools for diversified, optimised stable yield.",
    summary:
      "Funds are split across USDC/XLM pools. Rebalanced weekly to chase the highest stable yield. Minimises single-protocol risk while keeping APY competitive.",
    lockDays: null,
    penaltyPct: 0,
    badge: "Multi-pool",
    features: ["Multi-stablecoin exposure", "Weekly rebalance", "No exit fee"],
    supportedAssets: ["USDC", "XLM"],
  },
  {
    id: "custom-savings",
    type: "custom",
    name: "Custom Goal",
    description:
      "Name your goal, set a target amount and timeline. Track progress toward a specific savings target — holiday, house deposit, anything.",
    summary:
      "Same underlying yield as Flexible Savings, but wrapped in goal tracking. Set a name, target amount, and target date. See progress in your portfolio.",
    lockDays: null,
    penaltyPct: 0,
    badge: "Goal-based",
    features: ["Named savings goal", "Target amount tracking", "Withdraw anytime"],
    supportedAssets: ["USDC", "XLM"],
  },
];

/** Fallback APY labels used only when the yield API is unavailable. */
export const FALLBACK_VAULT_APY: Record<SavingsVaultType, { apy: number; apyLabel: string }> = {
  flexible: { apy: 0.052, apyLabel: "4–6%" },
  "auto-compound": { apy: 0.088, apyLabel: "8–10%" },
  "stablecoin-yield": { apy: 0.105, apyLabel: "9–12%" },
  custom: { apy: 0.052, apyLabel: "4–6%" },
};

export interface SavingsVault extends SavingsVaultDefinition {
  apy: number;
  apyLabel: string;
}
