"use client";

import { useRef, useState } from "react";
import { Check, AlertTriangle } from "lucide-react";
import { cn } from "@/lib/utils";
import {
  getDeadlineBadgeInfo,
  formatFullDeadline,
  type DeadlineBadgeVariant,
} from "@/lib/savings/deadline";

const VARIANT_STYLES: Record<DeadlineBadgeVariant, string> = {
  green: "text-emerald-700 bg-emerald-50",
  amber: "text-amber-700 bg-amber-50",
  red: "text-red-700 bg-red-50",
  "due-today": "text-red-700 bg-red-50 font-bold",
  completed: "text-emerald-700 bg-emerald-50",
  overdue: "text-red-700 bg-red-50",
};

export interface DeadlineBadgeProps {
  deadline: string;
  status?: string;
  className?: string;
}

export function DeadlineBadge({ deadline, status, className }: DeadlineBadgeProps) {
  const [showTooltip, setShowTooltip] = useState(false);
  const tooltipId = useRef(
    `deadline-tooltip-${Math.random().toString(36).slice(2, 9)}`
  ).current;

  const info = getDeadlineBadgeInfo(deadline, status);
  const fullDate = formatFullDeadline(deadline);

  return (
    <div className="relative inline-flex">
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
          "inline-flex items-center gap-1 rounded-full px-2.5 py-1 text-[11px] font-medium",
          "focus-visible:ring-2 focus-visible:ring-black focus-visible:outline-none",
          VARIANT_STYLES[info.variant],
          className
        )}
      >
        {info.variant === "completed" && <Check className="h-3 w-3" aria-hidden="true" />}
        {info.variant === "overdue" && (
          <AlertTriangle className="h-3 w-3" aria-hidden="true" />
        )}
        {info.label}
      </span>

      {showTooltip && (
        <div
          id={tooltipId}
          role="tooltip"
          className="absolute bottom-full left-1/2 z-20 mb-2 w-max max-w-[220px] -translate-x-1/2 rounded-lg border border-black/8 bg-white px-3 py-2 text-xs font-medium text-black/70 shadow-lg shadow-black/10"
        >
          Goal deadline: {fullDate}
          <div className="absolute top-full left-1/2 -translate-x-1/2 border-4 border-transparent border-t-black/8" />
        </div>
      )}
    </div>
  );
}

export default DeadlineBadge;
