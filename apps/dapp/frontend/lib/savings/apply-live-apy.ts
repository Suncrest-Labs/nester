import type { YieldPool } from "@/lib/api/yield-opportunities";
import {
  FALLBACK_VAULT_APY,
  type SavingsVault,
  type SavingsVaultDefinition,
  type SavingsVaultType,
} from "@/lib/savings/vault-definitions";
import { VAULT_PROTOCOL_SLUGS } from "@/lib/savings/protocol-slug-map";

function poolsForVaultType(type: SavingsVaultType, pools: YieldPool[]): YieldPool[] {
  const slugs = VAULT_PROTOCOL_SLUGS[type];
  return pools.filter((pool) =>
    slugs.some((slug) => pool.project.toLowerCase().includes(slug))
  );
}

function aggregateApyPercent(type: SavingsVaultType, matching: YieldPool[]): number | null {
  if (matching.length === 0) return null;

  const apys = matching.map((p) => p.apy).filter((v) => Number.isFinite(v) && v > 0);
  if (apys.length === 0) return null;

  switch (type) {
    case "auto-compound":
      return Math.max(...apys);
    case "stablecoin-yield":
      return apys.reduce((sum, v) => sum + v, 0) / apys.length;
    default:
      return apys.reduce((sum, v) => sum + v, 0) / apys.length;
  }
}

export function formatApyLabel(apyPercent: number): string {
  return `${apyPercent.toFixed(1)}%`;
}

export function applyLiveApyToVault(
  definition: SavingsVaultDefinition,
  pools: YieldPool[],
  useFallback: boolean
): SavingsVault {
  const fallback = FALLBACK_VAULT_APY[definition.type];
  if (useFallback) {
    return { ...definition, apy: fallback.apy, apyLabel: fallback.apyLabel };
  }

  const matching = poolsForVaultType(definition.type, pools);
  const apyPercent = aggregateApyPercent(definition.type, matching);
  if (apyPercent == null) {
    return { ...definition, apy: fallback.apy, apyLabel: fallback.apyLabel };
  }

  return {
    ...definition,
    apy: apyPercent / 100,
    apyLabel: formatApyLabel(apyPercent),
  };
}

export function buildSavingsVaults(
  definitions: SavingsVaultDefinition[],
  pools: YieldPool[] | undefined,
  isError: boolean
): SavingsVault[] {
  const useFallback = isError || !pools;
  return definitions.map((def) => applyLiveApyToVault(def, pools ?? [], useFallback));
}
