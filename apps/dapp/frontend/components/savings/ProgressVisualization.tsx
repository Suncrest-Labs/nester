"use client";

import { motion } from "framer-motion";
import { Target, Sparkles, TrendingUp, Wallet } from "lucide-react";
import { cn } from "@/lib/utils";
import { useReducedMotion } from "@/hooks/useReducedMotion";
import { MaturityTimeline } from "./MaturityTimeline";
import { ProjectionBand } from "./ProjectionBand";
import { MultiAssetComposition } from "./MultiAssetComposition";
import type { GoalProgressData } from "@/lib/types/progress";

interface ProgressVisualizationProps {
  progress: GoalProgressData;
  compact?: boolean;
}

function formatCurrency(value: number, currency: string): string {
  return `${value.toLocaleString(undefined, {
    minimumFractionDigits: 0,
    maximumFractionDigits: 2,
  })} ${currency}`;
}

function SegmentedProgressBar({
  current,
  target,
  principal,
  yield: yieldAmount,
  locked,
  flexible,
  compact,
}: {
  current: number;
  target: number;
  principal: number;
  yield: number;
  locked: number;
  flexible: number;
  compact?: boolean;
}) {
  const progress = target > 0 ? Math.min(100, (current / target) * 100) : 0;
  const lockedPct = target > 0 ? Math.min(100, (locked / target) * 100) : 0;
  const flexiblePct = target > 0 ? Math.min(100, (flexible / target) * 100) : 0;

  const showSegments = yieldAmount > 0 || locked > 0;

  return (
    <div>
      <div
        className={cn(
          "overflow-hidden rounded-full bg-black/8 dark:bg-white/8",
          compact ? "h-2" : "h-3"
        )}
        role="progressbar"
        aria-valuenow={Math.round(progress)}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-label="Goal progress"
      >
        <div className="relative h-full w-full">
          {showSegments ? (
            <>
              {flexiblePct > 0 && (
                <div
                  className="absolute left-0 top-0 h-full rounded-l-full bg-black/40 dark:bg-white/40 transition-all"
                  style={{ width: `${flexiblePct}%` }}
                  title={`Flexible: ${flexiblePct.toFixed(1)}%`}
                />
              )}
              {lockedPct > 0 && (
                <div
                  className="absolute left-0 top-0 h-full bg-amber-500 transition-all"
                  style={{
                    width: `${Math.min(100, flexiblePct + lockedPct)}%`,
                    left: `${flexiblePct}%`,
                    borderTopRightRadius: flexiblePct + lockedPct >= 100 ? 9999 : 0,
                    borderBottomRightRadius: flexiblePct + lockedPct >= 100 ? 9999 : 0,
                  }}
                  title={`Locked: ${lockedPct.toFixed(1)}%`}
                />
              )}
            </>
          ) : (
            <div
              className={cn(
                "h-full rounded-full transition-all",
                progress >= 100 ? "bg-emerald-500" : "bg-black dark:bg-white"
              )}
              style={{ width: `${progress}%` }}
            />
          )}
        </div>
      </div>

      {!compact && showSegments && (
        <div className="mt-2 flex items-center gap-4 text-[11px] font-medium text-black/60 dark:text-white/60">
          {flexiblePct > 0 && (
            <span className="flex items-center gap-1">
              <span className="h-2 w-2 rounded-full bg-black/40 dark:bg-white/40" aria-hidden="true" />
              Flexible {flexiblePct.toFixed(0)}%
            </span>
          )}
          {lockedPct > 0 && (
            <span className="flex items-center gap-1">
              <span className="h-2 w-2 rounded-full bg-amber-500" aria-hidden="true" />
              Locked {lockedPct.toFixed(0)}%
            </span>
          )}
        </div>
      )}
    </div>
  );
}

function CompositionBreakdown({
  principal,
  yield: yieldAmount,
  currency,
  compact,
}: {
  principal: number;
  yield: number;
  currency: string;
  compact?: boolean;
}) {
  const total = principal + yieldAmount;
  if (total <= 0 || yieldAmount <= 0) return null;

  return (
    <div className={cn("space-y-2", compact && "mt-2")}>
      <div className="flex items-center justify-between text-xs">
        <div className="flex items-center gap-1.5">
          <Wallet className="h-3 w-3 text-black/40 dark:text-white/40" aria-hidden="true" />
          <span className="text-black/60 dark:text-white/60">Principal</span>
        </div>
        <span className="font-mono font-medium text-black dark:text-white">
          {formatCurrency(principal, currency)}
        </span>
      </div>
      <div className="flex items-center justify-between text-xs">
        <div className="flex items-center gap-1.5">
          <TrendingUp className="h-3 w-3 text-emerald-500" aria-hidden="true" />
          <span className="text-emerald-600 dark:text-emerald-400 font-medium">Yield earned</span>
        </div>
        <span className="font-mono font-medium text-emerald-600 dark:text-emerald-400">
          +{formatCurrency(yieldAmount, currency)}
        </span>
      </div>
    </div>
  );
}

function StatesBanner({
  progress,
  status,
  compact,
}: {
  progress: GoalProgressData;
  status?: string;
  compact?: boolean;
}) {
  const isCompleted = status === "completed" || progress.current_amount >= progress.target_amount;
  const isNew = progress.current_amount <= 0;
  const isAtRisk = progress.projection && !progress.projection.on_track;

  if (isCompleted) {
    return (
      <div className={cn(
        "flex items-center gap-2 rounded-xl border border-emerald-200 bg-emerald-50/80 dark:border-emerald-800/30 dark:bg-emerald-900/20 px-3 py-2",
        compact && "px-2 py-1.5"
      )}>
        <Sparkles className={cn("shrink-0 text-emerald-600", compact ? "h-3.5 w-3.5" : "h-4 w-4")} aria-hidden="true" />
        <p className={cn("font-semibold text-emerald-700 dark:text-emerald-300", compact ? "text-[11px]" : "text-xs")}>
          Goal completed!
        </p>
      </div>
    );
  }

  if (isNew) {
    return (
      <div className={cn(
        "flex items-center gap-2 rounded-xl border border-black/8 dark:border-white/8 bg-black/[0.02] dark:bg-white/[0.02] px-3 py-2",
        compact && "px-2 py-1.5"
      )}>
        <Target className={cn("shrink-0 text-black/40 dark:text-white/40", compact ? "h-3.5 w-3.5" : "h-4 w-4")} aria-hidden="true" />
        <p className={cn("text-black/60 dark:text-white/60 font-medium", compact ? "text-[11px]" : "text-xs")}>
          Start saving to track your progress
        </p>
      </div>
    );
  }

  if (isAtRisk && !compact) {
    return (
      <div className="flex items-center gap-2 rounded-xl border border-amber-200 bg-amber-50/50 dark:border-amber-800/30 dark:bg-amber-900/10 px-3 py-2">
        <Target className="h-4 w-4 shrink-0 text-amber-600" aria-hidden="true" />
        <p className="text-xs text-amber-800 dark:text-amber-300 font-medium">
          Goal at risk — consider increasing your contributions
        </p>
      </div>
    );
  }

  return null;
}

export function ProgressVisualization({
  progress,
  compact = false,
}: ProgressVisualizationProps) {
  const reduced = useReducedMotion();
  const pct = progress.target_amount > 0
    ? Math.min(100, (progress.current_amount / progress.target_amount) * 100)
    : 0;

  return (
    <motion.div
      initial={reduced ? undefined : { opacity: 0, y: 8 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.35 }}
      className="space-y-4"
      data-testid="progress-visualization"
    >
      <StatesBanner progress={progress} status={progress.status} compact={compact} />

      <SegmentedProgressBar
        current={progress.current_amount}
        target={progress.target_amount}
        principal={progress.principal_amount}
        yield={progress.yield_amount}
        locked={progress.locked_positions.reduce((s, p) => s + p.amount, 0)}
        flexible={progress.flexible_amount}
        compact={compact}
      />

      {!compact && (
        <>
          <div className="flex items-center justify-between text-xs">
            <span className="font-medium text-black/60 dark:text-white/60">
              {pct.toFixed(0)}% complete
            </span>
            <span className="font-medium text-black/60 dark:text-white/60">
              {formatCurrency(progress.current_amount, progress.currency)} of{" "}
              {formatCurrency(progress.target_amount, progress.currency)}
            </span>
          </div>

          <CompositionBreakdown
            principal={progress.principal_amount}
            yield={progress.yield_amount}
            currency={progress.currency}
          />

          {progress.locked_positions.length > 0 && (
            <MaturityTimeline positions={progress.locked_positions} />
          )}

          {progress.projection && (
            <ProjectionBand projection={progress.projection} />
          )}

          {progress.asset_composition && progress.asset_composition.length > 0 && (
            <MultiAssetComposition composition={progress.asset_composition} />
          )}
        </>
      )}
    </motion.div>
  );
}
