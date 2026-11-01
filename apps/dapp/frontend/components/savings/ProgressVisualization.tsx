"use client";

import { cn } from "@/lib/utils";
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
  yield: yieldAmount,
  locked,
  flexible,
  compact,
}: {
  current: number;
  target: number;
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
        className="h-1 overflow-hidden rounded-full bg-foreground/10"
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
                  className="absolute left-0 top-0 h-full bg-foreground/40 transition-[width]"
                  style={{ width: `${flexiblePct}%` }}
                  title={`Flexible: ${flexiblePct.toFixed(1)}%`}
                />
              )}
              {lockedPct > 0 && (
                <div
                  className="absolute top-0 h-full bg-foreground transition-[width]"
                  style={{
                    left: `${flexiblePct}%`,
                    width: `${Math.min(lockedPct, 100 - flexiblePct)}%`,
                  }}
                  title={`Locked: ${lockedPct.toFixed(1)}%`}
                />
              )}
            </>
          ) : (
            <div
              className="h-full rounded-full bg-foreground transition-[width]"
              style={{ width: `${progress}%` }}
            />
          )}
        </div>
      </div>

      {!compact && showSegments && (
        <div className="mt-2 flex items-center gap-4 text-[13px] text-muted-foreground">
          {flexiblePct > 0 && (
            <span className="flex items-center gap-1.5">
              <span className="h-2 w-2 rounded-full bg-foreground/40" aria-hidden="true" />
              Flexible {flexiblePct.toFixed(0)}%
            </span>
          )}
          {lockedPct > 0 && (
            <span className="flex items-center gap-1.5">
              <span className="h-2 w-2 rounded-full bg-foreground" aria-hidden="true" />
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
}: {
  principal: number;
  yield: number;
  currency: string;
}) {
  const total = principal + yieldAmount;
  if (total <= 0 || yieldAmount <= 0) return null;

  return (
    <dl className="space-y-2 text-sm">
      <div className="flex items-center justify-between">
        <dt className="text-muted-foreground">Principal</dt>
        <dd className="tabular-nums text-foreground">{formatCurrency(principal, currency)}</dd>
      </div>
      <div className="flex items-center justify-between">
        <dt className="text-muted-foreground">Yield earned</dt>
        <dd className="tabular-nums text-foreground">+{formatCurrency(yieldAmount, currency)}</dd>
      </div>
    </dl>
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
  const text = compact ? "text-[13px]" : "text-sm";

  if (isCompleted) {
    return <p className={cn(text, "text-foreground")}>Goal completed!</p>;
  }

  if (isNew) {
    return <p className={cn(text, "text-muted-foreground")}>Start saving to track your progress</p>;
  }

  if (isAtRisk && !compact) {
    return (
      <p className="text-sm text-amber-700 dark:text-amber-400">
        This goal is behind. Saving more each month would bring it back on track.
      </p>
    );
  }

  return null;
}

export function ProgressVisualization({
  progress,
  compact = false,
}: ProgressVisualizationProps) {
  const pct = progress.target_amount > 0
    ? Math.min(100, (progress.current_amount / progress.target_amount) * 100)
    : 0;

  return (
    <div className="space-y-4" data-testid="progress-visualization">
      <StatesBanner progress={progress} status={progress.status} compact={compact} />

      <SegmentedProgressBar
        current={progress.current_amount}
        target={progress.target_amount}
        yield={progress.yield_amount}
        locked={progress.locked_positions.reduce((s, p) => s + p.amount, 0)}
        flexible={progress.flexible_amount}
        compact={compact}
      />

      {!compact && (
        <>
          <div className="flex items-center justify-between text-[13px] text-muted-foreground">
            <span className="tabular-nums">{pct.toFixed(0)}% complete</span>
            <span className="tabular-nums">
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
            <div className="pt-6">
              <MaturityTimeline positions={progress.locked_positions} />
            </div>
          )}

          {progress.projection && (
            <div className="pt-6">
              <ProjectionBand projection={progress.projection} />
            </div>
          )}

          {progress.asset_composition && progress.asset_composition.length > 0 && (
            <div className="pt-6">
              <MultiAssetComposition composition={progress.asset_composition} />
            </div>
          )}
        </>
      )}
    </div>
  );
}
