"use client";

import { useEffect } from "react";

export function ServiceWorkerRegister() {
    useEffect(() => {
        if (typeof window === "undefined" || !("serviceWorker" in navigator)) {
            return;
        }

        // The worker caches static assets cache-first, which includes hashed
        // /_next/ chunks. In development that pins a module graph that no
        // longer exists on disk: deleting a component leaves the browser
        // loading a chunk that still imports it, and the page dies with
        // "module factory is not available" until site data is cleared by
        // hand. Any already-registered worker is torn down so a machine that
        // has run this build before recovers on its next load.
        if (process.env.NODE_ENV !== "production") {
            void navigator.serviceWorker.getRegistrations().then((registrations) => {
                for (const registration of registrations) {
                    void registration.unregister();
                }
            });
            void caches?.keys().then((keys) => {
                for (const key of keys) void caches.delete(key);
            });
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
