import { z } from "zod/v4";
import { parseAmountToStroops, isValidAmountInput } from "@/lib/decimal";

// Helper for validating USDC precision (max 6 decimals)
export const validateUSDCPrecision = (val: string | number) => {
  const numStr = String(val);
  if (!numStr.includes(".")) return true;
  const decimals = numStr.split(".")[1];
  return decimals.length <= 6;
};

// Helper for bank account (10-digit Nigerian format)
export const validateBankAccount = () => {
  return z
    .string({ message: "Account number is required" })
    .min(1, { message: "Account number is required" })
    .length(10, { message: "Account number must be 10 digits" })
    .regex(/^\d+$/, { message: "Account number must contain only numbers" });
};

/**
 * Reusable amount validator using precise decimal parsing.
 *
 * Uses parseAmountToStroops internally for exact decimal arithmetic,
 * avoiding floating-point precision loss. Validates:
 * - Format is valid (not scientific notation, not empty, etc.)
 * - Amount is positive (> 0)
 * - Decimal places don't exceed asset precision
 * - Amount doesn't exceed user's balance
 *
 * @param options Configuration for validation rules
 * @returns A Zod schema validator
 */
export const validateAmount = (options?: {
  min?: number;
  max?: number;
  maxDecimals?: number;
  balance?: number;
  minMessage?: string;
  maxMessage?: string;
  balanceMessage?: string;
}) => {
  const {
    min = 0,
    max,
    maxDecimals = 6,
    balance,
    minMessage = `Minimum amount is ${min}`,
    maxMessage,
    balanceMessage = `Amount exceeds your balance`,
  } = options || {};

  return z
    .string({ message: "Amount is required" })
    .min(1, { message: "Amount is required" })
    // Use precise decimal parsing instead of Number()
    .refine(
      (val: string) => {
        const result = parseAmountToStroops(val, maxDecimals);
        return result.valid;
      },
      {
        message: "Invalid amount",
      }
    )
    // Additional check: respect minimum amount (after decimal parsing validates format)
    .refine(
      (val: string) => {
        if (min <= 0) return true;
        const result = parseAmountToStroops(val, maxDecimals);
        if (!result.valid) return true; // Let the previous check handle this
        const minStroops = parseAmountToStroops(min.toString(), maxDecimals);
        return result.stroops! >= minStroops.stroops!;
      },
      { message: minMessage }
    )
    // Check: respect maximum amount
    .refine(
      (val: string) => {
        if (max === undefined) return true;
        const result = parseAmountToStroops(val, maxDecimals);
        if (!result.valid) return true;
        const maxStroops = parseAmountToStroops(max.toString(), maxDecimals);
        return result.stroops! <= maxStroops.stroops!;
      },
      { message: maxMessage || `Maximum amount is ${max}` }
    )
    // Check: respect balance
    .refine(
      (val: string) => {
        if (balance === undefined || balance <= 0) return true;
        const validation = isValidAmountInput(val, maxDecimals, balance);
        return validation.valid;
      },
      { message: balanceMessage || `Amount exceeds your balance of ${balance?.toLocaleString()}` }
    );
};

// Specific helper for validating balance
export const validateBalance = (balance: number) => {
  return validateAmount({ balance });
};
