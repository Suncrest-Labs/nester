export const CONSENT_STORAGE_KEY = "nester-consent";

export interface ConsentPreferences {
  necessary: true;
  analytics: boolean;
  thirdParty: boolean;
  decided: boolean;
}

export const DEFAULT_CONSENT: ConsentPreferences = {
  necessary: true,
  analytics: false,
  thirdParty: false,
  decided: false,
};

export function parseConsent(raw: string | null): ConsentPreferences {
  if (!raw) return DEFAULT_CONSENT;
  try {
    const parsed = JSON.parse(raw) as Partial<ConsentPreferences>;
    return {
      necessary: true,
      analytics: !!parsed.analytics,
      thirdParty: !!parsed.thirdParty,
      decided: !!parsed.decided,
    };
  } catch {
    return DEFAULT_CONSENT;
  }
}

/** Synchronous check for third-party data consent (usable outside React). */
export function hasThirdPartyConsent(): boolean {
  if (typeof window === "undefined") return false;
  return parseConsent(window.localStorage.getItem(CONSENT_STORAGE_KEY)).thirdParty;
}

export function serializeConsent(prefs: ConsentPreferences): string {
  return JSON.stringify(prefs);
}
