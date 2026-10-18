import { describe, expect, it } from "vitest";
import { parseDepositAmountToStroops } from "@/lib/stellar/transaction";

describe("parseDepositAmountToStroops (#1508)", () => {
  it.each([
    { input: "0.3", expected: 3_000_000n },
    { input: "1234567.1234567", expected: 12_345_671_234_567n },
    { input: "1", expected: 10_000_000n },
    { input: "0.0000001", expected: 1n },
    {
      input: "999999999999999999.1234567",
      expected: 9_999_999_999_999_999_991_234_567n,
    },
    { input: 42_000_000n, expected: 42_000_000n },
  ])("parses valid amount $input to $expected stroops", ({ input, expected }) => {
    expect(parseDepositAmountToStroops(input)).toBe(expected);
  });

  it.each([
    { input: "0.00000001", reason: /exceeds the maximum of 7 decimal places/i },
    { input: "1.12345678", reason: /exceeds the maximum of 7 decimal places/i },
    { input: "0", reason: /must be greater than zero/i },
    { input: "0.0000000", reason: /must be greater than zero/i },
    { input: "-1.5", reason: /expected a positive decimal string/i },
    { input: "1e7", reason: /expected a positive decimal string/i },
    { input: "", reason: /expected a positive decimal string/i },
    { input: 0n, reason: /must be greater than zero/i },
    { input: -10n, reason: /must be greater than zero/i },
  ])("rejects invalid amount $input", ({ input, reason }) => {
    expect(() => parseDepositAmountToStroops(input)).toThrow(reason);
  });

  it("rejects raw numbers at runtime", () => {
    expect(() =>
      // @ts-expect-error number is deliberately disallowed by DepositParams
      parseDepositAmountToStroops(0.3)
    ).toThrow(/floating-point numbers are not accepted/i);
  });
});
