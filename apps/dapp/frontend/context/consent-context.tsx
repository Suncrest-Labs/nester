"use client";

import React, { createContext, useCallback, useContext, useEffect, useState } from "react";
import {
  CONSENT_STORAGE_KEY,
  DEFAULT_CONSENT,
  type ConsentPreferences,
  parseConsent,
  serializeConsent,
} from "@/lib/consent";

interface ConsentContextType {
  consent: ConsentPreferences;
  showBanner: boolean;
  showPreferences: boolean;
  acceptAll: () => void;
  rejectOptional: () => void;
  savePreferences: (analytics: boolean, thirdParty: boolean) => void;
  openPreferences: () => void;
  closePreferences: () => void;
}

const ConsentContext = createContext<ConsentContextType | undefined>(undefined);

export function ConsentProvider({ children }: { children: React.ReactNode }) {
  const [consent, setConsent] = useState<ConsentPreferences>(DEFAULT_CONSENT);
  const [showBanner, setShowBanner] = useState(false);
  const [showPreferences, setShowPreferences] = useState(false);
  const [hydrated, setHydrated] = useState(false);

  useEffect(() => {
    const stored = parseConsent(localStorage.getItem(CONSENT_STORAGE_KEY));
    setConsent(stored);
    setShowBanner(!stored.decided);
    setHydrated(true);
  }, []);

  const persist = useCallback((next: ConsentPreferences) => {
    setConsent(next);
    localStorage.setItem(CONSENT_STORAGE_KEY, serializeConsent(next));
    setShowBanner(false);
    setShowPreferences(false);
  }, []);

  const acceptAll = useCallback(() => {
    persist({ necessary: true, analytics: true, thirdParty: true, decided: true });
  }, [persist]);

  const rejectOptional = useCallback(() => {
    persist({ necessary: true, analytics: false, thirdParty: false, decided: true });
  }, [persist]);

  const savePreferences = useCallback(
    (analytics: boolean, thirdParty: boolean) => {
      persist({ necessary: true, analytics, thirdParty, decided: true });
    },
    [persist]
  );

  const openPreferences = useCallback(() => setShowPreferences(true), []);
  const closePreferences = useCallback(() => setShowPreferences(false), []);

  return (
    <ConsentContext.Provider
      value={{
        consent,
        showBanner: hydrated && showBanner,
        showPreferences: hydrated && showPreferences,
        acceptAll,
        rejectOptional,
        savePreferences,
        openPreferences,
        closePreferences,
      }}
    >
      {children}
    </ConsentContext.Provider>
  );
}

export function useConsent() {
  const ctx = useContext(ConsentContext);
  if (!ctx) {
    throw new Error("useConsent must be used within ConsentProvider");
  }
  return ctx;
}
