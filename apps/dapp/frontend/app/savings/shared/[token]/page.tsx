"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { useParams } from "next/navigation";
import { format } from "date-fns";
import { savingsGoals, type SharedGoalView } from "@/lib/api/savings-goals";
import { BigNumber, PANEL, SKELETON } from "@/components/ui/page-kit";
import { GoalProgressBar, formatGoalAmount } from "@/components/savings/GoalProgressBar";

function toNumber(value: string | number | undefined): number {
  if (value === undefined) return 0;
  return typeof value === "number" ? value : parseFloat(value) || 0;
}

function Frame({ children }: { children: React.ReactNode }) {
  return (
    <main className="flex min-h-screen items-center justify-center bg-background p-6">
      <div className="w-full max-w-md">{children}</div>
    </main>
  );
}

export default function SharedGoalPage() {
  const { token } = useParams<{ token: string }>();
  const [goal, setGoal] = useState<SharedGoalView | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!token) return;
    savingsGoals
      .getShared(token)
      .then(setGoal)
      .catch(() =>
        setError("This goal link doesn't work. It may have been turned off by its owner.")
      )
      .finally(() => setLoading(false));
  }, [token]);

  if (loading) {
    return (
      <Frame>
        <div aria-busy="true" className={`h-56 ${SKELETON}`} />
      </Frame>
    );
  }

  if (error || !goal) {
    return (
      <Frame>
        <div className={PANEL}>
          <p role="alert" className="text-sm text-foreground">
            {error || "This goal couldn't be found."}
          </p>
          <p className="mt-2 text-sm text-muted-foreground">
            Ask the person who shared it for a new link.
          </p>
        </div>
      </Frame>
    );
  }

  const current = toNumber(goal.current_amount);
  const target = toNumber(goal.target_amount);
  const progress = Math.min(100, Math.max(0, goal.progress_pct ?? 0));
  const deadline = goal.deadline ? format(new Date(goal.deadline), "MMMM d, yyyy") : "No date set";
  const displayName =
    goal.name || (goal.category ? goal.category.replace(/_/g, " ") : "Savings goal");
  const isCompleted = goal.status === "completed";
  const status = isCompleted
    ? "Reached"
    : !goal.status || goal.status === "active"
      ? "In progress"
      : goal.status.charAt(0).toUpperCase() + goal.status.slice(1);

  return (
    <Frame>
      <p className="mb-4 text-[13px] font-medium text-muted-foreground">Shared goal</p>
      <div className={PANEL}>
        <h1 className="text-sm font-medium capitalize text-foreground">
          {goal.emoji && (
            <span className="mr-2" aria-hidden="true">
              {goal.emoji}
            </span>
          )}
          {displayName}
        </h1>

        <BigNumber className="mt-4">{formatGoalAmount(current)}</BigNumber>
        <p className="mt-1 text-sm text-muted-foreground">
          of <span className="tabular-nums">{formatGoalAmount(target, goal.currency)}</span>
        </p>

        <GoalProgressBar percent={progress} className="mt-5" />

        <dl className="mt-5 space-y-2 text-sm">
          <div className="flex justify-between">
            <dt className="text-muted-foreground">Progress</dt>
            <dd className="tabular-nums text-foreground">{progress.toFixed(0)}%</dd>
          </div>
          <div className="flex justify-between">
            <dt className="text-muted-foreground">Target date</dt>
            <dd className="text-foreground">{deadline}</dd>
          </div>
          <div className="flex justify-between">
            <dt className="text-muted-foreground">Status</dt>
            <dd className="text-foreground">{status}</dd>
          </div>
        </dl>

        {isCompleted && <p className="mt-5 text-sm text-foreground">This goal has been reached.</p>}
      </div>

      <p className="mt-6 text-center text-[13px] text-muted-foreground">
        Tracked with{" "}
        <Link href="/" className="underline underline-offset-4 hover:text-foreground">
          Nester
        </Link>
      </p>
    </Frame>
  );
}
