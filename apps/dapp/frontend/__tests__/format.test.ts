import { describe, it, expect } from "vitest";
import { formatCurrency, formatNumber, formatDate } from "@/lib/i18n/format";

describe("formatCurrency", () => {
    it("formats USD for the en locale", () => {
        expect(formatCurrency(1234.5, "USD", "en")).toBe("$1,234.50");
    });

    it("formats EUR for the fr locale using French grouping/decimal marks", () => {
        const result = formatCurrency(1234.5, "EUR", "fr");
        // fr-FR uses non-breaking spaces as the thousands separator and a
        // comma decimal mark; normalize whitespace before comparing.
        expect(result.replace(/ | /g, " ")).toBe("1 234,50 €");
    });

    it("rounds to 2 decimal places regardless of input precision", () => {
        expect(formatCurrency(10, "USD", "en")).toBe("$10.00");
        expect(formatCurrency(10.999, "USD", "en")).toBe("$11.00");
    });

    it("falls back to a plain number + currency code for a non-ISO currency", () => {
        const result = formatCurrency(100, "XLM", "en");
        expect(result).toContain("100.00");
        expect(result).toContain("XLM");
    });

    it("defaults to the en locale when none is passed", () => {
        expect(formatCurrency(5, "USD")).toBe("$5.00");
    });
});

describe("formatNumber", () => {
    it("formats a number using locale grouping", () => {
        expect(formatNumber(1234567, "en")).toBe("1,234,567");
    });
});

describe("formatDate", () => {
    it("formats a date deterministically for a fixed locale", () => {
        const result = formatDate("2026-01-15T00:00:00Z", "en", {
            year: "numeric",
            month: "short",
            day: "numeric",
            timeZone: "UTC",
        });
        expect(result).toBe("Jan 15, 2026");
    });
});
