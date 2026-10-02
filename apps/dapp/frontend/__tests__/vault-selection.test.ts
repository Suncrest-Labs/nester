import { describe, it, expect } from "vitest";
import { pickLargestBalanceVault, selectSourceVault } from "@/lib/decimal";

interface FakeVault {
  id: string;
  current_balance: string;
}

const vault = (id: string, current_balance: string): FakeVault => ({
  id,
  current_balance,
});

describe("pickLargestBalanceVault", () => {
  it("returns null for an empty candidate list", () => {
    expect(pickLargestBalanceVault([])).toBeNull();
  });

  it("picks the vault with the largest balance", () => {
    const vaults = [vault("a", "10"), vault("b", "50"), vault("c", "25")];
    expect(pickLargestBalanceVault(vaults)?.id).toBe("b");
  });

  it("does not lose precision to floating point summation/comparison", () => {
    // 0.1 + 0.7 in float math famously != 0.8; make sure decimal comparison
    // is used instead of Number()-based comparison.
    const vaults = [vault("a", "0.1"), vault("b", "0.7")];
    const best = pickLargestBalanceVault(vaults);
    expect(best?.id).toBe("b");
  });
});

describe("selectSourceVault", () => {
  it("selects a vault with sufficient balance among several matching-currency vaults", () => {
    const vaults = [vault("a", "5"), vault("b", "100"), vault("c", "40")];
    const selected = selectSourceVault(vaults, 30);
    expect(selected?.id).toBe("b");
  });

  it("prefers the largest qualifying vault when multiple vaults have enough balance", () => {
    const vaults = [vault("a", "40"), vault("b", "100"), vault("c", "35")];
    const selected = selectSourceVault(vaults, 30);
    expect(selected?.id).toBe("b");
  });

  it("returns null when no single vault has sufficient balance, even though the sum would", () => {
    // Two vaults with 0.5 each sum to 1, but neither alone can cover a
    // withdrawal of 0.8 — no multi-vault debit splitting is implemented.
    const vaults = [vault("a", "0.5"), vault("b", "0.5")];
    expect(selectSourceVault(vaults, 0.8)).toBeNull();
  });

  it("returns null for an empty candidate list", () => {
    expect(selectSourceVault([], 10)).toBeNull();
  });

  it("does not reject a valid amount due to float drift (0.1 + 0.7 vs 0.8)", () => {
    const vaults = [vault("a", "0.8")];
    expect(selectSourceVault(vaults, 0.8)?.id).toBe("a");
  });
});
