"use client";

/**
 * vault-contracts.ts
 *
 * Lightweight registry of deployed vault contract addresses and their
 * supported assets. This is the only remaining static vault metadata after
 * live data migration; everything else (APY, TVL, utilization) comes from
 * the /yield-opportunities API via useVaultMarkets.
 */

import { getContractId } from "@/lib/contracts";

export type SupportedAsset = "USDC" | "XLM";

export interface VaultContract {
  id: string;
  name: string;
  apy: number;
  apyLabel: string;
  description: string;
  risk: string;
  lockDays: number | null;
  earlyWithdrawalPenaltyPct: number;
  performanceFeePct: number;
  managementFeePct: number;
  asset: SupportedAsset;
  supportedAssets: SupportedAsset[];
  /**
   * Deployed contract address, or null when the environment does not carry one.
   *
   * Null rather than "" (#1094): an empty string is indistinguishable from a
   * real address at the type level, so every consumer silently passed it on to
   * the transaction builder. Null makes the compiler point at each place that
   * has to decide what "not deployed" means.
   */
  contractAddress: string | null;
  contractXlmAddress?: string | null;
}

const VAULT_USDC_CONTRACT = getContractId("vault");
const VAULT_XLM_CONTRACT = getContractId("vaultXlm");

export const vaultContracts: VaultContract[] = [
  {
    id: "flex-savings",
    name: "Flex Savings",
    apy: 0.05,
    apyLabel: "4-6%",
    description:
      "Your default landing vault. No lock period, withdraw anytime. Ideal as a starting point before moving to higher-yield vaults.",
    risk: "Very Low",
    lockDays: null,
    earlyWithdrawalPenaltyPct: 0,
    performanceFeePct: 5,
    managementFeePct: 0,
    asset: "USDC",
    supportedAssets: ["USDC", "XLM"],
    contractAddress: VAULT_USDC_CONTRACT,
    contractXlmAddress: VAULT_XLM_CONTRACT,
  },
  {
    id: "conservative",
    name: "Conservative",
    apy: 0.07,
    apyLabel: "6-8%",
    description:
      "Focus on safety and stability using battle-tested lending protocols like Blend and Aave.",
    risk: "Low",
    lockDays: 30,
    earlyWithdrawalPenaltyPct: 0.1,
    performanceFeePct: 10,
    managementFeePct: 0.5,
    asset: "USDC",
    supportedAssets: ["USDC", "XLM"],
    contractAddress: VAULT_USDC_CONTRACT,
    contractXlmAddress: VAULT_XLM_CONTRACT,
  },
  {
    id: "balanced",
    name: "Balanced",
    apy: 0.095,
    apyLabel: "8-11%",
    description:
      "Optimized mix of stable lending and high-liquidity automated market maker pools.",
    risk: "Medium",
    lockDays: 45,
    earlyWithdrawalPenaltyPct: 0.1,
    performanceFeePct: 10,
    managementFeePct: 0.5,
    asset: "USDC",
    supportedAssets: ["USDC", "XLM"],
    contractAddress: VAULT_USDC_CONTRACT,
    contractXlmAddress: VAULT_XLM_CONTRACT,
  },
  {
    id: "growth",
    name: "Growth",
    apy: 0.13,
    apyLabel: "11-15%",
    description:
      "Dynamic strategies focusing on higher-yielding opportunities with automated risk management.",
    risk: "Moderate High",
    lockDays: 60,
    earlyWithdrawalPenaltyPct: 0.1,
    performanceFeePct: 10,
    managementFeePct: 0.5,
    asset: "USDC",
    supportedAssets: ["USDC"],
    contractAddress: VAULT_USDC_CONTRACT,
  },
  {
    id: "defi500",
    name: "DeFi500 Index",
    apy: 0.108,
    apyLabel: "Variable",
    description:
      "A diversified index fund of top DeFi protocols, rebalanced monthly for broad exposure.",
    risk: "Dynamic",
    lockDays: 90,
    earlyWithdrawalPenaltyPct: 0.1,
    performanceFeePct: 10,
    managementFeePct: 0.5,
    asset: "USDC",
    supportedAssets: ["USDC"],
    contractAddress: VAULT_USDC_CONTRACT,
  },
];

export function getVaultContractById(id: string): VaultContract | undefined {
  return vaultContracts.find((v) => v.id === id);
}

/**
 * First deployed vault that settles in `asset`.
 *
 * A vault holds a single currency, so an allocation spanning several assets
 * needs one deposit per asset. Vaults without a contract address are skipped:
 * they cannot take a deposit, and offering them produces a signature prompt
 * that always fails.
 */
export function getVaultContractByAsset(asset: string): VaultContract | undefined {
  const wanted = settlementAssetFor(asset);
  if (!wanted) return undefined;
  return vaultContracts.find(
    (v) => v.asset.toUpperCase() === wanted && !!v.contractAddress,
  );
}

/**
 * The currency a pool's position actually settles in.
 *
 * Pool symbols are the wrapper token a protocol issues — Gami pays EARNUSDC
 * for a USDC deposit, EARNXLM for XLM — so matching a vault on the raw symbol
 * finds nothing and every pool looks undepositable. Only the underlying is
 * mapped: an unrecognised symbol returns undefined rather than being guessed
 * into the wrong vault.
 */
export function settlementAssetFor(symbol: string): SupportedAsset | undefined {
  const s = symbol.toUpperCase();
  if (s.includes("USDC")) return "USDC";
  if (s.includes("XLM")) return "XLM";
  return undefined;
}
