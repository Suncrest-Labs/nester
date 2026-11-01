"use client";

import { useId, useState } from "react";
import { cn } from "@/lib/utils";
import {
  getDeadlineBadgeInfo,
  formatFullDeadline,
  type DeadlineBadgeVariant,
} from "@/lib/savings/deadline";

const VARIANT_STYLES: Record<DeadlineBadgeVariant, string> = {
  green: "text-muted-foreground",
  amber: "text-amber-700 dark:text-amber-400",
  red: "text-red-600 dark:text-red-400",
  "due-today": "text-red-600 dark:text-red-400",
  completed: "text-foreground",
  overdue: "text-red-600 dark:text-red-400",
};

export interface DeadlineBadgeProps {
  deadline: string;
  status?: string;
  className?: string;
}

export function DeadlineBadge({ deadline, status, className }: DeadlineBadgeProps) {
  const [showTooltip, setShowTooltip] = useState(false);
  const tooltipId = `deadline-tooltip-${useId()}`;

  const info = getDeadlineBadgeInfo(deadline, status);
  const fullDate = formatFullDeadline(deadline);

  return (
    <span className="relative inline-flex">
      <span
        tabIndex={0}
        role="status"
        aria-label={info.ariaLabel}
        aria-describedby={tooltipId}
        onMouseEnter={() => setShowTooltip(true)}
        onMouseLeave={() => setShowTooltip(false)}
        onFocus={() => setShowTooltip(true)}
        onBlur={() => setShowTooltip(false)}
        className={cn(
          "inline-flex items-center rounded text-[13px] tabular-nums",
          "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-foreground",
          VARIANT_STYLES[info.variant],
          className
        )}
      >
        {info.label}
      </span>

      {showTooltip && (
        <span
          id={tooltipId}
          role="tooltip"
          className="absolute bottom-full left-1/2 z-20 mb-2 w-max max-w-[220px] -translate-x-1/2 rounded-lg border bg-card px-3 py-2 text-xs text-foreground"
        >
          Goal deadline: {fullDate}
        </span>
      )}
    </span>
  );
}

export default DeadlineBadge;
