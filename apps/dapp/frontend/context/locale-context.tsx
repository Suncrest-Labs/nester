"use client";

import React, { createContext, useCallback, useContext, useEffect, useState } from "react";
import { DEFAULT_LOCALE, isSupportedLocale, type Locale } from "@/lib/i18n/config";
import {
    formatCurrency as formatCurrencyBase,
    formatDate as formatDateBase,
    formatNumber as formatNumberBase,
} from "@/lib/i18n/format";
import en from "@/lib/i18n/locales/en.json";
import fr from "@/lib/i18n/locales/fr.json";

const CATALOGS: Record<Locale, Messages> = { en, fr };

type Messages = typeof en;

const LOCALE_STORAGE_KEY = "nester-locale";

function getMessage(catalog: Messages, key: string): string {
    const value = key
        .split(".")
        .reduce<unknown>(
            (acc, part) =>
                acc && typeof acc === "object" ? (acc as Record<string, unknown>)[part] : undefined,
            catalog
        );
    return typeof value === "string" ? value : key;
}

interface LocaleContextValue {
    locale: Locale;
    setLocale: (locale: Locale) => void;
    t: (key: string) => string;
    formatCurrency: (amount: number, currency: string) => string;
    formatNumber: (value: number, options?: Intl.NumberFormatOptions) => string;
    formatDate: (value: Date | string | number, options?: Intl.DateTimeFormatOptions) => string;
}

const LocaleContext = createContext<LocaleContextValue | undefined>(undefined);

export function LocaleProvider({ children }: { children: React.ReactNode }) {
    const [locale, setLocaleState] = useState<Locale>(DEFAULT_LOCALE);

    useEffect(() => {
        if (typeof window === "undefined") return;
        const stored = localStorage.getItem(LOCALE_STORAGE_KEY);
        if (isSupportedLocale(stored)) {
            // Deferred a tick so the client's first render matches the
            // server-rendered default locale, avoiding a hydration mismatch.
            const timer = setTimeout(() => setLocaleState(stored), 0);
            return () => clearTimeout(timer);
        }
    }, []);

    const setLocale = useCallback((next: Locale) => {
        setLocaleState(next);
        if (typeof window !== "undefined") {
            localStorage.setItem(LOCALE_STORAGE_KEY, next);
        }
    }, []);

    const t = useCallback((key: string) => getMessage(CATALOGS[locale], key), [locale]);

    const formatCurrency = useCallback(
        (amount: number, currency: string) => formatCurrencyBase(amount, currency, locale),
        [locale]
    );
    const formatNumber = useCallback(
        (value: number, options?: Intl.NumberFormatOptions) => formatNumberBase(value, locale, options),
        [locale]
    );
    const formatDate = useCallback(
        (value: Date | string | number, options?: Intl.DateTimeFormatOptions) =>
            formatDateBase(value, locale, options),
        [locale]
    );

    return (
        <LocaleContext.Provider
            value={{ locale, setLocale, t, formatCurrency, formatNumber, formatDate }}
        >
            {children}
        </LocaleContext.Provider>
    );
}

export function useLocale(): LocaleContextValue {
    const ctx = useContext(LocaleContext);
    if (!ctx) {
        throw new Error("useLocale must be used within a LocaleProvider");
    }
    return ctx;
}

/** Convenience hook for components that only need the translate function. */
export function useTranslations(): (key: string) => string {
    return useLocale().t;
}
