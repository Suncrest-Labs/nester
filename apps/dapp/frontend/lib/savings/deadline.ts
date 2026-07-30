/**
 * Helpers for computing and presenting savings-goal deadline urgency.
 */

export type DeadlineBadgeVariant =
  | "green"
  | "amber"
  | "red"
  | "due-today"
  | "completed"
  | "overdue";

export interface DeadlineBadgeInfo {
  variant: DeadlineBadgeVariant;
  label: string;
  ariaLabel: string;
}

/**
 * Returns the number of whole days remaining until `deadline`, measured
 * from "now". Never returns a negative number — once the deadline has
 * passed this returns 0. Use `isPastDeadline` if you need to distinguish
 * "due today" from "already overdue".
 */
export function daysRemaining(deadline: string, now: Date = new Date()): number {
  const end = new Date(deadline);
  const diffMs = end.getTime() - now.getTime();
  return Math.max(0, Math.ceil(diffMs / (1000 * 60 * 60 * 24)));
}

/**
 * Returns true if `deadline` is strictly in the past relative to `now`.
 */
export function isPastDeadline(deadline: string, now: Date = new Date()): boolean {
  const end = new Date(deadline);
  return end.getTime() < now.getTime();
}

/**
 * Resolves the badge variant, display text, and aria-label for a savings
 * goal, given its deadline and completion status.
 */
export function getDeadlineBadgeInfo(
  deadline: string,
  status: string | undefined,
  now: Date = new Date()
): DeadlineBadgeInfo {
  const isCompleted = status === "completed";
  const past = isPastDeadline(deadline, now);

  if (isCompleted) {
    return {
      variant: "completed",
      label: "Goal Achieved",
      ariaLabel: "Goal achieved",
    };
  }

  if (past) {
    return {
      variant: "overdue",
      label: "Overdue",
      ariaLabel: "Goal deadline has passed and the goal is not yet complete",
    };
  }

  const days = daysRemaining(deadline, now);

  if (days === 0) {
    return {
      variant: "due-today",
      label: "Due today",
      ariaLabel: "Goal deadline is today",
    };
  }

  if (days <= 6) {
    return {
      variant: "red",
      label: `${days} day${days === 1 ? "" : "s"} left`,
      ariaLabel: `${days} day${days === 1 ? "" : "s"} remaining until goal deadline`,
    };
  }

  if (days <= 30) {
    return {
      variant: "amber",
      label: `${days} days left`,
      ariaLabel: `${days} days remaining until goal deadline`,
    };
  }

  return {
    variant: "green",
    label: `${days} days left`,
    ariaLabel: `${days} days remaining until goal deadline`,
  };
}

export function formatFullDeadline(deadline: string): string {
  const date = new Date(deadline);
  return date.toLocaleDateString("en-US", {
    month: "long",
    day: "numeric",
    year: "numeric",
  });
}
