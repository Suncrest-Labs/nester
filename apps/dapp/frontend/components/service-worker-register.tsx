"use client";

import { useEffect } from "react";

export function ServiceWorkerRegister() {
    useEffect(() => {
        if (typeof window === "undefined" || !("serviceWorker" in navigator)) {
            return;
        }
        // Registering after `load` keeps the SW off the critical rendering path.
        const register = () => {
            navigator.serviceWorker.register("/sw.js").catch((err) => {
                console.warn("Service worker registration failed:", err);
            });
        };
        window.addEventListener("load", register);
        return () => window.removeEventListener("load", register);
    }, []);

    return null;
}
