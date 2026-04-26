"use client";

import React, { createContext, useContext, useEffect, useMemo, useState } from "react";
import { config } from "@/lib/config";
import { safeStorage } from "@/lib/storage";

export type Currency = "USD" | "GBP" | "EUR" | "NGN";

export const CURRENCY_SYMBOLS: Record<Currency, string> = {
    USD: "$",
    GBP: "£",
    EUR: "€",
    NGN: "₦",
};

export const EXCHANGE_RATES: Record<Currency, number> = {
    USD: 1,
    GBP: 0.79,
    EUR: 0.92,
    NGN: config.defaultNgnRate,
};

const CURRENCY_STORAGE_KEY = "nester_currency";
const RATES_STORAGE_KEY = "nester_rates_v1";
const RATE_STALE_THRESHOLD_MS = 24 * 60 * 60 * 1000; // 24h

interface StoredRates {
    rates: Record<Currency, number>;
    updatedAt: string;
}

interface SettingsContextType {
    currency: Currency;
    setCurrency: (val: Currency) => void;
    formatValue: (usdValue: number) => string;
    exchangeRate: number;
    /** ISO timestamp of when the rates were last refreshed (or app build time). */
    ratesUpdatedAt: string;
    /** True when rates are older than RATE_STALE_THRESHOLD_MS. */
    ratesAreStale: boolean;
}

const SettingsContext = createContext<SettingsContextType | undefined>(undefined);

const buildTimeRates: StoredRates = {
    rates: EXCHANGE_RATES,
    // Captured at module-load time as a stand-in until a live feed is wired.
    updatedAt: new Date().toISOString(),
};

export function SettingsProvider({ children }: { children: React.ReactNode }) {
    const [currency, setCurrencyState] = useState<Currency>("USD");
    const [ratesEnvelope, setRatesEnvelope] = useState<StoredRates>(buildTimeRates);

    // Restore currency on mount. Accept both the legacy raw-string form
    // ("USD") and the JSON-encoded form ('"USD"') so existing users don't
    // get reset on upgrade.
    useEffect(() => {
        const raw = safeStorage.getRaw(CURRENCY_STORAGE_KEY);
        if (!raw) return;
        let candidate = raw;
        try {
            const parsed = JSON.parse(raw);
            if (typeof parsed === "string") candidate = parsed;
        } catch {
            // Legacy plain-string value; use raw as-is.
        }
        if (candidate in EXCHANGE_RATES) {
            setCurrencyState(candidate as Currency);
        }
    }, []);

    // Restore stored rates envelope on mount.
    useEffect(() => {
        const stored = safeStorage.get<StoredRates | null>(RATES_STORAGE_KEY, null);
        if (
            stored &&
            typeof stored === "object" &&
            stored.rates &&
            typeof stored.updatedAt === "string"
        ) {
            setRatesEnvelope({
                rates: { ...EXCHANGE_RATES, ...stored.rates },
                updatedAt: stored.updatedAt,
            });
        } else {
            // First load — persist the build-time fallback so the timestamp
            // becomes meaningful (and the staleness banner doesn't fire on
            // first render).
            safeStorage.set(RATES_STORAGE_KEY, buildTimeRates);
        }
    }, []);

    // Cross-tab sync for both currency choice and rates envelope.
    useEffect(() => {
        const offCurrency = safeStorage.subscribe<string>(CURRENCY_STORAGE_KEY, (next) => {
            if (typeof next === "string" && next in EXCHANGE_RATES) {
                setCurrencyState(next as Currency);
            }
        });
        const offRates = safeStorage.subscribe<StoredRates>(RATES_STORAGE_KEY, (next) => {
            if (next && next.rates && typeof next.updatedAt === "string") {
                setRatesEnvelope({
                    rates: { ...EXCHANGE_RATES, ...next.rates },
                    updatedAt: next.updatedAt,
                });
            }
        });
        return () => {
            offCurrency();
            offRates();
        };
    }, []);

    const setCurrency = (val: Currency) => {
        setCurrencyState(val);
        // Plain string write — keep parity with safeStorage.subscribe payloads
        // by JSON-encoding consistently.
        safeStorage.set(CURRENCY_STORAGE_KEY, val);
    };

    const formatValue = (usdValue: number) => {
        const rate = ratesEnvelope.rates[currency] ?? EXCHANGE_RATES[currency];
        const localValue = usdValue * rate;
        const symbol = CURRENCY_SYMBOLS[currency];

        return `${symbol}${localValue.toLocaleString(undefined, {
            minimumFractionDigits: 2,
            maximumFractionDigits: 2,
        })}`;
    };

    const exchangeRate = ratesEnvelope.rates[currency] ?? EXCHANGE_RATES[currency];

    const ratesAreStale = useMemo(() => {
        const updated = Date.parse(ratesEnvelope.updatedAt);
        if (!Number.isFinite(updated)) return true;
        return Date.now() - updated > RATE_STALE_THRESHOLD_MS;
    }, [ratesEnvelope.updatedAt]);

    return (
        <SettingsContext.Provider
            value={{
                currency,
                setCurrency,
                formatValue,
                exchangeRate,
                ratesUpdatedAt: ratesEnvelope.updatedAt,
                ratesAreStale,
            }}
        >
            {children}
        </SettingsContext.Provider>
    );
}

export function useSettings() {
    const context = useContext(SettingsContext);
    if (!context) {
        throw new Error("useSettings must be used within a SettingsProvider");
    }
    return context;
}
