"use client";

import { useState } from "react";
import {
  ArrowLeft,
  ArrowRight,
  Briefcase,
  Check,
  ExternalLink,
  GraduationCap,
  Heart,
  Home,
  Plane,
  Shield,
  Target,
  X,
} from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { cn } from "@/lib/utils";
import { savingsGoals, type CreateSavingsGoalInput } from "@/lib/api/savings-goals";
import { useYieldOpportunities } from "@/hooks/useYieldOpportunities";

// ── Types ─────────────────────────────────────────────────────────────────────

interface GoalTemplate {
  id: string;
  label: string;
  emoji: string;
  category: string;
  descriptionPlaceholder: string;
  Icon: React.ElementType;
}

const GOAL_TEMPLATES: GoalTemplate[] = [
  { id: "emergency_fund", label: "Emergency Fund", emoji: "🛡️", category: "emergency_fund", descriptionPlaceholder: "3–6 months of expenses", Icon: Shield },
  { id: "housing", label: "Housing", emoji: "🏠", category: "housing", descriptionPlaceholder: "Down payment or rent savings", Icon: Home },
  { id: "travel", label: "Travel", emoji: "✈️", category: "travel", descriptionPlaceholder: "Dream vacation fund", Icon: Plane },
  { id: "education", label: "Education", emoji: "🎓", category: "education", descriptionPlaceholder: "Tuition, courses, or training", Icon: GraduationCap },
  { id: "business", label: "Business", emoji: "💼", category: "business", descriptionPlaceholder: "Startup capital or equipment", Icon: Briefcase },
  { id: "health", label: "Health", emoji: "❤️", category: "health", descriptionPlaceholder: "Medical expenses or fitness", Icon: Heart },
  { id: "other", label: "Start from scratch", emoji: "🎯", category: "other", descriptionPlaceholder: "Describe your goal…", Icon: Target },
];

function todayPlus(days: number): string {
  const d = new Date();
  d.setDate(d.getDate() + days);
  return d.toISOString().slice(0, 10);
}

const STEPS = ["Template", "Goal details", "Choose yield", "First deposit"] as const;
type StepIndex = 0 | 1 | 2 | 3;

// ── Sub-components ────────────────────────────────────────────────────────────

function ProgressBar({ step }: { step: StepIndex }) {
  return (
    <div className="mb-8" role="progressbar" aria-valuenow={step + 1} aria-valuemin={1} aria-valuemax={STEPS.length}>
      <div className="mb-3 flex items-center justify-between">
        {STEPS.map((label, i) => (
          <div key={label} className="flex flex-col items-center gap-1">
            <div
              className={cn(
                "flex h-7 w-7 items-center justify-center rounded-full text-xs font-bold transition-colors",
                i < step
                  ? "bg-black text-white"
                  : i === step
                  ? "bg-black text-white ring-4 ring-black/10"
                  : "bg-black/8 text-black/30"
              )}
            >
              {i < step ? <Check className="h-3.5 w-3.5" /> : i + 1}
            </div>
            <span className={cn("text-[10px] font-semibold hidden sm:block", i === step ? "text-black" : "text-black/30")}>
              {label}
            </span>
          </div>
        ))}
      </div>
      <div className="h-1 w-full overflow-hidden rounded-full bg-black/8">
        <div
          className="h-full rounded-full bg-black transition-all duration-300"
          style={{ width: `${((step) / (STEPS.length - 1)) * 100}%` }}
        />
      </div>
    </div>
  );
}

// ── Step 1: Template ──────────────────────────────────────────────────────────

function StepTemplate({ onSelect }: { onSelect: (t: GoalTemplate) => void }) {
  return (
    <div>
      <h2 className="mb-1 text-lg font-semibold text-black">What are you saving for?</h2>
      <p className="mb-6 text-sm text-black/60">Pick a template to get started or create your own.</p>
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
        {GOAL_TEMPLATES.map((t) => (
          <button
            key={t.id}
            type="button"
            onClick={() => onSelect(t)}
            className="flex flex-col items-center gap-2 rounded-2xl border border-black/8 bg-white p-4 text-center transition-all hover:border-black/20 hover:shadow-sm active:scale-95"
          >
            <span className="text-2xl" aria-hidden="true">{t.emoji}</span>
            <span className="text-xs font-semibold text-black">{t.label}</span>
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
      <div className="mb-5 flex items-center gap-3">
        <span className="text-3xl" aria-hidden="true">{template.emoji}</span>
        <div>
          <h2 className="text-lg font-semibold text-black">{template.label}</h2>
          <p className="text-sm text-black/60">Set your goal details</p>
        </div>
      </div>
      <div className="space-y-4">
        <div>
          <label htmlFor="onboarding-name" className="mb-1.5 block text-xs font-medium text-black/60">
            Goal name
          </label>
          <input
            id="onboarding-name"
            type="text"
            value={details.name}
            onChange={(e) => onChange({ ...details, name: e.target.value })}
            placeholder={template.descriptionPlaceholder}
            maxLength={100}
            className="h-11 w-full rounded-xl border border-black/10 bg-black/[0.02] px-4 text-sm text-black outline-none transition-colors focus:border-black/25"
          />
        </div>
        <div className="grid grid-cols-2 gap-3">
          <div>
            <label htmlFor="onboarding-amount" className="mb-1.5 block text-xs font-medium text-black/60">
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
              className="h-11 w-full rounded-xl border border-black/10 bg-black/[0.02] px-4 font-mono text-sm text-black outline-none transition-colors focus:border-black/25
                         [appearance:textfield] [&::-webkit-outer-spin-button]:appearance-none [&::-webkit-inner-spin-button]:appearance-none"
            />
          </div>
          <div>
            <label className="mb-1.5 block text-xs font-medium text-black/60">Currency</label>
            <div className="flex h-11 rounded-xl border border-black/10 bg-black/[0.02] p-1" role="group" aria-label="Select currency">
              {(["USDC", "XLM"] as const).map((c) => (
                <button
                  key={c}
                  type="button"
                  onClick={() => onChange({ ...details, currency: c })}
                  className={cn(
                    "flex-1 rounded-lg text-xs font-semibold transition-colors",
                    details.currency === c ? "bg-black text-white" : "text-black/60 hover:text-black"
                  )}
                  aria-pressed={details.currency === c}
                >
                  {c}
                </button>
              ))}
            </div>
          </div>
        </div>
        <div>
          <label htmlFor="onboarding-deadline" className="mb-1.5 block text-xs font-medium text-black/60">
            Target deadline
          </label>
          <input
            id="onboarding-deadline"
            type="date"
            value={details.deadline}
            min={minDate}
            onChange={(e) => onChange({ ...details, deadline: e.target.value })}
            className="h-11 w-full rounded-xl border border-black/10 bg-black/[0.02] px-4 text-sm text-black outline-none transition-colors focus:border-black/25"
          />
        </div>
      </div>
    </div>
  );
}

// ── Step 3: Choose yield protocol ─────────────────────────────────────────────

function StepYield({ onSkip }: { onSkip: () => void }) {
  const { data: pools = [], isLoading } = useYieldOpportunities("Stellar", 6);

  return (
    <div>
      <h2 className="mb-1 text-lg font-semibold text-black">Earn while you save</h2>
      <p className="mb-5 text-sm text-black/60">
        Link your goal to a yield protocol and watch your savings grow automatically.
      </p>

      {isLoading ? (
        <div className="space-y-2">
          {[0, 1, 2].map((i) => (
            <div key={i} className="h-14 animate-pulse rounded-2xl bg-black/5" />
          ))}
        </div>
      ) : pools.length > 0 ? (
        <div className="mb-5 space-y-2">
          {pools.slice(0, 4).map((pool) => (
            <div
              key={pool.pool}
              className="flex items-center justify-between rounded-2xl border border-black/8 bg-white px-4 py-3"
            >
              <div>
                <p className="text-sm font-semibold text-black capitalize">{pool.project}</p>
                <p className="text-xs text-black/50">{pool.symbol} · Risk {pool.riskScore.toFixed(1)}</p>
              </div>
              <div className="text-right">
                <p className="font-mono text-sm font-semibold text-emerald-600">{pool.apy.toFixed(2)}%</p>
                <p className="text-[10px] text-black/40 uppercase font-bold">APY</p>
              </div>
            </div>
          ))}
        </div>
      ) : null}

      <a
        href="/vaults"
        className="flex items-center justify-center gap-2 rounded-xl border border-black/10 py-3 text-xs font-semibold text-black transition-colors hover:bg-black/5"
      >
        <ExternalLink className="h-3.5 w-3.5" aria-hidden="true" />
        Explore all yield opportunities
      </a>
      <p className="mt-3 text-center text-[11px] text-black/40">
        You can always link a vault later.{" "}
        <button type="button" onClick={onSkip} className="underline hover:text-black">
          Skip this step
        </button>
      </p>
    </div>
  );
}

// ── Step 4: First deposit ─────────────────────────────────────────────────────

function StepDeposit({ currency, onSkip }: { currency: "USDC" | "XLM"; onSkip: () => void }) {
  return (
    <div className="text-center">
      <div className="mx-auto mb-5 flex h-16 w-16 items-center justify-center rounded-2xl bg-black/5 text-3xl">
        💰
      </div>
      <h2 className="mb-1 text-lg font-semibold text-black">Make your first deposit</h2>
      <p className="mb-6 text-sm text-black/60">
        Kick-start your goal with an initial deposit in {currency}. You can deposit more at any time.
      </p>
      <a
        href="/vaults"
        className="mb-4 flex items-center justify-center gap-2 rounded-xl bg-black py-3.5 text-sm font-semibold text-white transition-opacity hover:opacity-75"
      >
        Go to Vaults to deposit
        <ArrowRight className="h-4 w-4" aria-hidden="true" />
      </a>
      <button
        type="button"
        onClick={onSkip}
        className="text-xs font-medium text-black/50 underline hover:text-black"
      >
        Skip for now — I'll deposit later
      </button>
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
    setDetails((d) => ({ ...d, name: t.label !== "Start from scratch" ? t.label : "" }));
    setStep(1);
  }

  function validateDetails(): string {
    const amount = parseFloat(details.targetAmount);
    if (!details.name.trim()) return "Goal name is required.";
    if (!details.targetAmount || isNaN(amount) || amount <= 0) return "Enter a positive target amount.";
    const minDate = todayPlus(2);
    if (!details.deadline || details.deadline < minDate) return "Deadline must be at least 2 days in the future.";
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
      setError(err instanceof Error ? err.message : "Failed to create goal. Please try again.");
    } finally {
      setSubmitting(false);
    }
  }

  function handleBack() {
    if (step > 0) setStep((s) => (s - 1) as StepIndex);
  }

  return (
    <>
      <div
        className="fixed inset-0 z-40 bg-black/20 backdrop-blur-sm"
        onClick={onDismiss}
        aria-hidden="true"
      />
      <div
        className="fixed inset-x-4 top-12 z-50 mx-auto max-w-xl rounded-3xl bg-white p-8 shadow-2xl
                   sm:inset-auto sm:left-1/2 sm:top-1/2 sm:-translate-x-1/2 sm:-translate-y-1/2"
        role="dialog"
        aria-modal="true"
        aria-label="Savings goal setup"
      >
        <div className="mb-2 flex items-center justify-between">
          <span className="text-xs font-semibold text-black/40 uppercase tracking-widest">
            Set up your savings
          </span>
          <button
            type="button"
            onClick={onDismiss}
            aria-label="Close onboarding"
            className="flex h-8 w-8 items-center justify-center rounded-xl border border-black/10 text-black/40 hover:text-black transition-colors"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        <ProgressBar step={step} />

        {step === 0 && <StepTemplate onSelect={handleTemplateSelect} />}

        {step === 1 && selectedTemplate && (
          <>
            <StepDetails template={selectedTemplate} details={details} onChange={setDetails} />
            {error && (
              <p className="mt-3 rounded-lg bg-red-50 px-4 py-2.5 text-xs font-medium text-red-700" role="alert">
                {error}
              </p>
            )}
            <div className="mt-6 flex gap-3">
              <button
                type="button"
                onClick={handleBack}
                className="flex items-center gap-1.5 rounded-xl border border-black/10 px-4 py-3 text-xs font-semibold text-black/60 hover:bg-black/5 transition-colors"
              >
                <ArrowLeft className="h-3.5 w-3.5" />
                Back
              </button>
              <button
                type="button"
                onClick={handleDetailsNext}
                disabled={submitting}
                className="flex flex-1 items-center justify-center gap-2 rounded-xl bg-black py-3 text-xs font-semibold text-white transition-opacity hover:opacity-75 disabled:opacity-50"
              >
                {submitting ? "Creating goal…" : "Create goal & continue"}
                {!submitting && <ArrowRight className="h-3.5 w-3.5" />}
              </button>
            </div>
          </>
        )}

        {step === 2 && (
          <>
            <StepYield onSkip={() => setStep(3)} />
            <div className="mt-6 flex gap-3">
              <button
                type="button"
                onClick={() => setStep(3)}
                className="flex flex-1 items-center justify-center gap-2 rounded-xl bg-black py-3 text-xs font-semibold text-white transition-opacity hover:opacity-75"
              >
                Continue
                <ArrowRight className="h-3.5 w-3.5" />
              </button>
            </div>
          </>
        )}

        {step === 3 && (
          <StepDeposit
            currency={details.currency}
            onSkip={onComplete}
          />
        )}
      </div>
    </>
  );
}
