import type { SavingsVaultType } from "@/lib/savings/vault-definitions";

/**
 * Maps each savings product type to DeFiLlama `project` slug fragments
 * returned by GET /api/v1/yield-opportunities.
 */
export const VAULT_PROTOCOL_SLUGS: Record<SavingsVaultType, string[]> = {
  flexible: ["blend"],
  "auto-compound": ["blend", "lobstr"],
  "stablecoin-yield": ["blend", "aquarius", "soroswap"],
  custom: ["blend"],
};
