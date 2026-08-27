/**
 * Precise decimal parsing utility for Stellar stroops.
 *
 * This module provides exact decimal arithmetic for user-facing amounts
 * without using floating-point math, which loses precision at scale.
 *
 * Why not floats?
 * - IEEE 754 doubles have 53-bit precision; amounts > 2^53 lose precision
 * - Multiplying floats by 10^7 (stroops conversion) can introduce rounding errors
 * - For financial/crypto amounts, precision loss = money loss
 *
 * Solution:
 * - Parse user strings using decimal.js (arbitrary precision)
 * - Convert to integer stroops using bigint (exact integers)
 * - Format back using bigint division and string manipulation
 */

import Decimal from "decimal.js";

// ─── Types ──────────────────────────────────────────────────────────────────

export interface ParseResult {
  valid: boolean;
  stroops?: bigint;
  error?: {
    code:
      | "INVALID_FORMAT"
      | "NEGATIVE_OR_ZERO"
      | "SCIENTIFIC_NOTATION"
      | "TOO_MANY_DECIMALS"
      | "INSUFFICIENT_BALANCE";
    message: string;
  };
}

export interface ValidationResult {
  valid: boolean;
  reason?: string;
}

// ─── Core Parsing ───────────────────────────────────────────────────────────

/**
 * Strict decimal parser that rejects scientific notation and invalid formats.
 *
 * @param input The string to parse
 * @returns A Decimal.js Decimal object
 * @throws if the input is not a valid decimal string (includes scientific notation)
 */
export function parseDecimalStrict(input: string): Decimal {
  // Trim whitespace
  const trimmed = input.trim();

  // Reject empty strings
  if (!trimmed) {
    throw new Error("Input cannot be empty");
  }

  // Reject scientific notation (e or E)
  if (/[eE]/.test(trimmed)) {
    throw new Error("Scientific notation is not allowed");
  }

  // Try to parse with Decimal.js
  const decimal = new Decimal(trimmed);

  // Verify it parsed successfully (Decimal throws on invalid input by default)
  if (decimal.isNaN()) {
    throw new Error("Invalid decimal format");
  }

  return decimal;
}

/**
 * Parse a user-input string amount into integer stroops.
 *
 * Stroops are the smallest unit on Stellar:
 * - 1 XLM = 10,000,000 stroops (7 decimals)
 * - 1 USDC = 1,000,000 stroops (6 decimals)
 *
 * Rejection criteria:
 * - Negative or zero amounts
 * - Scientific notation (1e3, 1E-3, etc.)
 * - More decimal places than the asset supports
 * - Invalid format (non-numeric strings, multiple dots, etc.)
 *
 * @param input The user-provided amount string
 * @param decimals The number of decimal places for the asset (6 for USDC, 7 for XLM)
 * @returns ParseResult with stroops if valid, or error details if invalid
 */
export function parseAmountToStroops(
  input: string,
  decimals: number
): ParseResult {
  try {
    // Step 1: Parse strictly (rejects scientific notation, invalid formats)
    const decimal = parseDecimalStrict(input);

    // Step 2: Reject negative or zero amounts
    if (decimal.isNegative() || decimal.isZero()) {
      return {
        valid: false,
        error: {
          code: "NEGATIVE_OR_ZERO",
          message: "Amount must be positive and greater than zero",
        },
      };
    }

    // Step 3: Check decimal places don't exceed asset precision
    // decimal.decimalPlaces() returns the number of fractional digits
    const decimalPlaces = decimal.decimalPlaces();
    if (decimalPlaces > decimals) {
      return {
        valid: false,
        error: {
          code: "TOO_MANY_DECIMALS",
          message: `Maximum ${decimals} decimal places allowed, but got ${decimalPlaces}`,
        },
      };
    }

    // Step 4: Convert to stroops using bigint arithmetic
    // Multiply by 10^decimals to get integer stroops
    const factor = new Decimal(10).pow(decimals);
    const stroopsDecimal = decimal.times(factor);

    // Check if result fits in bigint (it should for any reasonable amount)
    // The result must be an integer at this point
    if (!stroopsDecimal.isInteger()) {
      // This should never happen given our checks above, but be safe
      return {
        valid: false,
        error: {
          code: "INVALID_FORMAT",
          message: "Internal error: could not convert to integer stroops",
        },
      };
    }

    // Convert to bigint
    const stroops = BigInt(stroopsDecimal.toString());

    return {
      valid: true,
      stroops,
    };
  } catch (err) {
    // Catch any exceptions from parseDecimalStrict or Decimal.js
    const message = err instanceof Error ? err.message : String(err);

    // Classify the error
    type ErrorCode =
      | "INVALID_FORMAT"
      | "NEGATIVE_OR_ZERO"
      | "SCIENTIFIC_NOTATION"
      | "TOO_MANY_DECIMALS"
      | "INSUFFICIENT_BALANCE";

    let code: ErrorCode = "INVALID_FORMAT";
    if (message.includes("Scientific")) {
      code = "SCIENTIFIC_NOTATION";
    }

    return {
      valid: false,
      error: {
        code,
        message,
      },
    };
  }
}

/**
 * Format integer stroops back to a human-readable decimal string.
 *
 * Strips trailing zeros for cleaner display. For example:
 * - 10000000 stroops (7 decimals) → "1" (not "1.0000000")
 * - 1 stroop (7 decimals) → "0.0000001"
 *
 * @param stroops The amount in stroops (as bigint)
 * @param decimals The number of decimal places for the asset
 * @returns A decimal string suitable for display
 */
export function formatStroopsToDisplay(stroops: bigint, decimals: number): string {
  // Convert stroops to string and pad with leading zeros if needed
  const stroopsStr = stroops.toString();
  const padded = stroopsStr.padStart(decimals + 1, "0");

  // Insert decimal point: split into integer and fractional parts
  const integerPart = padded.slice(0, -decimals) || "0";
  const fractionalPart = padded.slice(-decimals);

  // Combine and trim trailing zeros
  let result = `${integerPart}.${fractionalPart}`;
  result = result.replace(/\.?0+$/, ""); // Remove trailing zeros and unnecessary decimal point

  return result;
}

/**
 * Validate an amount input string.
 *
 * Checks:
 * - Format is valid (not scientific notation, not negative, not zero, etc.)
 * - Decimal places don't exceed asset precision
 * - Amount doesn't exceed user's balance
 *
 * @param input The user-provided amount string
 * @param decimals The number of decimal places for the asset
 * @param balance The user's available balance (as human-readable number)
 * @returns ValidationResult with details if invalid
 */
export function isValidAmountInput(
  input: string,
  decimals: number,
  balance: number
): ValidationResult {
  // First, parse the amount
  const parseResult = parseAmountToStroops(input, decimals);

  if (!parseResult.valid) {
    // Map error codes to user-friendly messages
    const messages: Record<string, string> = {
      INVALID_FORMAT: "Invalid amount format",
      NEGATIVE_OR_ZERO: "Amount must be greater than zero",
      SCIENTIFIC_NOTATION: "Scientific notation is not allowed",
      TOO_MANY_DECIMALS: `Too many decimal places for this asset (max ${decimals})`,
    };

    return {
      valid: false,
      reason: messages[parseResult.error?.code || ""] || parseResult.error?.message,
    };
  }

  // Next, check balance
  // Convert balance to stroops for comparison
  const balanceDecimal = new Decimal(balance.toString());
  const balanceFactor = new Decimal(10).pow(decimals);
  // Floor before BigInt: balances are floats (an accruing position value is
  // never a whole number of stroops), and BigInt() throws SyntaxError on any
  // fractional string. Flooring is also the correct direction — it can only
  // understate spendable balance, never overstate it.
  const balanceStroops = balanceDecimal.times(balanceFactor).floor();
  const balanceStroopsBigInt = BigInt(balanceStroops.toFixed(0));

  if (parseResult.stroops! > balanceStroopsBigInt) {
    return {
      valid: false,
      reason: `Insufficient balance`,
    };
  }

  return { valid: true };
}

/**
 * Convert stroops (as bigint) to a human-readable decimal number for calculations.
 * Use sparingly — only when you need to pass to systems that require floats.
 * For display, use formatStroopsToDisplay instead.
 *
 * @param stroops The amount in stroops (as bigint)
 * @param decimals The number of decimal places for the asset
 * @returns A JavaScript number (precision loss possible for large amounts)
 *
 * @deprecated Avoid if possible. Prefer using stroops (bigint) directly.
 */
export function stroopsToNumber(stroops: bigint, decimals: number): number {
  const divisor = BigInt(10 ** decimals);
  return Number(stroops) / Number(divisor);
}

/**
 * Convert stroops back to a decimal string (for downstream APIs expecting string amounts).
 *
 * @param stroops The amount in stroops (as bigint)
 * @param decimals The number of decimal places for the asset
 * @returns A decimal string
 */
export function stroopsToDecimalString(stroops: bigint, decimals: number): string {
  return formatStroopsToDisplay(stroops, decimals);
}
