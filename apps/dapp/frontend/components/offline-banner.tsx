"use client";

import { AnimatePresence, motion } from "framer-motion";
import { WifiOff } from "lucide-react";
import { formatDistanceToNow } from "date-fns";
import { useOfflineStatus } from "@/hooks/useOfflineStatus";

export function OfflineBanner() {
  const { isOffline, lastSynced } = useOfflineStatus();

  return (
    <AnimatePresence>
      {isOffline && (
        <motion.div
          initial={{ y: -48, opacity: 0 }}
          animate={{ y: 0, opacity: 1 }}
          exit={{ y: -48, opacity: 0 }}
          transition={{ duration: 0.2 }}
          role="alert"
          aria-live="assertive"
          className="fixed top-0 left-0 right-0 z-[200] flex items-center justify-center gap-2 bg-amber-500 px-4 py-2.5 text-sm font-medium text-white"
        >
          <WifiOff className="h-4 w-4 shrink-0" />
          <span>
            You&apos;re offline. Balances shown are from your last sync
            {lastSynced
              ? ` (${formatDistanceToNow(lastSynced, { addSuffix: false })} ago)`
              : ""}
            .
          </span>
        </motion.div>
      )}
    </AnimatePresence>
  );
}
