"use client";

import { useEffect, useState } from "react";
import { motion, AnimatePresence } from "framer-motion";
import { Sparkles, X, ChevronRight, ChevronLeft } from "lucide-react";
import { profileApi, type RiskProfile } from "@/lib/api/profile";
import type { VaultRecommendationPlan } from "@/lib/api/intelligence";
import { getStoredToken } from "@/lib/api/client";
import { CreateVaultWizard } from "@/components/vault/CreateVaultWizard";
import { cn } from "@/lib/utils";

const GOAL_CHIPS = ["Emergency fund", "Education", "Investment", "Other"];

const RISK_QUESTIONS = [
  {
    id: "horizon",
    question: "When might you need this money?",
    options: [
      { label: "Within 1 year", value: "conservative" as RiskProfile },
      { label: "1–3 years", value: "moderate" as RiskProfile },
      { label: "3+ years", value: "aggressive" as RiskProfile },
    ],
  },
  {
    id: "volatility",
    question: "How do you feel about short-term dips?",
    options: [
      { label: "Very uncomfortable", value: "conservative" as RiskProfile },
      { label: "Some ups and downs are OK", value: "moderate" as RiskProfile },
      { label: "I can ride volatility", value: "aggressive" as RiskProfile },
    ],
  },
  {
    id: "priority",
    question: "What matters most to you?",
    options: [
      { label: "Safety first", value: "conservative" as RiskProfile },
      { label: "Balance of safety and yield", value: "moderate" as RiskProfile },
      { label: "Maximum yield", value: "aggressive" as RiskProfile },
    ],
  },
];

function scoreRiskProfile(answers: RiskProfile[]): RiskProfile {
  const counts: Record<RiskProfile, number> = {
    conservative: 0,
    moderate: 0,
    aggressive: 0,
  };
  for (const a of answers) counts[a]++;
  if (counts.aggressive >= 2) return "aggressive";
  if (counts.conservative >= 2) return "conservative";
  return "moderate";
}

interface Props {
  open: boolean;
  onClose: () => void;
  onComplete: () => void;
}

export function OnboardingWizard({ open, onClose, onComplete }: Props) {
  const [step, setStep] = useState(1);
  const [goalText, setGoalText] = useState("");
  const [riskAnswers, setRiskAnswers] = useState<RiskProfile[]>([]);
  const [riskProfile, setRiskProfile] = useState<RiskProfile>("moderate");
  const [recommendation, setRecommendation] = useState<VaultRecommendationPlan | null>(null);
  const [loadingRec, setLoadingRec] = useState(false);
  const [showCreateVault, setShowCreateVault] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!open) {
      setStep(1);
      setGoalText("");
      setRiskAnswers([]);
      setRecommendation(null);
      setError("");
    }
  }, [open]);

  const handleRiskAnswer = (index: number, value: RiskProfile) => {
    const next = [...riskAnswers];
    next[index] = value;
    setRiskAnswers(next);
  };

  const finishOnboarding = async (completed: boolean) => {
    try {
      await profileApi.update({
        savings_goal: goalText || undefined,
        risk_profile: riskProfile,
        onboarding_completed: completed,
      });
    } catch {
      // Best-effort persistence
    }
    onComplete();
    onClose();
  };

  const loadRecommendation = async () => {
    const profile = scoreRiskProfile(riskAnswers);
    setRiskProfile(profile);
    setLoadingRec(true);
    setError("");
    try {
      const token = getStoredToken();
      const res = await fetch("/api/v1/recommend/vault", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          ...(token ? { Authorization: `Bearer ${token}` } : {}),
        },
        body: JSON.stringify({
          risk_tolerance: profile,
          time_horizon_months: profile === "aggressive" ? 36 : profile === "moderate" ? 18 : 12,
          initial_deposit_usdc: 500,
          savings_goal: goalText,
        }),
      });
      if (!res.ok) throw new Error("Recommendation failed");
      const plan = (await res.json()) as VaultRecommendationPlan;
      setRecommendation(plan);
      setStep(3);
    } catch {
      setError("Could not load a personalized recommendation. You can still explore vaults.");
      setStep(3);
    } finally {
      setLoadingRec(false);
    }
  };

  if (!open) return null;

  return (
    <>
      <AnimatePresence>
        <motion.div
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0 }}
          className="fixed inset-0 z-[120] flex items-center justify-center bg-black/50 px-4 backdrop-blur-sm"
        >
          <motion.div
            initial={{ opacity: 0, y: 20 }}
            animate={{ opacity: 1, y: 0 }}
            className="w-full max-w-lg rounded-[28px] border border-white/10 bg-[#fafafa] p-6 shadow-2xl"
          >
            <div className="mb-6 flex items-start justify-between">
              <div>
                <p className="text-xs font-mono uppercase tracking-widest text-muted-foreground">
                  Welcome to Nester
                </p>
                <h2 className="mt-1 font-heading text-2xl font-light">
                  {step === 1 && "What are you saving for?"}
                  {step === 2 && "Understand your risk profile"}
                  {step === 3 && "Your personalized plan"}
                  {step === 4 && "Create your vault"}
                </h2>
              </div>
              <button
                type="button"
                onClick={() => finishOnboarding(false)}
                className="rounded-full border border-border p-2 text-muted-foreground hover:text-foreground"
              >
                <X className="h-4 w-4" />
              </button>
            </div>

            {step === 1 && (
              <div>
                <textarea
                  value={goalText}
                  onChange={(e) => setGoalText(e.target.value)}
                  placeholder="e.g. Emergency fund for 6 months of expenses"
                  className="w-full rounded-2xl border border-border bg-white px-4 py-3 text-sm outline-none focus:border-black/20"
                  rows={3}
                />
                <div className="mt-3 flex flex-wrap gap-2">
                  {GOAL_CHIPS.map((chip) => (
                    <button
                      key={chip}
                      type="button"
                      onClick={() => setGoalText(chip)}
                      className="rounded-full border border-border bg-white px-3 py-1.5 text-xs font-medium hover:border-black/20"
                    >
                      {chip}
                    </button>
                  ))}
                </div>
              </div>
            )}

            {step === 2 && (
              <div className="space-y-5 max-h-[50vh] overflow-y-auto pr-1">
                {RISK_QUESTIONS.map((q, qi) => (
                  <div key={q.id}>
                    <p className="text-sm font-medium text-foreground">{q.question}</p>
                    <div className="mt-2 space-y-2">
                      {q.options.map((opt) => (
                        <button
                          key={opt.label}
                          type="button"
                          onClick={() => handleRiskAnswer(qi, opt.value)}
                          className={cn(
                            "w-full rounded-xl border px-3 py-2.5 text-left text-sm transition-colors",
                            riskAnswers[qi] === opt.value
                              ? "border-foreground bg-foreground text-background"
                              : "border-border bg-white hover:border-black/15"
                          )}
                        >
                          {opt.label}
                        </button>
                      ))}
                    </div>
                  </div>
                ))}
              </div>
            )}

            {step === 3 && (
              <div>
                {loadingRec && (
                  <div className="flex items-center gap-2 text-sm text-muted-foreground">
                    <Sparkles className="h-4 w-4 animate-pulse" />
                    Prometheus is building your recommendation…
                  </div>
                )}
                {error && <p className="text-sm text-amber-700">{error}</p>}
                {recommendation && (
                  <div className="rounded-2xl border border-border bg-white p-4 text-sm">
                    <p className="text-xs uppercase tracking-widest text-muted-foreground">
                      Risk profile: {riskProfile}
                    </p>
                    <ul className="mt-3 space-y-2">
                      {recommendation.recommended_vaults.map((v) => (
                        <li key={v.vault_id} className="flex justify-between gap-2">
                          <span>{v.rationale.slice(0, 60)}…</span>
                          <span className="font-medium shrink-0">{v.allocation_pct}%</span>
                        </li>
                      ))}
                    </ul>
                    <p className="mt-3 text-muted-foreground">
                      Expected yield: ~${recommendation.expected_yield_usdc.toFixed(0)} USDC
                    </p>
                  </div>
                )}
              </div>
            )}

            {step === 4 && (
              <p className="text-sm text-muted-foreground">
                Launch the vault wizard to deploy your recommended strategy on Stellar.
              </p>
            )}

            <div className="mt-6 flex items-center justify-between gap-3">
              {step > 1 && step < 4 && (
                <button
                  type="button"
                  onClick={() => setStep((s) => s - 1)}
                  className="flex items-center gap-1 rounded-full border border-border px-4 py-2 text-sm"
                >
                  <ChevronLeft className="h-4 w-4" /> Back
                </button>
              )}
              <div className="ml-auto flex gap-2">
                {step < 3 && (
                  <button
                    type="button"
                    disabled={step === 2 && riskAnswers.length < 3}
                    onClick={() => {
                      if (step === 1) setStep(2);
                      else void loadRecommendation();
                    }}
                    className="flex items-center gap-1 rounded-full bg-foreground px-5 py-2.5 text-sm font-medium text-background disabled:opacity-40"
                  >
                    Next <ChevronRight className="h-4 w-4" />
                  </button>
                )}
                {step === 3 && (
                  <>
                    <button
                      type="button"
                      onClick={() => finishOnboarding(true)}
                      className="rounded-full border border-border px-4 py-2.5 text-sm"
                    >
                      Skip for now
                    </button>
                    <button
                      type="button"
                      onClick={() => {
                        setShowCreateVault(true);
                        setStep(4);
                      }}
                      className="rounded-full bg-foreground px-5 py-2.5 text-sm font-medium text-background"
                    >
                      Create vault
                    </button>
                  </>
                )}
                {step === 4 && (
                  <button
                    type="button"
                    onClick={() => finishOnboarding(true)}
                    className="rounded-full bg-foreground px-5 py-2.5 text-sm font-medium text-background"
                  >
                    Done
                  </button>
                )}
              </div>
            </div>
          </motion.div>
        </motion.div>
      </AnimatePresence>
      {showCreateVault && (
        <div className="fixed inset-0 z-[130] overflow-y-auto bg-white">
          <div className="mx-auto max-w-3xl p-6">
            <CreateVaultWizard />
          </div>
        </div>
      )}
    </>
  );
}
