"use client";

import { useState } from "react";
import {
  Briefcase,
  GraduationCap,
  Heart,
  Home,
  Plane,
  Shield,
  Target,
  X,
} from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { savingsGoals, type CreateSavingsGoalInput } from "@/lib/api/savings-goals";
import { useYieldOpportunities } from "@/hooks/useYieldOpportunities";
import { BUTTON_PRIMARY, BUTTON_SECONDARY, INPUT, SegmentedControl } from "@/components/ui/page-kit";
import { GoalProgressBar } from "./GoalProgressBar";
import { FIELD_LABEL, MODAL_BACKDROP, MODAL_PANEL, NUMBER_INPUT } from "./modal-styles";

// ── Types ─────────────────────────────────────────────────────────────────────

interface GoalTemplate {
  id: string;
  label: string;
  category: string;
  descriptionPlaceholder: string;
  Icon: React.ElementType;
}

const GOAL_TEMPLATES: GoalTemplate[] = [
  { id: "emergency_fund", label: "Emergency fund", category: "emergency_fund", descriptionPlaceholder: "3–6 months of expenses", Icon: Shield },
  { id: "housing", label: "Housing", category: "housing", descriptionPlaceholder: "Down payment or rent", Icon: Home },
  { id: "travel", label: "Travel", category: "travel", descriptionPlaceholder: "A trip you're planning", Icon: Plane },
  { id: "education", label: "Education", category: "education", descriptionPlaceholder: "Tuition, courses or training", Icon: GraduationCap },
  { id: "business", label: "Business", category: "business", descriptionPlaceholder: "Startup capital or equipment", Icon: Briefcase },
  { id: "health", label: "Health", category: "health", descriptionPlaceholder: "Medical costs or fitness", Icon: Heart },
  { id: "other", label: "Start from scratch", category: "other", descriptionPlaceholder: "Name your goal", Icon: Target },
];

function todayPlus(days: number): string {
  const d = new Date();
  d.setDate(d.getDate() + days);
  return d.toISOString().slice(0, 10);
}

const STEPS = ["Template", "Goal details", "Earning", "Savings Rate"] as const;
type StepIndex = 0 | 1 | 2 | 3;

const TITLE = "text-[16px] font-semibold text-foreground";
const LEDE = "mt-1 text-sm text-muted-foreground";

// ── Sub-components ────────────────────────────────────────────────────────────

function StepIndicator({ step }: { step: StepIndex }) {
  return (
    <div className="mb-6">
      <p className="text-[13px] text-muted-foreground">
        Step {step + 1} of {STEPS.length}: {STEPS.find((_, i) => i === step)}
      </p>
      <GoalProgressBar
        percent={((step + 1) / STEPS.length) * 100}
        label="Setup progress"
        className="mt-2"
      />
    </div>
  );
}

// ── Step 1: Template ──────────────────────────────────────────────────────────

function StepTemplate({ onSelect }: { onSelect: (t: GoalTemplate) => void }) {
  return (
    <div>
      <h2 className={TITLE}>What are you saving for?</h2>
      <p className={LEDE}>Pick a starting point. You can change the name and amount next.</p>
      <div className="mt-5 grid grid-cols-2 gap-3 sm:grid-cols-3">
        {GOAL_TEMPLATES.map((t) => (
          <button
            key={t.id}
            type="button"
            onClick={() => onSelect(t)}
            className="flex flex-col items-start gap-3 rounded-xl border p-4 text-left transition-colors hover:bg-foreground/5 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-foreground"
          >
            <t.Icon className="h-4 w-4 text-muted-foreground" aria-hidden="true" />
            <span className="text-sm text-foreground">{t.label}</span>
          </button>
        ))}
      </div>
    </div>
  );
}

// ── Step 2: Goal details ──────────────────────────────────────────────────────

interface GoalDetails {
  name: string;
  targetAmount: string;
  currency: "USDC" | "XLM";
  deadline: string;
}

function StepDetails({
  template,
  details,
  onChange,
}: {
  template: GoalTemplate;
  details: GoalDetails;
  onChange: (d: GoalDetails) => void;
}) {
  const minDate = todayPlus(2);
  return (
    <div>
      <h2 className={TITLE}>{template.label}</h2>
      <p className={LEDE}>Set a target and a date.</p>
      <div className="mt-5 space-y-4">
        <div>
          <label htmlFor="onboarding-name" className={FIELD_LABEL}>
            Goal name
          </label>
          <input
            id="onboarding-name"
            type="text"
            value={details.name}
            onChange={(e) => onChange({ ...details, name: e.target.value })}
            placeholder={template.descriptionPlaceholder}
            maxLength={100}
            className={INPUT}
          />
        </div>
        <div className="grid grid-cols-2 gap-3">
          <div>
            <label htmlFor="onboarding-amount" className={FIELD_LABEL}>
              Target amount
            </label>
            <input
              id="onboarding-amount"
              type="number"
              min="0.01"
              step="0.01"
              value={details.targetAmount}
              onChange={(e) => onChange({ ...details, targetAmount: e.target.value })}
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
              value={details.currency}
              onChange={(c) => onChange({ ...details, currency: c })}
            />
          </div>
        </div>
        <div>
          <label htmlFor="onboarding-deadline" className={FIELD_LABEL}>
            Target date
          </label>
          <input
            id="onboarding-deadline"
            type="date"
            value={details.deadline}
            min={minDate}
            onChange={(e) => onChange({ ...details, deadline: e.target.value })}
            className={INPUT}
          />
        </div>
      </div>
    </div>
  );
}

// ── Step 3: Earning (separate from the goal) ──────────────────────────────────

function StepYield({ onSkip }: { onSkip: () => void }) {
  const { data: yieldData, isLoading } = useYieldOpportunities("Stellar", 6);
  const pools = yieldData?.pools ?? [];

  return (
    <div>
      <h2 className={TITLE}>Earn while you save</h2>
      <p className={LEDE}>
        Your goal tracks a target amount and date. Deposits into the Nester Savings Rate
        don&apos;t add to its progress yet.
      </p>

      {isLoading ? (
        <div className="mt-5 space-y-2">
          {[0, 1, 2].map((i) => (
            <div key={i} className="h-12 animate-pulse rounded-lg bg-foreground/[0.06]" />
          ))}
        </div>
      ) : pools.length > 0 ? (
        <ul className="mt-5 divide-y rounded-xl border">
          {pools.slice(0, 4).map((pool) => (
            <li key={pool.pool} className="flex items-center justify-between px-4 py-3">
              <div>
                <p className="text-sm capitalize text-foreground">{pool.project}</p>
                <p className="text-[13px] text-muted-foreground">
                  {pool.symbol} · Risk {pool.riskScore.toFixed(1)}
                </p>
              </div>
              <p className="text-sm tabular-nums text-foreground">{pool.apy.toFixed(2)}% APY</p>
            </li>
          ))}
        </ul>
      ) : null}

      <div className="mt-5 flex flex-wrap items-center gap-4">
        <a href="/dashboard" className={BUTTON_SECONDARY}>
          See the Nester Savings Rate
        </a>
        <button
          type="button"
          onClick={onSkip}
          className="text-[13px] text-muted-foreground underline-offset-4 hover:text-foreground hover:underline"
        >
          Skip this step
        </button>
      </div>
    </div>
  );
}

// ── Step 4: Savings rate (separate from the goal) ─────────────────────────────

function StepSavingsRate({ onDone }: { onDone: () => void }) {
  return (
    <div>
      <h2 className={TITLE}>Earn on the Savings Rate</h2>
      <p className={LEDE}>
        The Nester Savings Rate earns a realized rate on USDC. It&apos;s separate from this goal:
        deposits there don&apos;t count toward the goal yet.
      </p>
      <div className="mt-6 flex justify-end gap-3">
        <a href="/dashboard" className={BUTTON_SECONDARY}>
          Open the Savings Rate
        </a>
        <button type="button" onClick={onDone} className={BUTTON_PRIMARY}>
          Done
        </button>
      </div>
    </div>
  );
}

// ── Main wizard ───────────────────────────────────────────────────────────────

interface SavingsOnboardingWizardProps {
  onComplete: () => void;
  onDismiss: () => void;
}

export function SavingsOnboardingWizard({ onComplete, onDismiss }: SavingsOnboardingWizardProps) {
  const queryClient = useQueryClient();
  const [step, setStep] = useState<StepIndex>(0);
  const [selectedTemplate, setSelectedTemplate] = useState<GoalTemplate | null>(null);
  const [details, setDetails] = useState<GoalDetails>({
    name: "",
    targetAmount: "",
    currency: "USDC",
    deadline: todayPlus(30),
  });
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);

  function handleTemplateSelect(t: GoalTemplate) {
    setSelectedTemplate(t);
    setDetails((d) => ({ ...d, name: t.id !== "other" ? t.label : "" }));
    setStep(1);
  }

  function validateDetails(): string {
    const amount = parseFloat(details.targetAmount);
    if (!details.name.trim()) return "Give your goal a name.";
    if (!details.targetAmount || isNaN(amount) || amount <= 0) return "Enter a target amount above zero.";
    const minDate = todayPlus(2);
    if (!details.deadline || details.deadline < minDate) return "Pick a date at least 2 days from today.";
    return "";
  }

  async function handleDetailsNext() {
    const msg = validateDetails();
    if (msg) {
      setError(msg);
      return;
    }
    setError("");
    setSubmitting(true);
    try {
      const input: CreateSavingsGoalInput = {
        target_amount: parseFloat(details.targetAmount),
        currency: details.currency,
        deadline: new Date(details.deadline + "T00:00:00Z").toISOString(),
        description: details.name.trim(),
        category: selectedTemplate?.category ?? "other",
      };
      await savingsGoals.create(input);
      await queryClient.invalidateQueries({ queryKey: ["savings-goals"] });
      setStep(2);
    } catch (err) {
      setError(err instanceof Error ? err.message : "The goal wasn't created. Try again.");
    } finally {
      setSubmitting(false);
    }
  }

  function handleBack() {
    if (step > 0) setStep((s) => (s - 1) as StepIndex);
  }

  return (
    <>
      <div className={MODAL_BACKDROP} onClick={onDismiss} aria-hidden="true" />
      <div
        className={`${MODAL_PANEL} sm:max-w-xl`}
        role="dialog"
        aria-modal="true"
        aria-label="Savings goal setup"
      >
        <div className="mb-4 flex items-center justify-between">
          <span className="text-[13px] font-medium text-muted-foreground">New goal</span>
          <button
            type="button"
            onClick={onDismiss}
            aria-label="Close onboarding"
            className="flex h-8 w-8 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-foreground/5 hover:text-foreground"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        <StepIndicator step={step} />

        {step === 0 && <StepTemplate onSelect={handleTemplateSelect} />}

        {step === 1 && selectedTemplate && (
          <>
            <StepDetails template={selectedTemplate} details={details} onChange={setDetails} />
            {error && (
              <p className="mt-4 text-sm text-red-600 dark:text-red-400" role="alert">
                {error}
              </p>
            )}
            <div className="mt-6 flex justify-end gap-3">
              <button type="button" onClick={handleBack} className={BUTTON_SECONDARY}>
                Back
              </button>
              <button
                type="button"
                onClick={handleDetailsNext}
                disabled={submitting}
                className={BUTTON_PRIMARY}
              >
                {submitting ? "Creating goal…" : "Create goal & continue"}
              </button>
            </div>
          </>
        )}

        {step === 2 && (
          <>
            <StepYield onSkip={() => setStep(3)} />
            <div className="mt-6 flex justify-end">
              <button type="button" onClick={() => setStep(3)} className={BUTTON_PRIMARY}>
                Continue
              </button>
            </div>
          </>
        )}

        {step === 3 && <StepSavingsRate onDone={onComplete} />}
      </div>
    </>
  );
}
