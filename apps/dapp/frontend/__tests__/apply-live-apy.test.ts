import { describe, it, expect } from "vitest";
import type { YieldPool } from "@/lib/api/yield-opportunities";
import {
  SAVINGS_VAULT_DEFINITIONS,
  FALLBACK_VAULT_APY,
} from "@/lib/savings/vault-definitions";
import { applyLiveApyToVault, buildSavingsVaults } from "@/lib/savings/apply-live-apy";

const mockPools: YieldPool[] = [
  {
    pool: "p1",
    project: "blend-protocol",
    symbol: "USDC",
    apy: 5.2,
    apyBase: 5.0,
    apyReward: 0.2,
    tvlUsd: 1_000_000,
    apyPct7d: 0.1,
    chain: "Stellar",
    riskScore: 20,
  },
  {
    pool: "p2",
    project: "lobstr",
    symbol: "USDC",
    apy: 8.5,
    apyBase: 8.0,
    apyReward: 0.5,
    tvlUsd: 500_000,
    apyPct7d: 0.2,
    chain: "Stellar",
    riskScore: 35,
  },
  {
    pool: "p3",
    project: "aquarius",
    symbol: "USDC",
    apy: 10.0,
    apyBase: 9.5,
    apyReward: 0.5,
    tvlUsd: 300_000,
    apyPct7d: -0.1,
    chain: "Stellar",
    riskScore: 45,
  },
];

describe("applyLiveApyToVault", () => {
  it("uses live APY from matching protocol pools", () => {
    const flexible = SAVINGS_VAULT_DEFINITIONS.find((v) => v.type === "flexible")!;
    const result = applyLiveApyToVault(flexible, mockPools, false);
    expect(result.apyLabel).toBe("5.2%");
    expect(result.apy).toBeCloseTo(0.052);
  });

  it("uses max APY for auto-compound vault type", () => {
    const auto = SAVINGS_VAULT_DEFINITIONS.find((v) => v.type === "auto-compound")!;
    const result = applyLiveApyToVault(auto, mockPools, false);
    expect(result.apyLabel).toBe("8.5%");
  });

  it("falls back to static APY on API error", () => {
    const flexible = SAVINGS_VAULT_DEFINITIONS.find((v) => v.type === "flexible")!;
    const result = applyLiveApyToVault(flexible, mockPools, true);
    expect(result.apyLabel).toBe(FALLBACK_VAULT_APY.flexible.apyLabel);
    expect(result.apy).toBe(FALLBACK_VAULT_APY.flexible.apy);
  });
});

describe("buildSavingsVaults", () => {
  it("returns all vault definitions with live APY applied", () => {
    const vaults = buildSavingsVaults(SAVINGS_VAULT_DEFINITIONS, mockPools, false);
    expect(vaults).toHaveLength(4);
    expect(vaults.every((v) => v.apyLabel.endsWith("%"))).toBe(true);
  });
});
