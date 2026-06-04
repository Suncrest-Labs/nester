"use client";

import { useEffect, useRef, useState } from "react";
import { motion, AnimatePresence } from "framer-motion";
import { Cookie, X } from "lucide-react";
import { useConsent } from "@/context/consent-context";
import { cn } from "@/lib/utils";

function PreferencesPanel({
  analytics,
  thirdParty,
  onAnalyticsChange,
  onThirdPartyChange,
  onSave,
  onClose,
}: {
  analytics: boolean;
  thirdParty: boolean;
  onAnalyticsChange: (v: boolean) => void;
  onThirdPartyChange: (v: boolean) => void;
  onSave: () => void;
  onClose: () => void;
}) {
  return (
    <div
      role="dialog"
      aria-labelledby="consent-preferences-title"
      aria-modal="true"
      className="rounded-2xl border border-white/10 bg-[#0D0E1C] p-5 shadow-2xl"
    >
      <div className="flex items-start justify-between gap-4 mb-4">
        <h2 id="consent-preferences-title" className="text-base font-semibold text-white">
          Cookie Preferences
        </h2>
        <button
          onClick={onClose}
          aria-label="Close preferences"
          className="rounded-lg p-1.5 text-white/50 hover:text-white hover:bg-white/10 transition-colors"
        >
          <X className="h-4 w-4" />
        </button>
      </div>

      <div className="space-y-4">
        <label className="flex items-start gap-3 opacity-60 cursor-not-allowed">
          <input type="checkbox" checked disabled className="mt-1" aria-label="Necessary cookies always enabled" />
          <div>
            <p className="text-sm font-medium text-white">Necessary</p>
            <p className="text-xs text-white/50 mt-0.5">Required for wallet connection and core app functionality.</p>
          </div>
        </label>

        <label className="flex items-start gap-3 cursor-pointer">
          <input
            type="checkbox"
            checked={analytics}
            onChange={(e) => onAnalyticsChange(e.target.checked)}
            className="mt-1"
            aria-label="Enable analytics cookies"
          />
          <div>
            <p className="text-sm font-medium text-white">Analytics</p>
            <p className="text-xs text-white/50 mt-0.5">Help us understand usage patterns to improve the DApp.</p>
          </div>
        </label>

        <label className="flex items-start gap-3 cursor-pointer">
          <input
            type="checkbox"
            checked={thirdParty}
            onChange={(e) => onThirdPartyChange(e.target.checked)}
            className="mt-1"
            aria-label="Enable third-party data feeds"
          />
          <div>
            <p className="text-sm font-medium text-white">Third-party data feeds</p>
            <p className="text-xs text-white/50 mt-0.5">CoinGecko and DeFiLlama price and yield data.</p>
          </div>
        </label>
      </div>

      <button
        onClick={onSave}
        className="mt-5 w-full rounded-xl bg-gradient-to-r from-[#B6509E] to-[#2EBAC6] px-4 py-2.5 text-sm font-semibold text-white hover:opacity-90 transition-opacity"
      >
        Save Preferences
      </button>
    </div>
  );
}

export function CookieConsentBanner() {
  const {
    showBanner,
    showPreferences,
    acceptAll,
    rejectOptional,
    savePreferences,
    closePreferences,
    openPreferences,
    consent,
  } = useConsent();

  const [analytics, setAnalytics] = useState(consent.analytics);
  const [thirdParty, setThirdParty] = useState(consent.thirdParty);
  const bannerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    setAnalytics(consent.analytics);
    setThirdParty(consent.thirdParty);
  }, [consent]);

  useEffect(() => {
    const visible = showBanner || showPreferences;
    if (!visible) return;

    const handleEsc = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        if (showPreferences) closePreferences();
        else rejectOptional();
      }
    };
    document.addEventListener("keydown", handleEsc);
    return () => document.removeEventListener("keydown", handleEsc);
  }, [showBanner, showPreferences, closePreferences, rejectOptional]);

  const visible = showBanner || showPreferences;

  return (
    <AnimatePresence>
      {visible && (
        <motion.div
          ref={bannerRef}
          initial={{ y: 100, opacity: 0 }}
          animate={{ y: 0, opacity: 1 }}
          exit={{ y: 100, opacity: 0 }}
          role="dialog"
          aria-labelledby="cookie-consent-title"
          aria-describedby="cookie-consent-desc"
          aria-modal="true"
          className="fixed bottom-0 left-0 right-0 z-[200] p-4 sm:p-6"
        >
          <div className="mx-auto max-w-2xl">
            {showPreferences ? (
              <PreferencesPanel
                analytics={analytics}
                thirdParty={thirdParty}
                onAnalyticsChange={setAnalytics}
                onThirdPartyChange={setThirdParty}
                onSave={() => savePreferences(analytics, thirdParty)}
                onClose={closePreferences}
              />
            ) : (
              <div className="rounded-2xl border border-white/10 bg-[#0D0E1C] p-5 shadow-2xl sm:p-6">
                <div className="flex items-start gap-4">
                  <div className="hidden sm:flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-white/10">
                    <Cookie className="h-5 w-5 text-[#2EBAC6]" aria-hidden="true" />
                  </div>
                  <div className="flex-1 min-w-0">
                    <h2 id="cookie-consent-title" className="text-base font-semibold text-white">
                      We value your privacy
                    </h2>
                    <p id="cookie-consent-desc" className="mt-1.5 text-sm text-white/60 leading-relaxed">
                      We use cookies and third-party data feeds to improve your experience.
                      You can accept all, reject optional cookies, or manage your preferences.
                    </p>
                    <div className="mt-4 flex flex-wrap gap-2">
                      <button
                        onClick={acceptAll}
                        className="rounded-xl bg-gradient-to-r from-[#B6509E] to-[#2EBAC6] px-4 py-2 text-sm font-semibold text-white hover:opacity-90 transition-opacity"
                      >
                        Accept All
                      </button>
                      <button
                        onClick={rejectOptional}
                        className={cn(
                          "rounded-xl border border-white/20 px-4 py-2 text-sm font-medium text-white/80",
                          "hover:bg-white/10 transition-colors"
                        )}
                      >
                        Reject Optional
                      </button>
                      <button
                        onClick={openPreferences}
                        className="rounded-xl px-4 py-2 text-sm font-medium text-[#2EBAC6] hover:underline"
                      >
                        Manage Preferences
                      </button>
                    </div>
                  </div>
                  <button
                    onClick={rejectOptional}
                    aria-label="Dismiss cookie banner"
                    className="shrink-0 rounded-lg p-1.5 text-white/40 hover:text-white hover:bg-white/10 transition-colors"
                  >
                    <X className="h-4 w-4" />
                  </button>
                </div>
              </div>
            )}
          </div>
        </motion.div>
      )}
    </AnimatePresence>
  );
}
