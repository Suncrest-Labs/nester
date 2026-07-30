package intelligence

import "time"

// Recommendation represents an advisory suggestion from Prometheus.
type Recommendation struct {
	Type        string    `json:"type"` // "rebalance", "yield_alert", "risk_warning"
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Confidence  float64   `json:"confidence"`
	CreatedAt   time.Time `json:"created_at"`
}

// SentimentReport represents the aggregate market sentiment from AI analysis.
type SentimentReport struct {
	Score       float64   `json:"score"` // -1.0 (very bearish) to 1.0 (very bullish)
	Summary     string    `json:"summary"`
	TopFactors  []string  `json:"top_factors"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// PortfolioInsights represents AI-generated analysis of a user's holdings.
type PortfolioInsights struct {
	RiskScore       float64   `json:"risk_score"`
	Diversification float64   `json:"diversification"`
	Suggestions     []string  `json:"suggestions"`
	GeneratedAt     time.Time `json:"generated_at"`
}

// SavingsPlanRequest represents the user goal input.
type SavingsPlanRequest struct {
	GoalUSDC                   float64 `json:"goal_usdc"`
	TimeHorizonMonths          int     `json:"time_horizon_months"`
	MaxMonthlyContributionUSDC float64 `json:"max_monthly_contribution_usdc"`
	VaultID                    string  `json:"vault_id,omitempty"`
}

// ScheduleEntry represents one month in the savings plan.
type ScheduleEntry struct {
	Month           int     `json:"month"`
	Deposit         float64 `json:"deposit"`
	ExpectedBalance float64 `json:"expected_balance"`
	YieldEarned     float64 `json:"yield_earned"`
}

// MilestoneProjection represents a checkpoint in the plan.
type MilestoneProjection struct {
	Month           int     `json:"month"`
	ExpectedBalance float64 `json:"expected_balance"`
}

// SavingsPlanResponse represents the generated savings schedule.
type SavingsPlanResponse struct {
	Achievable             bool                  `json:"achievable"`
	RequiredMonthlyDeposit float64               `json:"required_monthly_deposit"`
	MonthlySchedule        []ScheduleEntry       `json:"monthly_schedule"`
	TotalYieldEarned       float64               `json:"total_yield_earned"`
	Narrative              string                `json:"narrative"`
	Milestones             []MilestoneProjection `json:"milestones"`
}

// SavingsGoalContext mirrors the intelligence service's SavingsGoalContext
// pydantic model, sent when requesting AI progress coaching for a goal.
type SavingsGoalContext struct {
	ID            string  `json:"id,omitempty"`
	TargetAmount  float64 `json:"target_amount"`
	Currency      string  `json:"currency"`
	Deadline      string  `json:"deadline"`
	Description   string  `json:"description,omitempty"`
	CurrentAmount float64 `json:"current_amount"`
	ProgressPct   float64 `json:"progress_pct"`
}

// PortfolioContext mirrors the intelligence service's PortfolioContext model.
type PortfolioContext struct {
	TotalBalanceUSD float64          `json:"total_balance_usd"`
	// omitempty: a nil slice must be omitted rather than sent as JSON null —
	// the intelligence service's pydantic model rejects null for this list field.
	Vaults []map[string]any `json:"vaults,omitempty"`
}

// CoachingRequest mirrors the intelligence service's CoachingRequest model.
type CoachingRequest struct {
	Goal      SavingsGoalContext `json:"goal"`
	Portfolio PortfolioContext   `json:"portfolio"`
	// AIInsightsEnabled (#935): explicit opt-out flag, defaulted to true via
	// omitempty so on-demand (user-initiated) coaching requests that don't
	// set it are unaffected. Scheduled/batch callers that already checked
	// the user's nudges_enabled preference should set this explicitly so
	// the intelligence service enforces it too, independent of the caller.
	AIInsightsEnabled *bool `json:"ai_insights_enabled,omitempty"`
}

// DepositScheduleItem mirrors the intelligence service's DepositScheduleItem model.
type DepositScheduleItem struct {
	Date      string  `json:"date"`
	AmountUSD float64 `json:"amount_usdc"`
	Note      string  `json:"note,omitempty"`
}

// CoachingResponse mirrors the intelligence service's CoachingResponse model:
// a weekly AI-generated progress assessment and deposit schedule for a goal.
type CoachingResponse struct {
	ProgressAssessment string                `json:"progress_assessment"`
	DepositSchedule    []DepositScheduleItem `json:"deposit_schedule"`
	Nudges             []string              `json:"nudges"`
	Confidence         string                `json:"confidence"`
}

// DigestGenerateRequest mirrors the intelligence service's
// DigestGenerateRequest model (#859): a request for the periodic insight
// digest for one user/period. The intelligence service pulls facts itself
// (ledger via the relay, goals/vaults/performance via existing endpoints)
// so this request carries only the identifiers, not the data.
type DigestGenerateRequest struct {
	UserID string `json:"user_id"`
	Period string `json:"period"` // "weekly" | "monthly"
}

// DigestAttentionItem mirrors the intelligence service's AttentionItem
// model: a constructive, actionable heads-up surfaced in the digest.
type DigestAttentionItem struct {
	Kind       string `json:"kind"`
	Message    string `json:"message"`
	ActionLabel string `json:"action_label,omitempty"`
	ActionHref string `json:"action_href,omitempty"`
}

// DigestGenerateResponse mirrors the intelligence service's DigestResponse
// model: the assembled facts (opaque to Go — narrated by the LLM but
// computed deterministically), the grounded narrative, and attention items.
type DigestGenerateResponse struct {
	Period            string                 `json:"period"`
	PeriodStart       string                 `json:"period_start"`
	PeriodEnd         string                 `json:"period_end"`
	Facts             map[string]any         `json:"facts"`
	FactsHash         string                 `json:"facts_hash"`
	Narrative         string                 `json:"narrative"`
	AttentionItems    []DigestAttentionItem  `json:"attention_items"`
	HonestZeroPeriod  bool                   `json:"honest_zero_period"`
	Cached            bool                   `json:"cached"`
	GeneratedAt       string                 `json:"generated_at"`
}

type NudgeCopyRequest struct {
	NudgeType string            `json:"nudge_type"`
	Segment   string            `json:"segment"`
	Facts     map[string]string `json:"facts"`
	RequestID string            `json:"request_id"`
}

type NudgeCopyResponse struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}
