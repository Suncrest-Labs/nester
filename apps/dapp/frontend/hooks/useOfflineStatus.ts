"use client";

import { useEffect, useState } from "react";

export function useOfflineStatus() {
  const [isOffline, setIsOffline] = useState(
    typeof navigator !== "undefined" ? !navigator.onLine : false
  );
  const [lastSynced, setLastSynced] = useState<Date | null>(
    typeof navigator !== "undefined" && navigator.onLine ? new Date() : null
  );

  useEffect(() => {
    const handleOffline = () => setIsOffline(true);
    const handleOnline = () => {
      setIsOffline(false);
      setLastSynced(new Date());
    };

    window.addEventListener("offline", handleOffline);
    window.addEventListener("online", handleOnline);

    return () => {
      window.removeEventListener("offline", handleOffline);
      window.removeEventListener("online", handleOnline);
    };
  }, []);

  return { isOffline, lastSynced };
}
