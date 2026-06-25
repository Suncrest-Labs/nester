"use client";

import { format } from "date-fns";
import {
  Briefcase,
  GraduationCap,
  Heart,
  Home,
  Plane,
  Shield,
  Target,
  type LucideIcon,
} from "lucide-react";
import { cn } from "@/lib/utils";
import type { SavingsGoal } from "@/lib/api/savings-goals";
import { useSavingsGoals } from "@/hooks/useSavingsGoals";
import { useWallet } from "@/components/wallet-provider";

const CATEGORY_ICONS: Record<string, LucideIcon> = {
  emergency_fund: Shield,
  education: GraduationCap,
  housing: Home,
  travel: Plane,
  business: Briefcase,
  health: Heart,
  retirement: Target,
  other: Target,
};

function goalDisplayName(goal: SavingsGoal): string {
  if (goal.description?.trim()) return goal.description.trim();
  if (goal.category) {
    return goal.category.replace(/_/g, " ").replace(/\b\w/g, (c) => c.toUpperCase());
  }
  return "Savings Goal";
}

function toNumber(value: string | number): number {
  return typeof value === "number" ? value : parseFloat(value) || 0;
}

function GoalsSkeleton() {
  return (
    <div className="space-y-3" data-testid="savings-goals-skeleton">
      {[0, 1].map((i) => (
        <div key={i} className="animate-pulse rounded-2xl border border-black/8 bg-white p-5">
          <div className="mb-3 h-4 w-40 rounded bg-black/10" />
          <div className="mb-4 h-2 w-full rounded-full bg-black/10" />
          <div className="flex justify-between">
            <div className="h-3 w-24 rounded bg-black/10" />
            <div className="h-3 w-20 rounded bg-black/10" />
          </div>
        </div>
      ))}
    </div>
  );
}

function GoalCard({ goal }: { goal: SavingsGoal }) {
  const Icon = CATEGORY_ICONS[goal.category ?? "other"] ?? Target;
  const current = toNumber(goal.current_amount);
  const target = toNumber(goal.target_amount);
  const progress = Math.min(100, Math.max(0, goal.progress_pct ?? 0));
  const deadline = goal.deadline ? format(new Date(goal.deadline), "MMM d, yyyy") : "—";

  return (
    <div className="rounded-2xl border border-black/8 bg-white p-5" data-testid="savings-goal-card">
      <div className="mb-3 flex items-start justify-between gap-3">
        <div className="flex items-center gap-3 min-w-0">
          <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-black/5">
            <Icon className="h-4 w-4 text-black/50" aria-hidden="true" />
          </div>
          <div className="min-w-0">
            <p className="truncate text-sm font-semibold text-black">{goalDisplayName(goal)}</p>
            <p className="text-[11px] text-black/50 font-medium">
              Target {target.toLocaleString()} {goal.currency}
            </p>
          </div>
        </div>
        <div className="text-right shrink-0">
          <p className="font-mono text-sm text-black">{current.toLocaleString()}</p>
          <p className="text-[10px] text-black/50 uppercase font-bold tracking-wide">Saved</p>
        </div>
      </div>
      <div className="mb-2 h-2 overflow-hidden rounded-full bg-black/8">
        <div
          className="h-full rounded-full bg-black transition-all"
          style={{ width: `${progress}%` }}
          role="progressbar"
          aria-valuenow={progress}
          aria-valuemin={0}
          aria-valuemax={100}
        />
      </div>
      <div className="flex items-center justify-between text-[11px] text-black/50 font-medium">
        <span>{progress.toFixed(0)}% complete</span>
        <span>Due {deadline}</span>
      </div>
    </div>
  );
}

export function SavingsGoalsSection({ onCreateGoal }: { onCreateGoal?: () => void }) {
  const { isConnected } = useWallet();
  const { data: goals, isLoading, isError, refetch, isFetching } = useSavingsGoals();

  if (!isConnected) {
    return (
      <div
        className="mb-10 rounded-3xl border border-black/8 bg-white p-8 text-center"
        data-testid="savings-goals-connect-prompt"
      >
        <p className="text-sm font-semibold text-black">Connect your wallet to see your savings goals</p>
        <p className="mt-1 text-xs text-black/60 font-medium">
          Link your wallet to track progress toward your personal targets.
        </p>
      </div>
    );
  }

  return (
    <div className="mb-10" data-testid="savings-goals-section">
      <div className="mb-4 flex items-center justify-between gap-3">
        <div>
          <h2 className="text-sm font-semibold text-black">Your Savings Goals</h2>
          <p className="text-xs text-black/60 font-medium mt-0.5">Progress toward your targets</p>
        </div>
      </div>

      {isLoading ? (
        <GoalsSkeleton />
      ) : isError ? (
        <div
          className="rounded-2xl border border-red-100 bg-red-50 p-5 text-center"
          data-testid="savings-goals-error"
        >
          <p className="text-sm font-medium text-red-800">Failed to load savings goals</p>
          <button
            type="button"
            onClick={() => refetch()}
            disabled={isFetching}
            className="mt-3 rounded-lg bg-black px-4 py-2 text-xs font-semibold text-white disabled:opacity-50"
          >
            {isFetching ? "Retrying…" : "Retry"}
          </button>
        </div>
      ) : !goals?.length ? (
        <div
          className="rounded-3xl border border-black/8 bg-white p-10 text-center"
          data-testid="savings-goals-empty"
        >
          <div className="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-2xl bg-black/5">
            <Target className="h-6 w-6 text-black/40" aria-hidden="true" />
          </div>
          <p className="text-sm font-semibold text-black">No savings goals yet</p>
          <p className="mx-auto mt-1 max-w-xs text-xs text-black/60 font-medium">
            Set a target amount and deadline to start tracking your progress.
          </p>
          {onCreateGoal && (
            <button
              type="button"
              onClick={onCreateGoal}
              className="mt-5 rounded-xl bg-black px-6 py-2.5 text-xs font-semibold text-white transition-opacity hover:opacity-75"
            >
              Create a Goal
            </button>
          )}
        </div>
      ) : (
        <div className={cn("grid gap-3", goals.length > 1 && "sm:grid-cols-2")}>
          {goals.map((goal) => (
            <GoalCard key={goal.id} goal={goal} />
          ))}
        </div>
      )}
    </div>
  );
}
