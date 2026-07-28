"use client";

import { useEffect, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import Link from "next/link";
import { motion } from "framer-motion";
import { useQueryClient } from "@tanstack/react-query";
import {
  ArrowLeft,
  ChevronRight,
  Pause,
  Play,
  Pencil,
  Archive,
  Plus,
  RefreshCcw,
  Target,
  Calendar,
  Wallet,
  ArrowDownLeft,
} from "lucide-react";
import { AppShell } from "@/components/app-shell";
import { useWallet } from "@/components/wallet-provider";
import { cn } from "@/lib/utils";
import { useToast } from "@/components/ui/toast/toast-provider";
import {
  savingsGoals,
  type SavingsGoal,
  type SavingsGoalContribution,
} from "@/lib/api/savings-goals";
import { useSavingsGoal, useSavingsGoalContributions } from "@/hooks/useSavingsGoal";
import { DeadlineBadge } from "@/components/savings/DeadlineBadge";
import { EditGoalModal } from "@/components/savings/EditGoalModal";
import { formatFullDeadline } from "@/lib/savings/deadline";
import { ProgressVisualization } from "@/components/savings/ProgressVisualization";
import type { GoalProgressData } from "@/lib/types/progress";

function toNumber(value: string | number): number {
  return typeof value === "number" ? value : parseFloat(value) || 0;
}

function buildProgressData(goal: SavingsGoal): GoalProgressData {
  const current = toNumber(goal.current_amount);
  const target = toNumber(goal.target_amount);
  const principal = goal.principal_amount != null ? toNumber(goal.principal_amount) : current;
  const yieldAmt = goal.yield_amount != null ? toNumber(goal.yield_amount) : 0;
  const locked = goal.locked_positions?.map((p) => ({
    id: p.id,
    amount: toNumber(p.amount),
    currency: goal.currency,
    locked_at: p.locked_at,
    matures_at: p.matures_at,
    boost_percent: p.boost_percent,
    yield_earned: toNumber(p.yield_earned),
  })) ?? [];
  const flexible = goal.flexible_amount != null ? toNumber(goal.flexible_amount) : Math.max(0, current - locked.reduce((s, p) => s + p.amount, 0));

  return {
    current_amount: current,
    target_amount: target,
    currency: goal.currency,
    principal_amount: principal,
    yield_amount: yieldAmt,
    locked_positions: locked,
    flexible_amount: flexible,
    projection: goal.projection ? {
      vault_id: goal.vault_id,
      currency: goal.currency,
      current_apy: 0,
      timeline: goal.projection.timeline,
      success_probability: goal.projection.success_probability,
      on_track: goal.projection.on_track,
      monthly_gap: goal.projection.monthly_gap,
    } : undefined,
    asset_composition: goal.asset_composition,
    deadline: goal.deadline,
    status: goal.status,
  };
}

function goalDisplayName(goal: SavingsGoal): string {
  if (goal.description?.trim()) return goal.description.trim();
  if (goal.category) {
    return goal.category.replace(/_/g, " ").replace(/\b\w/g, (c) => c.toUpperCase());
  }
  return "Savings Goal";
}

function formatAmount(value: string | number, currency: string): string {
  return `${toNumber(value).toLocaleString(undefined, {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })} ${currency}`;
}

function categoryLabel(category?: string): string {
  if (!category) return "Other";
  return category.replace(/_/g, " ").replace(/\b\w/g, (c) => c.toUpperCase());
}

// ── Auto-compound toggle ────────────────────────────────────────────────────

function AutoCompoundToggle({
  goal,
  disabled,
  onToggle,
}: {
  goal: SavingsGoal;
  disabled?: boolean;
  onToggle: (next: boolean) => void;
}) {
  const enabled = !!goal.auto_compound;
  return (
    <button
      type="button"
      role="switch"
      aria-checked={enabled}
      aria-label="Toggle auto-compound"
      disabled={disabled}
      onClick={() => onToggle(!enabled)}
      className={cn(
        "relative inline-flex h-6 w-11 shrink-0 items-center rounded-full transition-colors focus-visible:ring-2 focus-visible:ring-black disabled:opacity-50",
        enabled ? "bg-black dark:bg-white" : "bg-black/15 dark:bg-white/15"
      )}
    >
      <span
        className={cn(
          "inline-block h-5 w-5 transform rounded-full bg-white dark:bg-[#100F0F] shadow transition-transform",
          enabled ? "translate-x-5" : "translate-x-0.5"
        )}
      />
    </button>
  );
}

// ── Contribution history ────────────────────────────────────────────────────

function ContributionRow({ contribution }: { contribution: SavingsGoalContribution }) {
  const date = new Date(contribution.created_at);
  const dateLabel = Number.isNaN(date.getTime())
    ? "—"
    : date.toLocaleDateString("en-US", { month: "short", day: "numeric", year: "numeric" });

  return (
    <div className="flex items-center justify-between gap-3 px-5 py-3.5">
      <div className="flex items-center gap-3 min-w-0">
        <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-emerald-50">
          <ArrowDownLeft className="h-4 w-4 text-emerald-600" aria-hidden="true" />
        </div>
        <div className="min-w-0">
          <p className="truncate text-sm font-medium text-black dark:text-white capitalize">
            {contribution.source?.replace(/_/g, " ") || "Contribution"}
          </p>
          <p className="text-[11px] text-black/50 dark:text-white/50 font-medium">{dateLabel}</p>
        </div>
      </div>
      <span className="shrink-0 font-mono text-sm font-medium text-emerald-600">
        +{formatAmount(contribution.amount, contribution.currency)}
      </span>
    </div>
  );
}

function ContributionHistory({ goalId }: { goalId: string }) {
  const { data, isLoading, isError, refetch, isFetching } = useSavingsGoalContributions(goalId);

  return (
    <div className="rounded-2xl border border-black/8 dark:border-white/8 bg-white dark:bg-[#100F0F]">
      <div className="border-b border-black/8 dark:border-white/8 px-5 py-4">
        <h2 className="text-sm font-semibold text-black dark:text-white">Contribution History</h2>
        <p className="mt-0.5 text-xs text-black/60 dark:text-white/60 font-medium">
          Deposits and yield applied toward this goal
        </p>
      </div>

      {isLoading ? (
        <div className="divide-y divide-black/5" data-testid="contributions-skeleton">
          {[0, 1, 2].map((i) => (
            <div key={i} className="flex items-center justify-between px-5 py-3.5">
              <div className="flex items-center gap-3">
                <div className="h-8 w-8 animate-pulse rounded-lg bg-black/10 dark:bg-white/10" />
                <div className="space-y-1.5">
                  <div className="h-3 w-28 animate-pulse rounded bg-black/10 dark:bg-white/10" />
                  <div className="h-2.5 w-20 animate-pulse rounded bg-black/10 dark:bg-white/10" />
                </div>
              </div>
              <div className="h-3 w-16 animate-pulse rounded bg-black/10 dark:bg-white/10" />
            </div>
          ))}
        </div>
      ) : isError ? (
        <div className="px-5 py-8 text-center" data-testid="contributions-error">
          <p className="text-sm font-medium text-black/70 dark:text-white/70">Couldn&apos;t load contributions</p>
          <button
            type="button"
            onClick={() => refetch()}
            disabled={isFetching}
            className="mt-3 rounded-lg bg-black dark:bg-blue-600 px-4 py-2 text-xs font-semibold text-white disabled:opacity-50"
          >
            {isFetching ? "Retrying…" : "Retry"}
          </button>
        </div>
      ) : !data?.length ? (
        <div className="px-5 py-10 text-center" data-testid="contributions-empty">
          <div className="mx-auto mb-3 flex h-10 w-10 items-center justify-center rounded-xl bg-black/5 dark:bg-white/5">
            <Wallet className="h-5 w-5 text-black/40 dark:text-white/40" aria-hidden="true" />
          </div>
          <p className="text-sm font-semibold text-black dark:text-white">No contributions yet</p>
          <p className="mx-auto mt-1 max-w-xs text-xs text-black/60 dark:text-white/60 font-medium">
            Make your first deposit to start building toward this goal.
          </p>
        </div>
      ) : (
        <div className="divide-y divide-black/5">
          {data.map((c) => (
            <ContributionRow key={c.id} contribution={c} />
          ))}
        </div>
      )}
    </div>
  );
}

// ── Page ────────────────────────────────────────────────────────────────────

export default function SavingsGoalDetailPage() {
  const { isConnected } = useWallet();
  const router = useRouter();
  const params = useParams();
  const queryClient = useQueryClient();
  const toast = useToast();
  const id = params?.id?.toString() ?? "";

  const { data: goal, isLoading, isError } = useSavingsGoal(id);
  const [editOpen, setEditOpen] = useState(false);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!isConnected) router.push("/");
  }, [isConnected, router]);

  if (!isConnected) return null;

  async function refresh() {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ["savings-goal", id] }),
      queryClient.invalidateQueries({ queryKey: ["savings-goals"] }),
    ]);
  }

  async function runAction(label: string, fn: () => Promise<unknown>) {
    setBusy(true);
    try {
      await fn();
      await refresh();
      toast.success(`Goal ${label}.`);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : `Failed to ${label} goal.`);
    } finally {
      setBusy(false);
    }
  }

  async function handleArchive() {
    setBusy(true);
    try {
      await savingsGoals.archive(id);
      await queryClient.invalidateQueries({ queryKey: ["savings-goals"] });
      toast.success("Goal archived.");
      router.push("/savings");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to archive goal.");
      setBusy(false);
    }
  }

  async function handleAutoCompound(next: boolean) {
    await runAction(next ? "auto-compound enabled" : "auto-compound disabled", () =>
      savingsGoals.update(id, { auto_compound: next })
    );
  }

  return (
    <AppShell>
      {/* Breadcrumb */}
      <motion.nav
        initial={{ opacity: 0, x: -8 }}
        animate={{ opacity: 1, x: 0 }}
        transition={{ duration: 0.3 }}
        className="mb-7 flex items-center gap-1.5 text-xs text-black/50 dark:text-white/50 font-medium"
        aria-label="Breadcrumb"
      >
        <Link
          href="/savings"
          className="inline-flex items-center gap-1.5 hover:text-black dark:hover:text-white transition-colors focus-visible:ring-2 focus-visible:ring-black"
        >
          <ArrowLeft className="h-3.5 w-3.5" aria-hidden="true" />
          Savings
        </Link>
        <ChevronRight className="h-3.5 w-3.5 text-black/30 dark:text-white/30" aria-hidden="true" />
        <span className="text-black/70 dark:text-white/70 truncate max-w-[200px]">
          {goal ? goalDisplayName(goal) : "Goal"}
        </span>
      </motion.nav>

      {isLoading ? (
        <div className="p-12 text-center text-sm text-black/50 dark:text-white/50">Loading goal…</div>
      ) : isError || !goal ? (
        <div className="rounded-3xl border border-black/8 dark:border-white/8 bg-white dark:bg-[#100F0F] p-12 text-center">
          <div className="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-2xl bg-black/5 dark:bg-white/5">
            <Target className="h-6 w-6 text-black/40 dark:text-white/40" aria-hidden="true" />
          </div>
          <p className="text-sm font-semibold text-black dark:text-white">Goal not found</p>
          <p className="mx-auto mt-1 max-w-xs text-xs text-black/60 dark:text-white/60 font-medium">
            This savings goal may have been removed or is unavailable.
          </p>
          <Link
            href="/savings"
            className="mt-5 inline-block rounded-xl bg-black dark:bg-blue-600 px-6 py-2.5 text-xs font-semibold text-white transition-opacity hover:opacity-75"
          >
            Back to Savings
          </Link>
        </div>
      ) : (
        (() => {
          const current = toNumber(goal.current_amount);
          const target = toNumber(goal.target_amount);
          const isPaused = goal.status === "paused";
          const isCompleted = goal.status === "completed";

          return (
            <>
              {/* Header */}
              <motion.div
                initial={{ opacity: 0, y: 12 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ duration: 0.35, delay: 0.05 }}
                className="mb-8"
              >
                <div className="flex items-start justify-between gap-4 flex-wrap">
                  <div className="min-w-0">
                    <div className="mb-2 flex items-center gap-2 flex-wrap">
                      <span className="rounded-full bg-black/5 dark:bg-white/5 px-2.5 py-0.5 text-[11px] font-medium text-black/60 dark:text-white/60">
                        {categoryLabel(goal.category)}
                      </span>
                      {isPaused && (
                        <span className="rounded-full bg-amber-100 px-2.5 py-0.5 text-[11px] font-bold uppercase text-amber-700">
                          Paused
                        </span>
                      )}
                      {isCompleted && (
                        <span className="rounded-full bg-emerald-100 px-2.5 py-0.5 text-[11px] font-bold uppercase text-emerald-700">
                          Completed
                        </span>
                      )}
                      <DeadlineBadge deadline={goal.deadline} status={goal.status} />
                    </div>
                    <h1 className="text-2xl text-black dark:text-white sm:text-3xl font-semibold truncate">
                      {goalDisplayName(goal)}
                    </h1>
                    <p className="mt-2 text-sm text-black/60 dark:text-white/60 font-medium">
                      Target {formatAmount(goal.target_amount, goal.currency)} by{" "}
                      {formatFullDeadline(goal.deadline)}
                    </p>
                  </div>

                  {/* Action buttons */}
                  <div className="flex items-center gap-2 flex-wrap shrink-0">
                    <Link
                      href="/savings#vault-grid"
                      className="flex items-center gap-1.5 rounded-xl bg-black dark:bg-blue-600 px-4 py-2.5 text-xs font-semibold text-white transition-opacity hover:opacity-75 focus-visible:ring-2 focus-visible:ring-black"
                    >
                      <Plus className="h-3.5 w-3.5" aria-hidden="true" />
                      Deposit
                    </Link>
                    {!isCompleted && (
                      <button
                        type="button"
                        disabled={busy}
                        onClick={() =>
                          isPaused
                            ? runAction("resumed", () => savingsGoals.resume(id))
                            : runAction("paused", () => savingsGoals.pause(id))
                        }
                        className="flex items-center gap-1.5 rounded-xl border border-black/10 dark:border-white/10 px-4 py-2.5 text-xs font-semibold text-black/70 dark:text-white/70 transition-colors hover:border-black/20 dark:hover:border-white/20 hover:text-black dark:hover:text-white disabled:opacity-50 focus-visible:ring-2 focus-visible:ring-black"
                      >
                        {isPaused ? (
                          <Play className="h-3.5 w-3.5" aria-hidden="true" />
                        ) : (
                          <Pause className="h-3.5 w-3.5" aria-hidden="true" />
                        )}
                        {isPaused ? "Resume" : "Pause"}
                      </button>
                    )}
                    <button
                      type="button"
                      disabled={busy}
                      onClick={() => setEditOpen(true)}
                      className="flex items-center gap-1.5 rounded-xl border border-black/10 dark:border-white/10 px-4 py-2.5 text-xs font-semibold text-black/70 dark:text-white/70 transition-colors hover:border-black/20 dark:hover:border-white/20 hover:text-black dark:hover:text-white disabled:opacity-50 focus-visible:ring-2 focus-visible:ring-black"
                    >
                      <Pencil className="h-3.5 w-3.5" aria-hidden="true" />
                      Edit
                    </button>
                    <button
                      type="button"
                      disabled={busy}
                      onClick={handleArchive}
                      className="flex items-center gap-1.5 rounded-xl border border-black/10 dark:border-white/10 px-4 py-2.5 text-xs font-semibold text-black/70 dark:text-white/70 transition-colors hover:border-red-200 hover:text-red-600 disabled:opacity-50 focus-visible:ring-2 focus-visible:ring-black"
                    >
                      <Archive className="h-3.5 w-3.5" aria-hidden="true" />
                      Archive
                    </button>
                  </div>
                </div>
              </motion.div>

              {/* Progress + details */}
              <motion.div
                initial={{ opacity: 0, y: 12 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ duration: 0.35, delay: 0.1 }}
                className="grid gap-5 lg:grid-cols-5"
              >
                {/* Left: progress + contributions */}
                <div className="space-y-5 lg:col-span-3">
                  {/* Progress card */}
                  <div className="rounded-2xl border border-black/8 dark:border-white/8 bg-white dark:bg-[#100F0F] p-6">
                    <div className="mb-4 flex items-end justify-between gap-4">
                      <div>
                        <p className="text-xs text-black/60 dark:text-white/60 font-medium">Current balance</p>
                        <p className="mt-1 font-mono text-3xl text-black dark:text-white">
                          {formatAmount(goal.current_amount, goal.currency)}
                        </p>
                      </div>
                      <div className="text-right">
                        <p className="text-xs text-black/60 dark:text-white/60 font-medium">Target</p>
                        <p className="mt-1 font-mono text-lg text-black/70 dark:text-white/70">
                          {formatAmount(goal.target_amount, goal.currency)}
                        </p>
                      </div>
                    </div>

                    <ProgressVisualization progress={buildProgressData(goal)} />
                  </div>

                  {/* Contributions */}
                  <ContributionHistory goalId={id} />
                </div>

                {/* Right: details */}
                <div className="space-y-5 lg:col-span-2">
                  <div className="rounded-2xl border border-black/8 dark:border-white/8 bg-white dark:bg-[#100F0F] p-5">
                    <p className="mb-4 text-xs text-black/60 dark:text-white/60 font-semibold uppercase tracking-widest">
                      Goal Details
                    </p>
                    <dl className="space-y-3.5">
                      <div className="flex items-start justify-between gap-3">
                        <dt className="flex items-center gap-2 text-xs text-black/60 dark:text-white/60">
                          <Target className="h-3.5 w-3.5 text-black/40 dark:text-white/40" aria-hidden="true" />
                          Target
                        </dt>
                        <dd className="text-right font-mono text-xs text-black dark:text-white font-medium">
                          {formatAmount(goal.target_amount, goal.currency)}
                        </dd>
                      </div>
                      <div className="flex items-start justify-between gap-3">
                        <dt className="flex items-center gap-2 text-xs text-black/60 dark:text-white/60">
                          <Wallet className="h-3.5 w-3.5 text-black/40 dark:text-white/40" aria-hidden="true" />
                          Currency
                        </dt>
                        <dd className="text-right text-xs text-black dark:text-white font-medium">{goal.currency}</dd>
                      </div>
                      <div className="flex items-start justify-between gap-3">
                        <dt className="flex items-center gap-2 text-xs text-black/60 dark:text-white/60">
                          <Calendar className="h-3.5 w-3.5 text-black/40 dark:text-white/40" aria-hidden="true" />
                          Deadline
                        </dt>
                        <dd className="text-right text-xs text-black dark:text-white font-medium">
                          {formatFullDeadline(goal.deadline)}
                        </dd>
                      </div>
                      {goal.description && (
                        <div className="border-t border-black/6 dark:border-white/6 pt-3.5">
                          <dt className="mb-1 text-xs text-black/60 dark:text-white/60">Description</dt>
                          <dd className="text-xs text-black/80 dark:text-white/80 leading-relaxed">{goal.description}</dd>
                        </div>
                      )}
                    </dl>
                  </div>

                  {/* Auto-compound */}
                  <div className="rounded-2xl border border-black/8 dark:border-white/8 bg-white dark:bg-[#100F0F] p-5">
                    <div className="flex items-center justify-between gap-3">
                      <div className="flex items-center gap-3 min-w-0">
                        <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-black/5 dark:bg-white/5">
                          <RefreshCcw className="h-4 w-4 text-black/50 dark:text-white/50" aria-hidden="true" />
                        </div>
                        <div className="min-w-0">
                          <p className="text-sm font-semibold text-black dark:text-white">Auto-compound</p>
                          <p className="text-[11px] text-black/60 dark:text-white/60 font-medium">
                            Reinvest yield toward this goal
                          </p>
                        </div>
                      </div>
                      <AutoCompoundToggle goal={goal} disabled={busy} onToggle={handleAutoCompound} />
                    </div>
                  </div>
                </div>
              </motion.div>

              {editOpen && <EditGoalModal goal={goal} onClose={() => setEditOpen(false)} />}
            </>
          );
        })()
      )}
    </AppShell>
  );
}
