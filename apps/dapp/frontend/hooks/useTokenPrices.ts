"use client";

import { useEffect, useState } from "react";
import { hasThirdPartyConsent } from "@/lib/consent";

export interface TokenPrices {
    XLM: number;
    USDC: number;
}

let cachedPrices: TokenPrices | null = null;
let cacheTimestamp = 0;
const CACHE_TTL_MS = 60_000; // 1 minute

async function fetchPrices(): Promise<TokenPrices> {
    const now = Date.now();
    if (cachedPrices && now - cacheTimestamp < CACHE_TTL_MS) {
        return cachedPrices;
    }

    if (!hasThirdPartyConsent()) {
        return cachedPrices ?? { XLM: 0, USDC: 1.0 };
    }

    try {
        const res = await fetch(
            "https://api.coingecko.com/api/v3/simple/price?ids=stellar&vs_currencies=usd",
            { cache: "no-store" }
        );
        if (!res.ok) throw new Error("price fetch failed");
        const data = await res.json() as { stellar?: { usd?: number } };
        const xlm = data.stellar?.usd ?? 0;
        cachedPrices = { XLM: xlm, USDC: 1.0 };
        cacheTimestamp = now;
        return cachedPrices;
    } catch {
        return cachedPrices ?? { XLM: 0, USDC: 1.0 };
    }
}

export function useTokenPrices() {
    const [prices, setPrices] = useState<TokenPrices>(
        cachedPrices ?? { XLM: 0, USDC: 1.0 }
    );
    const [loading, setLoading] = useState(!cachedPrices);

    useEffect(() => {
        let cancelled = false;

        const load = () => {
            fetchPrices().then((p) => {
                if (!cancelled) {
                    setPrices(p);
                    setLoading(false);
                }
            });
        };

        load();

        const interval = setInterval(load, CACHE_TTL_MS);

        const onStorage = (e: StorageEvent) => {
            if (e.key === "nester-consent") load();
        };
        window.addEventListener("storage", onStorage);

        return () => {
            cancelled = true;
            clearInterval(interval);
            window.removeEventListener("storage", onStorage);
        };
    }, []);

    return { prices, loading };
}
