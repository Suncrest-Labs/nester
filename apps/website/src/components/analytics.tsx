"use client";

import { useEffect, useState } from "react";
import Script from "next/script";

export function Analytics() {
    const [hasAnalyticsConsent, setHasAnalyticsConsent] = useState(false);

    useEffect(() => {
        const checkConsent = () => {
            const cookies = document.cookie.split(";").map((c) => c.trim());
            const consentCookie = cookies.find((c) => c.startsWith("nester-cookie-consent="));
            if (consentCookie) {
                try {
                    const val = decodeURIComponent(consentCookie.split("=")[1]);
                    const parsed = JSON.parse(val);
                    if (parsed.analytics) {
                        setHasAnalyticsConsent(true);
                    } else {
                        setHasAnalyticsConsent(false);
                    }
                } catch (e) {
                    // Invalid cookie, ignore
                }
            } else {
                setHasAnalyticsConsent(false);
            }
        };

        // Check on mount
        checkConsent();

        // Listen for updates from the cookie consent banner
        window.addEventListener("cookie-consent-updated", checkConsent);
        return () => window.removeEventListener("cookie-consent-updated", checkConsent);
    }, []);

    if (!hasAnalyticsConsent) return null;

    return (
        <Script
            strategy="afterInteractive"
            data-domain="nester.finance"
            src="https://plausible.io/js/script.js"
        />
    );
}
