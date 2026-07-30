export const SUPPORTED_LOCALES = ["en", "fr"] as const;

export type Locale = (typeof SUPPORTED_LOCALES)[number];

export const DEFAULT_LOCALE: Locale = "en";

export const LOCALE_LABELS: Record<Locale, string> = {
    en: "English",
    fr: "Français",
};

// Maps an app locale to the BCP-47 tag Intl.NumberFormat/DateTimeFormat expect.
export const LOCALE_TAGS: Record<Locale, string> = {
    en: "en-US",
    fr: "fr-FR",
};

export function isSupportedLocale(value: string | null | undefined): value is Locale {
    return !!value && (SUPPORTED_LOCALES as readonly string[]).includes(value);
}
