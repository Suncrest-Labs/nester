"use client";

import { formatDistanceToNowStrict, isBefore } from "date-fns";
import { motion } from "framer-motion";
import { Lock, Unlock, TrendingUp } from "lucide-react";
import { cn } from "@/lib/utils";
import { useReducedMotion } from "@/hooks/useReducedMotion";
import type { LockedPosition } from "@/lib/types/progress";

function LockItem({ position, index }: { position: LockedPosition; index: number }) {
  const reduced = useReducedMotion();
  const maturityDate = new Date(position.matures_at);
  const isMatured = isBefore(maturityDate, new Date());
  const distance = formatDistanceToNowStrict(maturityDate, { addSuffix: true });

  return (
    <motion.div
      initial={reduced ? undefined : { opacity: 0, x: -8 }}
      animate={{ opacity: 1, x: 0 }}
      transition={{ duration: 0.3, delay: index * 0.05 }}
      className={cn(
        "flex items-center gap-3 rounded-xl border p-3 transition-colors",
        isMatured
          ? "border-emerald-200 bg-emerald-50/50 dark:border-emerald-800/30 dark:bg-emerald-900/10"
          : "border-black/8 dark:border-white/8 bg-black/[0.02] dark:bg-white/[0.02]"
      )}
    >
      <div
        className={cn(
          "flex h-8 w-8 shrink-0 items-center justify-center rounded-lg",
          isMatured ? "bg-emerald-100 dark:bg-emerald-900/30" : "bg-black/5 dark:bg-white/5"
        )}
      >
        {isMatured ? (
          <Unlock className="h-4 w-4 text-emerald-600 dark:text-emerald-400" aria-hidden="true" />
        ) : (
          <Lock className="h-4 w-4 text-black/50 dark:text-white/50" aria-hidden="true" />
        )}
      </div>
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <p className="text-sm font-semibold text-black dark:text-white">
            {position.amount.toLocaleString()} {position.currency}
          </p>
          <span className="inline-flex items-center gap-0.5 rounded-full bg-amber-100 px-1.5 py-0.5 text-[10px] font-bold text-amber-700">
            <TrendingUp className="h-2.5 w-2.5" aria-hidden="true" />
            +{position.boost_percent}%
          </span>
        </div>
        <p className={cn("text-[11px] font-medium", isMatured ? "text-emerald-600 dark:text-emerald-400" : "text-black/50 dark:text-white/50")}>
          {isMatured ? `Matured ${distance}` : `Matures ${distance}`}
        </p>
      </div>
      {position.yield_earned > 0 && (
        <span className="shrink-0 text-xs font-mono text-emerald-600 dark:text-emerald-400 font-medium">
          +{position.yield_earned.toLocaleString()}
        </span>
      )}
    </motion.div>
  );
}

export function MaturityTimeline({ positions }: { positions: LockedPosition[] }) {
  if (!positions.length) return null;

  const sorted = [...positions].sort(
    (a, b) => new Date(a.matures_at).getTime() - new Date(b.matures_at).getTime()
  );

  return (
    <div className="space-y-2" data-testid="maturity-timeline">
      <h3 className="text-xs font-semibold text-black/60 dark:text-white/60 uppercase tracking-wider mb-3">
        Locked Positions
      </h3>
      <div className="relative space-y-2">
        {sorted.map((pos, i) => (
          <LockItem key={pos.id} position={pos} index={i} />
        ))}
      </div>
    </div>
  );
}
