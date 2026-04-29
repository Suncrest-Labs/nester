"use client";

import { useEffect, useState } from "react";

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
        // Fallback: keep previous cache or use zeros
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

        fetchPrices().then((p) => {
            if (!cancelled) {
                setPrices(p);
                setLoading(false);
            }
        });

        // Refresh every minute while mounted
        const interval = setInterval(() => {
            fetchPrices().then((p) => {
                if (!cancelled) setPrices(p);
            });
        }, CACHE_TTL_MS);

        return () => {
            cancelled = true;
            clearInterval(interval);
        };
    }, []);

    return { prices, loading };
}
