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

export function SettingsProvider({ children }: { children: React.ReactNode }) {
    const [currency, setCurrencyState] = useState<Currency>("USD");
    const [theme, setThemeState] = useState<Theme>("system");
    const [isDarkMode, setIsDarkMode] = useState(false);

    // Load settings on mount and apply theme
    useEffect(() => {
        const savedCurrency = localStorage.getItem("nester_currency") as Currency;
        if (savedCurrency && EXCHANGE_RATES[savedCurrency]) {
            const timer = setTimeout(() => {
                setCurrencyState(savedCurrency);
            }, 0);
            return () => clearTimeout(timer);
        }
    }, []);

    // Handle theme loading and application
    useEffect(() => {
        const savedTheme = (localStorage.getItem("nester_theme") as Theme) || "system";
        setThemeState(savedTheme);

        // Determine if dark mode should be active
        const applyTheme = (themeToApply: Theme) => {
            const root = document.documentElement;
            let shouldBeDark = false;

            if (themeToApply === "dark") {
                shouldBeDark = true;
            } else if (themeToApply === "light") {
                shouldBeDark = false;
            } else {
                // System preference
                shouldBeDark = window.matchMedia("(prefers-color-scheme: dark)").matches;
            }

            setIsDarkMode(shouldBeDark);

            if (shouldBeDark) {
                root.classList.add("dark");
            } else {
                root.classList.remove("dark");
            }
        };

        applyTheme(savedTheme);

        // Listen for system theme changes when in system mode
        if (savedTheme === "system") {
            const mediaQuery = window.matchMedia("(prefers-color-scheme: dark)");
            const handleChange = (e: MediaQueryListEvent) => {
                applyTheme("system");
            };

            mediaQuery.addEventListener("change", handleChange);
            return () => mediaQuery.removeEventListener("change", handleChange);
        }
    }, []);

    const setCurrency = (val: Currency) => {
        setCurrencyState(val);
        localStorage.setItem("nester_currency", val);
    };

    const setTheme = (val: Theme) => {
        setThemeState(val);
        localStorage.setItem("nester_theme", val);

        // Apply theme immediately
        const root = document.documentElement;
        let shouldBeDark = false;

        if (val === "dark") {
            shouldBeDark = true;
        } else if (val === "light") {
            shouldBeDark = false;
        } else {
            shouldBeDark = window.matchMedia("(prefers-color-scheme: dark)").matches;
        }

        setIsDarkMode(shouldBeDark);

        if (shouldBeDark) {
            root.classList.add("dark");
        } else {
            root.classList.remove("dark");
        }
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
                isDarkMode
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
