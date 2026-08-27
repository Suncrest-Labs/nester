import { describe, it, expect } from "vitest";
import {
  parseAmountToStroops,
  formatStroopsToDisplay,
  isValidAmountInput,
} from "@/lib/decimal";

describe("Vault Action Modals - Decimal Precision Integration", () => {
  describe("Valid Amount Input Scenarios", () => {
    it("should accept and parse valid decimal amounts", () => {
      const input = "10.50";
      const result = parseAmountToStroops(input, 6);
      expect(result.valid).toBe(true);
      expect(result.stroops).toEqual(BigInt(10500000));
    });

    it("should accept whole number amounts", () => {
      const input = "100";
      const result = parseAmountToStroops(input, 6);
      expect(result.valid).toBe(true);
      expect(result.stroops).toEqual(BigInt(100000000));
    });

    it("should accept maximum decimal places (6 for USDC)", () => {
      const input = "10.123456";
      const result = parseAmountToStroops(input, 6);
      expect(result.valid).toBe(true);
      expect(result.stroops).toEqual(BigInt(10123456));
    });
  });

  describe("Rejected Amount Scenarios", () => {
    it("should reject negative amounts", () => {
      const result = parseAmountToStroops("-10", 6);
      expect(result.valid).toBe(false);
      expect(result.error?.code).toBe("NEGATIVE_OR_ZERO");
    });

    it("should reject zero amount", () => {
      const result = parseAmountToStroops("0", 6);
      expect(result.valid).toBe(false);
      expect(result.error?.code).toBe("NEGATIVE_OR_ZERO");
    });

    it("should reject amounts exceeding balance", () => {
      const balance = 100;
      const result = isValidAmountInput("200", 6, balance);
      expect(result.valid).toBe(false);
    });

    it("should reject amounts with too many decimals", () => {
      const result = parseAmountToStroops("10.1234567", 6);
      expect(result.valid).toBe(false);
      expect(result.error?.code).toBe("TOO_MANY_DECIMALS");
    });

    it("should reject scientific notation", () => {
      const result = parseAmountToStroops("1e3", 6);
      expect(result.valid).toBe(false);
      expect(result.error?.code).toBe("SCIENTIFIC_NOTATION");
    });

    it("should reject non-numeric input", () => {
      const result = parseAmountToStroops("abc", 6);
      expect(result.valid).toBe(false);
      expect(result.error?.code).toBe("INVALID_FORMAT");
    });
  });

  describe("Display Value Accuracy", () => {
    it("should show canonical display value matching parsed stroops", () => {
      const input = "10.5";
      const parsed = parseAmountToStroops(input, 6);
      expect(parsed.valid).toBe(true);
      const formatted = formatStroopsToDisplay(parsed.stroops!, 6);
      expect(formatted).toBe(input);
    });

    it("should handle trailing zeros consistently", () => {
      const input = "1.00";
      const parsed = parseAmountToStroops(input, 6);
      expect(parsed.valid).toBe(true);
      const formatted = formatStroopsToDisplay(parsed.stroops!, 6);
      // Canonical form should be "1" (trailing zeros stripped)
      expect(formatted).toBe("1");
    });

    it("should preserve significant decimal places", () => {
      const input = "0.123456";
      const parsed = parseAmountToStroops(input, 6);
      expect(parsed.valid).toBe(true);
      const formatted = formatStroopsToDisplay(parsed.stroops!, 6);
      expect(formatted).toBe(input);
    });
  });

  describe("Balance Checks", () => {
    it("should allow deposit equal to balance", () => {
      const balance = 1000;
      const result = isValidAmountInput("1000", 6, balance);
      expect(result.valid).toBe(true);
    });

    it("should reject deposit exceeding balance by small amount", () => {
      const balance = 100;
      const result = isValidAmountInput("100.000001", 6, balance);
      expect(result.valid).toBe(false);
    });

    it("should use stroops for precise balance comparison", () => {
      const balanceUSDC = 100.123456;
      const requestedUSDC = "100.123456";

      const balanceParsed = parseAmountToStroops(balanceUSDC.toString(), 6);
      const requestedParsed = parseAmountToStroops(requestedUSDC, 6);

      expect(requestedParsed.stroops).toEqual(balanceParsed.stroops);

      const validation = isValidAmountInput(requestedUSDC, 6, balanceUSDC);
      expect(validation.valid).toBe(true);
    });
  });

  describe("Precision and Stroops Conversion", () => {
    it("should convert decimal input to correct stroops", () => {
      // 1 USDC = 1,000,000 stroops (6 decimals)
      const input = "10.5";
      const result = parseAmountToStroops(input, 6);
      expect(result.valid).toBe(true);
      expect(result.stroops).toEqual(BigInt(10500000));
    });

    it("should handle minimum unit (1 stroop) correctly", () => {
      // 1 stroop USDC = 0.000001
      const input = "0.000001";
      const result = parseAmountToStroops(input, 6);
      expect(result.valid).toBe(true);
      expect(result.stroops).toEqual(BigInt(1));
    });

    it("should preserve precision for large amounts", () => {
      // Test that bigint is used, not float
      const largeAmount = "999999999.999999";
      const result = parseAmountToStroops(largeAmount, 6);
      expect(result.valid).toBe(true);
      expect(result.stroops).toEqual(BigInt("999999999999999"));
    });

    it("should handle XLM (7 decimals) correctly", () => {
      // 1 XLM = 10,000,000 stroops (7 decimals)
      const input = "1";
      const result = parseAmountToStroops(input, 7);
      expect(result.valid).toBe(true);
      expect(result.stroops).toEqual(BigInt(10000000));
    });
  });

  describe("Round-trip Accuracy", () => {
    it("should maintain consistency through parse and format cycle", () => {
      const testValues = ["1", "0.5", "100.123", "0.000001"];
      
      testValues.forEach((input) => {
        const parsed = parseAmountToStroops(input, 6);
        expect(parsed.valid).toBe(true);
        
        const formatted = formatStroopsToDisplay(parsed.stroops!, 6);
        const reparsed = parseAmountToStroops(formatted, 6);
        
        expect(reparsed.valid).toBe(true);
        expect(reparsed.stroops).toEqual(parsed.stroops);
      });
    });
  });

  describe("Error Messages and Feedback", () => {
    it("should provide clear validation error messages", () => {
      const testCases = [
        { input: "-1", expectedCode: "NEGATIVE_OR_ZERO", expectedKeyword: "positive" },
        { input: "1e3", expectedCode: "SCIENTIFIC_NOTATION", expectedKeyword: "scientific" },
        { input: "0.0000001", expectedCode: "TOO_MANY_DECIMALS", expectedKeyword: "decimal" },
      ];

      testCases.forEach(({ input, expectedCode, expectedKeyword }) => {
        const result = parseAmountToStroops(input, 6);
        expect(result.valid).toBe(false);
        expect(result.error?.code).toBe(expectedCode);
        expect(result.error?.message?.toLowerCase()).toContain(expectedKeyword);
      });
    });

    it("should not silently coerce invalid input to zero", () => {
      const result = parseAmountToStroops("invalid", 6);
      expect(result.valid).toBe(false);
      expect(result.error).toBeDefined();
    });
  });

  describe("Edge Cases and Security", () => {
    it("should handle very large numbers without precision loss", () => {
      const largeAmount = "999999999.999999";
      const result = parseAmountToStroops(largeAmount, 6);
      expect(result.valid).toBe(true);
      
      const formatted = formatStroopsToDisplay(result.stroops!, 6);
      const reparsed = parseAmountToStroops(formatted, 6);
      expect(reparsed.stroops).toEqual(result.stroops);
    });

    it("should resolve IEEE 754 floating point issues", () => {
      // 0.1 + 0.2 = 0.30000000000000004 in IEEE 754
      const sum1 = parseAmountToStroops("0.1", 6);
      const sum2 = parseAmountToStroops("0.2", 6);
      const sumResult = parseAmountToStroops("0.3", 6);

      expect(sum1.valid).toBe(true);
      expect(sum2.valid).toBe(true);
      expect(sumResult.valid).toBe(true);

      const total = sum1.stroops! + sum2.stroops!;
      expect(total).toEqual(sumResult.stroops);
    });

    it("should not leak sensitive data in error messages", () => {
      const result = parseAmountToStroops("invalid", 6);
      expect(result.error?.message).not.toContain("0x");
      expect(result.error?.message).not.toContain("GBPU");
    });
  });
});
