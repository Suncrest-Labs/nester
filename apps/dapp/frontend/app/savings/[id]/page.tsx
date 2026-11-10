"use client";

import { useEffect, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import Link from "next/link";
import { useQueryClient } from "@tanstack/react-query";
import { ArrowLeft } from "lucide-react";
import { AppShell } from "@/components/app-shell";
import { SavingsDetailSkeleton } from "@/components/skeletons/page-skeletons";
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
import { formatGoalAmount } from "@/components/savings/GoalProgressBar";
import type { GoalProgressData } from "@/lib/types/progress";
import {
  BUTTON_SECONDARY,
  BigNumber,
  COLUMNS,
  EmptyPanel,
  PANEL,
  Panel,
  PageHeader,
  Section,
} from "@/components/ui/page-kit";

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

function sentenceCase(value: string): string {
  const s = value.replace(/_/g, " ");
  return s.charAt(0).toUpperCase() + s.slice(1);
}

function goalDisplayName(goal: SavingsGoal): string {
  if (goal.description?.trim()) return goal.description.trim();
  if (goal.category) return sentenceCase(goal.category);
  return "Savings goal";
}

function formatAmount(value: string | number, currency: string): string {
  return `${toNumber(value).toLocaleString(undefined, {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })} ${currency}`;
}

function formatShortDate(iso: string): string {
  const date = new Date(iso);
  return Number.isNaN(date.getTime())
    ? "—"
    : date.toLocaleDateString("en-US", { month: "short", day: "numeric", year: "numeric" });
}

const FREQUENCY_LABEL: Record<string, string> = {
  weekly: "every week",
  biweekly: "every two weeks",
  monthly: "every month",
};

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
        "relative inline-flex h-6 w-11 shrink-0 items-center rounded-full transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-foreground disabled:opacity-50",
        enabled ? "bg-foreground" : "bg-foreground/15"
      )}
    >
      <span
        className={cn(
          "inline-block h-5 w-5 rounded-full bg-background transition-transform",
          enabled ? "translate-x-5" : "translate-x-0.5"
        )}
      />
    </button>
  );
}

// ── Contribution history ────────────────────────────────────────────────────

function ContributionRow({ contribution }: { contribution: SavingsGoalContribution }) {
  return (
    <li className="flex items-center justify-between gap-3 py-3">
      <div className="min-w-0">
        <p className="truncate text-sm text-foreground">
          {contribution.source ? sentenceCase(contribution.source) : "Contribution"}
        </p>
        <p className="text-[13px] text-muted-foreground">{formatShortDate(contribution.created_at)}</p>
      </div>
      <span className="shrink-0 text-sm tabular-nums text-foreground">
        +{formatAmount(contribution.amount, contribution.currency)}
      </span>
    </li>
  );
}

function ContributionHistory({ goalId }: { goalId: string }) {
  const { data, isLoading, isError, refetch, isFetching } = useSavingsGoalContributions(goalId);

  return (
    <div className={PANEL}>
      {isLoading ? (
        <div className="space-y-3" data-testid="contributions-skeleton" aria-busy="true">
          {[0, 1, 2].map((i) => (
            <div key={i} className="h-10 animate-pulse rounded-lg bg-foreground/[0.06]" />
          ))}
        </div>
      ) : isError ? (
        <div data-testid="contributions-error">
          <p role="alert" className="text-sm text-foreground">
            The history for this goal couldn&apos;t be loaded.
          </p>
          <button
            type="button"
            onClick={() => refetch()}
            disabled={isFetching}
            className={`mt-4 ${BUTTON_SECONDARY}`}
          >
            {isFetching ? "Retrying…" : "Try again"}
          </button>
        </div>
      ) : !data?.length ? (
        <div data-testid="contributions-empty">
          <p className="text-sm text-foreground">Nothing recorded yet</p>
          <p className="mt-2 max-w-[60ch] text-sm text-muted-foreground">
            Amounts counted toward this goal will be listed here.
          </p>
        </div>
      ) : (
        <ul className="-my-3 divide-y">
          {data.map((c) => (
            <ContributionRow key={c.id} contribution={c} />
          ))}
        </ul>
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
      <Link
        href="/savings"
        className="mb-6 inline-flex items-center gap-1.5 rounded text-[13px] text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-foreground"
      >
        <ArrowLeft className="h-3.5 w-3.5" aria-hidden="true" />
        All goals
      </Link>

      {isLoading ? (
        <SavingsDetailSkeleton />
      ) : isError || !goal ? (
        <EmptyPanel
          title="This goal couldn't be found"
          action={
            <Link href="/savings" className={BUTTON_SECONDARY}>
              Back to goals
            </Link>
          }
        >
          It may have been archived, or the link is out of date. Your other goals are still on the
          goals page.
        </EmptyPanel>
      ) : (
        (() => {
          const current = toNumber(goal.current_amount);
          const isPaused = goal.status === "paused";
          const isCompleted = goal.status === "completed";
          const schedule = goal.active_schedule;

          return (
            <>
              <PageHeader
                title={goalDisplayName(goal)}
                lede={
                  <p className="text-sm text-muted-foreground">
                    {goal.category ? sentenceCase(goal.category) : "Other"}
                    {isPaused && " · Paused"}
                    {isCompleted && " · Reached"}
                  </p>
                }
              />

              <div className={`mt-8 ${COLUMNS}`}>
                <div>
                  <section aria-labelledby="goal-saved-label" className="py-2">
                    <BigNumber className="text-6xl tracking-tight sm:text-7xl">
                      {formatGoalAmount(current)}
                    </BigNumber>
                    <p id="goal-saved-label" className="mt-3 text-sm text-muted-foreground">
                      Saved of{" "}
                      <span className="tabular-nums text-foreground/80">
                        {formatAmount(goal.target_amount, goal.currency)}
                      </span>{" "}
                      by {formatFullDeadline(goal.deadline)}
                    </p>
                  </section>

                  <div className="mt-6">
                    <ProgressVisualization progress={buildProgressData(goal)} />
                  </div>

                  <Section
                    id="contributions-heading"
                    title="History"
                    lede="Amounts recorded toward this goal."
                  >
                    <ContributionHistory goalId={id} />
                  </Section>
                </div>

                <div className="grid gap-6">
                  <Panel title="Details">
                    <dl className="mt-4 space-y-3 text-sm">
                      <div className="flex items-start justify-between gap-3">
                        <dt className="text-muted-foreground">Target</dt>
                        <dd className="text-right tabular-nums text-foreground">
                          {formatAmount(goal.target_amount, goal.currency)}
                        </dd>
                      </div>
                      <div className="flex items-start justify-between gap-3">
                        <dt className="text-muted-foreground">Target date</dt>
                        <dd className="text-right text-foreground">
                          {formatFullDeadline(goal.deadline)}
                          <span className="mt-0.5 block">
                            <DeadlineBadge deadline={goal.deadline} status={goal.status} />
                          </span>
                        </dd>
                      </div>
                      <div className="flex items-start justify-between gap-3">
                        <dt className="text-muted-foreground">Schedule</dt>
                        <dd className="text-right text-foreground">
                          {schedule ? (
                            <>
                              <span className="tabular-nums">
                                {formatAmount(schedule.amount, schedule.currency)}
                              </span>{" "}
                              {FREQUENCY_LABEL[schedule.frequency] ?? schedule.frequency}
                              <span className="mt-0.5 block text-[13px] text-muted-foreground">
                                Next on {formatShortDate(schedule.next_run_at)}
                              </span>
                            </>
                          ) : (
                            <span className="text-muted-foreground">None</span>
                          )}
                        </dd>
                      </div>
                    </dl>

                    <div className="mt-6 flex flex-wrap gap-3">
                      {!isCompleted && (
                        <button
                          type="button"
                          disabled={busy}
                          onClick={() =>
                            isPaused
                              ? runAction("resumed", () => savingsGoals.resume(id))
                              : runAction("paused", () => savingsGoals.pause(id))
                          }
                          className={BUTTON_SECONDARY}
                        >
                          {isPaused ? "Resume" : "Pause"}
                        </button>
                      )}
                      <button
                        type="button"
                        disabled={busy}
                        onClick={() => setEditOpen(true)}
                        className={BUTTON_SECONDARY}
                      >
                        Edit
                      </button>
                      <button
                        type="button"
                        disabled={busy}
                        onClick={handleArchive}
                        className={cn(BUTTON_SECONDARY, "text-red-600 dark:text-red-400")}
                      >
                        Archive
                      </button>
                    </div>
                  </Panel>

                  <Panel>
                    <div className="flex items-center justify-between gap-4">
                      <div className="min-w-0">
                        <h3 className="text-sm font-medium text-foreground">Auto-compound</h3>
                        <p className="mt-1 text-sm text-muted-foreground">
                          Reinvest yield toward this goal.
                        </p>
                      </div>
                      <AutoCompoundToggle goal={goal} disabled={busy} onToggle={handleAutoCompound} />
                    </div>
                  </Panel>

                  <Panel title="Where the money is">
                    <p className="mt-2 text-sm text-foreground/80">
                      Goals don&apos;t hold money. Your savings stay in the Nester Savings Rate,
                      and this goal tracks progress toward your target.
                    </p>
                    <Link href="/dashboard" className={`mt-4 ${BUTTON_SECONDARY}`}>
                      Open the Savings Rate
                    </Link>
                  </Panel>
                </div>
              </div>

              {editOpen && <EditGoalModal goal={goal} onClose={() => setEditOpen(false)} />}
            </>
          );
        })()
      )}
    </AppShell>
  );
}
