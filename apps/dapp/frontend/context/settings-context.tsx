"use client";

import React, { createContext, useContext, useEffect, useState } from "react";
import { config } from "@/lib/config";

export type Currency = "USD" | "GBP" | "EUR" | "NGN";
export type Theme = "light" | "dark" | "system";

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

const THEME_STORAGE_KEY = "nester-theme";
const LEGACY_THEME_KEY = "nester_theme";

interface SettingsContextType {
    currency: Currency;
    setCurrency: (val: Currency) => void;
    formatValue: (usdValue: number) => string;
    exchangeRate: number;
    theme: Theme;
    setTheme: (val: Theme) => void;
    isDarkMode: boolean;
}

const SettingsContext = createContext<SettingsContextType | undefined>(undefined);

function resolveDark(theme: Theme): boolean {
    if (theme === "dark") return true;
    if (theme === "light") return false;
    if (typeof window !== "undefined") {
        return window.matchMedia("(prefers-color-scheme: dark)").matches;
    }
    return true;
}

function applyThemeClass(theme: Theme) {
    const root = document.documentElement;
    if (resolveDark(theme)) {
        root.classList.add("dark");
    } else {
        root.classList.remove("dark");
    }
}

export function SettingsProvider({ children }: { children: React.ReactNode }) {
    const [currency, setCurrencyState] = useState<Currency>("USD");
    const [theme, setThemeState] = useState<Theme>("dark");
    const [isDarkMode, setIsDarkMode] = useState(true);

    useEffect(() => {
        const savedCurrency = localStorage.getItem("nester_currency") as Currency;
        if (savedCurrency && EXCHANGE_RATES[savedCurrency]) {
            const timer = setTimeout(() => setCurrencyState(savedCurrency), 0);
            return () => clearTimeout(timer);
        }
    }, []);

    useEffect(() => {
        if (typeof window === "undefined") return;

        const saved =
            (localStorage.getItem(THEME_STORAGE_KEY) as Theme | null) ??
            (localStorage.getItem(LEGACY_THEME_KEY) as Theme | null) ??
            "dark";
        const resolved: Theme =
            saved === "light" || saved === "dark" || saved === "system" ? saved : "dark";

        const timer = setTimeout(() => {
            setThemeState(resolved);
            applyThemeClass(resolved);
            setIsDarkMode(resolveDark(resolved));
        }, 0);
        
        if (resolved === "system") {
            const mq = window.matchMedia("(prefers-color-scheme: dark)");
            const onChange = () => {
                setIsDarkMode(mq.matches);
            };
            mq.addListener(onChange);
            return () => {
                mq.removeListener(onChange);
                clearTimeout(timer);
            };
        }
        
        return () => clearTimeout(timer);
    }, []);

    const setCurrency = (val: Currency) => {
        setCurrencyState(val);
        localStorage.setItem("nester_currency", val);
    };

    const setTheme = (val: Theme) => {
        setThemeState(val);
        localStorage.setItem(THEME_STORAGE_KEY, val);
        localStorage.removeItem(LEGACY_THEME_KEY);
        applyThemeClass(val);
        setIsDarkMode(resolveDark(val));
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
                exchangeRate,
                theme,
                setTheme,
                isDarkMode,
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
