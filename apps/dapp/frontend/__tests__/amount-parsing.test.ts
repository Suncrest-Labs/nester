import { describe, it, expect } from "vitest";
import {
  parseAmountToStroops,
  formatStroopsToDisplay,
  isValidAmountInput,
  parseDecimalStrict,
} from "@/lib/decimal";

describe("Decimal Amount Parsing", () => {
  describe("parseAmountToStroops - Valid Inputs", () => {
    const testCases = [
      ["1", 7, BigInt(10000000), "whole number"],
      ["1.0", 7, BigInt(10000000), "trailing zero"],
      ["0.0000001", 7, BigInt(1), "minimum stroop"],
      ["0.1", 7, BigInt(1000000), "0.1 XLM"],
      ["100.5", 7, BigInt(1005000000), "100.5 XLM"],
      ["1", 6, BigInt(1000000), "1 USDC"],
      ["0.123456", 6, BigInt(123456), "0.123456 USDC"],
    ];

    testCases.forEach(([input, decimals, expected, description]) => {
      it(`should parse "${input}" correctly: ${description}`, () => {
        const result = parseAmountToStroops(input as string, decimals as number);
        expect(result.valid).toBe(true);
        expect(result.stroops).toEqual(expected);
        expect(result.error).toBeUndefined();
      });
    });
  });

  describe("formatStroopsToDisplay - Valid Stroops", () => {
    const testCases = [
      [BigInt(10000000), 7, "1", "1 XLM"],
      [BigInt(1), 7, "0.0000001", "1 stroop"],
      [BigInt(1000000), 7, "0.1", "0.1 XLM"],
      [BigInt(1000000), 6, "1", "1 USDC"],
      [BigInt(1), 6, "0.000001", "1 cent"],
      [BigInt(123456), 6, "0.123456", "0.123456 USDC"],
    ];

    testCases.forEach(([stroops, decimals, expected, description]) => {
      it(`should format correctly: ${description}`, () => {
        const result = formatStroopsToDisplay(stroops as bigint, decimals as number);
        expect(result).toBe(expected);
      });
    });
  });

  describe("Round-trip: parse → format → parse", () => {
    it("should maintain precision through parse and format cycle", () => {
      const testValues = ["1", "0.5", "100.123456"];
      testValues.forEach((input) => {
        const parsed = parseAmountToStroops(input, 6);
        expect(parsed.valid).toBe(true);
        const formatted = formatStroopsToDisplay(parsed.stroops!, 6);
        const reparsed = parseAmountToStroops(formatted, 6);
        expect(reparsed.stroops).toEqual(parsed.stroops);
      });
    });
  });

  describe("Rejection Cases", () => {
    it("should reject negative amounts", () => {
      const result = parseAmountToStroops("-1", 7);
      expect(result.valid).toBe(false);
      expect(result.error?.code).toBe("NEGATIVE_OR_ZERO");
    });

    it("should reject zero", () => {
      const result = parseAmountToStroops("0", 7);
      expect(result.valid).toBe(false);
      expect(result.error?.code).toBe("NEGATIVE_OR_ZERO");
    });

    it("should reject scientific notation", () => {
      const result = parseAmountToStroops("1e3", 7);
      expect(result.valid).toBe(false);
      expect(result.error?.code).toBe("SCIENTIFIC_NOTATION");
    });

    it("should reject too many decimals", () => {
      const result = parseAmountToStroops("0.00000001", 7);
      expect(result.valid).toBe(false);
      expect(result.error?.code).toBe("TOO_MANY_DECIMALS");
    });

    it("should reject invalid format", () => {
      const result = parseAmountToStroops("abc", 7);
      expect(result.valid).toBe(false);
      expect(result.error?.code).toBe("INVALID_FORMAT");
    });
  });

  describe("Balance Checks", () => {
    it("should allow amount equal to balance", () => {
      const result = isValidAmountInput("100", 6, 100);
      expect(result.valid).toBe(true);
    });

    it("should reject amount exceeding balance", () => {
      const result = isValidAmountInput("101", 6, 100);
      expect(result.valid).toBe(false);
    });

    it("should handle precise balance comparison", () => {
      const result = isValidAmountInput("100.123456", 6, 100.123456);
      expect(result.valid).toBe(true);
    });
  });

  describe("Precision - No Float Issues", () => {
    it("should resolve 0.1 + 0.2 = 0.3", () => {
      const sum1 = parseAmountToStroops("0.1", 6);
      const sum2 = parseAmountToStroops("0.2", 6);
      const sumResult = parseAmountToStroops("0.3", 6);

      const total = sum1.stroops! + sum2.stroops!;
      expect(total).toEqual(sumResult.stroops);
    });

    it("should handle large numbers without precision loss", () => {
      const input = "999999999.999999";
      const result = parseAmountToStroops(input, 6);
      expect(result.valid).toBe(true);
      expect(result.stroops).toEqual(BigInt("999999999999999"));
    });
  });

  describe("Error Messages", () => {
    it("should provide clear messages", () => {
      const neg = parseAmountToStroops("-1", 7);
      expect(neg.error?.message?.toLowerCase()).toContain("positive");

      const sci = parseAmountToStroops("1e3", 7);
      expect(sci.error?.message?.toLowerCase()).toContain("scientific");

      const many = parseAmountToStroops("0.00000001", 7);
      expect(many.error?.message?.toLowerCase()).toContain("decimal");
    });
  });
});
