"use client";

import * as React from "react";
import { motion, AnimatePresence } from "framer-motion";
import { Button } from "@/components/ui/button";

interface CookieState {
    necessary: boolean;
    analytics: boolean;
}

export function CookieConsent() {
    const [isVisible, setIsVisible] = React.useState(false);
    const [showPreferences, setShowPreferences] = React.useState(false);
    
    const [preferences, setPreferences] = React.useState<CookieState>({
        necessary: true,
        analytics: false,
    });

    React.useEffect(() => {
        const getCookie = () => {
            const cookies = document.cookie.split(";").map((c) => c.trim());
            const consentCookie = cookies.find((c) => c.startsWith("nester-cookie-consent="));
            if (consentCookie) {
                try {
                    const val = decodeURIComponent(consentCookie.split("=")[1]);
                    return JSON.parse(val) as CookieState;
                } catch (e) {
                    return null;
                }
            }
            return null;
        };

        const saved = getCookie();
        if (!saved) {
            const timer = setTimeout(() => setIsVisible(true), 1000);
            return () => clearTimeout(timer);
        } else {
            setPreferences(saved);
        }
    }, []);

    React.useEffect(() => {
        const handleOpen = () => {
            setIsVisible(true);
            setShowPreferences(true);
        };
        window.addEventListener("open-cookie-consent", handleOpen);
        return () => window.removeEventListener("open-cookie-consent", handleOpen);
    }, []);

    const saveAndClose = (newState: CookieState) => {
        const value = encodeURIComponent(JSON.stringify(newState));
        // 1-year expiry
        document.cookie = `nester-cookie-consent=${value}; max-age=31536000; path=/`;
        setPreferences(newState);
        setIsVisible(false);
        setShowPreferences(false);
        window.dispatchEvent(new Event("cookie-consent-updated"));
    };

    const handleAcceptAll = () => saveAndClose({ necessary: true, analytics: true });
    const handleDenyAll = () => saveAndClose({ necessary: true, analytics: false });
    const handleSavePreferences = () => saveAndClose(preferences);

    return (
        <AnimatePresence>
            {isVisible && (
                <div role="dialog" aria-modal="true" aria-labelledby="cookie-banner-title">
                    {/* Backdrop for preferences view only */}
                    {showPreferences && (
                        <motion.div
                            initial={{ opacity: 0 }}
                            animate={{ opacity: 1 }}
                            exit={{ opacity: 0 }}
                            className="fixed inset-0 z-[90] bg-black/40 backdrop-blur-sm"
                            onClick={() => setShowPreferences(false)}
                        />
                    )}

                    <motion.div
                        initial={{ opacity: 0, y: 20, scale: 0.95 }}
                        animate={{ opacity: 1, y: 0, scale: 1 }}
                        exit={{ opacity: 0, y: 20, scale: 0.95 }}
                        transition={{ duration: 0.3 }}
                        className={`fixed bottom-4 left-4 right-4 z-[100] bg-white rounded-[20px] shadow-2xl border border-border/10 overflow-hidden ${
                            showPreferences 
                                ? "top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 bottom-auto right-auto w-[90%] max-w-md max-h-[90vh] overflow-y-auto m-0 rounded-2xl" 
                                : "sm:left-auto sm:right-6 sm:bottom-6 sm:w-full sm:max-w-[420px]"
                        }`}
                    >
                        {!showPreferences ? (
                            <div className="p-5 sm:p-6 flex flex-col gap-6">
                                <p id="cookie-banner-title" className="text-[15px] text-[#111827] leading-relaxed font-sans">
                                    We use cookies to enhance your user experience, provide personalised content and analyse traffic.{" "}
                                    <a href="#" className="underline decoration-1 underline-offset-2 hover:text-nester-blue transition-colors">Cookie Policy</a>
                                </p>

                                <div className="flex flex-col sm:flex-row items-center gap-3">
                                    <div className="flex gap-2 w-full sm:w-auto">
                                        <Button
                                            onClick={handleAcceptAll}
                                            className="flex-1 sm:flex-none bg-[#111827] hover:bg-black text-white rounded-full px-6 h-11 font-medium text-sm transition-transform active:scale-95 shadow-none"
                                        >
                                            Accept All
                                        </Button>
                                        <Button
                                            onClick={handleDenyAll}
                                            variant="outline"
                                            className="flex-1 sm:flex-none bg-white border-[#E5E7EB] hover:bg-gray-50 text-[#111827] rounded-full px-6 h-11 font-medium text-sm transition-transform active:scale-95"
                                        >
                                            Deny All
                                        </Button>
                                    </div>
                                    <button
                                        onClick={() => setShowPreferences(true)}
                                        className="text-sm font-medium text-muted-foreground hover:text-foreground underline decoration-1 underline-offset-2 transition-colors mt-2 sm:mt-0 ml-auto"
                                    >
                                        Manage
                                    </button>
                                </div>
                            </div>
                        ) : (
                            <div className="flex flex-col h-full">
                                <div className="p-6 border-b border-border/10">
                                    <h2 id="cookie-banner-title" className="text-xl font-bold font-heading text-foreground mb-2">Cookie Preferences</h2>
                                    <p className="text-sm text-muted-foreground leading-relaxed">
                                        Manage your cookie preferences. Necessary cookies cannot be disabled as they are required for the website to function properly.
                                    </p>
                                </div>
                                <div className="p-6 space-y-6">
                                    <div className="flex items-start justify-between gap-4">
                                        <div>
                                            <h3 className="font-semibold text-foreground">Necessary Cookies</h3>
                                            <p className="text-sm text-muted-foreground mt-1">Required for core functionality like security and network management.</p>
                                        </div>
                                        <div className="relative inline-flex items-center cursor-not-allowed">
                                            <div className="w-11 h-6 bg-emerald-500 rounded-full opacity-60"></div>
                                            <div className="absolute left-[22px] top-1 w-4 h-4 bg-white rounded-full shadow-sm"></div>
                                        </div>
                                    </div>
                                    <div className="flex items-start justify-between gap-4">
                                        <div>
                                            <h3 className="font-semibold text-foreground">Analytics Cookies</h3>
                                            <p className="text-sm text-muted-foreground mt-1">Help us improve the website by understanding how you interact with it.</p>
                                        </div>
                                        <button 
                                            type="button"
                                            role="switch"
                                            aria-checked={preferences.analytics}
                                            onClick={() => setPreferences(p => ({ ...p, analytics: !p.analytics }))}
                                            className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 ${preferences.analytics ? 'bg-emerald-500' : 'bg-input'}`}
                                        >
                                            <span className={`inline-block h-4 w-4 transform rounded-full bg-background shadow-sm transition-transform ${preferences.analytics ? 'translate-x-[22px]' : 'translate-x-1'}`} />
                                        </button>
                                    </div>
                                </div>
                                <div className="p-6 bg-muted/30 border-t border-border/10 flex flex-col sm:flex-row gap-3">
                                    <Button 
                                        onClick={handleSavePreferences}
                                        className="w-full sm:flex-1 bg-[#111827] hover:bg-black text-white rounded-full h-11 font-medium"
                                    >
                                        Save Preferences
                                    </Button>
                                    <Button 
                                        onClick={handleAcceptAll}
                                        variant="outline"
                                        className="w-full sm:flex-1 rounded-full h-11 font-medium"
                                    >
                                        Accept All
                                    </Button>
                                </div>
                            </div>
                        )}
                    </motion.div>
                </div>
            )}
        </AnimatePresence>
    );
}
