import { cn } from "@/lib/utils";

export function GoalProgressBar({
  percent,
  label = "Goal progress",
  className,
}: {
  percent: number;
  label?: string;
  className?: string;
}) {
  const value = Math.min(100, Math.max(0, percent));
  return (
    <div
      className={cn("h-1 overflow-hidden rounded-full bg-foreground/10", className)}
      role="progressbar"
      aria-label={label}
      aria-valuenow={Math.round(value)}
      aria-valuemin={0}
      aria-valuemax={100}
    >
      <div className="h-full rounded-full bg-foreground transition-[width]" style={{ width: `${value}%` }} />
    </div>
  );
}

export function formatGoalAmount(value: number, currency?: string): string {
  const n = value.toLocaleString(undefined, { minimumFractionDigits: 0, maximumFractionDigits: 2 });
  return currency ? `${n} ${currency}` : n;
}
