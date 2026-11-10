"use client";

import { useState } from "react";
import { X } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { savingsGoals, type CreateSavingsGoalInput } from "@/lib/api/savings-goals";
import { BUTTON_PRIMARY, BUTTON_SECONDARY, INPUT, SegmentedControl } from "@/components/ui/page-kit";
import { GOAL_CATEGORIES, MODAL_BACKDROP, MODAL_PANEL, NUMBER_INPUT, FIELD_LABEL } from "./modal-styles";

interface CreateGoalModalProps {
  onClose: () => void;
}

function todayPlus(days: number): string {
  const d = new Date();
  d.setDate(d.getDate() + days);
  return d.toISOString().slice(0, 10);
}

export function CreateGoalModal({ onClose }: CreateGoalModalProps) {
  const queryClient = useQueryClient();
  const [description, setDescription] = useState("");
  const [targetAmount, setTargetAmount] = useState("");
  const [currency, setCurrency] = useState<"USDC" | "XLM">("USDC");
  const [deadline, setDeadline] = useState(todayPlus(30));
  const [category, setCategory] = useState("other");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");

  const minDate = todayPlus(2);

  function validate(): string {
    const amount = parseFloat(targetAmount);
    if (!description.trim()) return "Give your goal a name.";
    if (!targetAmount || isNaN(amount) || amount <= 0) return "Enter a target amount above zero.";
    if (!deadline || deadline < minDate) return "Pick a date at least 2 days from today.";
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
      const input: CreateSavingsGoalInput = {
        target_amount: parseFloat(targetAmount),
        currency,
        deadline: new Date(deadline + "T00:00:00Z").toISOString(),
        description: description.trim(),
        category,
      };
      await savingsGoals.create(input);
      await queryClient.invalidateQueries({ queryKey: ["savings-goals"] });
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : "The goal wasn't created. Try again.");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <>
      <div className={MODAL_BACKDROP} onClick={onClose} aria-hidden="true" />
      <div className={MODAL_PANEL} role="dialog" aria-modal="true" aria-labelledby="create-goal-title">
        <div className="mb-6 flex items-center justify-between">
          <h2 id="create-goal-title" className="text-[16px] font-semibold text-foreground">
            Create a goal
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
            <label htmlFor="goal-description" className={FIELD_LABEL}>
              Name
            </label>
            <input
              id="goal-description"
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
              <label htmlFor="goal-amount" className={FIELD_LABEL}>
                Target amount
              </label>
              <input
                id="goal-amount"
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
            <label htmlFor="goal-deadline" className={FIELD_LABEL}>
              Target date
            </label>
            <input
              id="goal-deadline"
              type="date"
              value={deadline}
              min={minDate}
              onChange={(e) => setDeadline(e.target.value)}
              className={INPUT}
            />
          </div>

          <div>
            <label htmlFor="goal-category" className={FIELD_LABEL}>
              Category
            </label>
            <select
              id="goal-category"
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
              {submitting ? "Creating…" : "Create goal"}
            </button>
          </div>
        </form>
      </div>
    </>
  );
}
