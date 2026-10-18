import { describe, expect, it } from "vitest";
import { vaultContracts } from "@/lib/vault-contracts";
import { INITIAL_NOTIFICATIONS } from "@/lib/notifications";

describe("Stellar protocol integration claims (#1555)", () => {
  it("vault descriptions reference Stellar-native protocols (Blend, Aquarius) and not Aave", () => {
    for (const vault of vaultContracts) {
      expect(vault.description).not.toMatch(/aave/i);
    }
    const conservative = vaultContracts.find((v) => v.id === "conservative");
    expect(conservative?.description).toContain("Blend");
    expect(conservative?.description).toContain("Aquarius");
  });

  it("seed notifications reference Stellar-native protocols and not Aave", () => {
    for (const item of INITIAL_NOTIFICATIONS) {
      expect(item.message).not.toMatch(/aave/i);
    }
  });
});
