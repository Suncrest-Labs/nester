"use client";

import { useEffect, useState } from "react";

/**
 * Brief loading phase for dashboard pages until async portfolio/API hooks exist.
 * Disable with `enabled={false}` when a real loading flag is available.
 */
export function useShellLoading(enabled: boolean, minMs = 360) {
    const [loading, setLoading] = useState(enabled);

    useEffect(() => {
        if (!enabled) {
            setLoading(false);
            return;
        }
        setLoading(true);
        const t = window.setTimeout(() => setLoading(false), minMs);
        return () => window.clearTimeout(t);
    }, [enabled, minMs]);

    return loading;
}
