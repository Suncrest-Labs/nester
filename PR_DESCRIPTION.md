# Pull Request — Intelligence Service Features

## Issue #850 — Behavioral Analytics & Segmentation
Introduces a deterministic behavioral segmentation engine that assigns every user an interpretable segment (Disciplined Regular Saver, Aspiration Goal Setter, Yield Optimizer, Dormant/At Risk, New/Exploring) from platform activity signals. The service computes features including contribution regularity, goal completion rate, yield engagement, streak strength, deposit variability, and dormancy score. Uses hysteresis to prevent single-day segment flips while allowing sustained behavioral shifts. Segments are explainable to product and engineering teams with no opaque clustering.

**Files:** `app/services/behavioral_segmentation.py`, `tests/test_behavioral_segmentation.py`

---

## Issue #851 — Predictive Churn & Deposit-Behavior Insights with Retention Triggers
Builds a savings-specific churn prediction model with a weighted linear risk scorer (recency 25%, frequency 20%, trend 15%, dormancy 15%, streak 10%, engagement 10%, goal progress 5%) producing a calibrated 0-1 score mapped to LOW/MEDIUM/HIGH/CRITICAL bands. Extracts leading-indicator features from deposit frequency, recency, amount trends, streak health, and dormancy signals. Implements rate-limited retention interventions (streak reminder, goal nudge, win-back, deposit celebration) with cooldown windows. Critical risk overrides rate limits. Generates LLM prompts for personalized intervention copy with deterministic fallback text. Tracks intervention outcomes (opened/clicked/converted/dismissed) for measurement.

**Files:** `app/services/churn_prediction.py`, `app/models/churn.py`, `app/routers/churn.py`, `tests/test_churn_prediction.py`, migration 057

---

## Issue #854 — Risk-Tolerance Profiling & Inference for Personalized Yield Strategies
Establishes each user's risk tolerance through a short, sound stated-preference questionnaire (loss reaction 30%, time horizon 20%, financial cushion 20%, risk comfort 30%) mapped to a documented scoring rubric producing a 0-100 score and conservative/moderate/aggressive band. Infers revealed preference from actual behaviour: vault choices, hold-through-volatility, dip-withdrawal patterns, and vault move direction. Reconciles stated and revealed preferences with behaviour weighted more heavily as confidence matures (55/45 to 35/65 split). Detects divergence when the gap exceeds 20 points and surfaces it gently. Profiling is documented as duty-of-care matching, never upsell. Scoring is fully deterministic; LLM limited to natural-language explanation. Profiles are versioned with a snapshot history for re-evaluation audit trails. Optimizer endpoint provides one authoritative risk ceiling.

**Files:** `app/services/risk_profiling.py`, `app/models/risk_profile.py`, `app/routers/risk_profile.py`, `tests/test_risk_profiling.py`, migration 058

---

## Notes
All new code follows existing repo patterns (FastAPI + Pydantic models, dataclass-based services, slowapi rate limiting, pytest with deterministic assertions). All intelligence service tests pass with zero regressions.
