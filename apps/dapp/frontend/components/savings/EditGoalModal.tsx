"use client";

import { useState } from "react";
import { X } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import {
  savingsGoals,
  type CreateSavingsGoalInput,
  type SavingsGoal,
} from "@/lib/api/savings-goals";

interface EditGoalModalProps {
  goal: SavingsGoal;
  onClose: () => void;
}

function toNumber(value: string | number): number {
  return typeof value === "number" ? value : parseFloat(value) || 0;
}

/** Converts an ISO timestamp into a `yyyy-mm-dd` value for a date input. */
function toDateInput(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? "" : d.toISOString().slice(0, 10);
}

function todayPlus(days: number): string {
  const d = new Date();
  d.setDate(d.getDate() + days);
  return d.toISOString().slice(0, 10);
}

/** Pre-filled modal for editing an existing savings goal via PATCH (#732). */
export function EditGoalModal({ goal, onClose }: EditGoalModalProps) {
  const queryClient = useQueryClient();
  const [description, setDescription] = useState(goal.description ?? "");
  const [targetAmount, setTargetAmount] = useState(String(toNumber(goal.target_amount)));
  const [currency, setCurrency] = useState<"USDC" | "XLM">(
    goal.currency === "XLM" ? "XLM" : "USDC"
  );
  const [deadline, setDeadline] = useState(toDateInput(goal.deadline));
  const [category, setCategory] = useState(goal.category ?? "other");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");

  const minDate = todayPlus(2);

  function validate(): string {
    const amount = parseFloat(targetAmount);
    if (!description.trim()) return "Description is required.";
    if (!targetAmount || isNaN(amount) || amount <= 0) return "Enter a positive target amount.";
    if (!deadline) return "Select a target deadline.";
    return "";
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    const msg = validate();
    if (msg) {
      setError(msg);
      return;
    }
    setError("");
    setSubmitting(true);
    try {
      const input: Partial<CreateSavingsGoalInput> = {
        target_amount: parseFloat(targetAmount),
        currency,
        deadline: new Date(deadline + "T00:00:00Z").toISOString(),
        description: description.trim(),
        category,
      };
      await savingsGoals.update(goal.id, input);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["savings-goals"] }),
        queryClient.invalidateQueries({ queryKey: ["savings-goal", goal.id] }),
      ]);
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to update goal. Please try again.");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <>
      <div
        className="fixed inset-0 z-50 bg-black/25 backdrop-blur-sm"
        onClick={onClose}
        aria-hidden="true"
      />
      <div
        className="fixed inset-x-4 top-16 z-50 mx-auto max-w-lg rounded-3xl bg-white dark:bg-[#100F0F] p-8 shadow-2xl
                   sm:inset-auto sm:left-1/2 sm:top-1/2 sm:-translate-x-1/2 sm:-translate-y-1/2"
        role="dialog"
        aria-modal="true"
        aria-labelledby="edit-goal-title"
      >
        <div className="mb-6 flex items-center justify-between">
          <h2 id="edit-goal-title" className="text-lg font-semibold text-black dark:text-white">
            Edit Savings Goal
          </h2>
          <button
            onClick={onClose}
            aria-label="Close"
            className="flex h-8 w-8 items-center justify-center rounded-xl border border-black/10 dark:border-white/10 text-black/40 dark:text-white/40 hover:text-black dark:hover:text-white transition-colors"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        <form onSubmit={handleSubmit} noValidate className="space-y-5">
          <div>
            <label htmlFor="edit-goal-description" className="mb-1.5 block text-xs font-medium text-black/60 dark:text-white/60">
              Description
            </label>
            <input
              id="edit-goal-description"
              type="text"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="e.g. Emergency fund, Holiday trip…"
              maxLength={200}
              className="h-11 w-full rounded-xl border border-black/10 dark:border-white/10 bg-black/[0.02] dark:bg-white/[0.02] px-4 text-sm text-black dark:text-white outline-none transition-colors focus:border-black/25 dark:focus:border-white/25"
            />
          </div>

          <div className="grid grid-cols-2 gap-4">
            <div>
              <label htmlFor="edit-goal-amount" className="mb-1.5 block text-xs font-medium text-black/60 dark:text-white/60">
                Target amount
              </label>
              <input
                id="edit-goal-amount"
                type="number"
                min="0.01"
                step="0.01"
                value={targetAmount}
                onChange={(e) => setTargetAmount(e.target.value)}
                placeholder="0.00"
                className="h-11 w-full rounded-xl border border-black/10 dark:border-white/10 bg-black/[0.02] dark:bg-white/[0.02] px-4 font-mono text-sm text-black dark:text-white outline-none transition-colors focus:border-black/25 dark:focus:border-white/25
                           [appearance:textfield] [&::-webkit-outer-spin-button]:appearance-none [&::-webkit-inner-spin-button]:appearance-none"
              />
            </div>

            <div>
              <label className="mb-1.5 block text-xs font-medium text-black/60 dark:text-white/60">Currency</label>
              <div
                className="flex h-11 rounded-xl border border-black/10 dark:border-white/10 bg-black/[0.02] dark:bg-white/[0.02] p-1"
                role="group"
                aria-label="Select currency"
              >
                {(["USDC", "XLM"] as const).map((c) => (
                  <button
                    key={c}
                    type="button"
                    onClick={() => setCurrency(c)}
                    className={`flex-1 rounded-lg text-xs font-semibold transition-colors ${
                      currency === c
                        ? "bg-black dark:bg-blue-600 text-white"
                        : "text-black/60 dark:text-white/60 hover:text-black dark:hover:text-white"
                    }`}
                    aria-pressed={currency === c}
                  >
                    {c}
                  </button>
                ))}
              </div>
            </div>
          </div>

          <div>
            <label htmlFor="edit-goal-deadline" className="mb-1.5 block text-xs font-medium text-black/60 dark:text-white/60">
              Target deadline
            </label>
            <input
              id="edit-goal-deadline"
              type="date"
              value={deadline}
              min={minDate}
              onChange={(e) => setDeadline(e.target.value)}
              className="h-11 w-full rounded-xl border border-black/10 dark:border-white/10 bg-black/[0.02] dark:bg-white/[0.02] px-4 text-sm text-black dark:text-white outline-none transition-colors focus:border-black/25 dark:focus:border-white/25"
            />
          </div>

          <div>
            <label htmlFor="edit-goal-category" className="mb-1.5 block text-xs font-medium text-black/60 dark:text-white/60">
              Category
            </label>
            <select
              id="edit-goal-category"
              value={category}
              onChange={(e) => setCategory(e.target.value)}
              className="h-11 w-full rounded-xl border border-black/10 dark:border-white/10 bg-black/[0.02] dark:bg-white/[0.02] px-4 text-sm text-black dark:text-white outline-none transition-colors focus:border-black/25 dark:focus:border-white/25"
            >
              <option value="other">Other</option>
              <option value="emergency_fund">Emergency Fund</option>
              <option value="education">Education</option>
              <option value="housing">Housing</option>
              <option value="travel">Travel</option>
              <option value="business">Business</option>
              <option value="health">Health</option>
              <option value="retirement">Retirement</option>
            </select>
          </div>

          {error && (
            <p className="rounded-lg bg-red-50 px-4 py-2.5 text-xs font-medium text-red-700" role="alert">
              {error}
            </p>
          )}

          <div className="flex gap-3 pt-1">
            <button
              type="button"
              onClick={onClose}
              className="flex-1 rounded-xl border border-black/10 dark:border-white/10 py-3 text-xs font-semibold text-black/60 dark:text-white/60 hover:bg-black/5 dark:hover:bg-white/5 transition-colors"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={submitting}
              className="flex-1 rounded-xl bg-black dark:bg-blue-600 py-3 text-xs font-semibold text-white transition-opacity hover:opacity-75 disabled:opacity-50"
            >
              {submitting ? "Saving…" : "Save Changes"}
            </button>
          </div>
        </form>
      </div>
    </>
  );
}

export default EditGoalModal;
