"use client";

import React, { createContext, useContext, useEffect, useState } from "react";
import { config } from "@/lib/config";

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

interface SettingsContextType {
    currency: Currency;
    setCurrency: (val: Currency) => void;
    formatValue: (usdValue: number) => string;
    exchangeRate: number;
}

const SettingsContext = createContext<SettingsContextType | undefined>(undefined);

export function SettingsProvider({ children }: { children: React.ReactNode }) {
    const [currency, setCurrencyState] = useState<Currency>("USD");

    // Load on mount
    useEffect(() => {
        const savedCurrency = localStorage.getItem("nester_currency") as Currency;
        if (savedCurrency && EXCHANGE_RATES[savedCurrency]) {
            setCurrencyState(savedCurrency);
        }
    }, []);

    const setCurrency = (val: Currency) => {
        setCurrencyState(val);
        localStorage.setItem("nester_currency", val);
    };

    const formatValue = (usdValue: number) => {
        const rate = EXCHANGE_RATES[currency];
        const localValue = usdValue * rate;
        const symbol = CURRENCY_SYMBOLS[currency];

        return `${symbol}${localValue.toLocaleString(undefined, {
            minimumFractionDigits: 2,
            maximumFractionDigits: 2,
        })}`;
    };

    const exchangeRate = EXCHANGE_RATES[currency];

    return (
        <SettingsContext.Provider
            value={{
                currency,
                setCurrency,
                formatValue,
                exchangeRate
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
