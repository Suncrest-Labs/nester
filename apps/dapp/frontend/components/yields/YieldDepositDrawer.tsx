"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { AnimatePresence, motion } from "framer-motion";
import { X, ArrowRight, Target } from "lucide-react";
import { cn } from "@/lib/utils";
import type { YieldPool } from "@/lib/api/yields";
import { useSavingsGoals } from "@/hooks/useSavingsGoals";
import { useWallet } from "@/components/wallet-provider";

interface YieldDepositDrawerProps {
  pool: YieldPool | null;
  onClose: () => void;
}

function goalLabel(description?: string, category?: string): string {
  if (description?.trim()) return description.trim();
  if (category) return category.replace(/_/g, " ").replace(/\b\w/g, (c) => c.toUpperCase());
  return "Savings Goal";
}

export function YieldDepositDrawer({ pool, onClose }: YieldDepositDrawerProps) {
  const router = useRouter();
  const { isConnected } = useWallet();
  const { data: goals, isLoading } = useSavingsGoals();
  const [selectedGoalId, setSelectedGoalId] = useState("");
  const [amount, setAmount] = useState("");

  const activeGoals = goals?.filter((g) => g.status !== "completed" && g.status !== "paused") ?? [];

  const handleContinue = () => {
    if (!pool || !selectedGoalId) return;
    const params = new URLSearchParams({
      goalId: selectedGoalId,
      protocol: pool.project,
      symbol: pool.symbol,
    });
    if (amount) params.set("amount", amount);
    router.push(`/savings?${params.toString()}`);
    onClose();
  };

  return (
    <AnimatePresence>
      {pool && (
        <>
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            className="fixed inset-0 z-50 bg-black/40"
            onClick={onClose}
            aria-hidden="true"
          />
          <motion.aside
            initial={{ x: "100%" }}
            animate={{ x: 0 }}
            exit={{ x: "100%" }}
            transition={{ type: "spring", damping: 28, stiffness: 320 }}
            className="fixed right-0 top-0 z-50 flex h-full w-full max-w-md flex-col border-l border-black/8 dark:border-white/8 bg-white dark:bg-[#100F0F] shadow-2xl"
            role="dialog"
            aria-modal="true"
            aria-labelledby="yield-deposit-title"
          >
            <div className="flex items-start justify-between border-b border-black/8 dark:border-white/8 px-6 py-5">
              <div>
                <h2 id="yield-deposit-title" className="text-base font-semibold text-black dark:text-white">
                  Deposit into {pool.symbol}
                </h2>
                <p className="mt-0.5 text-xs text-black/50 dark:text-white/50 capitalize">
                  {pool.project} · {pool.apy.toFixed(2)}% APY
                </p>
              </div>
              <button
                type="button"
                onClick={onClose}
                aria-label="Close deposit drawer"
                className="flex h-9 w-9 items-center justify-center rounded-xl border border-black/10 dark:border-white/10 text-black/40 dark:text-white/40 hover:text-black dark:hover:text-white"
              >
                <X className="h-4 w-4" />
              </button>
            </div>

            <div className="flex-1 overflow-y-auto px-6 py-5 space-y-5">
              {!isConnected ? (
                <p className="text-sm text-black/60 dark:text-white/60">
                  Connect your wallet to link this deposit to a savings goal.
                </p>
              ) : isLoading ? (
                <div className="space-y-3 animate-pulse">
                  <div className="h-10 rounded-xl bg-black/8 dark:bg-white/8" />
                  <div className="h-10 rounded-xl bg-black/8 dark:bg-white/8" />
                </div>
              ) : activeGoals.length === 0 ? (
                <div className="rounded-2xl border border-black/8 dark:border-white/8 bg-black/[0.02] dark:bg-white/[0.02] p-6 text-center">
                  <Target className="mx-auto mb-3 h-8 w-8 text-black/30 dark:text-white/30" />
                  <p className="text-sm font-medium text-black dark:text-white">No active savings goals</p>
                  <p className="mt-1 text-xs text-black/50 dark:text-white/50">
                    Create a goal first, then deposit into this yield protocol.
                  </p>
                  <button
                    type="button"
                    onClick={() => {
                      router.push("/savings");
                      onClose();
                    }}
                    className="mt-4 rounded-xl bg-black dark:bg-blue-600 px-4 py-2 text-xs font-semibold text-white"
                  >
                    Create a Goal
                  </button>
                </div>
              ) : (
                <>
                  <div>
                    <label htmlFor="yield-goal-select" className="mb-1.5 block text-xs font-semibold text-black/70 dark:text-white/70">
                      Link to savings goal
                    </label>
                    <select
                      id="yield-goal-select"
                      value={selectedGoalId}
                      onChange={(e) => setSelectedGoalId(e.target.value)}
                      className="w-full rounded-xl border border-black/10 dark:border-white/10 bg-white dark:bg-[#100F0F] px-3 py-2.5 text-sm text-black dark:text-white outline-none focus:border-black/25 dark:focus:border-white/25"
                    >
                      <option value="">Select a goal…</option>
                      {activeGoals.map((goal) => (
                        <option key={goal.id} value={goal.id}>
                          {goalLabel(goal.description, goal.category)} ({goal.currency})
                        </option>
                      ))}
                    </select>
                  </div>

                  <div>
                    <label htmlFor="yield-deposit-amount" className="mb-1.5 block text-xs font-semibold text-black/70 dark:text-white/70">
                      Deposit amount (optional)
                    </label>
                    <input
                      id="yield-deposit-amount"
                      type="number"
                      min="0"
                      step="0.01"
                      placeholder="0.00"
                      value={amount}
                      onChange={(e) => setAmount(e.target.value)}
                      className="w-full rounded-xl border border-black/10 dark:border-white/10 bg-white dark:bg-[#100F0F] px-3 py-2.5 text-sm font-mono text-black dark:text-white outline-none focus:border-black/25 dark:focus:border-white/25"
                    />
                  </div>
                </>
              )}
            </div>

            {isConnected && activeGoals.length > 0 && (
              <div className="border-t border-black/8 dark:border-white/8 px-6 py-4">
                <button
                  type="button"
                  disabled={!selectedGoalId}
                  onClick={handleContinue}
                  className={cn(
                    "flex w-full items-center justify-center gap-2 rounded-xl py-3 text-sm font-semibold transition-opacity",
                    selectedGoalId
                      ? "bg-black dark:bg-blue-600 text-white hover:opacity-85"
                      : "bg-black/10 dark:bg-white/10 text-black/40 dark:text-white/40 cursor-not-allowed"
                  )}
                >
                  Continue to Deposit
                  <ArrowRight className="h-4 w-4" />
                </button>
              </div>
            )}
          </motion.aside>
        </>
      )}
    </AnimatePresence>
  );
}
