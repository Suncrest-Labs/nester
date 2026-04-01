import { z } from "zod/v4";

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

// Reusable amount validator
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
    balanceMessage,
  } = options || {};

  return z
    .string({ message: "Amount is required" })
    .min(1, { message: "Amount is required" })
    // Edge case: empty or invalid number format
    .refine((val) => !isNaN(Number(val)) && val.trim() !== "", { message: "Invalid number format" })
    // Edge case: leading zeros (e.g., "007.5") - reject them
    .refine((val) => {
      if (val.length > 1 && val.startsWith("0") && !val.startsWith("0.")) {
        return false;
      }
      return true;
    }, { message: "Invalid leading zero" })
    .refine((val) => Number(val) > min, { message: minMessage })
    .refine((val) => {
      if (max === undefined) return true;
      return Number(val) <= max;
    }, { message: maxMessage || `Maximum amount is ${max}` })
    .refine((val) => {
      if (balance === undefined) return true;
      return Number(val) <= balance;
    }, { message: balanceMessage || `Amount exceeds your balance of ${balance?.toLocaleString()}` })
    .refine((val) => {
      const numStr = String(val);
      if (!numStr.includes(".")) return true;
      const decimals = numStr.split(".")[1];
      return decimals.length <= maxDecimals;
    }, { message: `Maximum ${maxDecimals} decimal places allowed` });
};

// Specific helper for validating balance
export const validateBalance = (balance: number) => {
  return validateAmount({ balance });
};
