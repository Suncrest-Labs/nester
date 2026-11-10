"use client";

import { useState } from "react";
import { X } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import {
  savingsGoals,
  type CreateSavingsGoalInput,
  type SavingsGoal,
} from "@/lib/api/savings-goals";
import { BUTTON_PRIMARY, BUTTON_SECONDARY, INPUT, SegmentedControl } from "@/components/ui/page-kit";
import { FIELD_LABEL, GOAL_CATEGORIES, MODAL_BACKDROP, MODAL_PANEL, NUMBER_INPUT } from "./modal-styles";

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
    if (!description.trim()) return "Give your goal a name.";
    if (!targetAmount || isNaN(amount) || amount <= 0) return "Enter a target amount above zero.";
    if (!deadline) return "Pick a target date.";
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
      setError(err instanceof Error ? err.message : "Your changes weren't saved. Try again.");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <>
      <div className={MODAL_BACKDROP} onClick={onClose} aria-hidden="true" />
      <div className={MODAL_PANEL} role="dialog" aria-modal="true" aria-labelledby="edit-goal-title">
        <div className="mb-6 flex items-center justify-between">
          <h2 id="edit-goal-title" className="text-[16px] font-semibold text-foreground">
            Edit goal
          </h2>
          <button
            type="button"
            onClick={onClose}
            aria-label="Close"
            className="flex h-8 w-8 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-foreground/5 hover:text-foreground"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        <form onSubmit={handleSubmit} noValidate className="space-y-5">
          <div>
            <label htmlFor="edit-goal-description" className={FIELD_LABEL}>
              Name
            </label>
            <input
              id="edit-goal-description"
              type="text"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="Emergency fund, holiday…"
              maxLength={200}
              className={INPUT}
            />
          </div>

          <div className="grid grid-cols-2 gap-4">
            <div>
              <label htmlFor="edit-goal-amount" className={FIELD_LABEL}>
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
                className={NUMBER_INPUT}
              />
            </div>

            <div>
              <span className={FIELD_LABEL}>Currency</span>
              <SegmentedControl
                label="Select currency"
                options={[
                  { id: "USDC", label: "USDC" },
                  { id: "XLM", label: "XLM" },
                ]}
                value={currency}
                onChange={setCurrency}
              />
            </div>
          </div>

          <div>
            <label htmlFor="edit-goal-deadline" className={FIELD_LABEL}>
              Target date
            </label>
            <input
              id="edit-goal-deadline"
              type="date"
              value={deadline}
              min={minDate}
              onChange={(e) => setDeadline(e.target.value)}
              className={INPUT}
            />
          </div>

          <div>
            <label htmlFor="edit-goal-category" className={FIELD_LABEL}>
              Category
            </label>
            <select
              id="edit-goal-category"
              value={category}
              onChange={(e) => setCategory(e.target.value)}
              className={INPUT}
            >
              {GOAL_CATEGORIES.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.label}
                </option>
              ))}
            </select>
          </div>

          {error && (
            <p className="text-sm text-red-600 dark:text-red-400" role="alert">
              {error}
            </p>
          )}

          <div className="flex justify-end gap-3 pt-1">
            <button type="button" onClick={onClose} className={BUTTON_SECONDARY}>
              Cancel
            </button>
            <button type="submit" disabled={submitting} className={BUTTON_PRIMARY}>
              {submitting ? "Saving…" : "Save changes"}
            </button>
          </div>
        </form>
      </div>
    </>
  );
}

export default EditGoalModal;
