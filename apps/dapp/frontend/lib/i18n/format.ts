import { LOCALE_TAGS, type Locale } from "./config";

function resolveTag(locale: Locale): string {
    return LOCALE_TAGS[locale] ?? "en-US";
}

/**
 * Locale-aware currency formatter. Replaces ad-hoc `$${n.toFixed(2)}` /
 * `n.toLocaleString("en-US", ...)` calls scattered across the dApp so every
 * screen formats money the same way for the user's chosen locale + currency.
 */
export function formatCurrency(amount: number, currency: string, locale: Locale = "en"): string {
    try {
        return new Intl.NumberFormat(resolveTag(locale), {
            style: "currency",
            currency,
            minimumFractionDigits: 2,
            maximumFractionDigits: 2,
        }).format(amount);
    } catch {
        // Intl throws on an unknown ISO 4217 code (e.g. a crypto ticker like
        // "XLM"). Fall back to a plain number plus the raw currency code.
        return `${formatNumber(amount, locale, { minimumFractionDigits: 2, maximumFractionDigits: 2 })} ${currency}`;
    }
}

export function formatNumber(
    value: number,
    locale: Locale = "en",
    options?: Intl.NumberFormatOptions
): string {
    return new Intl.NumberFormat(resolveTag(locale), options).format(value);
}

export function formatDate(
    value: Date | string | number,
    locale: Locale = "en",
    options?: Intl.DateTimeFormatOptions
): string {
    const date = value instanceof Date ? value : new Date(value);
    return new Intl.DateTimeFormat(resolveTag(locale), options).format(date);
}
